#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab - per-method toggle tests.

验证「单独开放某一种认证方式」时，其余方式确实被拒绝：

    anon_only     (48631) : anon 成功；username / x509 拒绝
    username_only (48632) : username 成功；anon / x509 拒绝
    x509_only     (48633) : x509 成功；anon / username 拒绝

注意：本测试需要相应端口的服务器已启动；未启动的场景标记为 SKIP
（不影响退出码），便于按需逐个验证。

运行（按需启动对应服务器）：

    python main.py configs/anon_only.yaml
    python main.py configs/username_only.yaml
    python main.py configs/x509_only.yaml
    python tests/auth_mode_toggle_tests.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from _auth_common import attempt, report, server_online, summarize  # noqa: E402

# (场景名, URL, 允许的方式)
SCENARIOS = [
    ("anon_only", "opc.tcp://10.30.70.77:48631/ua_auth/", {"anon"}),
    ("username_only", "opc.tcp://10.30.70.77:48632/ua_auth/", {"username"}),
    ("x509_only", "opc.tcp://10.30.70.77:48633/ua_auth/", {"x509"}),
]

ALL_AUTHS = ["anon", "username", "x509"]


async def main() -> int:
    print("UA Auth Lab 单方式开关测试\n")
    print("=" * 70)
    skipped = 0
    for name, url, allowed in SCENARIOS:
        if not server_online(url):
            report(f"{name} 场景", "SKIP", f"服务器未启动 ({url})")
            skipped += 1
            continue
        for auth in ALL_AUTHS:
            a = await attempt(auth=auth, url=url)
            should_ok = auth in allowed
            if should_ok:
                report(
                    f"{name}: {auth} 应成功",
                    "PASS" if a.ok else "FAIL",
                    "" if a.ok else f"stage={a.stage or '-'} err={a.error}",
                )
            else:
                report(
                    f"{name}: {auth} 应被拒绝",
                    "PASS" if not a.ok else "FAIL",
                    "" if not a.ok else "连接成功(不应发生)",
                )
    print("=" * 70)
    if skipped == len(SCENARIOS):
        print("所有单方式场景均未启动，全部 SKIP。请先启动对应 configs/*.yaml。")
    return summarize("单方式开关测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
