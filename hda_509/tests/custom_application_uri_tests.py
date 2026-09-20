#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Custom ApplicationUri test.

Verifies that a NON-default ApplicationUri is really applied by the server:

    application.uri = urn:ua-hda:test:custom-server

must be visible in:
  * EndpointDescription.Server.ApplicationUri
  * ServerArray (ns=0;i=2254)
  * NamespaceArray[1] (ns=0;i=2255)
and must match the Server Application Certificate SAN URI.

Run with the custom-uri server started:

    python main.py config_scenarios/custom_uri_48625.yaml
    python tests/custom_application_uri_tests.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from asyncua import Client, ua  # noqa: E402

from conn import CERTS, StageFailed, read_node, setup_client, stage_connect  # noqa: E402

CUSTOM_URI = "urn:ua-hda:test:custom-server"
CUSTOM_URI_URL = "opc.tcp://127.0.0.1:48625/ua_mocker/"

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


async def run_custom_uri_assert() -> None:
    print()
    print("Custom ApplicationUri Server (48625)：\n")

    # ---- 通过 unsecured discovery 读取 endpoint -----------------------------
    disc = Client(CUSTOM_URI_URL)
    try:
        endpoints = await disc.connect_and_get_server_endpoints()
    finally:
        try:
            await disc.disconnect()
        except Exception:  # noqa: BLE001
            pass

    if not endpoints:
        report("GetEndpoints", "FAIL", "未返回任何端点（服务器可能未启动 48625）")
        return

    ep = endpoints[0]
    report("EndpointDescription.Server.ApplicationUri",
           "PASS" if ep.Server.ApplicationUri == CUSTOM_URI else "FAIL",
           ep.Server.ApplicationUri)

    # ---- 证书 SAN URI 是否与 ApplicationUri 一致 -----------------------------
    if ep.ServerCertificate:
        from cryptography import x509
        cert = x509.load_der_x509_certificate(ep.ServerCertificate)
        san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
        uris = san.get_values_for_type(x509.UniformResourceIdentifier)
        report("Server Certificate SAN URI 与 ApplicationUri 一致",
               "PASS" if CUSTOM_URI in uris else "FAIL", str(uris))

    # ---- ServerArray / NamespaceArray（通过受保护通道读取） --------------------
    # 直接 Client().connect() 是无保护通道，会被拒绝；改用安全连接。
    try:
        client = await setup_client(
            CUSTOM_URI_URL,
            app_cert=CERTS / "client_app_a_cert.pem",
            app_key=CERTS / "client_app_a_key.pem",
            server_cert=CERTS / "server_custom_uri_cert.pem",
            policy_name="Basic256Sha256",
            mode_name="SignAndEncrypt",
            application_uri="urn:example.org:FreeOpcUa:opcua-asyncio",
        )
        await stage_connect(client, auth="anon", policy_name="Basic256Sha256",
                            mode_name="SignAndEncrypt", print_steps=False)
        server_array = await client.get_node("ns=0;i=2254").read_value()
        ns_array = await client.get_node("ns=0;i=2255").read_value()
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        report("ServerArray 含 custom uri",
               "PASS" if CUSTOM_URI in server_array else "FAIL", str(server_array))
        report("NamespaceArray[1] == custom uri",
               "PASS" if len(ns_array) > 1 and ns_array[1] == CUSTOM_URI else "FAIL", str(ns_array))
    except Exception as e:  # noqa: BLE001
        report("ServerArray/NamespaceArray 读取", "FAIL", f"{type(e).__name__}: {e}")

    # ---- 用 custom-uri server 证书做一次安全连接 -------------------------------
    try:
        client = await setup_client(
            CUSTOM_URI_URL,
            app_cert=CERTS / "client_app_a_cert.pem",
            app_key=CERTS / "client_app_a_key.pem",
            server_cert=CERTS / "server_custom_uri_cert.pem",
            policy_name="Basic256Sha256",
            mode_name="SignAndEncrypt",
            application_uri="urn:example.org:FreeOpcUa:opcua-asyncio",
        )
        await stage_connect(client, auth="anon", policy_name="Basic256Sha256",
                            mode_name="SignAndEncrypt", print_steps=False)
        val = await read_node(client, "ns=1;s=int32_ch_1")
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        report("custom-uri 服务器安全连接 + 读节点", "PASS", f"int32_ch_1={val!r}")
    except StageFailed as e:
        report("custom-uri 服务器安全连接 + 读节点", "FAIL",
               f"stage {e.stage}: {type(e.cause).__name__}: {e.cause}")


async def main() -> int:
    await run_custom_uri_assert()
    print("\n汇总：")
    failed = [c for c, o, _ in RESULTS if o.startswith("FAIL")]
    for case, outcome, detail in RESULTS:
        print(f"  {outcome:5} {case}")
    if failed:
        print(f"\n{len(failed)} 个用例失败。")
        return 1
    print(f"\n全部 {len(RESULTS)} 个用例通过。")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
