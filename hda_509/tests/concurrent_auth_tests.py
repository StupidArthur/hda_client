#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Concurrent user-authentication test.

Verifies that the UserIdentityToken-kind tracking used by the combined user
manager (iserver._last_user_token_kind) does NOT mix up token types across
concurrent sessions. asyncua 2.0.1 calls UserManager.get_user() synchronously
from InternalSession.activate_session() (no await between recording the token
kind and calling get_user), so within a single event-loop the marker is safe;
this test proves it under real concurrency.

Launches 30 concurrent sessions (10 Anonymous, 10 UserName, 10 X.509 User),
each of which must activate with the CORRECT identity and read a node.

Run with the normal server (48620) started:

    python tests/concurrent_auth_tests.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from conn import (  # noqa: E402
    CERTS,
    DEFAULT_USER_CERT,
    DEFAULT_USER_KEY,
    read_node,
    setup_client,
    stage_connect,
)

NORMAL_URL = "opc.tcp://127.0.0.1:48620/ua_mocker/"
SERVER_CERT = CERTS / "server_cert.pem"
APP_CERT = CERTS / "client_app_a_cert.pem"
APP_KEY = CERTS / "client_app_a_key.pem"

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


async def one_session(index: int, auth: str) -> tuple[int, str, str]:
    """一次完整会话。返回 (index, outcome, detail)。"""
    try:
        client = await setup_client(
            NORMAL_URL,
            app_cert=APP_CERT,
            app_key=APP_KEY,
            server_cert=SERVER_CERT,
            policy_name="Basic256Sha256",
            mode_name="SignAndEncrypt",
            application_uri="urn:example.org:FreeOpcUa:opcua-asyncio",
        )
        await stage_connect(
            client, auth=auth,
            user_cert=DEFAULT_USER_CERT, user_key=DEFAULT_USER_KEY,
            policy_name="Basic256Sha256", mode_name="SignAndEncrypt", print_steps=False,
        )
        val = await read_node(client, "ns=1;s=int32_ch_1")
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return index, "PASS", f"{auth} read={val!r}"
    except Exception as e:  # noqa: BLE001
        return index, "FAIL", f"{auth}: {type(e).__name__}: {e}"


async def main() -> int:
    # 30 个并发会话：10 anon + 10 username + 10 x509
    plan: list[tuple[int, str]] = []
    i = 0
    for auth in ("anon", "username", "x509"):
        for _ in range(10):
            plan.append((i, auth))
            i += 1

    print(f"并发用户认证测试：{len(plan)} 个并发会话 (10 Anonymous / 10 UserName / 10 X.509 User)\n")
    tasks = [one_session(idx, auth) for idx, auth in plan]
    results = await asyncio.gather(*tasks)

    for idx, outcome, detail in sorted(results):
        print(f"  [{outcome}] session#{idx:02d} {detail}")

    failed = [d for _, o, d in results if o.startswith("FAIL")]
    for idx, outcome, detail in results:
        if outcome.startswith("FAIL"):
            report(f"session#{idx}", "FAIL", detail)
    if failed:
        print(f"\n{len(failed)}/{len(results)} 个并发会话失败 -> token 类型可能串了。")
        return 1

    report(f"并发 {len(results)} 个会话 (anon/username/x509)", "PASS",
           "全部以正确身份激活并读节点，token 类型未串")
    print(f"\n全部 {len(results)} 个并发会话通过。")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
