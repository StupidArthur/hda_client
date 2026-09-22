#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab - negative authentication tests.

场景：configs/all_auth.yaml (port 48630)。

区分两条独立链路：
  Application 层（OpenSecureChannel / CreateSession）
  User 层（ActivateSession）

Cases:
  1. 错误用户名                 -> ActivateSession 拒绝 (BadUserAccessDenied)
  2. 错误密码                   -> ActivateSession 拒绝 (BadUserAccessDenied)
  3. 未注册 User 证书           -> ActivateSession BadUserAccessDenied
  4. 过期 User 证书（已注册）    -> ActivateSession 拒绝（失败来自 User 证书）
  5. 错误 User 私钥             -> ActivateSession BadIdentityTokenInvalid
  6. App 证书冒充 User 证书      -> ActivateSession BadUserAccessDenied（身份隔离）
  7. 未受信 App 证书            -> CreateSession BadCertificateUntrusted
  8. 过期 App 证书              -> CreateSession BadCertificateTimeInvalid
  9. App 证书 URI 不匹配        -> CreateSession BadCertificateUriInvalid
 10. App 证书/私钥不匹配        -> OpenSecureChannel 阶段失败
 11. None/None Session 尝试     -> 无法建立（discovery-only，无 None Session endpoint）

严格 PASS/FAIL；服务器离线一律 FAIL。

运行（先启动服务器）：

    python main.py configs/all_auth.yaml
    python tests/auth_negative_tests.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from _auth_common import (  # noqa: E402
    APP_CERT,
    APP_KEY,
    APP_URI,
    CERTS,
    DEFAULT_URL,
    attempt,
    report,
    server_online,
    summarize,
)

WRONG_URI = "urn:example.org:FreeOpcUa:wrong-uri"


def _expect_user_reject(case: str, a) -> None:
    """期望在 User 层（ActivateSession）被拒，且 CreateSession 已通过。"""
    if a.ok:
        report(case, "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report(case, "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession":
        report(case, "PASS", f"CreateSession 通过, User 层拒绝: {a.error}")
    else:
        report(case, "FAIL", f"阶段不符(期望 ActivateSession): stage={a.stage or '-'} err={a.error}")


def _expect_app_reject(case: str, a, expected: str) -> None:
    """期望在 Application 层（CreateSession）被拒，含指定 StatusCode。"""
    if a.ok:
        report(case, "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report(case, "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and expected in a.error:
        report(case, "PASS", f"CreateSession -> {a.error}")
    else:
        report(case, "FAIL", f"阶段/错误不符(期望 CreateSession+{expected}): stage={a.stage or '-'} err={a.error}")


async def case_wrong_username() -> None:
    print("Case 1. 错误用户名（正确密码）：\n")
    a = await attempt(auth="username", username="wrong_user", password="test")
    _expect_user_reject("Case 1 错误用户名", a)


async def case_wrong_password() -> None:
    print("\nCase 2. 错误密码（正确用户名）：\n")
    a = await attempt(auth="username", username="test", password="wrong_pass")
    _expect_user_reject("Case 2 错误密码", a)


async def case_unregistered_user_cert() -> None:
    print("\nCase 3. 未注册 User 证书：\n")
    a = await attempt(
        auth="x509",
        user_cert=CERTS / "user_unregistered_cert.pem",
        user_key=CERTS / "user_unregistered_key.pem",
    )
    if a.ok:
        report("Case 3 未注册 User 证书", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 3 未注册 User 证书", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadUserAccessDenied" in a.error:
        report("Case 3 未注册 User 证书", "PASS", f"ActivateSession -> {a.error}")
    else:
        report("Case 3 未注册 User 证书", "FAIL",
               f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_expired_user_cert() -> None:
    print("\nCase 4. 过期 User 证书（已在白名单但过期）：\n")
    a = await attempt(
        auth="x509",
        user_cert=CERTS / "user_expired_cert.pem",
        user_key=CERTS / "user_expired_key.pem",
    )
    _expect_user_reject("Case 4 过期 User 证书", a)


async def case_wrong_user_key() -> None:
    print("\nCase 5. 错误 User 私钥（User 证书正确）：\n")
    a = await attempt(auth="x509", user_cert=CERTS / "user_cert.pem", user_key=CERTS / "user_wrong_key.pem")
    if a.ok:
        report("Case 5 错误 User 私钥", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 5 错误 User 私钥", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadIdentityTokenInvalid" in a.error:
        report("Case 5 错误 User 私钥", "PASS", f"ActivateSession -> {a.error}")
    else:
        report("Case 5 错误 User 私钥", "FAIL",
               f"阶段/错误不符(期望 ActivateSession+BadIdentityTokenInvalid): stage={a.stage or '-'} err={a.error}")


async def case_app_cert_as_user() -> None:
    print("\nCase 6. 把 Application 证书当成 User 证书：\n")
    a = await attempt(auth="x509", user_cert=APP_CERT, user_key=APP_KEY)
    if a.ok:
        report("Case 6 App 证书冒充 User 证书", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 6 App 证书冒充 User 证书", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadUserAccessDenied" in a.error:
        report("Case 6 App 证书冒充 User 证书", "PASS",
               f"CreateSession 通过, User 层拒绝同一张证书: ActivateSession -> {a.error}")
    else:
        report("Case 6 App 证书冒充 User 证书", "FAIL",
               f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_untrusted_app_cert() -> None:
    print("\nCase 7. 未受信 Application 证书：\n")
    a = await attempt(
        auth="anon",
        app_cert=CERTS / "client_app_untrusted_cert.pem",
        app_key=CERTS / "client_app_untrusted_key.pem",
        app_uri="urn:example.org:FreeOpcUa:untrusted",
    )
    _expect_app_reject("Case 7 未受信 App 证书", a, "BadCertificateUntrusted")


async def case_expired_app_cert() -> None:
    print("\nCase 8. 过期 Application 证书：\n")
    a = await attempt(
        auth="anon",
        app_cert=CERTS / "client_app_expired_cert.pem",
        app_key=CERTS / "client_app_expired_key.pem",
        app_uri=APP_URI,
    )
    _expect_app_reject("Case 8 过期 App 证书", a, "BadCertificateTimeInvalid")


async def case_wrong_app_uri() -> None:
    print("\nCase 9. Application 证书 URI 不匹配：\n")
    # 证书 SAN URI = wrong-uri，但客户端 advertise 正确的 APP_URI -> 不匹配。
    a = await attempt(
        auth="anon",
        app_cert=CERTS / "client_app_wrong_uri_cert.pem",
        app_key=CERTS / "client_app_wrong_uri_key.pem",
        app_uri=APP_URI,
    )
    _expect_app_reject("Case 9 App 证书 URI 不匹配", a, "BadCertificateUriInvalid")


async def case_app_cert_key_mismatch() -> None:
    print("\nCase 10. Application 证书/私钥不匹配：\n")
    a = await attempt(auth="anon", app_cert=APP_CERT, app_key=CERTS / "client_app_a_mismatch_key.pem",
                      skip_endpoints=True)
    if a.ok:
        report("Case 10 App 证书/私钥不匹配", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 10 App 证书/私钥不匹配", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "OpenSecureChannel":
        report("Case 10 App 证书/私钥不匹配", "PASS", f"OpenSecureChannel 阶段失败: {a.error}")
    else:
        report("Case 10 App 证书/私钥不匹配", "FAIL",
               f"阶段不符(期望 OpenSecureChannel): stage={a.stage or '-'} err={a.error}")


async def case_none_session() -> None:
    print("\nCase 11. None/None Session 尝试（discovery-only）：\n")
    a = await attempt(auth="anon", policy="None", mode="None")
    if a.ok:
        report("Case 11 None/None Session 拒绝", "FAIL", "None Session 成功(不应发生)")
    elif a.network_error:
        report("Case 11 None/None Session 拒绝", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "Endpoint selection" or "No matching endpoint" in a.error:
        report("Case 11 None/None Session 拒绝", "PASS",
               f"服务器不暴露 None/None Session endpoint: {a.error}")
    else:
        report("Case 11 None/None Session 拒绝", "PASS",
               f"None Session 被拒: stage={a.stage or '-'} err={a.error}")


async def main() -> int:
    print("UA Auth Lab 负向认证测试 (all_auth / 48630)\n")
    print("=" * 70)
    if not server_online():
        print(f"服务器未启动 ({DEFAULT_URL})，无法执行负向测试。")
        return 1
    await case_wrong_username()
    await case_wrong_password()
    await case_unregistered_user_cert()
    await case_expired_user_cert()
    await case_wrong_user_key()
    await case_app_cert_as_user()
    await case_untrusted_app_cert()
    await case_expired_app_cert()
    await case_wrong_app_uri()
    await case_app_cert_key_mismatch()
    await case_none_session()
    print("=" * 70)
    return summarize("负向认证测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
