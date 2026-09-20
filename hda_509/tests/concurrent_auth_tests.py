#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Concurrent user-authentication test (with degradation detection).

Verifies that the UserIdentityToken-kind tracking used by the combined user
manager (iserver._last_user_token_kind) does NOT mix up token types across
concurrent sessions. asyncua 2.0.1 calls UserManager.get_user() synchronously
from InternalSession.activate_session() (no await between recording the token
kind and calling get_user), so within a single event-loop the marker is safe;
this test proves it under real concurrency.

What is actually verified:
  valid anonymous          -> must succeed + read node
  valid username          -> must succeed + read node
  valid x509              -> must succeed + read node
  invalid username        -> must FAIL (BadUserAccessDenied)
  unregistered x509       -> must FAIL (BadUserAccessDenied)
  expired x509            -> must FAIL (BadUserAccessDenied)

The invalid identities must FAIL. If the global _last_user_token_kind were
mixed up to "anon", a Username/X509 token could be wrongly accepted as
Anonymous and succeed — this test would then fail. (We do NOT claim to prove
more than this: token-kind isolation across concurrent sessions.)

30 concurrent sessions (5 per identity class). Run with the normal server
(48620) started:

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

# (label, auth, username, password, user_cert, user_key, expect_success)
CASES = [
    ("valid-anon",      "anon",     None,   None,   None, None, True),
    ("valid-username",  "username", "test", "test", None, None, True),
    ("valid-x509",      "x509",     None,   None,   DEFAULT_USER_CERT, DEFAULT_USER_KEY, True),
    ("invalid-username","username", "test", "WRONG", None, None, False),
    ("unregistered-x509", "x509",   None,   None,   CERTS / "user_unregistered_cert.pem",
     CERTS / "user_unregistered_key.pem", False),
    ("expired-x509",    "x509",     None,   None,   CERTS / "user_expired_cert.pem",
     CERTS / "user_expired_key.pem", False),
]

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


async def one_session(index: int, label: str, auth: str, username, password,
                      user_cert, user_key, expect_success: bool) -> tuple[int, str, str, bool]:
    """一次会话尝试。返回 (index, label, detail, success)。"""
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
            username=username or "test",
            password=password or "test",
            user_cert=user_cert if user_cert else Path(""),
            user_key=user_key if user_key else Path(""),
            policy_name="Basic256Sha256", mode_name="SignAndEncrypt", print_steps=False,
        )
        val = await read_node(client, "ns=1;s=int32_ch_1")
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return index, label, f"connected read={val!r}", True
    except Exception as e:  # noqa: BLE001
        return index, label, f"{type(e).__name__}: {e}", False


async def main() -> int:
    plan = []
    idx = 0
    for label, auth, user, pwd, ucert, ukey, expect in CASES:
        for _ in range(5):
            plan.append((idx, label, auth, user, pwd, ucert, ukey, expect))
            idx += 1

    print("并发用户认证测试（含无效身份降级检测）：30 个并发会话\n")
    print("  valid: anon / username / x509       -> 必须成功")
    print("  invalid: wrong-password / unregistered-x509 / expired-x509 -> 必须失败\n")

    tasks = [one_session(i, label, auth, user, pwd, ucert, ukey, expect)
             for i, label, auth, user, pwd, ucert, ukey, expect in plan]
    results = await asyncio.gather(*tasks)

    failures = []
    for (i, label, auth, user, pwd, ucert, ukey, expect), (_, rlabel, detail, success) in zip(plan, results):
        ok = (success == expect)
        status = "PASS" if ok else "FAIL"
        print(f"  [{status}] session#{i:02d} {rlabel} -> {detail} "
              f"(期望 {'成功' if expect else '失败'})")
        if not ok:
            failures.append((i, rlabel, detail))

    for i, label, detail in failures:
        report(f"session#{i} {label}", "FAIL", detail)

    if failures:
        print(f"\n{len(failures)}/30 个会话结果与预期不符 -> 可能发生身份降级/token 串号。")
        return 1

    report("并发 30 会话 (valid+invalid 身份)", "PASS",
           "valid 全部成功, invalid 全部失败, 无身份降级")
    print(f"\n全部 {len(results)} 个并发会话行为正确。")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
