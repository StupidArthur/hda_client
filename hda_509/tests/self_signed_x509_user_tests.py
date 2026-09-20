#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Combined authentication test: self-signed Application Certificate +
independent self-signed User Certificate (X509IdentityToken).

Scenario: config_self_signed_x509_user.yaml (port 48627).

Two strictly separate trust domains:
  Application Trust   : self-signed client Application Certificate, validated
                        by the server Application trust store at CreateSession.
  User Authentication : independent self-signed User Certificate, validated by
                        the user-manager direct whitelist (+ validity) at
                        ActivateSession via X509IdentityToken.

Client Application Certificate != User Certificate (different cert/key pairs).
The server config here DISABLES Anonymous and Username (only X509 user auth).

Cases:
  1. all correct                      -> OpenSecureChannel/CreateSession/
                                         ActivateSession(X509)/Read all PASS
  2. untrusted Application cert       -> CreateSession BadCertificateUntrusted
  3. unregistered User cert           -> ActivateSession BadUserAccessDenied
  4. expired User cert (registered)   -> ActivateSession BadUserAccessDenied
                                         (proves failure is the USER cert)
  5. wrong User private key           -> ActivateSession BadIdentityTokenInvalid
  6. Anonymous (no X509 token)        -> ActivateSession rejected (no fallback)
  7. Application cert as User cert    -> ActivateSession rejected (identity
                                         separation)

Strict PASS/FAIL; server offline / network errors are never a PASS.

Run with the server started:

    python main.py config_self_signed_x509_user.yaml
    python tests/self_signed_x509_user_tests.py
"""

import asyncio
import socket
import sys
from pathlib import Path

from cryptography import x509

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from conn import (  # noqa: E402
    StageFailed,
    read_node,
    setup_client,
    stage_connect,
)

URL = "opc.tcp://127.0.0.1:48627/ua_mocker/"
SS = Path(__file__).resolve().parents[1] / "test_material" / "self_signed"
SERVER_CERT = SS / "server" / "server_self_signed_cert.pem"

APP_CERT = SS / "client" / "client_self_signed_cert.pem"
APP_KEY = SS / "client" / "client_self_signed_key.pem"
APP_URI = "urn:example.org:FreeOpcUa:selfsigned-client"

USER_CERT = SS / "user" / "user_self_signed_cert.pem"
USER_KEY = SS / "user" / "user_self_signed_key.pem"

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
    app_cert=APP_CERT,
    app_key=APP_KEY,
    app_uri=APP_URI,
    auth="x509",
    user_cert=USER_CERT,
    user_key=USER_KEY,
    print_steps: bool = False,
) -> Attempt:
    """尝试组合认证连接。auth 可以是 x509 / anon。"""
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
            client, auth=auth,
            user_cert=user_cert if user_cert else Path(""),
            user_key=user_key if user_key else Path(""),
            policy_name="Basic256Sha256", mode_name="SignAndEncrypt",
            print_steps=print_steps,
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


async def assert_certificates_distinct() -> None:
    """证明 Client Application Certificate 与 User Certificate 确实不同。"""
    app = x509.load_pem_x509_certificate(APP_CERT.read_bytes())
    user = x509.load_pem_x509_certificate(USER_CERT.read_bytes())
    der_diff = APP_CERT.read_bytes() != USER_CERT.read_bytes()
    pub_diff = app.public_key().public_bytes(
        __import__("cryptography").hazmat.primitives.serialization.Encoding.DER,
        __import__("cryptography").hazmat.primitives.serialization.PublicFormat.SubjectPublicKeyInfo,
    ) != user.public_key().public_bytes(
        __import__("cryptography").hazmat.primitives.serialization.Encoding.DER,
        __import__("cryptography").hazmat.primitives.serialization.PublicFormat.SubjectPublicKeyInfo,
    )
    check = der_diff and pub_diff
    report("Application Cert != User Cert (DER 不同 + PublicKey 不同)", "PASS" if check else "FAIL",
           f"DER_diff={der_diff} pubkey_diff={pub_diff}")


async def case_all_correct() -> None:
    print("Case 1. 全部正确（trusted App cert + registered User cert）：\n")
    if not server_online():
        report("Case 1 全部正确", "FAIL", "服务器未启动 (48627)")
        return
    a = await attempt(print_steps=True)
    if a.ok:
        report("Case 1 全部正确", "PASS", a.error)
    elif a.network_error:
        report("Case 1 全部正确", "FAIL", f"服务器离线: {a.error}")
    else:
        report("Case 1 全部正确", "FAIL", f"stage={a.stage or '-'} err={a.error}")


async def case_untrusted_app_cert() -> None:
    print("\nCase 2. Application Certificate 未受信任（User cert 正确）：\n")
    a = await attempt(
        app_cert=SS / "client" / "client_self_signed_untrusted_cert.pem",
        app_key=SS / "client" / "client_self_signed_untrusted_key.pem",
    )
    if a.ok:
        report("Case 2 未受信任 Application Cert", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 2 未受信任 Application Cert", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "CreateSession" and "BadCertificateUntrusted" in a.error:
        report("Case 2 未受信任 Application Cert", "PASS",
               f"Application 层失败(未进入 User Auth): CreateSession -> {a.error}")
    else:
        report("Case 2 未受信任 Application Cert", "FAIL",
               f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_unregistered_user_cert() -> None:
    print("\nCase 3. User Certificate 未注册（App cert 正确）：\n")
    a = await attempt(
        user_cert=SS / "user" / "user_self_signed_unregistered_cert.pem",
        user_key=SS / "user" / "user_self_signed_unregistered_key.pem",
    )
    if a.ok:
        report("Case 3 未注册 User Cert", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 3 未注册 User Cert", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadUserAccessDenied" in a.error:
        report("Case 3 未注册 User Cert", "PASS",
               f"OpenSecureChannel/CreateSession 通过, User 层失败: ActivateSession -> {a.error}")
    else:
        report("Case 3 未注册 User Cert", "FAIL",
               f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_expired_user_cert() -> None:
    print("\nCase 4. User Certificate 过期（已注册但过期）：\n")
    a = await attempt(
        user_cert=SS / "user" / "user_self_signed_expired_cert.pem",
        user_key=SS / "user" / "user_self_signed_expired_key.pem",
    )
    if a.ok:
        report("Case 4 过期 User Cert", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 4 过期 User Cert", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadUserAccessDenied" in a.error:
        report("Case 4 过期 User Cert", "PASS",
               f"App cert 正常(CreateSession 通过), 失败来自 User cert: ActivateSession -> {a.error}")
    else:
        report("Case 4 过期 User Cert", "FAIL",
               f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_wrong_user_key() -> None:
    print("\nCase 5. User Private Key 错误（User cert 正确）：\n")
    a = await attempt(
        user_cert=USER_CERT,
        user_key=SS / "user" / "user_self_signed_wrong_key.pem",
    )
    if a.ok:
        report("Case 5 错误 User 私钥", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 5 错误 User 私钥", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadIdentityTokenInvalid" in a.error:
        report("Case 5 错误 User 私钥", "PASS",
               f"CreateSession 通过, UserTokenSignature 校验失败: ActivateSession -> {a.error}")
    else:
        report("Case 5 错误 User 私钥", "FAIL",
               f"阶段/错误不符(期望 ActivateSession+BadIdentityTokenInvalid): stage={a.stage or '-'} err={a.error}")


async def case_anonymous_no_fallback() -> None:
    print("\nCase 6. 不发送 X509 UserIdentityToken（Anonymous 被禁用）：\n")
    a = await attempt(auth="anon")
    if a.ok:
        report("Case 6 Anonymous 不降级", "FAIL", "Anonymous Session 成功(不应发生)")
    elif a.network_error:
        report("Case 6 Anonymous 不降级", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession":
        report("Case 6 Anonymous 不降级", "PASS",
               f"OpenSecureChannel/CreateSession 通过, ActivateSession 拒绝(anonymous:false): {a.error}")
    else:
        report("Case 6 Anonymous 不降级", "FAIL",
               f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def case_app_cert_as_user_cert() -> None:
    print("\nCase 7. 把 Application Certificate 当成 User Certificate：\n")
    # SecureChannel 用正确的 App cert；User 令牌故意提交同一张 App cert。
    a = await attempt(user_cert=APP_CERT, user_key=APP_KEY)
    if a.ok:
        report("Case 7 App cert 冒充 User cert", "FAIL", "连接成功(不应发生)")
    elif a.network_error:
        report("Case 7 App cert 冒充 User cert", "FAIL", f"服务器离线: {a.error}")
    elif a.stage == "ActivateSession" and "BadUserAccessDenied" in a.error:
        report("Case 7 App cert 冒充 User cert", "PASS",
               f"CreateSession 通过(Application 层 OK), User 层拒绝同一张证书: ActivateSession -> {a.error}")
    else:
        report("Case 7 App cert 冒充 User cert", "FAIL",
               f"阶段/错误不符: stage={a.stage or '-'} err={a.error}")


async def main() -> int:
    print("组合认证测试：self-signed Application Cert + 独立 self-signed User Cert (48627)\n")
    await assert_certificates_distinct()
    await case_all_correct()
    await case_untrusted_app_cert()
    await case_unregistered_user_cert()
    await case_expired_user_cert()
    await case_wrong_user_key()
    await case_anonymous_no_fallback()
    await case_app_cert_as_user_cert()

    print("\n汇总：")
    failed = [c for c, o, _ in RESULTS if o.startswith("FAIL")]
    for case, outcome, detail in RESULTS:
        print(f"  {outcome:5} {case}")
    if failed:
        print(f"\n{len(failed)} 个用例失败。")
        return 1
    print(f"\n全部 {len(RESULTS)} 个组合认证用例通过。")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
