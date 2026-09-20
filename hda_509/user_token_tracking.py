# -*- coding: utf-8 -*-
"""
asyncua 2.0.1 限制的解决：跟踪 ActivateSession 使用的 UserIdentityToken 类型。

背景
----
asyncua 2.0.1 的 UserManager.get_user() 签名是：

    get_user(iserver, username=None, password=None, certificate=None)

其中 certificate 参数对"受保护通道上的 Anonymous 令牌"会传入客户端
Application 证书（即 OpenSecureChannel 里携带的证书），对 X509IdentityToken
会传入用户令牌里的证书。也就是说，仅凭 certificate 无法区分：

    Anonymous 令牌 + 受保护通道（certificate = 客户端应用证书）

和

    X509IdentityToken + 未注册用户证书（certificate = 该未注册证书）

这两者 username/password 均为 None。为了精确区分，我们在 create_session
创建的自定义 InternalSession 中，于 ActivateSession 开始时记录令牌类型
到 iserver._last_user_token_kind（该窗口内无 await，不会与其他会话竞争），
UserManager 据此做判断。

实现方式完全基于 asyncua 对外提供的扩展点：
    Server(iserver=CustomInternalServer) 且 InternalServer.create_session()
    返回自定义 InternalSession —— 无需修改 asyncua 源码。
"""

import logging

from asyncua import ua
from asyncua.server.internal_server import InternalServer
from asyncua.server.internal_session import InternalSession

logger = logging.getLogger(__name__)


def _token_kind(id_token) -> str:
    """归一化 UserIdentityToken 类型：anon / username / x509 / unknown。"""
    if isinstance(id_token, ua.ExtensionObject) and id_token.TypeId == ua.NodeId(ua.ObjectIds.Null):
        # OPC UA Part4 5.6.3：空/Null 用户令牌一律视为 Anonymous。
        return "anon"
    if isinstance(id_token, ua.AnonymousIdentityToken):
        return "anon"
    if isinstance(id_token, ua.UserNameIdentityToken):
        return "username"
    if isinstance(id_token, ua.X509IdentityToken):
        return "x509"
    return "unknown"


class UserTokenAwareSession(InternalSession):
    """在 ActivateSession 开始时记录本次使用的用户令牌类型与通道是否受保护。"""

    def activate_session(self, params, peer_certificate):
        kind = _token_kind(params.UserIdentityToken)
        # peer_certificate 参数是建立通道时对方（客户端）的 Application 证书：
        # 受保护通道非空，None/unsecured 通道为空 bytes。必须在 X509IdentityToken
        # 覆盖该变量之前记录，这样无论令牌类型如何都能判断底层通道是否受保护。
        channel_secured = bool(peer_certificate)
        # 同步窗口：设置后随即调用父类 activate_session()，期间无 await，
        # get_user() 同步执行，因此不存在跨会话竞争。
        self.iserver._last_user_token_kind = kind  # type: ignore[attr-defined]
        self.iserver._last_channel_secured = channel_secured  # type: ignore[attr-defined]
        return super().activate_session(params, peer_certificate)


class UserTokenAwareInternalServer(InternalServer):
    """返回 UserTokenAwareSession 的 InternalServer，供 Server(iserver=...) 注入。"""

    def create_session(self, name, user=None, external=False):
        return UserTokenAwareSession(
            self, self.aspace, self.subscription_service, name, user=user, external=external
        )
