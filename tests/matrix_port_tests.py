#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 全拆（方案 A）矩阵端口测试。

按 configs/matrix/manifest.json 逐端口验证「一端口一场景」：

  Case 1  正向  本端口的 (policy, mode, auth) 组合必须成功（读到节点值）
  Case 2  端点隔离  GetEndpoints 只暴露本端口声明的 1 个端点
  Case 3  Token 隔离 该端点只发布本端口声明的 1 种 UserIdentityToken
  Case 4  负向抽样  每端口 2 条（可配）：
              - 换认证方式（同端点） -> 必失败于 ActivateSession
              - 换端点组合（同认证） -> 必失败于 Endpoint selection

严格 PASS/FAIL：任何网络错误 / 服务器离线一律 FAIL，绝不计为通过。

先启动端口：

    python tools/gen_matrix_configs.py
    python tools/matrix_ctl.py start
    python tests/matrix_port_tests.py
    python tools/matrix_ctl.py stop
"""

import asyncio
import json
import sys
from pathlib import Path

from asyncua import Client, ua

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))
sys.path.insert(0, str(BASE_DIR / "client"))

from _auth_common import (  # noqa: E402
    APP_CERT,
    APP_KEY,
    APP_URI,
    CERTS,
    SERVER_CERT,
    USER_CERT,
    USER_KEY,
    attempt,
    report,
    server_online,
    summarize,
)
from conn import POLICY_CLASSES, read_node  # noqa: E402

MANIFEST = BASE_DIR / "configs" / "matrix" / "manifest.json"

# 并发度：端口之间互不干扰，同一端口内的 3 个用例仍按顺序执行
CONCURRENCY = 6


def load_manifest() -> dict:
    if not MANIFEST.exists():
        raise SystemExit(f"缺少清单: {MANIFEST}\n先运行: python tools/gen_matrix_configs.py")
    return json.loads(MANIFEST.read_text(encoding="utf-8"))


def _token_name(token_type: ua.UserTokenType) -> str:
    return {
        ua.UserTokenType.Anonymous: "Anonymous",
        ua.UserTokenType.UserName: "UserName",
        ua.UserTokenType.Certificate: "Certificate",
    }.get(token_type, str(token_type))


# 预期 token：auth -> UserTokenType
_EXPECT_TOKEN = {
    "anon": ua.UserTokenType.Anonymous,
    "username": ua.UserTokenType.UserName,
    "x509": ua.UserTokenType.Certificate,
}

_EXPECT_POLICY_URI = {
    "None": POLICY_CLASSES["None"].URI,
    "Basic128Rsa15": POLICY_CLASSES["Basic128Rsa15"].URI,
    "Basic256": POLICY_CLASSES["Basic256"].URI,
    "Basic256Sha256": POLICY_CLASSES["Basic256Sha256"].URI,
    "Aes128Sha256RsaOaep": POLICY_CLASSES["Aes128Sha256RsaOaep"].URI,
    "Aes256Sha256RsaPss": POLICY_CLASSES["Aes256Sha256RsaPss"].URI,
}
_EXPECT_MODE = {
    "None": ua.MessageSecurityMode.None_,
    "Sign": ua.MessageSecurityMode.Sign,
    "SignAndEncrypt": ua.MessageSecurityMode.SignAndEncrypt,
}


async def _fetch_endpoints(url: str) -> list:
    """用独立临时连接取 GetEndpoints 结果。"""
    disc = Client(url)
    try:
        return await disc.connect_and_get_server_endpoints()
    finally:
        try:
            await disc.disconnect()
        except Exception:  # noqa: BLE001
            pass


def _port_label(entry: dict) -> str:
    dep = " (废弃)" if entry["deprecated"] else ""
    return f"p{entry['port']} {entry['policy']}/{entry['mode']}{dep} {entry['auth']}"


# ---------------------------------------------------------------------------
# Case 1：正向
# ---------------------------------------------------------------------------

async def _check_positive(sem: asyncio.Semaphore, entry: dict) -> None:
    async with sem:
        a = await attempt(
            auth=entry["auth"],
            policy=entry["policy"],
            mode=entry["mode"],
            url=entry["url"],
            node=entry["read_node"],
            app_cert=APP_CERT,
            app_key=APP_KEY,
            app_uri=APP_URI,
            user_cert=USER_CERT,
            user_key=USER_KEY,
            print_steps=False,
        )
    label = f"[正向] {_port_label(entry)}"
    if a.ok:
        report(label, "PASS", a.error)
    elif a.network_error:
        report(label, "FAIL", f"服务器未监听: {a.error}")
    else:
        report(label, "FAIL", f"stage={a.stage or '-'} err={a.error}")


# ---------------------------------------------------------------------------
# Case 2 + 3：端点隔离 / Token 隔离（同一端口一次 GetEndpoints 同时断言）
# ---------------------------------------------------------------------------

async def _check_isolation(sem: asyncio.Semaphore, entry: dict) -> None:
    async with sem:
        if not server_online(entry["url"]):
            report(f"[端点隔离] {_port_label(entry)}", "FAIL", "服务器未监听")
            report(f"[Token隔离] {_port_label(entry)}", "FAIL", "服务器未监听")
            return
        try:
            endpoints = await _fetch_endpoints(entry["url"])
        except Exception as e:  # noqa: BLE001
            report(f"[端点隔离] {_port_label(entry)}", "FAIL", f"{type(e).__name__}: {e}")
            report(f"[Token隔离] {_port_label(entry)}", "FAIL", "GetEndpoints 失败")
            return

    # ---- Case 2：端点隔离 ----
    label_ep = f"[端点隔离] {_port_label(entry)}"
    if len(endpoints) != 1:
        detail = "; ".join(
            f"{e.SecurityPolicyUri}/{e.SecurityMode.name}" for e in endpoints
        )
        report(label_ep, "FAIL", f"暴露 {len(endpoints)} 个端点(应为1): {detail}")
        return
    ep = endpoints[0]
    exp_uri = _EXPECT_POLICY_URI[entry["policy"]]
    exp_mode = _EXPECT_MODE[entry["mode"]]
    if ep.SecurityPolicyUri == exp_uri and ep.SecurityMode == exp_mode:
        report(label_ep, "PASS", f"仅 {entry['policy']}/{entry['mode']}")
    else:
        report(
            label_ep, "FAIL",
            f"端点不符: 期望 {entry['policy']}/{entry['mode']}, "
            f"实得 {ep.SecurityPolicyUri}/{ep.SecurityMode.name}",
        )

    # ---- Case 3：Token 隔离 ----
    label_tk = f"[Token隔离] {_port_label(entry)}"
    kinds = {_token_name(t.TokenType) for t in ep.UserIdentityTokens}
    exp_kind = {
        ua.UserTokenType.Anonymous: "Anonymous",
        ua.UserTokenType.UserName: "UserName",
        ua.UserTokenType.Certificate: "Certificate",
    }[_EXPECT_TOKEN[entry["auth"]]]
    if kinds == {exp_kind}:
        report(label_tk, "PASS", f"仅 {exp_kind}")
    else:
        report(label_tk, "FAIL", f"期望仅 {exp_kind}, 实得 {sorted(kinds)}")


# ---------------------------------------------------------------------------
# Case 4：负向抽样
# ---------------------------------------------------------------------------

async def _check_negative(sem: asyncio.Semaphore, entry: dict, neg: dict, i: int) -> None:
    async with sem:
        a = await attempt(
            auth=neg["auth"],
            policy=neg["policy"],
            mode=neg["mode"],
            url=entry["url"],
            node=entry["read_node"],
            app_cert=APP_CERT,
            app_key=APP_KEY,
            app_uri=APP_URI,
            user_cert=USER_CERT,
            user_key=USER_KEY,
            print_steps=False,
        )
    label = (
        f"[负向{i}] {_port_label(entry)} 尝试 {neg['policy']}/{neg['mode']}/{neg['auth']}"
    )
    if a.ok:
        report(label, "FAIL", "连接成功(该组合不应被放行)")
        return
    if a.network_error:
        report(label, "FAIL", f"服务器离线: {a.error}")
        return

    expected = neg["expect_fail_stage"]
    if a.stage == expected:
        report(label, "PASS", f"{expected} 拒绝 ({neg['reason']})")
        return
    # 换端点组合的负向：可能在 GetEndpoints 阶段就断（客户端打开临时通道失败），
    # 也可能在 Endpoint selection 找不到匹配端点 —— 两者都算"端点隔离成立"。
    if expected == "Endpoint selection" and a.stage in ("GetEndpoints", "Endpoint selection"):
        report(label, "PASS", f"{a.stage} 拒绝 ({neg['reason']})")
        return
    report(label, "FAIL", f"阶段不符(期望 {expected}): stage={a.stage or '-'} err={a.error}")


# ---------------------------------------------------------------------------

async def main() -> int:
    manifest = load_manifest()
    entries = manifest["entries"]
    neg_count = manifest.get("negative_samples_per_port", 2)

    print("UA Auth Lab 全拆矩阵测试（方案 A）\n")
    print(f"  端口数      : {manifest['port_count']}")
    print(f"  端点组合    : {manifest['endpoint_count']} × 认证 {manifest['auth_count']}")
    print(f"  每端口负向  : {neg_count}")
    print(f"  预计连接    : {manifest['total_connections']}")
    print("=" * 78)

    # 预检：端口是否都已启动
    offline = [e["port"] for e in entries if not server_online(e["url"])]
    if offline:
        print(f"\n[FAIL] {len(offline)} 个端口未监听: {offline}")
        print("请先运行: python tools/matrix_ctl.py start")
        report("端口预检", "FAIL", f"{len(offline)} 个端口未监听")
        return summarize("全拆矩阵测试 ")

    sem = asyncio.Semaphore(CONCURRENCY)

    print("\nCase 1. 正向：本端口组合必须成功\n")
    await asyncio.gather(*(_check_positive(sem, e) for e in entries))

    print("\nCase 2/3. 端点隔离 + Token 隔离：每端口只暴露自己的那一个\n")
    await asyncio.gather(*(_check_isolation(sem, e) for e in entries))

    if neg_count:
        print(f"\nCase 4. 负向抽样：每端口 {neg_count} 条，其余组合必须失败\n")
        tasks = []
        for e in entries:
            for i, neg in enumerate(e.get("negatives", []), start=1):
                tasks.append(_check_negative(sem, e, neg, i))
        await asyncio.gather(*tasks)

    print("=" * 78)
    return summarize("全拆矩阵测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
