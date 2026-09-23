#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Representative session lifecycle and concurrent-session stability checks."""

import asyncio
import sys
from pathlib import Path
from urllib.parse import urlparse

BASE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE))
sys.path.insert(0, str(BASE / "client"))

from _auth_common import attempt, pick_port, report, summarize  # noqa: E402


async def one(entry: dict, auth: str) -> bool:
    parsed = urlparse(entry["url"])
    local_url = f"opc.tcp://127.0.0.1:{parsed.port}{parsed.path}"
    result = await attempt(
        auth=auth, policy=entry["policy"], mode=entry["mode"],
        url=local_url, node=entry["read_node"], print_steps=False,
    )
    return result.ok


async def main() -> int:
    entry = pick_port(
        auth="username", policy="Basic256Sha256", mode="SignAndEncrypt",
        validation="trusted", group="core",
    )

    sequential = [await one(entry, "username") for _ in range(5)]
    report("5 次 Session 建立/关闭", "PASS" if all(sequential) else "FAIL")

    concurrent = await asyncio.gather(*(one(entry, "username") for _ in range(12)))
    report("12 个同身份并发 Session", "PASS" if all(concurrent) else "FAIL")

    mixed_entries = {
        auth: pick_port(
            auth=auth, policy="Basic256Sha256", mode="SignAndEncrypt",
            validation="trusted", group="core",
        )
        for auth in ("anon", "username", "x509")
    }
    mixed = await asyncio.gather(*(
        one(mixed_entries[auth], auth)
        for auth in ("anon", "username", "x509") for _ in range(4)
    ))
    report("3 种身份跨端口并发 Session", "PASS" if all(mixed) else "FAIL")
    return summarize("Session 生命周期测试 ")


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
