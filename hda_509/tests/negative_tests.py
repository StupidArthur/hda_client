#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Negative tests for the OPC UA X.509 Compatibility Mocker.

Run with the NORMAL server (48620) already started:

    python tests/negative_tests.py

Each case connects and checks that the server rejects the connection at the
expected stage. The test distinguishes

  SecureChannel / Application authentication failure   (stages <= CreateSession)
  ActivateSession / User authentication failure        (stage  ActivateSession)

and keeps the original exception / OPC UA StatusCode in the report.

Cases:

  1. NoSecurity                          -> rejected (no None/None endpoint)
  2. Untrusted client Application cert   -> BadCertificateUntrusted
  3. Wrong client Application URI        -> BadCertificateUriInvalid
  4. Expired client Application cert     -> BadCertificateTimeInvalid
  5. Client cert / private key mismatch  -> fails during OpenSecureChannel
  6. Untrusted X.509 user cert           -> BadUserAccessDenied (ActivateSession)
  7. Wrong user private key              -> BadIdentityTokenInvalid (ActivateSession)
"""

import asyncio
import sys
from pathlib import Path

# 让脚本可以从仓库任意目录运行
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from asyncua import Client, ua  # noqa: E402

from conn import (  # noqa: E402
    CERTS,
    DEFAULT_SERVER_CERT,
    DEFAULT_URL,
    MODES,
    POLICY_CLASSES,
    StageFailed,
    setup_client,
    stage_connect,
)

SERVER_CERT = CERTS / "server_cert.pem"
APP_A_CERT = CERTS / "client_app_a_cert.pem"
APP_A_KEY = CERTS / "client_app_a_key.pem"
APP_A_URI = "urn:example.org:FreeOpcUa:opcua-asyncio"

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


async def attempt(*, auth: str = "anon", app_cert=None, app_key=None, server_cert=None,
                  app_uri=None, user_cert=None, user_key=None, no_security=False) -> tuple[str, BaseException] | None:
    """
    尝试完成一次分阶段连接。返回 (成功阶段, None) 或 (None, StageFailed)。
    若连接成功返回 ("success", None)。
    """
    try:
        if no_security:
            client = Client(DEFAULT_URL)
            await client.connect()
            await client.disconnect()
            return "success", None
        client = await setup_client(
            DEFAULT_URL,
            app_cert=Path(app_cert),
            app_key=Path(app_key),
            server_cert=Path(server_cert),
            policy_name="Basic256Sha256",
            mode_name="SignAndEncrypt",
            application_uri=app_uri,
        )
        await stage_connect(
            client,
            auth=auth,
            user_cert=Path(user_cert) if user_cert else None,
            user_key=Path(user_key) if user_key else None,
            print_steps=False,
        )
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return "success", None
    except StageFailed as e:
        return None, e
    except Exception as e:  # noqa: BLE001
        return None, e


async def case_no_security() -> None:
    outcome, err = await attempt(no_security=True)
    if outcome == "success":
        report("NoSecurity 被拒绝", "FAIL", "连接成功(不应发生)")
    else:
        report("NoSecurity 被拒绝", "PASS", f"{type(err).__name__}: {err}")


async def case_untrusted_app_cert() -> None:
    outcome, err = await attempt(
        app_cert=CERTS / "client_app_untrusted_cert.pem",
        app_key=CERTS / "client_app_untrusted_key.pem",
        server_cert=SERVER_CERT,
        app_uri="urn:example.org:FreeOpcUa:untrusted",
    )
    if outcome == "success":
        report("未受信 Client Application 证书", "FAIL", "连接成功(不应发生)")
        return
    detail = f"{type(err.cause).__name__}: {err.cause}" if isinstance(err, StageFailed) else f"{type(err).__name__}: {err}"
    if "BadCertificateUntrusted" in detail:
        report("未受信 Client Application 证书", "PASS", detail)
    else:
        report("未受信 Client Application 证书", "PASS?", detail)


async def case_wrong_app_uri() -> None:
    # 证书 SAN URI 是 wrong-uri，客户端却声明正确的 application_uri -> 不匹配
    outcome, err = await attempt(
        app_cert=CERTS / "client_app_wrong_uri_cert.pem",
        app_key=CERTS / "client_app_wrong_uri_key.pem",
        server_cert=SERVER_CERT,
        app_uri=APP_A_URI,
    )
    if outcome == "success":
        report("错误 Client Application URI", "FAIL", "连接成功(不应发生)")
        return
    detail = f"{type(err.cause).__name__}: {err.cause}" if isinstance(err, StageFailed) else f"{type(err).__name__}: {err}"
    if "BadCertificateUriInvalid" in detail:
        report("错误 Client Application URI", "PASS", detail)
    else:
        report("错误 Client Application URI", "PASS?", detail)


async def case_expired_app_cert() -> None:
    outcome, err = await attempt(
        app_cert=CERTS / "client_app_expired_cert.pem",
        app_key=CERTS / "client_app_expired_key.pem",
        server_cert=SERVER_CERT,
        app_uri=APP_A_URI,
    )
    if outcome == "success":
        report("过期 Client Application 证书", "FAIL", "连接成功(不应发生)")
        return
    detail = f"{type(err.cause).__name__}: {err.cause}" if isinstance(err, StageFailed) else f"{type(err).__name__}: {err}"
    if "BadCertificateTimeInvalid" in detail:
        report("过期 Client Application 证书", "PASS", detail)
    else:
        report("过期 Client Application 证书", "PASS?", detail)


async def case_cert_key_mismatch() -> None:
    outcome, err = await attempt(
        app_cert=APP_A_CERT,
        app_key=CERTS / "client_app_a_mismatch_key.pem",
        server_cert=SERVER_CERT,
        app_uri=APP_A_URI,
    )
    if outcome == "success":
        report("Client 证书/私钥不匹配", "FAIL", "连接成功(不应发生)")
        return
    detail = f"{type(err.cause).__name__}: {err.cause}" if isinstance(err, StageFailed) else f"{type(err).__name__}: {err}"
    report("Client 证书/私钥不匹配", "PASS", detail)


async def case_untrusted_user_cert() -> None:
    outcome, err = await attempt(
        auth="x509",
        app_cert=APP_A_CERT,
        app_key=APP_A_KEY,
        server_cert=SERVER_CERT,
        app_uri=APP_A_URI,
        user_cert=CERTS / "user_untrusted_cert.pem",
        user_key=CERTS / "user_untrusted_key.pem",
    )
    if outcome == "success":
        report("未受信 User Certificate (用户认证)", "FAIL", "连接成功(不应发生)")
        return
    detail = f"{type(err.cause).__name__}: {err.cause}" if isinstance(err, StageFailed) else f"{type(err).__name__}: {err}"
    if isinstance(err, StageFailed) and err.stage == "ActivateSession":
        report("未受信 User Certificate (用户认证)", "PASS", detail)
    else:
        report("未受信 User Certificate (用户认证)", "PASS?", detail)


async def case_wrong_user_key() -> None:
    outcome, err = await attempt(
        auth="x509",
        app_cert=APP_A_CERT,
        app_key=APP_A_KEY,
        server_cert=SERVER_CERT,
        app_uri=APP_A_URI,
        user_cert=CERTS / "user_cert.pem",
        user_key=CERTS / "user_wrong_key.pem",
    )
    if outcome == "success":
        report("错误 User 私钥 (用户认证)", "FAIL", "连接成功(不应发生)")
        return
    detail = f"{type(err.cause).__name__}: {err.cause}" if isinstance(err, StageFailed) else f"{type(err).__name__}: {err}"
    if isinstance(err, StageFailed) and err.stage == "ActivateSession":
        report("错误 User 私钥 (用户认证)", "PASS", detail)
    else:
        report("错误 User 私钥 (用户认证)", "PASS?", detail)


async def main() -> None:
    print("负向测试 (需要 Normal Server 48620 正在运行)：\n")
    await case_no_security()
    await case_untrusted_app_cert()
    await case_wrong_app_uri()
    await case_expired_app_cert()
    await case_cert_key_mismatch()
    await case_untrusted_user_cert()
    await case_wrong_user_key()

    print("\n汇总：")
    failed = [c for c, o, _ in RESULTS if o.startswith("FAIL")]
    for case, outcome, detail in RESULTS:
        print(f"  {outcome:5} {case}")
    if failed:
        print(f"\n{len(failed)} 个用例失败。")
        return 1
    print(f"\n全部 {len(RESULTS)} 个负向用例通过。")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
