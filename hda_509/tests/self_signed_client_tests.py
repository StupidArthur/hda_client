#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Tests for the self-signed Application Certificate scenario
(config_self_signed.yaml, port 48626).

The mock server directly trusts a SELF-SIGNED client Application Certificate
(no Test Root CA). These tests verify:

  1. trusted self-signed client cert      -> success (OpenSecureChannel,
                                             CreateSession, ActivateSession
                                             (Anonymous), Read)
  2. untrusted self-signed client cert    -> BadCertificateUntrusted
  3. expired self-signed client cert      -> BadCertificateTimeInvalid
  4. wrong ApplicationUri client cert     -> BadCertificateUriInvalid
  5. client cert / private key mismatch   -> fails at OpenSecureChannel

Strict PASS/FAIL; server offline / network errors are never a PASS; any FAIL
makes the process exit non-zero.

Run with the server started:

    python main.py config_self_signed.yaml
    python tests/self_signed_client_tests.py
"""

import asyncio
import socket
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from conn import (  # noqa: E402
    StageFailed,
    read_node,
    setup_client,
    stage_connect,
)

URL = "opc.tcp://127.0.0.1:48626/ua_mocker/"
SS = Path(__file__).resolve().parents[1] / "test_material" / "self_signed"
SERVER_CERT = SS / "server" / "server_self_signed_cert.pem"
CLIENT_CERT = SS / "client" / "client_self_signed_cert.pem"
CLIENT_KEY = SS / "client" / "client_self_signed_key.pem"
CLIENT_URI = "urn:example.org:FreeOpcUa:selfsigned-client"

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


def server_online() -> bool:
    from urllib.parse import urlparse
    parsed = urlparse(URL)
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
    def __init__(self, ok: bool, stage: str = "", error: str = ""):
        self.ok, self.stage, self.error = ok, stage, error

    @property
    def network_error(self) -> bool:
        return is_network_error(self.error)


async def attempt(
    *,
    app_cert=CLIENT_CERT,
    app_key=CLIENT_KEY,
    app_uri=CLIENT_URI,
    skip_endpoints: bool = False,
    print_steps: bool = False,
) -> Attempt:
    try:
        client = await setup_client(
            URL,
            app_cert=app_cert,
            app_key=app_key,
            server_cert=SERVER_CERT,
            policy_name="Basic256Sha256",
            mode_name="SignAndEncrypt",
            application_uri=app_uri,
        )
        await stage_connect(
            client, auth="anon",
            policy_name="Basic256Sha256", mode_name="SignAndEncrypt",
            print_steps=print_steps, skip_endpoints=skip_endpoints,
        )
        val = await read_node(client, "ns=1;s=int32_ch_1")
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return Attempt(ok=True, error=f"read int32_ch_1={val!r}")
    except StageFailed as e:
        return Attempt(ok=False, stage=e.stage, error=f"{type(e.cause).__name__}: {e.cause}")
    except Exception as e:  # noqa: BLE001
        return Attempt(ok=False, error=f"{type(e).__name__}: {e}")


async def case_trusted() -> None:
    print("1. trusted self-signed client certificate：\n")
    if not server_online():
        report("受信任 self-signed 客户端证书连接", "FAIL", "服务器未启动 (48626)")
        return
    a = await attempt(print_steps=True)
    if a.ok:
        report("受信任 self-signed 客户端证书连接", "PASS", a.error)
    elif a.network_error:
        report("受信任 self-signed 客户端证书连接", "FAIL", f"服务器离线: {a.error}")
    else:
        report("受信任 self-signed 客户端证书连接", "FAIL", f"stage={a.stage or '-'} err={a.error}")


async def case_untrusted() -> None:
    a = await attempt(
        app_cert=SS / "client" / "client_self_signed_untrusted_cert.pem",
        app_key=SS / "client" / "client_self_signed_untrusted_key.pem",
    )
    if a.ok:
        report("未受信任 self-signed 客户端证书", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("未受信任 self-signed 客户端证书", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and "BadCertificateUntrusted" in a.error:
        report("未受信任 self-signed 客户端证书", "PASS", f"CreateSession -> {a.error}")
    else:
        report("未受信任 self-signed 客户端证书", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_expired() -> None:
    a = await attempt(
        app_cert=SS / "client" / "client_self_signed_expired_cert.pem",
        app_key=SS / "client" / "client_self_signed_expired_key.pem",
    )
    if a.ok:
        report("过期 self-signed 客户端证书", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("过期 self-signed 客户端证书", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and "BadCertificateTimeInvalid" in a.error:
        report("过期 self-signed 客户端证书", "PASS", f"CreateSession -> {a.error}")
    else:
        report("过期 self-signed 客户端证书", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_wrong_uri() -> None:
    # 证书 SAN URI 是 wrong-uri，客户端声明正确 URI -> 不匹配
    a = await attempt(
        app_cert=SS / "client" / "client_self_signed_wrong_uri_cert.pem",
        app_key=SS / "client" / "client_self_signed_wrong_uri_key.pem",
        app_uri=CLIENT_URI,
    )
    if a.ok:
        report("ApplicationUri 不匹配 (self-signed)", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("ApplicationUri 不匹配 (self-signed)", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and "BadCertificateUriInvalid" in a.error:
        report("ApplicationUri 不匹配 (self-signed)", "PASS", f"CreateSession -> {a.error}")
    else:
        report("ApplicationUri 不匹配 (self-signed)", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_key_mismatch() -> None:
    a = await attempt(
        app_cert=CLIENT_CERT,
        app_key=SS / "client" / "client_self_signed_mismatch_key.pem",
        skip_endpoints=True,
    )
    if a.ok:
        report("Client 证书/私钥不匹配 (self-signed)", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Client 证书/私钥不匹配 (self-signed)", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "OpenSecureChannel" and server_online():
        report("Client 证书/私钥不匹配 (self-signed)", "PASS",
               f"OpenSecureChannel 阶段失败(服务器在线): {a.error}")
    elif a.stage == "OpenSecureChannel" and not server_online():
        report("Client 证书/私钥不匹配 (self-signed)", "FAIL", "服务器离线(不应把离线当 PASS)")
    else:
        report("Client 证书/私钥不匹配 (self-signed)", "FAIL", f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def main() -> int:
    print("Self-signed Client Trust 场景测试 (需要 config_self_signed.yaml, 48626)：\n")
    await case_trusted()
    await case_untrusted()
    await case_expired()
    await case_wrong_uri()
    await case_key_mismatch()

    print("\n汇总：")
    failed = [c for c, o, _ in RESULTS if o.startswith("FAIL")]
    for case, outcome, detail in RESULTS:
        print(f"  {outcome:5} {case}")
    if failed:
        print(f"\n{len(failed)} 个用例失败。")
        return 1
    print(f"\n全部 {len(RESULTS)} 个 self-signed 用例通过。")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
