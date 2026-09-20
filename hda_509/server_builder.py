# -*- coding: utf-8 -*-
"""
Common server builder for the OPC UA X.509 Compatibility Mocker.

单一构建入口，Normal / Self-signed 等所有场景共用；场景之间的差异全部
由 YAML 配置表达（端口、证书路径、策略列表等），因此不需要为每个场景
复制一份 server_main.py。

asyncua 2.0.1 说明：
    Server.set_security_policy() 接收一组 ua.SecurityPolicyType，每个
    类型直接对应一种 (SecurityPolicy, MessageSecurityMode, SecurityLevel)。
    因此要同时发布同一策略的 Sign 与 SignAndEncrypt，必须同时列出两个
    SecurityPolicyType（例如 Basic256Sha256_Sign + Basic256Sha256_SignAndEncrypt）。

    asyncua 2.0.1 实际支持并启用的组合（见
    asyncua.crypto.security_policies.SECURITY_POLICY_TYPE_MAP）：

        Basic256Sha256        Sign / SignAndEncrypt
        Aes128Sha256RsaOaep   Sign / SignAndEncrypt
        Aes256Sha256RsaPss    Sign / SignAndEncrypt

    Normal Server 默认发布 discovery-only None 端点（security.discovery:
    true）+ 6 个加密端点。None 端点仅用于 unsecured GetEndpoints /
    FindServers（第三方客户端发现流程）；任何在无保护通道上建立的
    Session 都会被拒绝（security.require_secured_session: true，由
    CombinedUserManager 强制）。

    客户端 Application Certificate 在 CreateSession 阶段由
    CertificateValidator 校验（TRUSTED_VALIDATION | PEER_CLIENT）：
      TIME_RANGE -> BadCertificateTimeInvalid
      URI        -> BadCertificateUriInvalid
      KEY_USAGE / EXT_KEY_USAGE -> BadCertificateUseNotAllowed
      TRUSTED    -> BadCertificateUntrusted

    用户 X.509 证书在 ActivateSession 阶段由 CombinedUserManager 校验
    （direct 模式：精确 DER 白名单 + 有效期检查），与上面的 Application
    Certificate 校验是两条完全独立的链路。

    权限：MockerRoleRuleset 让 Anonymous 身份（UserRole.Anonymous）也能
    读取 mock 测试节点；身份认证与授权角色分开处理。
"""

import logging
from pathlib import Path
from typing import Any

from asyncua import Server, ua
from asyncua.crypto.permission_rules import (
    ADMIN_TYPES,
    USER_TYPES,
    PermissionRuleset,
    UserRole,
)
from asyncua.crypto.security_policies import SecurityPolicyNone
from asyncua.crypto.truststore import TrustStore
from asyncua.crypto.validator import CertificateValidator, CertificateValidatorOptions

from user_manager import CombinedUserManager
from user_token_tracking import UserTokenAwareInternalServer

logger = logging.getLogger(__name__)

# 明确绑定 asyncua 2.0.1（DiscoveryCleanServer 依赖其 _setup_server_nodes 行为）。
from asyncua import __version__ as _ASYNCUA_VERSION  # noqa: E402

if not _ASYNCUA_VERSION.startswith("2.0."):
    raise RuntimeError(
        f"hda_509 绑定 asyncua 2.0.x（当前 {_ASYNCUA_VERSION}）。"
        f"升级 asyncua 前请先核对 DiscoveryCleanServer._setup_server_nodes 的实现。"
    )


class DiscoveryCleanServer(Server):
    """
    asyncua 2.0.1 专用 Server 子类：Discovery 干净化。

    背景：asyncua 2.0.1 的 Server._setup_server_nodes() 对每个安全策略同时
    （1）发布 EndpointDescription 到 iserver.endpoints，和
    （2）把 SecurityPolicyFactory 加入 self._policies（BinaryServer 用它建立
    SecureChannel）。因此一旦把 NoSecurity 放进 _security_policy，None/None 就会
    同时出现在 GetEndpoints 返回值里，尽管我们会在 ActivateSession 阶段拒绝
    None Session——EndpointDescription 语义不够干净。

    本子类在复用原有 _setup_server_nodes() 之后：
      保留 self._policies 中的 SecurityPolicyNone factory（unsecured
      Discovery 通道仍可建立），
      但从 iserver.endpoints 移除 SecurityMode == None 的 EndpointDescription，
      使 GetEndpoints 只返回真正支持 Session 的 secure endpoints。

    不修改 asyncua site-packages 源码。
    """

    async def _setup_server_nodes(self) -> None:
        await super()._setup_server_nodes()
        # 只移除 None/None 的 EndpointDescription，不动 _policies。
        self.iserver.endpoints[:] = [
            e for e in self.iserver.endpoints
            if not (e.SecurityPolicyUri == SecurityPolicyNone.URI
                    and e.SecurityMode == ua.MessageSecurityMode.None_)
        ]
        if self.iserver.endpoints:
            logger.info("Discovery 端点已清理: GetEndpoints 只返回 %d 个 secure endpoints",
                        len(self.iserver.endpoints))
        else:
            logger.warning("Discovery 端点清理后无任何端点（可能只配置了 NoSecurity）")


class MockerRoleRuleset(PermissionRuleset):
    """Mock 场景的权限规则。

    asyncua 默认的 SimpleRoleRuleset 给 UserRole.Anonymous 空权限，会导致
    Anonymous 连测试节点都读不了。这里明确区分「身份认证」与「授权角色」：
    - Anonymous 身份仍是 UserRole.Anonymous（不是 User）
    - 但作为 mock，授予 Anonymous 与 User 相同的用户级服务权限
      （读 / 写 / 浏览 / 订阅等），仍不授予 Admin 级服务
      （改地址空间 / RegisterServer 等）。
    """

    def __init__(self) -> None:
        admin_ids = set(map(ua.NodeId, ADMIN_TYPES))
        user_ids = set(map(ua.NodeId, USER_TYPES))
        self._permission_dict = {
            UserRole.Admin: admin_ids | user_ids,
            UserRole.User: user_ids,
            UserRole.Anonymous: user_ids,
        }

    def check_validity(self, user, action_type_id, body):
        return action_type_id in self._permission_dict[user.role]

# 策略名 -> ua.SecurityPolicyType。NoSecurity 仅当组态显式开启时才加入。
POLICY_NAME_TO_TYPE = {
    "NoSecurity": ua.SecurityPolicyType.NoSecurity,
    "Basic256Sha256_Sign": ua.SecurityPolicyType.Basic256Sha256_Sign,
    "Basic256Sha256_SignAndEncrypt": ua.SecurityPolicyType.Basic256Sha256_SignAndEncrypt,
    "Aes128Sha256RsaOaep_Sign": ua.SecurityPolicyType.Aes128Sha256RsaOaep_Sign,
    "Aes128Sha256RsaOaep_SignAndEncrypt": ua.SecurityPolicyType.Aes128Sha256RsaOaep_SignAndEncrypt,
    "Aes256Sha256RsaPss_Sign": ua.SecurityPolicyType.Aes256Sha256RsaPss_Sign,
    "Aes256Sha256RsaPss_SignAndEncrypt": ua.SecurityPolicyType.Aes256Sha256RsaPss_SignAndEncrypt,
}


def _policy_names(cfg: dict[str, Any]) -> list[str]:
    """从组态读取策略名列表（新版 security.policies 或旧版 security_policy）。"""
    security = cfg.get("security")
    if isinstance(security, dict) and isinstance(security.get("policies"), list):
        names = [str(p) for p in security["policies"]]
    elif isinstance(cfg.get("security_policy"), list):
        names = [str(p) for p in cfg["security_policy"]]
    else:
        names = ["Basic256Sha256_SignAndEncrypt"]
    # discovery: 发布一个 None/None 的 discovery-only 端点（Session 仍被拒绝）。
    if isinstance(security, dict) and security.get("discovery"):
        if "NoSecurity" not in names:
            names = ["NoSecurity"] + names
    return names


def _require_secured_session(cfg: dict[str, Any]) -> bool:
    """Session 是否必须建立在受保护通道上（默认 True）。"""
    security = cfg.get("security")
    if isinstance(security, dict) and "require_secured_session" in security:
        return bool(security["require_secured_session"])
    return True


def _load_trust(trust_dir: Path) -> TrustStore:
    """加载服务端信任存储（内含 Test CA 证书），用于校验客户端 Application Certificate。"""
    trust_store = TrustStore([trust_dir], [])
    # TrustStore.load() 读取目录下所有 der/pem 证书加入 OpenSSL X509Store。
    return trust_store


async def _setup_certificate_validator(server: Server, cfg: dict[str, Any]) -> None:
    """配置服务端对客户端 Application Certificate 的校验。"""
    security = cfg.get("security")
    if isinstance(security, dict) and security.get("trust_store"):
        trust_dir = Path(security["trust_store"])
    elif cfg.get("trust_store"):
        trust_dir = Path(cfg["trust_store"])
    else:
        logger.info("未配置 trust_store, 不校验客户端证书")
        return

    if not trust_dir.is_dir():
        raise ValueError(f"trust_store 目录不存在: {trust_dir}")

    trust_store = _load_trust(trust_dir)
    await trust_store.load()
    # TRUSTED_VALIDATION | PEER_CLIENT：检查时间范围 / URI / KeyUsage /
    # ExtendedKeyUsage / 是否受信 / 是否吊销，且角色为客户端。
    validator = CertificateValidator(
        CertificateValidatorOptions.TRUSTED_VALIDATION | CertificateValidatorOptions.PEER_CLIENT,
        trust_store,
    )
    server.set_certificate_validator(validator)
    logger.info("客户端证书校验已启用, 受信目录: %s", trust_dir)


def _cert_and_key(cfg: dict[str, Any]) -> tuple[str | None, str | None]:
    """取服务端证书与私钥路径（新版 security.application_certificate 或旧版顶层键）。"""
    security = cfg.get("security")
    if isinstance(security, dict):
        app_cert = security.get("application_certificate")
        if isinstance(app_cert, dict) and app_cert.get("cert") and app_cert.get("private_key"):
            return app_cert["cert"], app_cert["private_key"]
    if cfg.get("cert") and cfg.get("private_key"):
        return cfg["cert"], cfg["private_key"]
    return None, None


def _user_auth_config(cfg: dict[str, Any]) -> dict[str, Any]:
    """返回 user_auth 配置（新版本地 user_auth 键），缺省为匿名可用。"""
    user_auth = cfg.get("user_auth")
    if isinstance(user_auth, dict):
        return user_auth
    return {"anonymous": True, "username": False, "x509": False}


def _x509_user_cert_paths(user_auth: dict[str, Any]) -> list[Path]:
    """x509_user_cert 可以是单个字符串路径或路径列表。"""
    value = user_auth.get("x509_user_cert")
    if not value:
        return []
    if isinstance(value, list):
        return [Path(str(p)) for p in value]
    return [Path(str(value))]


async def build_server(cfg: dict[str, Any]) -> Server:
    """
    根据组态构建一个配置完成的 asyncua Server（未启动，由调用方 start）。

    :param cfg: load_config() 返回的组态字典
    :return: asyncua.Server
    """
    server = DiscoveryCleanServer(iserver=UserTokenAwareInternalServer())

    # ---- 应用描述 ---------------------------------------------------------
    application = cfg.get("application")
    application_name = "ua_hda X.509 Compatibility Mocker"
    application_uri = "urn:freeopcua:python:server"
    if isinstance(application, dict):
        if application.get("name"):
            application_name = application["name"]
        if application.get("uri"):
            application_uri = application["uri"]
    server.name = application_name

    endpoint_path = cfg.get("endpoint_path", "/ua_mocker/")
    host = cfg["server"]
    port = int(cfg["port"])
    endpoint = f"opc.tcp://{host}:{port}{endpoint_path}"
    server.set_endpoint(endpoint)
    logger.info("OPC UA 端点: %s", endpoint)

    await server.init()
    # 注意：application_uri 必须用 asyncua 的正式 API 设置。直接给
    # server.application_uri 赋值不会生效——asyncua 内部使用的是
    # _application_uri，且没有 property setter，默认值会一直保留。
    # set_application_uri() 会同步更新 NamespaceArray[1]，但不会更新
    # ServerArray（init() 时写入的仍是默认值），这里一并修正，保证
    # EndpointDescription.Server.ApplicationUri / ServerArray /
    # NamespaceArray[1] 一致。
    await server.set_application_uri(application_uri)
    sa_node = server.get_node(ua.NodeId(ua.ObjectIds.Server_ServerArray))
    await sa_node.write_value([application_uri])
    logger.info("ApplicationUri: %s", application_uri)

    # ---- 安全策略与证书 ----------------------------------------------------
    policy_names = _policy_names(cfg)
    try:
        policies = [POLICY_NAME_TO_TYPE[name] for name in policy_names]
    except KeyError as e:
        raise ValueError(
            f"不支持的安全策略: {e}，可选: {list(POLICY_NAME_TO_TYPE.keys())}"
        ) from e
    server.set_security_policy(policies, permission_ruleset=MockerRoleRuleset())
    logger.info("安全策略: %s", policy_names)

    cert_path, key_path = _cert_and_key(cfg)
    if cert_path and key_path:
        await server.load_certificate(cert_path)
        await server.load_private_key(key_path)
        logger.info("服务端证书已加载: %s", cert_path)
    elif policies != [ua.SecurityPolicyType.NoSecurity]:
        logger.warning("未配置服务端证书/私钥, 加密端点将无法启用")

    # ---- 客户端 Application Certificate 校验 ------------------------------
    await _setup_certificate_validator(server, cfg)

    # ---- 用户身份认证 ------------------------------------------------------
    user_auth = _user_auth_config(cfg)
    tokens = []
    if user_auth.get("anonymous", True):
        tokens.append(ua.AnonymousIdentityToken)
    if user_auth.get("username", False):
        tokens.append(ua.UserNameIdentityToken)
    if user_auth.get("x509", False):
        tokens.append(ua.X509IdentityToken)
    server.set_identity_tokens(tokens)
    logger.info("UserIdentityTokens: %s", [t.__name__ for t in tokens])

    users = {
        u["username"]: u["password"]
        for u in user_auth.get("users", [])
        if isinstance(u, dict) and u.get("username")
    }
    x509_paths = _x509_user_cert_paths(user_auth)
    user_manager = CombinedUserManager(
        allow_anonymous=user_auth.get("anonymous", True),
        users=users,
        x509_user_cert_paths=x509_paths,
        x509_user_name=user_auth.get("x509_user_name", "x509_user"),
        require_secured_channel=_require_secured_session(cfg),
    )
    server.iserver.set_user_manager(user_manager)
    logger.info("用户认证: anonymous=%s username=%s x509=%s require_secured_session=%s",
                user_auth.get("anonymous", True),
                bool(users),
                bool(x509_paths),
                _require_secured_session(cfg))

    return server
