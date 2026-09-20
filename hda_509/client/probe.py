#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Probe client: enumerates and prints every endpoint the server actually
publishes (GetEndpoints), in a human readable form.

Output example for one endpoint:

    Endpoint #1
    URL:              opc.tcp://127.0.0.1:48620/ua_mocker/
    SecurityPolicy:   Basic256Sha256
    SecurityMode:     SignAndEncrypt
    Server Certificate:
      Subject:        ...
      Issuer:         ...
      Serial:         ...
      NotBefore:      ...
      NotAfter:       ...
      Application URI: ...
      DNS:            ['localhost']
      IP:             ['127.0.0.1']
      Public Key:     RSA(2048)
      Key Size:       2048
      SHA-256:        ...
    UserIdentityTokens:
      - Anonymous
      - UserName
      - Certificate

GetEndpoints is served over a short-lived secured channel (the normal server
does not publish None/None, so there is no unencrypted GetEndpoints).

    python client/probe.py
    python client/probe.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ \
        --server-cert test_material/certs/server_self_signed_cert.pem
"""

import asyncio
import sys
from datetime import datetime, timezone
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives.asymmetric import rsa

from conn import (
    DEFAULT_APP_CERT,
    DEFAULT_APP_KEY,
    add_common_args,
    fetch_endpoints,
    run_async,
    setup_client,
)


def _short_policy_uri(uri: str) -> str:
    """把 'http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256' 简化为 'Basic256Sha256'。"""
    if "#" in uri:
        return uri.rsplit("#", 1)[1]
    return uri


def _token_type_name(token_type) -> str:
    return {
        0: "Anonymous",
        1: "UserName",
        2: "Certificate",
        3: "IssuedToken",
    }.get(int(token_type), str(token_type))


def _fmt_dt(dt) -> str:
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.astimezone().strftime("%Y-%m-%d %H:%M:%S %Z")


def _public_key_summary(pub) -> str:
    if isinstance(pub, rsa.RSAPublicKey):
        return f"RSA({pub.key_size})"
    return type(pub).__name__


def _describe_server_cert(server_cert_der: bytes, indent: str = "  ") -> str:
    if not server_cert_der:
        return f"{indent}(no server certificate in endpoint)"
    cert = x509.load_der_x509_certificate(server_cert_der)
    lines = [
        f"{indent}Server Certificate:",
        f"{indent}  Subject:        {cert.subject.rfc4514_string()}",
        f"{indent}  Issuer:         {cert.issuer.rfc4514_string()}",
        f"{indent}  Serial:         {cert.serial_number:x}",
        f"{indent}  NotBefore:      {_fmt_dt(cert.not_valid_before_utc)}",
        f"{indent}  NotAfter:       {_fmt_dt(cert.not_valid_after_utc)}",
    ]
    try:
        san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
        lines.append(f"{indent}  Application URI: {san.get_values_for_type(x509.UniformResourceIdentifier)}")
        lines.append(f"{indent}  DNS:            {san.get_values_for_type(x509.DNSName)}")
        lines.append(f"{indent}  IP:             {[str(ip) for ip in san.get_values_for_type(x509.IPAddress)]}")
    except x509.ExtensionNotFound:
        lines.append(f"{indent}  Application URI: (none)")
    lines.append(f"{indent}  Public Key:     {_public_key_summary(cert.public_key())}")
    lines.append(f"{indent}  SHA-256:        {cert.fingerprint(cert.signature_hash_algorithm).hex()}")
    return "\n".join(lines)


def _describe_endpoint(index: int, ep) -> str:
    lines = [
        f"Endpoint #{index}",
        f"URL:              {ep.EndpointUrl}",
        f"SecurityPolicy:   {_short_policy_uri(ep.SecurityPolicyUri)}",
        f"SecurityMode:     {ep.SecurityMode.name}",
    ]
    lines.append(_describe_server_cert(ep.ServerCertificate))
    token_lines = []
    for token in ep.UserIdentityTokens:
        token_lines.append(f"  - {_token_type_name(token.TokenType)}")
    lines.append("UserIdentityTokens:")
    lines.extend(token_lines)
    return "\n".join(lines)


async def run(args) -> None:
    client = await setup_client(
        args.url,
        app_cert=Path(args.app_cert),
        app_key=Path(args.app_key),
        server_cert=Path(args.server_cert),
        policy_name=args.policy,
        mode_name=args.mode,
        application_uri=args.app_uri,
    )

    endpoints = await fetch_endpoints(client)
    print(f"GetEndpoints 返回 {len(endpoints)} 个 Endpoint：\n")
    for i, ep in enumerate(endpoints, start=1):
        print(_describe_endpoint(i, ep))
        print()


def main() -> int:
    import argparse

    parser = argparse.ArgumentParser(description="OPC UA endpoint probe")
    add_common_args(parser, with_identity=False)
    args = parser.parse_args()
    return run_async(run(args))


if __name__ == "__main__":
    sys.exit(main())
