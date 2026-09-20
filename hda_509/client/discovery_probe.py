#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Discovery probe: unsecured GetEndpoints / FindServers.

This is how a THIRD-PARTY client typically discovers a server:

    connect (None / unsecured channel)
      -> GetEndpoints
      -> read ServerCertificate / SecurityPolicy / SecurityMode / UserTokens
      -> pick a secure endpoint
      -> open a SecureChannel with the chosen security parameters

Unlike probe.py, this client does NOT need to know the server certificate or
security parameters in advance: it opens an unsecured discovery channel
(requires the server to publish the discovery-only None endpoint, i.e.
security.discovery: true, which the normal / self-signed / custom-uri
configs do) and lists everything.

    python client/discovery_probe.py
    python client/discovery_probe.py --url opc.tcp://127.0.0.1:48621/ua_mocker/
    python client/discovery_probe.py --url opc.tcp://127.0.0.1:48625/ua_mocker/

It also verifies that an unsecured SESSION (full connect as Anonymous) is
rejected by the server, confirming that the None endpoint is discovery-only.
"""

import asyncio
import sys
from pathlib import Path

from asyncua import Client

from conn import DEFAULT_URL, run_async, setup_client
from endpoint_dump import print_endpoints

# 本文件与 conn.py 同目录
sys.path.insert(0, str(Path(__file__).resolve().parent))


async def run(args) -> None:
    # ---- unsecured discovery -------------------------------------------------
    disc = Client(args.url)
    try:
        endpoints = await disc.connect_and_get_server_endpoints()
    finally:
        try:
            await disc.disconnect()
        except Exception:  # noqa: BLE001
            pass
    print_endpoints(endpoints)

    # ---- 验证 unsecured session 被拒绝（discovery-only None） -----------------
    print("验证 unsecured Session 行为：")
    anon = Client(args.url)
    try:
        await anon.connect()
        print("  [FAIL] unsecured Anonymous Session 竟然成功（不应发生）")
        try:
            await anon.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return 1
    except Exception as e:  # noqa: BLE001
        msg = f"{type(e).__name__}: {e}"
        if _is_network_error(e):
            print(f"  [FAIL] 服务器离线或网络错误: {msg}")
            return 1
        print(f"  [PASS] unsecured Session 被拒绝: {msg}")
    return 0


def _is_network_error(e: BaseException) -> bool:
    import socket
    if isinstance(e, (ConnectionRefusedError, ConnectionResetError, socket.gaierror, OSError)):
        return True
    text = f"{type(e).__name__}: {e}".lower()
    return any(k in text for k in ("connection refused", "name or service not known", "timed out"))


def main() -> int:
    import argparse

    parser = argparse.ArgumentParser(description="Unsecured discovery probe")
    parser.add_argument("--url", default=DEFAULT_URL, help=f"OPC UA 端点 (默认 {DEFAULT_URL})")
    args = parser.parse_args()
    return run_async(run(args))


if __name__ == "__main__":
    sys.exit(main())
