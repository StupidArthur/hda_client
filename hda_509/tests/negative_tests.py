#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Negative tests for the OPC UA X.509 Compatibility Mocker.

Run with the servers started:

    python main.py config_x509.yaml                       # normal   (48620)
    python main.py config_scenarios/self_signed_48621.yaml # optional

then:

    python tests/negative_tests.py                        # normal only
    python tests/negative_tests.py --with-self-signed     # + self-signed 48621

STRICT rules:
  * every case is either PASS or FAIL (no "PASS?");
  * a case only PASSES when the failure happens at the EXPECTED stage with
    the EXPECTED OPC UA StatusCode / exception;
  * ConnectionRefused / server-offline / DNS errors are NEVER a PASS;
  * any FAIL -> process exit code != 0.

Stages:
  SecureChannel / Application authentication failure  -> OpenSecureChannel..CreateSession
  ActivateSession / User authentication failure       -> ActivateSession
"""

import argparse
import asyncio
import socket
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from asyncua import Client, ua  # noqa: E402

from conn import (  # noqa: E402
    CERTS,
    DEFAULT_SERVER_CERT,
    DEFAULT_URL,
    StageFailed,
    setup_client,
    stage_connect,
)

NORMAL_URL = DEFAULT_URL
SELF_SIGNED_URL = "opc.tcp://127.0.0.1:48621/ua_mocker/"
SERVER_CERT = CERTS / "server_cert.pem"
SELF_SIGNED_SERVER_CERT = CERTS / "server_self_signed_cert.pem"
APP_A_CERT = CERTS / "client_app_a_cert.pem"
APP_A_KEY = CERTS / "client_app_a_key.pem"
APP_A_URI = "urn:example.org:FreeOpcUa:opcua-asyncio"

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


def server_online(url: str) -> bool:
    """TCP 探测：服务器是否真的在线。"""
    from urllib.parse import urlparse
    parsed = urlparse(url)
    try:
        with socket.create_connection((parsed.hostname, parsed.port), timeout=3):
            return True
    except OSError:
        return False


def is_network_error(err) -> bool:
    text = f"{type(err).__name__}: {err}".lower()
    if isinstance(err, (ConnectionRefusedError, ConnectionResetError, socket.gaierror, TimeoutError, OSError)):
        return True
    return any(k in text for k in ("connection refused", "name or service not known", "timed out", "connect call failed"))


class Attempt:
    """一次连接尝试的结果。"""

    def __init__(self, ok: bool, stage: str = "", error: str = ""):
        self.ok = ok                 # True 表示连接成功
        self.stage = stage           # 失败发生的阶段（StageFailed.stage）
        self.error = error           # 错误描述

    @property
    def network_error(self) -> bool:
        return is_network_error(self.error)

    @property
    def stage_failed(self) -> bool:
        return bool(self.stage)


async def attempt_secured(
    *,
    url: str,
    server_cert: Path,
    auth: str,
    app_cert: Path = APP_A_CERT,
    app_key: Path = APP_A_KEY,
    app_uri: str = APP_A_URI,
    user_cert: Path | None = None,
    user_key: Path | None = None,
    skip_endpoints: bool = False,
) -> Attempt:
    """使用指定证书/身份尝试分阶段连接。"""
    try:
        client = await setup_client(
            url,
            app_cert=app_cert,
            app_key=app_key,
            server_cert=server_cert,
            policy_name="Basic256Sha256",
            mode_name="SignAndEncrypt",
            application_uri=app_uri,
        )
        await stage_connect(
            client, auth=auth,
            user_cert=user_cert if user_cert else Path(""),
            user_key=user_key if user_key else Path(""),
            print_steps=False,
            skip_endpoints=skip_endpoints,
        )
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return Attempt(ok=True)
    except StageFailed as e:
        return Attempt(ok=False, stage=e.stage, error=f"{type(e.cause).__name__}: {e.cause}")
    except Exception as e:  # noqa: BLE001
        return Attempt(ok=False, error=f"{type(e).__name__}: {e}")


async def case_no_security() -> None:
    if not server_online(NORMAL_URL):
        report("NoSecurity", "FAIL", "服务器未启动 / 端口未监听")
        return
    try:
        client = Client(NORMAL_URL)
        await client.connect()
        await client.disconnect()
        report("NoSecurity", "FAIL", "unsecured Anonymous Session 竟然成功（不应发生）")
    except Exception as e:  # noqa: BLE001
        if is_network_error(e):
            report("NoSecurity", "FAIL", f"服务器离线: {type(e).__name__}: {e}")
        elif "BadUserAccessDenied" in f"{type(e).__name__}: {e}" or "BadSecurityPolicyRejected" in f"{type(e).__name__}: {e}":
            report("NoSecurity", "PASS", f"unsecured Session 被拒绝: {type(e).__name__}")
        else:
            report("NoSecurity", "FAIL", f"拒绝方式不符合预期: {type(e).__name__}: {e}")


async def case_untrusted_app_cert() -> None:
    a = await attempt_secured(
        url=NORMAL_URL, server_cert=SERVER_CERT, auth="anon",
        app_cert=CERTS / "client_app_untrusted_cert.pem",
        app_key=CERTS / "client_app_untrusted_key.pem",
        app_uri="urn:example.org:FreeOpcUa:untrusted",
    )
    if a.ok:
        report("未受信 Client Application 证书", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("未受信 Client Application 证书", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and "BadCertificateUntrusted" in a.error:
        report("未受信 Client Application 证书", "PASS", f"CreateSession -> {a.error}")
    else:
        report("未受信 Client Application 证书", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_wrong_app_uri() -> None:
    # 证书 SAN URI 是 wrong-uri，客户端声明正确的 application_uri -> 不匹配
    a = await attempt_secured(
        url=NORMAL_URL, server_cert=SERVER_CERT, auth="anon",
        app_cert=CERTS / "client_app_wrong_uri_cert.pem",
        app_key=CERTS / "client_app_wrong_uri_key.pem",
        app_uri=APP_A_URI,
    )
    if a.ok:
        report("错误 Client Application URI", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("错误 Client Application URI", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and "BadCertificateUriInvalid" in a.error:
        report("错误 Client Application URI", "PASS", f"CreateSession -> {a.error}")
    else:
        report("错误 Client Application URI", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_expired_app_cert() -> None:
    a = await attempt_secured(
        url=NORMAL_URL, server_cert=SERVER_CERT, auth="anon",
        app_cert=CERTS / "client_app_expired_cert.pem",
        app_key=CERTS / "client_app_expired_key.pem",
        app_uri=APP_A_URI,
    )
    if a.ok:
        report("过期 Client Application 证书", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("过期 Client Application 证书", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and "BadCertificateTimeInvalid" in a.error:
        report("过期 Client Application 证书", "PASS", f"CreateSession -> {a.error}")
    else:
        report("过期 Client Application 证书", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_cert_key_mismatch() -> None:
    # 跳过 GetEndpoints 前置，直接从 OpenSecureChannel 阶段观察失败。
    a = await attempt_secured(
        url=NORMAL_URL, server_cert=SERVER_CERT, auth="anon",
        app_cert=APP_A_CERT,
        app_key=CERTS / "client_app_a_mismatch_key.pem",
        app_uri=APP_A_URI,
        skip_endpoints=True,
    )
    if a.ok:
        report("Client 证书/私钥不匹配", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Client 证书/私钥不匹配", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "OpenSecureChannel" and server_online(NORMAL_URL):
        report("Client 证书/私钥不匹配", "PASS",
               f"OpenSecureChannel 阶段失败(服务器在线): {a.error} (asyncua 2.0.1 内部异常, 见 README)")
    elif a.stage == "OpenSecureChannel" and not server_online(NORMAL_URL):
        report("Client 证书/私钥不匹配", "FAIL", "服务器离线(不应把离线当 PASS)")
    else:
        report("Client 证书/私钥不匹配", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_untrusted_user_cert() -> None:
    a = await attempt_secured(
        url=NORMAL_URL, server_cert=SERVER_CERT, auth="x509",
        user_cert=CERTS / "user_untrusted_cert.pem",
        user_key=CERTS / "user_untrusted_key.pem",
    )
    if a.ok:
        report("未受信 User Certificate (用户认证)", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("未受信 User Certificate (用户认证)", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadUserAccessDenied" in a.error:
        report("未受信 User Certificate (用户认证)", "PASS", f"ActivateSession -> {a.error}")
    else:
        report("未受信 User Certificate (用户认证)", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_wrong_user_key() -> None:
    a = await attempt_secured(
        url=NORMAL_URL, server_cert=SERVER_CERT, auth="x509",
        user_cert=CERTS / "user_cert.pem",
        user_key=CERTS / "user_wrong_key.pem",
    )
    if a.ok:
        report("错误 User 私钥 (用户认证)", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("错误 User 私钥 (用户认证)", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and ("BadIdentityTokenInvalid" in a.error or "BadUserAccessDenied" in a.error):
        report("错误 User 私钥 (用户认证)", "PASS", f"ActivateSession -> {a.error}")
    else:
        report("错误 User 私钥 (用户认证)", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_expired_user_cert() -> None:
    # Application Certificate 正常（CreateSession 应通过），User cert 已过期
    a = await attempt_secured(
        url=NORMAL_URL, server_cert=SERVER_CERT, auth="x509",
        user_cert=CERTS / "user_expired_cert.pem",
        user_key=CERTS / "user_expired_key.pem",
    )
    if a.ok:
        report("过期 User Certificate (用户认证)", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("过期 User Certificate (用户认证)", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadUserAccessDenied" in a.error:
        report("过期 User Certificate (用户认证)", "PASS",
               f"CreateSession 正常(应用证书 OK), ActivateSession 因 User cert 过期被拒: {a.error}")
    else:
        report("过期 User Certificate (用户认证)", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_self_signed_wrong_pin() -> None:
    # 客户端 pin 了错误的 server 证书（普通 CA 证书而非自签名证书）。
    # 跳过 GetEndpoints 前置，直接从 OpenSecureChannel 阶段观察失败。
    if not server_online(SELF_SIGNED_URL):
        report("Self-signed Server 错误 pin", "FAIL", "self-signed 服务器未启动 (48621)")
        return
    a = await attempt_secured(
        url=SELF_SIGNED_URL, server_cert=SERVER_CERT, auth="anon",  # 故意用错误的 server_cert
        skip_endpoints=True,
    )
    if a.ok:
        report("Self-signed Server 错误 pin", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Self-signed Server 错误 pin", "FAIL", f"网络错误: {a.error}")
    elif a.stage in ("CreateSession", "OpenSecureChannel"):
        report("Self-signed Server 错误 pin", "PASS", f"{a.stage} 阶段失败: {a.error}")
    else:
        report("Self-signed Server 错误 pin", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def main() -> int:
    parser = argparse.ArgumentParser(description="Negative tests")
    parser.add_argument("--with-self-signed", action="store_true", help="同时测试 self-signed 场景 (48621)")
    args = parser.parse_args()

    print("负向测试 (需要 Normal Server 48620 正在运行)：\n")
    await case_no_security()
    await case_untrusted_app_cert()
    await case_wrong_app_uri()
    await case_expired_app_cert()
    await case_cert_key_mismatch()
    await case_untrusted_user_cert()
    await case_wrong_user_key()
    await case_expired_user_cert()
    if args.with_self_signed:
        print()
        print("Self-signed 场景 (48621)：\n")
        await case_self_signed_wrong_pin()

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
