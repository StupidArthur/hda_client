# -*- coding: utf-8 -*-
"""
UserManager 实现：组合 Anonymous / UserName / X.509 User 三种身份认证。

重要概念区分（与 asyncua 内部保持一致，不要混淆）：

* Application Certificate
     在 CreateSession 阶段由 certificate_validator 校验，属于
     SecureChannel / application authentication，与本文件无关。

* User X.509 Certificate
     在 ActivateSession 阶段通过 X509IdentityToken 提交，由本
     UserManager 校验（精确匹配注册的用户证书 DER），属于
     user authentication。

username 认证使用简单测试账号（仅用于 mock）。

asyncua 2.0.1 限制（见 user_token_tracking.py）：
    get_user() 的 certificate 参数在"Anonymous + 受保护通道"时是客户端
    Application 证书，在 X509IdentityToken 时才是用户证书。因此仅凭
    certificate 无法区分 Anonymous 与"未注册用户证书的 X509IdentityToken"。
    本类通过 iserver._last_user_token_kind（由 UserTokenAwareSession 设置）
    精确区分令牌类型。
"""

import logging
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import serialization
from asyncua.crypto.permission_rules import User, UserRole

logger = logging.getLogger(__name__)


def _pem_to_der(path: Path) -> bytes:
    """把 PEM 用户证书文件解析为 DER 字节（asyncua 在 get_user 中传的是 DER）。"""
    cert = x509.load_pem_x509_certificate(Path(path).read_bytes())
    return cert.public_bytes(serialization.Encoding.DER)


class CombinedUserManager:
    """
    组合式用户管理：
    - Anonymous：返回普通 User（可在配置中关闭）
    - Username：匹配配置中的用户名/密码
    - X.509 User：匹配注册的用户证书（精确 DER 匹配）
    """

    def __init__(
        self,
        *,
        allow_anonymous: bool = True,
        users: dict[str, str] | None = None,
        x509_user_cert_paths: list[Path] | None = None,
        x509_user_name: str = "x509_user",
    ) -> None:
        self._allow_anonymous = allow_anonymous
        self._users: dict[str, str] = users or {}
        self._x509_user_name = x509_user_name
        self._x509_certs: set[bytes] = set()
        for cert_path in x509_user_cert_paths or []:
            cert_der = _pem_to_der(cert_path)
            self._x509_certs.add(cert_der)
            logger.info("已注册 X.509 用户证书: %s", cert_path)

    def get_user(self, iserver, username=None, password=None, certificate=None):
        """asyncua UserManager 接口。返回 None 表示拒绝（BadUserAccessDenied）。"""
        token_kind = getattr(iserver, "_last_user_token_kind", "unknown")

        if token_kind == "x509":
            # 用户令牌是 X509IdentityToken：必须使用注册的用户证书。
            if certificate is not None and bytes(certificate) in self._x509_certs:
                return User(role=UserRole.User, name=self._x509_user_name)
            logger.warning("X.509 用户证书未注册, 用户认证被拒绝")
            return None

        if token_kind == "username":
            expected = self._users.get(username)
            if expected is not None and expected == password:
                return User(role=UserRole.User, name=username)
            logger.warning("用户名/密码无效: username=%r", username)
            return None

        if token_kind == "anon":
            if self._allow_anonymous:
                return User(role=UserRole.User, name="anonymous")
            logger.warning("Anonymous 未开放, 用户认证被拒绝")
            return None

        logger.warning("未知用户令牌类型, 用户认证被拒绝")
        return None
