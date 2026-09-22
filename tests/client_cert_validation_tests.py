#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 客户端应用证书校验模式测试（trusted / basic / none）。

矩阵主测试（matrix_port_tests.py）验证的是"一端口一场景"的端点/认证隔离；
本文件验证的是**客户端 Application Certificate 校验强度**这一条独立维度，
回答"任意客户端能不能连进来"。

背景（OPC UA 两层模型）：
  * 应用证书（Application Instance Certificate）—— 证明"哪个客户端应用"，
    只在 MessageSecurityMode != None 的通道上出现，由 CreateSession 阶段的
    CertificateValidator 校验。
  * 用户身份（UserIdentityToken）—— 证明"哪个用户"，由 ActivateSession 阶段
    校验，与本文件的模式无关。

服务端 client_cert_validation 三种模式：
  trusted = 时间/URI/KeyUsage/EKU + 必须受信（只认 trust_store 里的 CA/证书）
  basic   = 时间/URI/KeyUsage/EKU，但**不查信任目录**
  none    = **完全不校验**（不挂校验器）

用例：
  A. 回归       trusted 端口 + 未受信证书            -> 拒 BadCertificateUntrusted
  B. basic 放行  basic   端口 + 未受信证书(URI 匹配)  -> 成功
  B. basic 边界  basic   端口 + 过期证书              -> 拒 BadCertificateTimeInvalid
  B. basic 边界  basic   端口 + URI 不匹配            -> 拒 BadCertificateUriInvalid
  C. none 全放行 none    端口 + 未受信证书            -> 成功
  C. none 全放行 none    端口 + 过期证书              -> 成功
  C. none 全放行 none    端口 + URI 不匹配            -> 成功
  D. 用户层不受影响
                 basic   用户名端口 + test/test       -> 成功
                 basic   用户名端口 + 错误密码         -> 拒 BadUserAccessDenied
                 none    用户名端口 + test/test       -> 成功
  E. 对照       同一张未受信证书在 trusted/basic/none 三个端口上的结果

严格 PASS/FAIL；服务器离线 / 网络错误一律 FAIL。

运行（先启动矩阵端口）：

    python tools/matrix_ctl.py start
    python tests/client_cert_validation_tests.py
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

# 未受信但完全合规（时间有效、SAN URI = ...:untrusted）的自签客户端证书
UNTRUSTED_CERT = CERTS / "client_app_untrusted_cert.pem"
UNTRUSTED_KEY = CERTS / "client_app_untrusted_key.pem"
UNTRUSTED_URI = "urn:example.org:FreeOpcUa:untrusted"

# 已过期（SAN URI 与 APP_URI 一致）
EXPIRED_CERT = CERTS / "client_app_expired_cert.pem"
EXPIRED_KEY = CERTS / "client_app_expired_key.pem"

# SAN URI 与客户端 advertise 的 URI 不一致
WRONG_URI_CERT = CERTS / "client_app_wrong_uri_cert.pem"
WRONG_URI_KEY = CERTS / "client_app_wrong_uri_key.pem"

# 统一用现代策略的加密端点，确保"客户端必须出示应用证书"
EP_POLICY = "Basic256Sha256"
EP_MODE = "SignAndEncrypt"


def _port(validation: str, auth: str = "anon") -> dict:
    return pick_port(auth=auth, policy=EP_POLICY, mode=EP_MODE, validation=validation)


# ---------------------------------------------------------------------------
# 判定helper
# ---------------------------------------------------------------------------

def _expect_ok(case: str, a) -> None:
    """期望连接成功（应用到节点）。"""
    if a.ok:
        report(case, "PASS", a.error)
    elif a.network_error:
        report(case, "FAIL", f"服务器离线: {a.error}")
    else:
        report(case, "FAIL", f"应成功但被拒: stage={a.stage or '-'} err={a.error}")


def _expect_app_reject(case: str, a, expected: str) -> None:
    """期望在 CreateSession（应用证书校验）被拒，含指定 StatusCode。"""
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


def _expect_user_reject(case: str, a, expected: str) -> None:
    """期望在 ActivateSession（用户身份校验）被拒。"""
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


async def _connect(port: dict, *, app_cert: Path, app_key: Path, app_uri: str,
                   auth: str = "anon", username: str = "test",
                   password: str = "test") -> object:
    return await attempt(
        auth=auth,
        policy=port["policy"],
        mode=port["mode"],
        url=port["url"],
        node=port["read_node"],
        app_cert=app_cert,
        app_key=app_key,
        app_uri=app_uri,
        username=username,
        password=password,
    )


# ---------------------------------------------------------------------------
# A. 回归：trusted 端口必须仍然严格
# ---------------------------------------------------------------------------

async def case_a_trusted_strict(p_trusted: dict) -> None:
    print(f"A. 回归：trusted 端口仍严格  [{p_trusted['port']}]：\n")
    a = await _connect(p_trusted, app_cert=UNTRUSTED_CERT, app_key=UNTRUSTED_KEY,
                       app_uri=UNTRUSTED_URI)
    _expect_app_reject("A  trusted + 未受信证书 应被拒", a, "BadCertificateUntrusted")


# ---------------------------------------------------------------------------
# B. basic：不查信任，但保留 时间 / URI 校验
# ---------------------------------------------------------------------------

async def case_b_basic(p_basic: dict) -> None:
    print(f"\nB. basic：不查信任，仍查时间/URI  [{p_basic['port']}]：\n")

    a = await _connect(p_basic, app_cert=UNTRUSTED_CERT, app_key=UNTRUSTED_KEY,
                       app_uri=UNTRUSTED_URI)
    _expect_ok("B1 basic + 未受信证书(URI 匹配) 应放行", a)

    a = await _connect(p_basic, app_cert=EXPIRED_CERT, app_key=EXPIRED_KEY,
                       app_uri=APP_URI)
    _expect_app_reject("B2 basic + 过期证书 应被拒", a, "BadCertificateTimeInvalid")

    a = await _connect(p_basic, app_cert=WRONG_URI_CERT, app_key=WRONG_URI_KEY,
                       app_uri=APP_URI)
    _expect_app_reject("B3 basic + URI 不匹配 应被拒", a, "BadCertificateUriInvalid")


# ---------------------------------------------------------------------------
# C. none：完全不校验
# ---------------------------------------------------------------------------

async def case_c_none(p_none: dict) -> None:
    print(f"\nC. none：完全不校验  [{p_none['port']}]：\n")

    a = await _connect(p_none, app_cert=UNTRUSTED_CERT, app_key=UNTRUSTED_KEY,
                       app_uri=UNTRUSTED_URI)
    _expect_ok("C1 none + 未受信证书 应放行", a)

    a = await _connect(p_none, app_cert=EXPIRED_CERT, app_key=EXPIRED_KEY,
                       app_uri=APP_URI)
    _expect_ok("C2 none + 过期证书 应放行", a)

    a = await _connect(p_none, app_cert=WRONG_URI_CERT, app_key=WRONG_URI_KEY,
                       app_uri=APP_URI)
    _expect_ok("C3 none + URI 不匹配 应放行", a)


# ---------------------------------------------------------------------------
# D. 用户层（用户名密码）不受本模式影响
# ---------------------------------------------------------------------------

async def case_d_user_layer(p_basic_user: dict, p_none_user: dict) -> None:
    print(f"\nD. 用户层不受影响  "
          f"[basic={p_basic_user['port']}  none={p_none_user['port']}]：\n")

    # 用受信证书 + 正确口令 -> 应成功
    a = await _connect(p_basic_user, app_cert=APP_CERT, app_key=APP_KEY,
                       app_uri=APP_URI, auth="username")
    _expect_ok("D1 basic 用户名端口 + test/test 应成功", a)

    # 用未受信证书（basic 放行） + 正确口令 -> 仍应成功（证明应用层与用户层解耦）
    a = await _connect(p_basic_user, app_cert=UNTRUSTED_CERT, app_key=UNTRUSTED_KEY,
                       app_uri=UNTRUSTED_URI, auth="username")
    _expect_ok("D2 basic 用户名端口 + 未受信证书 + test/test 应成功", a)

    # 用受信证书 + 错误口令 -> 用户层拒绝
    a = await _connect(p_basic_user, app_cert=APP_CERT, app_key=APP_KEY,
                       app_uri=APP_URI, auth="username", password="wrong_pass")
    _expect_user_reject("D3 basic 用户名端口 + 错误密码 应被拒", a, "BadUserAccessDenied")

    # none 端口用户名认证仍可用
    a = await _connect(p_none_user, app_cert=UNTRUSTED_CERT, app_key=UNTRUSTED_KEY,
                       app_uri=UNTRUSTED_URI, auth="username")
    _expect_ok("D4 none 用户名端口 + test/test 应成功", a)


# ---------------------------------------------------------------------------

async def main() -> int:
    p_trusted = _port("trusted")
    p_basic = _port("basic")
    p_none = _port("none")
    p_basic_user = _port("basic", auth="username")
    p_none_user = _port("none", auth="username")

    print("UA Auth Lab 客户端应用证书校验模式测试\n")
    print(f"  端点          : {EP_POLICY}/{EP_MODE}（加密端点，客户端必须出示应用证书）")
    print(f"  trusted 端口  : {p_trusted['port']}")
    print(f"  basic   端口  : {p_basic['port']}   username={p_basic_user['port']}")
    print(f"  none    端口  : {p_none['port']}   username={p_none_user['port']}")
    print("=" * 78)

    ports = [p_trusted, p_basic, p_none, p_basic_user, p_none_user]
    offline = [p["port"] for p in ports if not server_online(p["url"])]
    if offline:
        print(f"\n[FAIL] 端口未监听: {offline}")
        print("请先运行: python tools/matrix_ctl.py start")
        report("端口预检", "FAIL", f"{offline} 未监听")
        return summarize("应用证书校验模式测试 ")

    await case_a_trusted_strict(p_trusted)
    await case_b_basic(p_basic)
    await case_c_none(p_none)
    await case_d_user_layer(p_basic_user, p_none_user)

    print("\nE. 对照：同一张未受信证书在三档模式下的结果\n")
    print(f"  {'端口':>6}  {'模式':<8}  期望        结果")
    for p in (p_trusted, p_basic, p_none):
        a = await _connect(p, app_cert=UNTRUSTED_CERT, app_key=UNTRUSTED_KEY,
                           app_uri=UNTRUSTED_URI)
        expect = "拒绝" if p["validation"] == "trusted" else "放行"
        got = "放行" if a.ok else "拒绝"
        flag = "OK " if expect == got else "!! "
        detail = a.error if a.ok else f"{a.stage or '-'}: {a.error}"
        print(f"  {p['port']:>6}  {p['validation']:<8}  {expect}        {got}   [{flag}] {detail}")

    print("=" * 78)
    return summarize("应用证书校验模式测试 ")


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
