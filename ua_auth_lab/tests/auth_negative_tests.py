#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 负向认证测试（manifest 驱动）。

矩阵抽样（matrix_port_tests.py）只覆盖"方式不对/端点不对"，本文件覆盖
**材料与凭证不对**这一类，是独立维度。用例按需从 configs/matrix/manifest.json
挑端口，不再硬编码端口号。

两条独立链路：
  Application 层（OpenSecureChannel / CreateSession）——客户端应用身份
  User 层（ActivateSession）——用户身份

Cases:
  1. 错误用户名                -> ActivateSession BadUserAccessDenied   (username 端口)
  2. 错误密码                  -> ActivateSession BadUserAccessDenied   (username 端口)
  3. 未注册 User 证书          -> ActivateSession BadUserAccessDenied   (x509 端口)
  4. 过期 User 证书(已在白名单)  -> ActivateSession 拒绝（失败源是 User 证书）(x509 端口)
  5. 错误 User 私钥            -> ActivateSession BadIdentityTokenInvalid (x509 端口)
  6. App 证书冒充 User 证书     -> ActivateSession BadUserAccessDenied（身份隔离）(x509 端口)
  7. 未受信 App 证书           -> CreateSession BadCertificateUntrusted   (加密端口)
  8. 过期 App 证书             -> CreateSession BadCertificateTimeInvalid (加密端口)
  9. App 证书 URI 不匹配        -> CreateSession BadCertificateUriInvalid  (加密端口)
 10. App 证书/私钥不匹配        -> OpenSecureChannel 阶段失败              (加密端口)
 11. None/None Session 尝试    -> 无法建立（加密端口不发布 None Session 端点）

严格 PASS/FAIL；服务器离线 / 网络错误一律 FAIL。

运行（先启动矩阵端口）：

    python tools/matrix_ctl.py start
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
    attempt,
    pick_port,
    report,
    server_online,
    summarize,
)

WRONG_URI = "urn:example.org:FreeOpcUa:wrong-uri"

# ---- 端口选取（manifest 驱动）--------------------------------------------
# User 层用例需要对应认证开放的端口；Application 层用例与用户认证无关，
# 统一挑一个加密端口（Basic256Sha256/SignAndEncrypt，现代策略，避免废弃策略干扰）。
EP_POLICY = "Basic256Sha256"
EP_MODE = "SignAndEncrypt"


def _user_port(auth: str) -> dict:
    return pick_port(auth=auth, policy=EP_POLICY, mode=EP_MODE)


def _app_port() -> dict:
    """Application 层用例端口（anon 即可，认证方式不影响 CreateSession 判定）。"""
    return pick_port(auth="anon", policy=EP_POLICY, mode=EP_MODE)


def _reject_port() -> dict:
    """需要"没有 None Session 端点"的加密端口（用例 11）。"""
    return pick_port(auth="anon", require_encrypted=True)


def _expect_user_reject(case: str, a) -> None:
    """期望在 User 层（ActivateSession）被拒，且 CreateSession 已通过。"""
    if a.ok:
        report(case, "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report(case, "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession":
        report(case, "PASS", f"CreateSession 通过, User 层拒绝: {a.error}")
    else:
        report(case, "FAIL",
               f"阶段不符(期望 ActivateSession): stage={a.stage or '-'} err={a.error}")


def _expect_user_reject_status(case: str, a, expected: str) -> None:
    """期望 User 层被拒且带指定 StatusCode。"""
    if a.ok:
        report(case, "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report(case, "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and expected in a.error:
        report(case, "PASS", f"ActivateSession -> {a.error}")
    else:
        report(case, "FAIL",
               f"阶段/错误不符(期望 ActivateSession+{expected}): "
               f"stage={a.stage or '-'} err={a.error}")


def _expect_app_reject(case: str, a, expected: str) -> None:
    """期望在 Application 层（CreateSession）被拒，含指定 StatusCode。"""
    if a.ok:
        report(case, "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report(case, "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and expected in a.error:
        report(case, "PASS", f"CreateSession -> {a.error}")
    else:
        report(case, "FAIL",
               f"阶段/错误不符(期望 CreateSession+{expected}): "
               f"stage={a.stage or '-'} err={a.error}")


async def case_wrong_username(p: dict) -> None:
    print(f"Case 1. 错误用户名（正确密码）  [{p['port']} {p['policy']}/{p['mode']}]：\n")
    a = await attempt(auth="username", username="wrong_user", password="test",
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_user_reject_status("Case 1 错误用户名", a, "BadUserAccessDenied")


async def case_wrong_password(p: dict) -> None:
    print(f"\nCase 2. 错误密码（正确用户名）  [{p['port']}]：\n")
    a = await attempt(auth="username", username="test", password="wrong_pass",
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_user_reject_status("Case 2 错误密码", a, "BadUserAccessDenied")


async def case_unregistered_user_cert(p: dict) -> None:
    print(f"\nCase 3. 未注册 User 证书  [{p['port']}]：\n")
    a = await attempt(auth="x509",
                      user_cert=CERTS / "user_unregistered_cert.pem",
                      user_key=CERTS / "user_unregistered_key.pem",
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_user_reject_status("Case 3 未注册 User 证书", a, "BadUserAccessDenied")


async def case_expired_user_cert(p: dict) -> None:
    print(f"\nCase 4. 过期 User 证书（已在白名单）  [{p['port']}]：\n")
    a = await attempt(auth="x509",
                      user_cert=CERTS / "user_expired_cert.pem",
                      user_key=CERTS / "user_expired_key.pem",
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_user_reject("Case 4 过期 User 证书", a)


async def case_wrong_user_key(p: dict) -> None:
    print(f"\nCase 5. 错误 User 私钥（User 证书正确）  [{p['port']}]：\n")
    a = await attempt(auth="x509", user_cert=CERTS / "user_cert.pem",
                      user_key=CERTS / "user_wrong_key.pem",
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_user_reject_status("Case 5 错误 User 私钥", a, "BadIdentityTokenInvalid")


async def case_app_cert_as_user(p: dict) -> None:
    print(f"\nCase 6. 把 Application 证书当成 User 证书  [{p['port']}]：\n")
    a = await attempt(auth="x509", user_cert=APP_CERT, user_key=APP_KEY,
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
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


async def case_untrusted_app_cert(p: dict) -> None:
    print(f"\nCase 7. 未受信 Application 证书  [{p['port']}]：\n")
    a = await attempt(auth="anon",
                      app_cert=CERTS / "client_app_untrusted_cert.pem",
                      app_key=CERTS / "client_app_untrusted_key.pem",
                      app_uri="urn:example.org:FreeOpcUa:untrusted",
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_app_reject("Case 7 未受信 App 证书", a, "BadCertificateUntrusted")


async def case_expired_app_cert(p: dict) -> None:
    print(f"\nCase 8. 过期 Application 证书  [{p['port']}]：\n")
    a = await attempt(auth="anon",
                      app_cert=CERTS / "client_app_expired_cert.pem",
                      app_key=CERTS / "client_app_expired_key.pem",
                      app_uri=APP_URI,
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_app_reject("Case 8 过期 App 证书", a, "BadCertificateTimeInvalid")


async def case_wrong_app_uri(p: dict) -> None:
    print(f"\nCase 9. Application 证书 URI 不匹配  [{p['port']}]：\n")
    # 证书 SAN URI = wrong-uri，客户端却 advertise 正确的 APP_URI -> 不匹配。
    a = await attempt(auth="anon",
                      app_cert=CERTS / "client_app_wrong_uri_cert.pem",
                      app_key=CERTS / "client_app_wrong_uri_key.pem",
                      app_uri=APP_URI,
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"])
    _expect_app_reject("Case 9 App 证书 URI 不匹配", a, "BadCertificateUriInvalid")


async def case_app_key_mismatch(p: dict) -> None:
    print(f"\nCase 10. Application 证书/私钥不匹配  [{p['port']}]：\n")
    a = await attempt(auth="anon", app_cert=APP_CERT,
                      app_key=CERTS / "client_app_a_mismatch_key.pem",
                      policy=p["policy"], mode=p["mode"], url=p["url"], node=p["read_node"],
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


async def case_none_session(p: dict) -> None:
    print(f"\nCase 11. None/None Session 尝试（加密端口不发布）  [{p['port']}]：\n")
    a = await attempt(auth="anon", policy="None", mode="None",
                      url=p["url"], node=p["read_node"])
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
    user_ports = {auth: _user_port(auth) for auth in ("username", "x509")}
    app_port = _app_port()
    reject_port = _reject_port()

    print("UA Auth Lab 负向认证测试（manifest 驱动）\n")
    print(f"  User 层端口   : username={user_ports['username']['port']}  "
          f"x509={user_ports['x509']['port']}")
    print(f"  App 层端口    : {app_port['port']}")
    print(f"  None 拒绝端口 : {reject_port['port']}")
    print("=" * 74)

    # 预检：涉及的端口都要在
    needed = {user_ports["username"]["port"], user_ports["x509"]["port"],
              app_port["port"], reject_port["port"]}
    for p in (user_ports["username"], user_ports["x509"], app_port, reject_port):
        if not server_online(p["url"]):
            print(f"\n[FAIL] 端口 {p['port']} 未监听，无法执行负向测试。")
            print("请先运行: python tools/matrix_ctl.py start")
            report("负向测试预检", "FAIL", f"端口 {p['port']} 未监听")
            return summarize("负向认证测试 ")
    _ = needed

    await case_wrong_username(user_ports["username"])
    await case_wrong_password(user_ports["username"])
    await case_unregistered_user_cert(user_ports["x509"])
    await case_expired_user_cert(user_ports["x509"])
    await case_wrong_user_key(user_ports["x509"])
    await case_app_cert_as_user(user_ports["x509"])
    await case_untrusted_app_cert(app_port)
    await case_expired_app_cert(app_port)
    await case_wrong_app_uri(app_port)
    await case_app_key_mismatch(app_port)
    await case_none_session(reject_port)

    print("=" * 74)
    return summarize("负向认证测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
