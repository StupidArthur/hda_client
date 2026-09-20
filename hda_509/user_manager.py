# -*- coding: utf-8 -*-
"""
UserManager 实现：组合 Anonymous / UserName / X.509 User 三种身份认证。

重要概念区分（与 asyncua 内部保持一致，不要混淆）：

* Application Certificate
     在 CreateSession 阶段由 certificate_validator 校验，属于
     SecureChannel / application authentication，与本文件无关。

* User X.509 Certificate
     在 ActivateSession 阶段通过 X509IdentityToken 提交，由本
     UserManager 校验，属于 user authentication。

X.509 User 校验语义（当前为 "direct" 模式，不是完整用户 PKI）：
    user_auth.x509_validation: direct
      - 精确 DER 白名单匹配（registered user certificates）
      - 加上有效期（expiration）检查
    本轮不实现：User Certificate CA-chain trust / Intermediate CA / CRL /
    CertificateGroup / TrustList。

身份认证与授权角色分开：
    - Anonymous 身份使用 UserRole.Anonymous（不是 User）
    - UserName / X.509 User 使用 UserRole.User
    - mock 场景若需让 Anonymous 读取测试节点，通过权限规则处理，
      而不是把 Anonymous 偷偷当成 User。

require_secured_channel：
    当组态 security.require_secured_session 为 true（默认）时，任何在
    unsecured (None) 通道上发起的 Session 激活都会被拒绝。配合
    security.discovery 发布的 None 端点，实现"仅 Discovery 可用、Session
    必须安全"的标准行为（详见 README / server_builder.py）。
    底层通道是否受保护由 user_token_tracking.UserTokenAwareSession 在
    peer_certificate 被 X509 用户令牌覆盖之前记录（_last_channel_secured）。

asyncua 2.0.1 限制（见 user_token_tracking.py）：
    get_user() 的 certificate 参数在"Anonymous + 受保护通道"时是客户端
    Application 证书，在 X509IdentityToken 时才是用户证书。因此仅凭
    certificate 无法区分 Anonymous 与"未注册用户证书的 X509IdentityToken"。
    本类通过 iserver._last_user_token_kind 精确区分令牌类型。
"""

import logging
from datetime import datetime, timezone
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import serialization
from asyncua.crypto.permission_rules import User, UserRole

logger = logging.getLogger(__name__)


def load_user_cert(path: Path) -> tuple[bytes, x509.Certificate]:
    """读取用户证书 PEM，返回 (DER, parsed certificate)。"""
    cert = x509.load_pem_x509_certificate(Path(path).read_bytes())
    return cert.public_bytes(serialization.Encoding.DER), cert


class CombinedUserManager:
    """
    组合式用户管理：
    - Anonymous：UserRole.Anonymous（可在配置中关闭）
    - Username：匹配配置中的用户名/密码
    - X.509 User：direct 模式 —— 注册用户证书的精确 DER 匹配 + 有效期检查
    """

    def __init__(
        self,
        *,
        allow_anonymous: bool = True,
        users: dict[str, str] | None = None,
        x509_user_cert_paths: list[Path] | None = None,
        x509_user_name: str = "x509_user",
        require_secured_channel: bool = True,
    ) -> None:
        self._allow_anonymous = allow_anonymous
        self._users: dict[str, str] = users or {}
        self._x509_user_name = x509_user_name
        self._require_secured_channel = require_secured_channel
        # (DER, parsed certificate)；DER 用于白名单匹配，parsed 用于有效期检查
        self._x509_entries: list[tuple[bytes, x509.Certificate]] = []
        for cert_path in x509_user_cert_paths or []:
            der, cert = load_user_cert(cert_path)
            self._x509_entries.append((der, cert))
            logger.info("已注册 X.509 用户证书: %s", cert_path)

    def get_user(self, iserver, username=None, password=None, certificate=None):
        """asyncua UserManager 接口。返回 None 表示拒绝（BadUserAccessDenied）。"""
        if self._require_secured_channel and not getattr(iserver, "_last_channel_secured", True):
            # 底层通道是 unsecured (None)：仅允许 Discovery，不允许 Session。
            logger.warning("拒绝在无保护通道上激活 Session（仅开放 Discovery）")
            return None

        token_kind = getattr(iserver, "_last_user_token_kind", "unknown")

        if token_kind == "x509":
            return self._authorize_x509_user(certificate)

        if token_kind == "username":
            expected = self._users.get(username)
            if expected is not None and expected == password:
                return User(role=UserRole.User, name=username)
            logger.warning("用户名/密码无效: username=%r", username)
            return None

        if token_kind == "anon":
            if self._allow_anonymous:
                # Anonymous 身份就是 Anonymous 角色；授权由权限规则决定。
                return User(role=UserRole.Anonymous, name="anonymous")
            logger.warning("Anonymous 未开放, 用户认证被拒绝")
            return None

        logger.warning("未知用户令牌类型, 用户认证被拒绝")
        return None

    def _authorize_x509_user(self, certificate):
        """direct 模式：注册用户证书精确 DER 匹配 + 有效期检查。"""
        if not certificate:
            return None
        now = datetime.now(timezone.utc)
        for der, cert in self._x509_entries:
            if bytes(certificate) == der:
                if cert.not_valid_before_utc > now or cert.not_valid_after_utc < now:
                    logger.warning(
                        "X.509 用户证书不在有效期内 (%s .. %s), 拒绝",
                        cert.not_valid_before_utc, cert.not_valid_after_utc,
                    )
                    return None
                return User(role=UserRole.User, name=self._x509_user_name)
        logger.warning("X.509 用户证书未注册, 用户认证被拒绝")
        return None
