#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 并发认证隔离测试（manifest 驱动）。

每个矩阵端口只开放一种认证方式，因此并发隔离的断言是：

    对某个端口，同时发起
      * N 个【该端口开放的方式】 -> 必须全部成功
      * N 个【该端口未开放的方式】 -> 必须全部失败（不得降级放行）

分别在 anon / username / x509 三个端口上各跑一轮，覆盖三种方式。

背景：asyncua 2.0.1 的 UserManager 无法仅凭 certificate 参数区分 Anonymous 与
未注册 X509 令牌；hda_509 通过 UserTokenAwareSession 在 ActivateSession 的同步
窗口记录令牌类型。本测试在高并发下验证该隔离不串、且三种开关互不放行。

严格 PASS/FAIL；服务器离线一律 FAIL。

运行（先启动矩阵端口）：

    python tools/matrix_ctl.py start
    python tests/auth_concurrent_tests.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from _auth_common import (  # noqa: E402
    CERTS,
    attempt,
    pick_port,
    report,
    server_online,
    summarize,
)

PER_KIND = 5
EP_POLICY = "Basic256Sha256"
EP_MODE = "SignAndEncrypt"

# 每个端口上要并发发起的【有效】与【无效】尝试
VALID_BY_AUTH = {
    "anon": {"auth": "anon"},
    "username": {"auth": "username"},
    "x509": {"auth": "x509"},
}
INVALID_BY_AUTH = {
    # 该端口应放行的方式 -> 同端口上必须被拒的其它方式
    "anon": [
        ("username(未开放)", {"auth": "username"}),
        ("x509(未开放)", {"auth": "x509"}),
    ],
    "username": [
        ("anon(未开放)", {"auth": "anon"}),
        ("x509(未开放)", {"auth": "x509"}),
    ],
    "x509": [
        ("anon(未开放)", {"auth": "anon"}),
        ("username(未开放)", {"auth": "username"}),
    ],
}


async def _run(url: str, node: str, policy: str, mode: str, kwargs: dict):
    a = await attempt(policy=policy, mode=mode, url=url, node=node, **kwargs)
    return a.ok, a.error


async def run_port_round(port: dict) -> None:
    """在单个端口上跑一轮：PER_KIND 个有效 + 各 PER_KIND 个无效。"""
    auth = port["auth"]
    label_base = f"p{port['port']} {port['policy']}/{port['mode']} {auth}"
    common = dict(url=port["url"], node=port["read_node"],
                  policy=port["policy"], mode=port["mode"])

    tasks = []
    expect = []
    for _ in range(PER_KIND):
        tasks.append(_run(**common, kwargs=dict(VALID_BY_AUTH[auth])))
        expect.append(True)
    for label, kwargs in INVALID_BY_AUTH[auth]:
        for _ in range(PER_KIND):
            tasks.append(_run(**common, kwargs=dict(kwargs)))
            expect.append(False)

    results = await asyncio.gather(*tasks)

    # 有效组：全成功
    valid_results = [ok for ok, _ in results[:PER_KIND]]
    n_ok = sum(valid_results)
    report(
        f"{label_base} 有效方式全部成功 ({n_ok}/{PER_KIND})",
        "PASS" if n_ok == PER_KIND and len(valid_results) == PER_KIND else "FAIL",
        next((e for ok, e in results[:PER_KIND] if not ok), ""),
    )

    # 无效组：按标签分组，全失败
    idx = PER_KIND
    for label, kwargs in INVALID_BY_AUTH[auth]:
        chunk = results[idx: idx + PER_KIND]
        idx += PER_KIND
        n_rejected = sum(1 for ok, _ in chunk if not ok)
        leaked = next((e for ok, e in chunk if ok), "")
        report(
            f"{label_base} 拒绝 {label} ({n_rejected}/{PER_KIND})",
            "PASS" if n_rejected == PER_KIND and len(chunk) == PER_KIND else "FAIL",
            f"出现放行(降级?): {leaked}" if leaked else "",
        )


async def main() -> int:
    print("UA Auth Lab 并发认证隔离测试（manifest 驱动）\n")
    print("=" * 74)

    ports = [pick_port(auth=a, policy=EP_POLICY, mode=EP_MODE)
             for a in ("anon", "username", "x509")]
    print("参与端口:")
    for p in ports:
        print(f"  p{p['port']}  {p['policy']}/{p['mode']}  auth={p['auth']}")

    offline = [p["port"] for p in ports if not server_online(p["url"])]
    if offline:
        print(f"\n[FAIL] 端口未监听: {offline}")
        print("请先运行: python tools/matrix_ctl.py start")
        report("并发测试预检", "FAIL", f"端口 {offline} 未监听")
        return summarize("并发认证隔离测试 ")

    print(f"\n每端口并发 {PER_KIND} 有效 + {PER_KIND * 2} 无效 ...\n")

    # 三个端口的轮次可并行（端口之间互不干扰）
    await asyncio.gather(*(run_port_round(p) for p in ports))

    print("=" * 74)
    return summarize("并发认证隔离测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
