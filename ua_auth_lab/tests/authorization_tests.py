#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Representative authorization checks after successful authentication."""

import asyncio
import sys
from pathlib import Path
from urllib.parse import urlparse

from asyncua import ua

BASE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE))
sys.path.insert(0, str(BASE / "client"))

from _auth_common import APP_CERT, APP_KEY, APP_URI, pick_port, report, summarize  # noqa: E402
from conn import setup_client, stage_connect  # noqa: E402

READ_ONLY = "ns=1;s=int32_ch_1"
WRITABLE = "ns=1;s=int32_wr_1"


async def check_identity(auth: str) -> None:
    entry = pick_port(
        auth=auth, policy="Basic256Sha256", mode="SignAndEncrypt",
        validation="trusted", group="core",
    )
    parsed = urlparse(entry["url"])
    local_url = f"opc.tcp://127.0.0.1:{parsed.port}{parsed.path}"
    client = await setup_client(
        local_url, app_cert=APP_CERT, app_key=APP_KEY,
        server_cert=BASE / "test_material" / "certs" / "server_cert.pem",
        policy_name=entry["policy"], mode_name=entry["mode"], application_uri=APP_URI,
    )
    try:
        await stage_connect(
            client, auth=auth, policy_name=entry["policy"], mode_name=entry["mode"],
            print_steps=False,
        )
        await client.get_node(READ_ONLY).read_value()
        report(f"{auth}: Browse/Read", "PASS")

        writable = client.get_node(WRITABLE)
        old = await writable.read_value()
        await writable.write_value(ua.Variant(int(old) + 1, ua.VariantType.Int32))
        new = await writable.read_value()
        report(f"{auth}: Write writable", "PASS" if new == int(old) + 1 else "FAIL")

        try:
            await client.get_node(READ_ONLY).write_value(
                ua.Variant(1, ua.VariantType.Int32)
            )
            report(f"{auth}: Reject write read-only", "FAIL", "write unexpectedly succeeded")
        except ua.UaStatusCodeError as exc:
            report(f"{auth}: Reject write read-only", "PASS", str(exc))
    except Exception as exc:  # noqa: BLE001
        report(f"{auth}: authorization", "FAIL", f"{type(exc).__name__}: {exc}")
    finally:
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass


async def main() -> int:
    for auth in ("anon", "username", "x509"):
        await check_identity(auth)
    return summarize("授权矩阵测试 ")


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
