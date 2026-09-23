#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Reference / Probe Client.

Verifies one complete connection path against the mocker:

    [1] GetEndpoints          OK
    [2] Endpoint selection    OK
    [3] OpenSecureChannel     OK
    [4] CreateSession         OK
    [5] ActivateSession       OK
    [6] Read test node        OK

Identity selection via --auth:

    anon       Application Certificate + Anonymous User
    username   Application Certificate + UserName User (test/test)
    x509       Application Certificate + X.509 User Certificate
               (the X.509 user certificate is a DIFFERENT pair from the
                application certificate; see README)

Examples (macOS / Windows identical):

    python client/reference_client.py
    python client/reference_client.py --auth username
    python client/reference_client.py --auth x509
    python client/reference_client.py --auth x509 \
        --policy Aes128Sha256RsaOaep --mode SignAndEncrypt
    python client/reference_client.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ \
        --server-cert test_material/certs/server_self_signed_cert.pem
"""

import asyncio
import sys
from pathlib import Path

from conn import (
    DEFAULT_APP_CERT,
    DEFAULT_APP_KEY,
    DEFAULT_PASSWORD,
    DEFAULT_USER_CERT,
    DEFAULT_USER_KEY,
    DEFAULT_USERNAME,
    StageFailed,
    add_common_args,
    read_node,
    run_async,
    setup_client,
    stage_connect,
)


def _app_uri_from_cert(cert_path: str) -> str | None:
    """从客户端 Application 证书 SAN URI 里读取 ApplicationUri。"""
    from cryptography import x509
    from cryptography.x509.oid import NameOID

    cert = x509.load_pem_x509_certificate(Path(cert_path).read_bytes())
    cn = cert.subject.get_attributes_for_oid(NameOID.COMMON_NAME)
    san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    uris = san.get_values_for_type(x509.UniformResourceIdentifier)
    print(f"客户端 Application 证书: CN={cn[0].value if cn else '?'} SAN-URI={uris}")
    return uris[0] if uris else None


async def run(args) -> None:
    application_uri = args.app_uri or _app_uri_from_cert(args.app_cert)

    client = await setup_client(
        args.url,
        app_cert=Path(args.app_cert),
        app_key=Path(args.app_key),
        server_cert=Path(args.server_cert),
        policy_name=args.policy,
        mode_name=args.mode,
        application_uri=application_uri,
    )

    try:
        client = await stage_connect(
            client,
            auth=args.auth,
            username=args.username,
            password=args.password,
            user_cert=Path(args.user_cert),
            user_key=Path(args.user_key),
            policy_name=args.policy,
            mode_name=args.mode,
        )
        # [6] Read test node
        val = await read_node(client, args.node)
        print(f"  [6] {'Read test node':<18} OK {args.node} = {val!r}")
        print("\n参考客户端连接与读取成功。")
    except StageFailed as e:
        print(f"\n阶段失败: {e.stage}")
        print(f"原因: {type(e.cause).__name__}: {e.cause}")
        sys.exit(1)
    finally:
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass


def main() -> int:
    import argparse

    parser = argparse.ArgumentParser(description="OPC UA X.509 compatibility reference client")
    add_common_args(parser)
    args = parser.parse_args()
    return run_async(run(args))


if __name__ == "__main__":
    sys.exit(main())
