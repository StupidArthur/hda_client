#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab - positive authentication matrix tests.

场景：configs/all_auth.yaml (port 48630)。

覆盖：
  1. 三种用户认证方式 × 3 种 SecurityPolicy × Sign/SignAndEncrypt
     = 18 组合，全部应成功（OpenSecureChannel/CreateSession/
     ActivateSession/Read）。
  2. GetEndpoints 断言：只返回 6 个 secure endpoints（不暴露 None/None
     Session endpoint），且每个 secure endpoint 都发布
     Anonymous + UserName + Certificate 三种 UserIdentityToken。

严格 PASS/FAIL；服务器离线一律 FAIL。

运行（先启动服务器）：

    python main.py configs/all_auth.yaml
    python tests/auth_matrix_tests.py
"""

import asyncio
import sys
from pathlib import Path

from asyncua import Client, ua

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from _auth_common import (  # noqa: E402
    DEFAULT_URL,
    RESULTS,
    attempt,
    report,
    server_online,
    summarize,
)
from endpoint_dump import short_policy_uri  # noqa: E402
from conn import POLICY_CLASSES  # noqa: E402

AUTHS = ["anon", "username", "x509"]
POLICIES = ["Basic256Sha256", "Aes128Sha256RsaOaep", "Aes256Sha256RsaPss"]
MODES = ["Sign", "SignAndEncrypt"]

EXPECTED_TOKENS = {
    ua.UserTokenType.Anonymous,
    ua.UserTokenType.UserName,
    ua.UserTokenType.Certificate,
}


async def fetch_endpoints(url: str):
    disc = Client(url)
    try:
        return await disc.connect_and_get_server_endpoints()
    finally:
        try:
            await disc.disconnect()
        except Exception:  # noqa: BLE001
            pass


async def case_full_matrix() -> None:
    print("Case 1. 三认证 × 3 策略 × 2 模式 = 18 组合（全部应成功）：\n")
    if not server_online():
        report("正向矩阵", "FAIL", f"服务器未启动 ({DEFAULT_URL})")
        return

    total = passed = 0
    for policy in POLICIES:
        for mode in MODES:
            for auth in AUTHS:
                total += 1
                a = await attempt(auth=auth, policy=policy, mode=mode)
                if a.ok:
                    passed += 1
                else:
                    report(
                        f"矩阵 {auth}/{policy}/{mode}",
                        "FAIL",
                        f"stage={a.stage or '-'} err={a.error}",
                    )
    ok = passed == total
    report(
        f"正向矩阵 {passed}/{total} 组合",
        "PASS" if ok else "FAIL",
        f"{passed}/{total}",
    )


async def case_endpoint_tokens() -> None:
    print("\nCase 2. GetEndpoints 端点与 UserIdentityToken 断言：\n")
    if not server_online():
        report("端点 token 断言", "FAIL", f"服务器未启动 ({DEFAULT_URL})")
        return

    endpoints = await fetch_endpoints(DEFAULT_URL)

    # 2a. 不暴露 None/None Session endpoint
    none_eps = [e for e in endpoints if e.SecurityMode == ua.MessageSecurityMode.None_]
    report(
        "GetEndpoints 不暴露 None/None Session endpoint",
        "PASS" if not none_eps else "FAIL",
        f"endpoints={len(endpoints)} none={len(none_eps)}",
    )

    # 2b. 6 个 secure endpoints
    report(
        "GetEndpoints 返回 6 个 secure endpoints",
        "PASS" if len(endpoints) == 6 else "FAIL",
        f"got={len(endpoints)}",
    )

    # 2c. 每个 secure endpoint 发布三种 token
    bad = []
    for ep in endpoints:
        kinds = {t.TokenType for t in ep.UserIdentityTokens}
        if kinds != EXPECTED_TOKENS:
            bad.append(f"{short_policy_uri(ep.SecurityPolicyUri)}/{ep.SecurityMode.name}={sorted(t.name for t in kinds)}")
    report(
        "每个 secure endpoint 发布 Anonymous+UserName+Certificate",
        "PASS" if not bad else "FAIL",
        "all 6 endpoints" if not bad else "; ".join(bad),
    )

    # 2d. 策略覆盖齐全（比较完整 SecurityPolicy URI）
    expect_uris = {
        POLICY_CLASSES[p].URI
        for p in ("Basic256Sha256", "Aes128Sha256RsaOaep", "Aes256Sha256RsaPss")
    }
    policies_seen = {e.SecurityPolicyUri for e in endpoints}
    report(
        "3 种 SecurityPolicy 全部发布",
        "PASS" if policies_seen == expect_uris else "FAIL",
        f"{sorted(short_policy_uri(u) for u in policies_seen)}",
    )


async def main() -> int:
    print("UA Auth Lab 正向认证矩阵测试 (all_auth / 48630)\n")
    print("=" * 70)
    await case_full_matrix()
    await case_endpoint_tokens()
    print("=" * 70)
    return summarize("正向矩阵测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
