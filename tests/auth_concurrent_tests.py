#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab - concurrent authentication isolation tests.

场景：configs/all_auth.yaml (port 48630)。

并发发起多类身份认证，验证：
  * 有效身份（anon / username / x509）全部成功；
  * 无效身份（错误密码 / 未注册 User 证书）全部失败；
  * 无效身份不会被降级当作 Anonymous 放行（令牌类型隔离）。

背景：asyncua 2.0.1 的 UserManager 无法仅凭 certificate 参数区分
Anonymous 与未注册 X509 令牌，hda_509 通过 UserTokenAwareSession 在
ActivateSession 同步窗口记录令牌类型。本测试在高并发下验证该隔离不串。

运行（先启动服务器）：

    python main.py configs/all_auth.yaml
    python tests/auth_concurrent_tests.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from _auth_common import (  # noqa: E402
    CERTS,
    DEFAULT_URL,
    attempt,
    report,
    server_online,
    summarize,
)

PER_KIND = 5

VALID_KINDS = {
    "valid anon": dict(auth="anon"),
    "valid username": dict(auth="username"),
    "valid x509": dict(auth="x509"),
}

INVALID_KINDS = {
    "invalid username": dict(auth="username", password="wrong"),
    "invalid x509 (unregistered)": dict(
        auth="x509",
        user_cert=CERTS / "user_unregistered_cert.pem",
        user_key=CERTS / "user_unregistered_key.pem",
    ),
}


async def _run(kind: str, kwargs: dict) -> tuple[str, bool, str]:
    a = await attempt(**kwargs)
    return kind, a.ok, a.error


async def main() -> int:
    print("UA Auth Lab 并发认证隔离测试 (all_auth / 48630)\n")
    print("=" * 70)
    if not server_online():
        print(f"服务器未启动 ({DEFAULT_URL})，无法执行并发测试。")
        return 1

    tasks = []
    for kind, kwargs in VALID_KINDS.items():
        for _ in range(PER_KIND):
            tasks.append(_run(kind, kwargs))
    for kind, kwargs in INVALID_KINDS.items():
        for _ in range(PER_KIND):
            tasks.append(_run(kind, kwargs))

    print(f"并发发起 {len(tasks)} 个会话（每类 {PER_KIND} 个）...\n")
    results = await asyncio.gather(*tasks)

    # 分类统计
    buckets: dict[str, list[tuple[bool, str]]] = {}
    for kind, ok, err in results:
        buckets.setdefault(kind, []).append((ok, err))

    # 有效身份：全部成功
    for kind in VALID_KINDS:
        items = buckets.get(kind, [])
        ok_count = sum(1 for ok, _ in items if ok)
        passed = ok_count == len(items) and len(items) == PER_KIND
        first_fail = next((e for ok, e in items if not ok), "")
        report(
            f"{kind} 全部成功 ({ok_count}/{PER_KIND})",
            "PASS" if passed else "FAIL",
            first_fail if not passed else "",
        )

    # 无效身份：全部失败
    for kind in INVALID_KINDS:
        items = buckets.get(kind, [])
        fail_count = sum(1 for ok, _ in items if not ok)
        passed = fail_count == len(items) and len(items) == PER_KIND
        first_ok = next((e for ok, e in items if ok), "")
        report(
            f"{kind} 全部拒绝 ({fail_count}/{PER_KIND})",
            "PASS" if passed else "FAIL",
            f"出现放行(降级?): {first_ok}" if not passed else "",
        )

    print("=" * 70)
    return summarize("并发认证隔离测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
