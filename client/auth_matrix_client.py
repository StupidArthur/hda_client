#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab - authentication matrix client.

遍历「用户认证方式 × SecurityPolicy × MessageSecurityMode」组合，逐个建立
分阶段连接并读取测试节点，输出 PASS/FAIL 矩阵。

默认矩阵（all_auth 场景，全部应 PASS）:

    auth   : anon / username / x509
    policy : Basic256Sha256 / Aes128Sha256RsaOaep / Aes256Sha256RsaPss
    mode   : Sign / SignAndEncrypt
    => 3 x 3 x 2 = 18 组合

用法:

    python client/auth_matrix_client.py
    python client/auth_matrix_client.py --url opc.tcp://127.0.0.1:48630/ua_auth/
    python client/auth_matrix_client.py --auth anon
    python client/auth_matrix_client.py --policy Basic256Sha256 --mode SignAndEncrypt
    # None/None（无保护通道）默认不在矩阵内，可手动验证：
    python client/auth_matrix_client.py --policy None --mode None --auth anon

退出码：全部 PASS 返回 0，否则返回 1。
"""

import argparse
import asyncio
import sys
from pathlib import Path

from conn import (
    DEFAULT_APP_CERT,
    DEFAULT_APP_KEY,
    DEFAULT_PASSWORD,
    DEFAULT_SERVER_CERT,
    DEFAULT_USER_CERT,
    DEFAULT_USER_KEY,
    DEFAULT_USERNAME,
    StageFailed,
    read_node,
    setup_client,
    stage_connect,
)

DEFAULT_URL = "opc.tcp://127.0.0.1:48630/ua_auth/"
DEFAULT_NODE = "ns=1;s=int32_ch_1"

ALL_AUTHS = ["anon", "username", "x509"]
ALL_POLICIES = ["Basic256Sha256", "Aes128Sha256RsaOaep", "Aes256Sha256RsaPss"]
ALL_MODES = ["Sign", "SignAndEncrypt"]


def _app_uri_from_cert(cert_path: str) -> str | None:
    """从客户端 Application 证书 SAN URI 读取 ApplicationUri。"""
    from cryptography import x509

    cert = x509.load_pem_x509_certificate(Path(cert_path).read_bytes())
    san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    uris = san.get_values_for_type(x509.UniformResourceIdentifier)
    return uris[0] if uris else None


async def run_one(args, auth: str, policy: str, mode: str) -> tuple[bool, str]:
    """跑一个组合，返回 (ok, detail)。"""
    application_uri = args.app_uri or _app_uri_from_cert(args.app_cert)
    client = await setup_client(
        args.url,
        app_cert=Path(args.app_cert),
        app_key=Path(args.app_key),
        server_cert=Path(args.server_cert),
        policy_name=policy,
        mode_name=mode,
        application_uri=application_uri,
    )
    try:
        client = await stage_connect(
            client,
            auth=auth,
            username=args.username,
            password=args.password,
            user_cert=Path(args.user_cert),
            user_key=Path(args.user_key),
            policy_name=policy,
            mode_name=mode,
            print_steps=False,
        )
        val = await read_node(client, args.node)
        return True, f"read {args.node}={val!r}"
    except StageFailed as e:
        return False, f"{e.stage}: {type(e.cause).__name__}: {e.cause}"
    finally:
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass


async def run(args) -> int:
    auths = args.auth.split(",") if args.auth else ALL_AUTHS
    policies = args.policy.split(",") if args.policy else ALL_POLICIES
    modes = args.mode.split(",") if args.mode else ALL_MODES

    print(f"URL   : {args.url}")
    print(f"矩阵  : auth={auths} x policy={policies} x mode={modes}")
    print("-" * 78)

    results = []
    for policy in policies:
        for mode in modes:
            for auth in auths:
                ok, detail = await run_one(args, auth, policy, mode)
                results.append(ok)
                flag = "PASS" if ok else "FAIL"
                print(f"  [{flag}] {auth:<9} {policy:<22} {mode:<14} {detail}")

    total = len(results)
    passed = sum(1 for ok in results if ok)
    print("-" * 78)
    print(f"结果: {passed}/{total} PASS")
    return 0 if passed == total else 1


def main() -> int:
    parser = argparse.ArgumentParser(description="UA Auth Lab authentication matrix client")
    parser.add_argument("--url", default=DEFAULT_URL, help=f"OPC UA 端点 (默认 {DEFAULT_URL})")
    parser.add_argument("--app-cert", default=str(DEFAULT_APP_CERT), help="客户端 Application 证书")
    parser.add_argument("--app-key", default=str(DEFAULT_APP_KEY), help="客户端 Application 私钥")
    parser.add_argument("--app-uri", default=None, help="客户端 ApplicationUri（默认取自证书 SAN URI）")
    parser.add_argument("--server-cert", default=str(DEFAULT_SERVER_CERT), help="服务端证书（用于 pin）")
    parser.add_argument("--auth", default=None, help="逗号分隔: anon,username,x509（默认全部）")
    parser.add_argument("--policy", default=None, help="逗号分隔: Basic256Sha256,Aes128Sha256RsaOaep,Aes256Sha256RsaPss（默认全部）")
    parser.add_argument("--mode", default=None, help="逗号分隔: Sign,SignAndEncrypt（默认全部）")
    parser.add_argument("--node", default=DEFAULT_NODE, help="要读取的测试节点")
    parser.add_argument("--user-cert", default=str(DEFAULT_USER_CERT), help="用户 X.509 证书")
    parser.add_argument("--user-key", default=str(DEFAULT_USER_KEY), help="用户 X.509 私钥")
    parser.add_argument("--username", default=DEFAULT_USERNAME)
    parser.add_argument("--password", default=DEFAULT_PASSWORD)
    args = parser.parse_args()
    return asyncio.run(run(args))


if __name__ == "__main__":
    sys.exit(main())
