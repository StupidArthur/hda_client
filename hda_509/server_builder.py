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

    Normal Server 默认不开放 None/None。若需要可在组态
    security.no_security: true 打开。

    客户端 Application Certificate 在 CreateSession 阶段由
    CertificateValidator 校验（TRUSTED_VALIDATION | PEER_CLIENT）：
      TIME_RANGE -> BadCertificateTimeInvalid
      URI        -> BadCertificateUriInvalid
      KEY_USAGE / EXT_KEY_USAGE -> BadCertificateUseNotAllowed
      TRUSTED    -> BadCertificateUntrusted

    用户 X.509 证书在 ActivateSession 阶段由 CombinedUserManager 校验，
    与上面的 Application Certificate 校验是两条完全独立的链路。
"""

import logging
from pathlib import Path
from typing import Any

from asyncua import Server, ua
from asyncua.crypto.truststore import TrustStore
from asyncua.crypto.validator import CertificateValidator, CertificateValidatorOptions

from user_manager import CombinedUserManager
from user_token_tracking import UserTokenAwareInternalServer

logger = logging.getLogger(__name__)

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
    if isinstance(security, dict) and security.get("no_security"):
        if "NoSecurity" not in names:
            names = ["NoSecurity"] + names
    return names


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


async def build_server(cfg: dict[str, Any]) -> Server:
    """
    根据组态构建一个配置完成的 asyncua Server（未启动，由调用方 start）。

    :param cfg: load_config() 返回的组态字典
    :return: asyncua.Server
    """
    server = Server(iserver=UserTokenAwareInternalServer())

    # ---- 应用描述 ---------------------------------------------------------
    application = cfg.get("application")
    if isinstance(application, dict):
        if application.get("name"):
            server.name = application["name"]
        if application.get("uri"):
            server.application_uri = application["uri"]
    else:
        server.name = "ua_hda X.509 Compatibility Mocker"
        server.application_uri = "urn:freeopcua:python:server"

    endpoint_path = cfg.get("endpoint_path", "/ua_mocker/")
    host = cfg["server"]
    port = int(cfg["port"])
    endpoint = f"opc.tcp://{host}:{port}{endpoint_path}"
    server.set_endpoint(endpoint)
    logger.info("OPC UA 端点: %s", endpoint)

    await server.init()

    # ---- 安全策略与证书 ----------------------------------------------------
    policy_names = _policy_names(cfg)
    try:
        policies = [POLICY_NAME_TO_TYPE[name] for name in policy_names]
    except KeyError as e:
        raise ValueError(
            f"不支持的安全策略: {e}，可选: {list(POLICY_NAME_TO_TYPE.keys())}"
        ) from e
    server.set_security_policy(policies)
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
    x509_paths: list[Path] = []
    if user_auth.get("x509_user_cert"):
        x509_paths = [Path(user_auth["x509_user_cert"])]
    user_manager = CombinedUserManager(
        allow_anonymous=user_auth.get("anonymous", True),
        users=users,
        x509_user_cert_paths=x509_paths,
        x509_user_name=user_auth.get("x509_user_name", "x509_user"),
    )
    server.iserver.set_user_manager(user_manager)
    logger.info("用户认证: anonymous=%s username=%s x509=%s",
                user_auth.get("anonymous", True),
                bool(users),
                bool(x509_paths))

    return server
