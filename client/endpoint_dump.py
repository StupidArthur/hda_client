# -*- coding: utf-8 -*-
"""
Shared helpers to render ua.EndpointDescription / server certificates in a
human readable form. Used by probe.py (secured probe) and
discovery_probe.py (unsecured discovery probe).
"""

from datetime import datetime, timezone

from cryptography import x509
from cryptography.hazmat.primitives.asymmetric import rsa


def short_policy_uri(uri: str) -> str:
    """把 'http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256' 简化为 'Basic256Sha256'。"""
    return uri.rsplit("#", 1)[1] if "#" in uri else uri


def token_type_name(token_type) -> str:
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


def describe_server_cert(server_cert_der: bytes, indent: str = "  ") -> str:
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


def describe_endpoint(index: int, ep) -> str:
    lines = [
        f"Endpoint #{index}",
        f"URL:              {ep.EndpointUrl}",
        f"SecurityPolicy:   {short_policy_uri(ep.SecurityPolicyUri)}",
        f"SecurityMode:     {ep.SecurityMode.name}",
        f"SecurityLevel:    {ep.SecurityLevel}",
    ]
    lines.append(describe_server_cert(ep.ServerCertificate))
    token_lines = [f"  - {token_type_name(token.TokenType)}" for token in ep.UserIdentityTokens]
    lines.append("UserIdentityTokens:")
    lines.extend(token_lines)
    return "\n".join(lines)


def describe_application(ep) -> str:
    """打印 Endpoint 里的 ApplicationDescription（ApplicationUri 等）。"""
    server = ep.Server
    lines = [
        "ApplicationDescription:",
        f"  ApplicationUri:  {server.ApplicationUri}",
        f"  ApplicationName: {server.ApplicationName.Text if server.ApplicationName else ''}",
        f"  ApplicationType: {server.ApplicationType.name}",
        f"  ProductUri:      {server.ProductUri}",
        f"  DiscoveryUrls:   {list(server.DiscoveryUrls)}",
    ]
    return "\n".join(lines)


def print_endpoints(endpoints: list, *, include_application: bool = True) -> None:
    print(f"GetEndpoints 返回 {len(endpoints)} 个 Endpoint：\n")
    for i, ep in enumerate(endpoints, start=1):
        print(describe_endpoint(i, ep))
        if include_application:
            print(describe_application(ep))
        print()
