#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Positive tests for the OPC UA X.509 Compatibility Mocker.

Run with the servers started:

    python main.py config_x509.yaml                      # normal   (48620)
    python main.py config_scenarios/self_signed_48621.yaml

then:

    python tests/positive_tests.py                       # normal 48620 only
    python tests/positive_tests.py --with-self-signed    # also tests 48621

Covers:
  * Application Certificate + Anonymous
  * Application Certificate + UserName (test/test)
  * Application Certificate + X.509 User Certificate
  * all published SecurityPolicy / MessageSecurityMode combinations
    (Basic256Sha256, Aes128Sha256RsaOaep, Aes256Sha256RsaPss x Sign/SignAndEncrypt)
  * node read / write / read-back on the mocker address space
"""

import argparse
import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from conn import (  # noqa: E402
    CERTS,
    DEFAULT_USER_CERT,
    DEFAULT_USER_KEY,
    MODES,
    StageFailed,
    read_node,
    setup_client,
    stage_connect,
)

NORMAL_URL = "opc.tcp://127.0.0.1:48620/ua_mocker/"
SELF_SIGNED_URL = "opc.tcp://127.0.0.1:48621/ua_mocker/"

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


async def one_connection(
    *,
    url: str,
    auth: str,
    policy: str,
    mode: str,
    server_cert: Path,
    app_cert: Path,
    app_key: Path,
    user_cert: Path = DEFAULT_USER_CERT,
    user_key: Path = DEFAULT_USER_KEY,
    write_node: bool = False,
) -> tuple[str, str]:
    """执行一次完整分阶段连接 + 读节点（可选写回读）。返回 (outcome, detail)。"""
    try:
        client = await setup_client(
            url,
            app_cert=app_cert,
            app_key=app_key,
            server_cert=server_cert,
            policy_name=policy,
            mode_name=mode,
            application_uri="urn:example.org:FreeOpcUa:opcua-asyncio",
        )
        await stage_connect(
            client, auth=auth, user_cert=user_cert, user_key=user_key,
            policy_name=policy, mode_name=mode, print_steps=False,
        )
        val = await read_node(client, "ns=1;s=int32_ch_1")
        if write_node:
            from asyncua import ua
            node = client.get_node("ns=1;s=double_wr_1")
            await node.write_value(123.45, varianttype=ua.VariantType.Double)
            val2 = await node.read_value()
            if val2 != 123.45:
                raise RuntimeError(f"写回读不一致: {val2!r}")
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return "PASS", f"read int32_ch_1={val!r}" + (" write/readback OK" if write_node else "")
    except StageFailed as e:
        return "FAIL", f"stage {e.stage}: {type(e.cause).__name__}: {e.cause}"
    except Exception as e:  # noqa: BLE001
        return "FAIL", f"{type(e).__name__}: {e}"


async def run_normal() -> None:
    print("Normal Server (48620)：\n")
    # 三种身份认证
    for auth, label in [("anon", "Application Certificate + Anonymous"),
                        ("username", "Application Certificate + UserName (test/test)"),
                        ("x509", "Application Certificate + X.509 User Certificate")]:
        outcome, detail = await one_connection(
            url=NORMAL_URL, auth=auth, policy="Basic256Sha256", mode="SignAndEncrypt",
            server_cert=CERTS / "server_cert.pem",
            app_cert=CERTS / "client_app_a_cert.pem",
            app_key=CERTS / "client_app_a_key.pem",
        )
        report(label, outcome, detail)

    # 写回读
    outcome, detail = await one_connection(
        url=NORMAL_URL, auth="username", policy="Basic256Sha256", mode="SignAndEncrypt",
        server_cert=CERTS / "server_cert.pem",
        app_cert=CERTS / "client_app_a_cert.pem",
        app_key=CERTS / "client_app_a_key.pem",
        write_node=True,
    )
    report("可写节点 写回读", outcome, detail)

    # 所有已发布的策略/模式组合
    print()
    print("SecurityPolicy / MessageSecurityMode 组合：\n")
    for policy in ["Basic256Sha256", "Aes128Sha256RsaOaep", "Aes256Sha256RsaPss"]:
        for mode in ["Sign", "SignAndEncrypt"]:
            outcome, detail = await one_connection(
                url=NORMAL_URL, auth="anon", policy=policy, mode=mode,
                server_cert=CERTS / "server_cert.pem",
                app_cert=CERTS / "client_app_a_cert.pem",
                app_key=CERTS / "client_app_a_key.pem",
            )
            report(f"{policy}/{mode}", outcome, detail)


async def run_self_signed() -> None:
    print()
    print("Self-signed Server (48621)：\n")
    outcome, detail = await one_connection(
        url=SELF_SIGNED_URL, auth="anon", policy="Basic256Sha256", mode="SignAndEncrypt",
        server_cert=CERTS / "server_self_signed_cert.pem",
        app_cert=CERTS / "client_app_a_cert.pem",
        app_key=CERTS / "client_app_a_key.pem",
    )
    report("Self-signed Server 证书 + 正常客户端", outcome, detail)
    outcome, detail = await one_connection(
        url=SELF_SIGNED_URL, auth="username", policy="Basic256Sha256", mode="SignAndEncrypt",
        server_cert=CERTS / "server_self_signed_cert.pem",
        app_cert=CERTS / "client_app_a_cert.pem",
        app_key=CERTS / "client_app_a_key.pem",
    )
    report("Self-signed Server + UserName", outcome, detail)


async def main() -> None:
    parser = argparse.ArgumentParser(description="Positive tests")
    parser.add_argument("--with-self-signed", action="store_true",
                        help="同时测试 self-signed 场景 (48621)")
    args = parser.parse_args()

    await run_normal()
    if args.with_self_signed:
        await run_self_signed()

    print("\n汇总：")
    failed = [c for c, o, _ in RESULTS if o.startswith("FAIL")]
    for case, outcome, detail in RESULTS:
        print(f"  {outcome:5} {case}")
    if failed:
        print(f"\n{len(failed)} 个用例失败。")
        return 1
    print(f"\n全部 {len(RESULTS)} 个正向用例通过。")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
