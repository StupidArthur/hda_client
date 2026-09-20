#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Positive tests for the OPC UA X.509 Compatibility Mocker.

Run with the servers started:

    python main.py config_x509.yaml                       # normal   (48620)
    python main.py config_scenarios/self_signed_48621.yaml # optional
    python main.py config_scenarios/custom_uri_48625.yaml  # optional

then:

    python tests/positive_tests.py                        # normal only
    python tests/positive_tests.py --with-self-signed     # + self-signed 48621
    python tests/positive_tests.py --with-custom-uri      # + custom uri 48625

Covers:
  * endpoint discovery assertions (exact SecurityPolicy / SecurityMode /
    UserIdentityTokens, not just "count == 6");
  * Application Certificate + Anonymous / UserName / X.509 User;
  * node read / write / read-back;
  * every published policy/mode combination.
"""

import argparse
import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from asyncua import Client, ua  # noqa: E402

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
from endpoint_dump import short_policy_uri  # noqa: E402

NORMAL_URL = "opc.tcp://127.0.0.1:48620/ua_mocker/"
SELF_SIGNED_URL = "opc.tcp://127.0.0.1:48621/ua_mocker/"
CUSTOM_URI_URL = "opc.tcp://127.0.0.1:48625/ua_mocker/"

# 预期安全端点：policy 短名 -> 预期 SecurityMode 集合（Normal 场景全量）
EXPECTED_SECURE_POLICIES_FULL = {
    "Basic256Sha256": {"Sign", "SignAndEncrypt"},
    "Aes128_Sha256_RsaOaep": {"Sign", "SignAndEncrypt"},
    "Aes256_Sha256_RsaPss": {"Sign", "SignAndEncrypt"},
}
# Self-signed 场景只发布 Basic256Sha256
EXPECTED_SECURE_POLICIES_SELF_SIGNED = {
    "Basic256Sha256": {"Sign", "SignAndEncrypt"},
}
# 预期 UserIdentityTokens（每个安全端点都应发布）
EXPECTED_TOKEN_TYPES = {0, 1, 2}  # Anonymous / UserName / Certificate

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


async def fetch_endpoints(url: str) -> list:
    """unsecured discovery GetEndpoints（需要服务器发布 discovery None 端点）。"""
    client = Client(url)
    try:
        return await client.connect_and_get_server_endpoints()
    finally:
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass


async def assert_endpoints(url: str, *, expected_policies: dict | None = None) -> None:
    """断言服务器真实发布的安全端点（策略 URI / 模式 / 用户令牌）。"""
    expected_policies = expected_policies or EXPECTED_SECURE_POLICIES_FULL
    endpoints = await fetch_endpoints(url)

    # 收集 (short_policy, mode_name)
    present: dict[str, set] = {}
    token_sets: dict[tuple, set] = {}
    modes_by_policy: dict[str, set] = {}
    for ep in endpoints:
        pol = short_policy_uri(ep.SecurityPolicyUri)
        mode = ep.SecurityMode.name
        modes_by_policy.setdefault(pol, set()).add(mode)
        token_sets[(pol, mode)] = {int(t.TokenType) for t in ep.UserIdentityTokens}

    ok = True
    for policy, expected_modes in expected_policies.items():
        got = modes_by_policy.get(policy, set())
        if got != expected_modes:
            ok = False
            report(f"Endpoint {policy} 模式断言", "FAIL",
                   f"期望 {sorted(expected_modes)}, 实际 {sorted(got)}")
            continue
        for mode in sorted(expected_modes):
            tokens = token_sets.get((policy, mode), set())
            if tokens == EXPECTED_TOKEN_TYPES:
                report(f"Endpoint {policy}/{mode} UserIdentityTokens", "PASS",
                       f"Anonymous+UserName+Certificate")
            else:
                ok = False
                report(f"Endpoint {policy}/{mode} UserIdentityTokens", "FAIL",
                       f"期望 {sorted(EXPECTED_TOKEN_TYPES)}, 实际 {sorted(tokens)}")

    # unsecured discovery 仍然可用（SecurityPolicyNone factory 保留），
    # 但 GetEndpoints 不应暴露 None/None Session Endpoint。
    if "None" in modes_by_policy:
        ok = False
        report("GetEndpoints 不暴露 None/None Session Endpoint", "FAIL",
               f"仍返回 None/None 端点: {sorted(modes_by_policy['None'])}")
    else:
        report("GetEndpoints 不暴露 None/None Session Endpoint", "PASS")

    if ok:
        report(f"Endpoint 发现断言 ({url})", "PASS", f"{len(endpoints)} 个端点")


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
    await assert_endpoints(NORMAL_URL)

    print()
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

    outcome, detail = await one_connection(
        url=NORMAL_URL, auth="username", policy="Basic256Sha256", mode="SignAndEncrypt",
        server_cert=CERTS / "server_cert.pem",
        app_cert=CERTS / "client_app_a_cert.pem",
        app_key=CERTS / "client_app_a_key.pem",
        write_node=True,
    )
    report("可写节点 写回读", outcome, detail)

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
    await assert_endpoints(SELF_SIGNED_URL, expected_policies=EXPECTED_SECURE_POLICIES_SELF_SIGNED)
    outcome, detail = await one_connection(
        url=SELF_SIGNED_URL, auth="anon", policy="Basic256Sha256", mode="SignAndEncrypt",
        server_cert=CERTS / "server_self_signed_cert.pem",
        app_cert=CERTS / "client_app_a_cert.pem",
        app_key=CERTS / "client_app_a_key.pem",
    )
    report("Self-signed Server 证书 + 正常客户端 (pin)", outcome, detail)
    outcome, detail = await one_connection(
        url=SELF_SIGNED_URL, auth="username", policy="Basic256Sha256", mode="SignAndEncrypt",
        server_cert=CERTS / "server_self_signed_cert.pem",
        app_cert=CERTS / "client_app_a_cert.pem",
        app_key=CERTS / "client_app_a_key.pem",
    )
    report("Self-signed Server + UserName", outcome, detail)


async def main() -> int:
    parser = argparse.ArgumentParser(description="Positive tests")
    parser.add_argument("--with-self-signed", action="store_true", help="同时测试 self-signed 场景 (48621)")
    parser.add_argument("--with-custom-uri", action="store_true", help="同时测试 custom-uri 场景 (48625)")
    args = parser.parse_args()

    await run_normal()
    if args.with_self_signed:
        await run_self_signed()
    if args.with_custom_uri:
        from custom_application_uri_tests import run_custom_uri_assert
        await run_custom_uri_assert()

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
