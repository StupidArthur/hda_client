# -*- coding: utf-8 -*-
"""
Shared reference-client helpers for the OPC UA X.509 Compatibility Mocker.

Provides staged connection with clear output:

    [1] GetEndpoints          OK
    [2] Endpoint selection    OK
    [3] OpenSecureChannel     OK
    [4] CreateSession         OK
    [5] ActivateSession       OK
    [6] Read test node        OK

Three identity modes are supported (all combine a client APPLICATION
certificate with a USER identity):

  anon       Application Certificate + Anonymous
  username   Application Certificate + UserName (test/test)
  x509       Application Certificate + X.509 User Certificate
             (the X.509 user cert is a DIFFERENT certificate from the
              application certificate; see README)

GetEndpoints is performed on a short-lived secure channel opened with the
same security policy, then that channel is closed and a fresh connection is
established for the real session. This keeps the stages observable without
depending on an unencrypted channel (the normal server does not publish
None/None by design).

Path resolution uses pathlib and never depends on the current working
directory or on shell tools, so the code behaves identically on macOS and
Windows.
"""

import argparse
import asyncio
import sys
from pathlib import Path

from asyncua import Client, ua
from asyncua.crypto.security_policies import (
    SecurityPolicyAes128Sha256RsaOaep,
    SecurityPolicyAes256Sha256RsaPss,
    SecurityPolicyBasic256Sha256,
    SecurityPolicyNone,
)

# ---------------------------------------------------------------------------
# Paths (relative to this file, so it works from any working directory)
# ---------------------------------------------------------------------------
BASE_DIR = Path(__file__).resolve().parents[1]
CERTS = BASE_DIR / "test_material" / "certs"

DEFAULT_URL = "opc.tcp://127.0.0.1:48630/ua_auth/"
DEFAULT_SERVER_CERT = CERTS / "server_cert.pem"
DEFAULT_APP_CERT = CERTS / "client_app_a_cert.pem"
DEFAULT_APP_KEY = CERTS / "client_app_a_key.pem"
DEFAULT_USER_CERT = CERTS / "user_cert.pem"
DEFAULT_USER_KEY = CERTS / "user_key.pem"
DEFAULT_USERNAME = "test"
DEFAULT_PASSWORD = "test"

# ---------------------------------------------------------------------------
# Policy / mode mapping
# ---------------------------------------------------------------------------
POLICY_CLASSES = {
    "None": SecurityPolicyNone,
    "Basic256Sha256": SecurityPolicyBasic256Sha256,
    "Aes128Sha256RsaOaep": SecurityPolicyAes128Sha256RsaOaep,
    "Aes256Sha256RsaPss": SecurityPolicyAes256Sha256RsaPss,
}

MODES = {
    "None": ua.MessageSecurityMode.None_,
    "Sign": ua.MessageSecurityMode.Sign,
    "SignAndEncrypt": ua.MessageSecurityMode.SignAndEncrypt,
}

STAGES = ["GetEndpoints", "Endpoint selection", "OpenSecureChannel", "CreateSession", "ActivateSession"]


class StageFailed(Exception):
    """Raised when a staged connection step fails (original exception preserved)."""

    def __init__(self, stage: str, cause: BaseException):
        super().__init__(f"{stage} failed: {type(cause).__name__}: {cause}")
        self.stage = stage
        self.cause = cause


def _policy_label(policy_name: str, mode_name: str) -> str:
    return f"{policy_name}/{mode_name}"


def find_endpoint(client: Client, policy_name: str, mode_name: str):
    """[2] 从 GetEndpoints 结果里挑选匹配的 Endpoint（找不到返回 None）。"""
    try:
        return Client.find_endpoint(
            client._endpoints,
            MODES[mode_name],
            POLICY_CLASSES[policy_name].URI,
        )
    except ua.UaError:
        return None


async def _open_temp_channel(client: Client) -> None:
    """在临时连接上打开受保护通道（不创建 session）。"""
    await client.connect_socket()
    await client.send_hello()
    await client.open_secure_channel()


async def _close_temp_channel(client: Client) -> None:
    try:
        await client.close_secure_channel()
    finally:
        client.disconnect_socket()


async def fetch_endpoints(client: Client) -> list:
    """[1] 用独立临时连接调用 GetEndpoints，返回服务器实际暴露的端点列表。"""
    try:
        await _open_temp_channel(client)
        endpoints = await client.get_endpoints()
        return endpoints
    finally:
        await _close_temp_channel(client)


async def stage_connect(
    client: Client,
    *,
    auth: str,
    username: str = DEFAULT_USERNAME,
    password: str = DEFAULT_PASSWORD,
    user_cert: Path = DEFAULT_USER_CERT,
    user_key: Path = DEFAULT_USER_KEY,
    policy_name: str = "Basic256Sha256",
    mode_name: str = "SignAndEncrypt",
    print_steps: bool = True,
    skip_endpoints: bool = False,
) -> Client:
    """
    分阶段连接并返回处于 Activated 状态的 client（后续可直接读节点）。

    skip_endpoints=True 时跳过 GetEndpoints / Endpoint selection（例如
    证书/私钥不匹配的负向测试需要从 OpenSecureChannel 阶段直接观察失败）。

    任一阶段失败都会抛出 StageFailed，保留原始 exception（其 message 里
    包含 OPC UA StatusCode 如 BadCertificateUntrusted / BadIdentityTokenRejected）。
    """
    if auth not in ("anon", "username", "x509"):
        raise ValueError(f"auth 必须是 anon/username/x509, 得到: {auth}")

    stage_no = {name: i for i, name in enumerate(STAGES, start=1)}

    def _ok(stage: str, detail: str = "") -> None:
        if print_steps:
            print(f"  [{stage_no[stage]}] {stage:<18} OK {detail}")

    if not skip_endpoints:
        # [1] GetEndpoints
        try:
            endpoints = await fetch_endpoints(client)
            client._endpoints = endpoints
            _ok("GetEndpoints", f"({len(endpoints)} endpoint(s))")
        except BaseException as e:  # noqa: BLE001
            raise StageFailed("GetEndpoints", e)

        # [2] Endpoint selection
        try:
            ep = find_endpoint(client, policy_name, mode_name)
            if ep is None:
                raise ua.UaError(
                    f"No matching endpoint for {_policy_label(policy_name, mode_name)}. "
                    f"Server exposes: "
                    f"{sorted({f'{e.SecurityPolicyUri} / {e.SecurityMode.name}' for e in endpoints})}"
                )
            _ok("Endpoint selection", ep.SecurityPolicyUri)
        except BaseException as e:  # noqa: BLE001
            raise StageFailed("Endpoint selection", e)

    # [3] OpenSecureChannel
    try:
        await client.connect_socket()
        await client.send_hello()
        await client.open_secure_channel()
        _ok("OpenSecureChannel")
    except BaseException as e:  # noqa: BLE001
        raise StageFailed("OpenSecureChannel", e)

    # [4] CreateSession  (server validates the client APPLICATION certificate here)
    try:
        await client.create_session()
        _ok("CreateSession")
    except BaseException as e:  # noqa: BLE001
        raise StageFailed("CreateSession", e)

    # [5] ActivateSession (server validates the USER identity here)
    try:
        if auth == "anon":
            await client.activate_session()
            _ok("ActivateSession", "Anonymous")
        elif auth == "username":
            await client.activate_session(username=username, password=password)
            _ok("ActivateSession", f"UserName '{username}'")
        else:  # x509
            await client.load_client_certificate(str(user_cert))
            await client.load_private_key(str(user_key))
            await client.activate_session(certificate=client.user_certificate)
            _ok("ActivateSession", "X509 User Certificate")
    except BaseException as e:  # noqa: BLE001
        raise StageFailed("ActivateSession", e)

    return client


async def read_node(client: Client, node_id: str):
    """[6] 读取测试节点值。"""
    return await client.get_node(node_id).read_value()


async def setup_client(
    url: str,
    *,
    app_cert: Path,
    app_key: Path,
    server_cert: Path,
    policy_name: str,
    mode_name: str,
    application_uri: str | None = None,
) -> Client:
    """构造已配置 application certificate 与 server certificate 的 Client。"""
    client = Client(url)
    if application_uri:
        client.application_uri = application_uri
    await client.set_security(
        POLICY_CLASSES[policy_name],
        str(app_cert),
        str(app_key),
        server_certificate=str(server_cert),
        mode=MODES[mode_name],
    )
    return client


def add_common_args(parser: argparse.ArgumentParser, *, with_identity: bool = True) -> None:
    """命令行参数：连接、证书、策略、身份。"""
    parser.add_argument("--url", default=DEFAULT_URL, help=f"OPC UA 端点 (默认 {DEFAULT_URL})")
    parser.add_argument("--app-cert", default=str(DEFAULT_APP_CERT), help="客户端 Application 证书")
    parser.add_argument("--app-key", default=str(DEFAULT_APP_KEY), help="客户端 Application 私钥")
    parser.add_argument("--app-uri", default=None, help="客户端 ApplicationUri（默认取自证书 SAN URI）")
    parser.add_argument(
        "--server-cert", default=str(DEFAULT_SERVER_CERT), help="服务端证书（用于 pin）"
    )
    parser.add_argument("--policy", default="Basic256Sha256", choices=list(POLICY_CLASSES))
    parser.add_argument(
        "--mode", default="SignAndEncrypt", choices=list(MODES), help="MessageSecurityMode"
    )
    parser.add_argument("--node", default="ns=1;s=int32_ch_1", help="要读取的测试节点")
    if with_identity:
        parser.add_argument(
            "--auth",
            default="anon",
            choices=["anon", "username", "x509"],
            help="用户认证方式",
        )
        parser.add_argument("--user-cert", default=str(DEFAULT_USER_CERT), help="用户 X.509 证书")
        parser.add_argument("--user-key", default=str(DEFAULT_USER_KEY), help="用户 X.509 私钥")
        parser.add_argument("--username", default=DEFAULT_USERNAME)
        parser.add_argument("--password", default=DEFAULT_PASSWORD)


def run_async(coro) -> int:
    """运行 asyncio 协程，捕获 StageFailed 并返回退出码。"""
    try:
        asyncio.run(coro)
        return 0
    except StageFailed as e:
        print(f"\n失败: {e}", file=sys.stderr)
        return 1
