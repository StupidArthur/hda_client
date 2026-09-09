# -*- coding: utf-8 -*-
"""
生成 X.509 测试证书：CA、服务端证书、客户端证书。
输出到 certs/ 目录：
  ca_cert.pem, ca_key.pem
  server_cert.pem, server_key.pem
  client_cert.pem, client_key.pem
  trust/ca_cert.pem        (服务端受信目录：信任由该 CA 签发的客户端证书)
"""

import datetime
import ipaddress
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

BASE = Path(__file__).resolve().parent / "certs"
TRUST = BASE / "trust"
BASE.mkdir(parents=True, exist_ok=True)
TRUST.mkdir(parents=True, exist_ok=True)

DAYS = 365 * 5
NOW = datetime.datetime.now(datetime.timezone.utc)


def gen_key() -> rsa.RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


def dump_key(path: Path, key: rsa.RSAPrivateKey) -> None:
    path.write_bytes(
        key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.TraditionalOpenSSL,
            serialization.NoEncryption(),
        )
    )


def dump_cert(path: Path, cert: x509.Certificate) -> None:
    path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))


def build_cert(
    subject: x509.Name,
    public_key,
    issuer_name: x509.Name,
    issuer_key: rsa.RSAPrivateKey,
    is_ca: bool,
    san: list[x509.GeneralName],
    eku: list[x509.ObjectIdentifier] | None,
) -> x509.Certificate:
    builder = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(issuer_name)
        .public_key(public_key)
        .serial_number(x509.random_serial_number())
        .not_valid_before(NOW)
        .not_valid_after(NOW + datetime.timedelta(days=DAYS))
        .add_extension(x509.BasicConstraints(ca=is_ca, path_length=None), critical=True)
        .add_extension(
            x509.KeyUsage(
                digital_signature=True,
                content_commitment=True,
                key_encipherment=True,
                data_encipherment=True,
                key_agreement=False,
                key_cert_sign=is_ca,
                crl_sign=is_ca,
                encipher_only=False,
                decipher_only=False,
            ),
            critical=True,
        )
    )
    if san:
        builder = builder.add_extension(x509.SubjectAlternativeName(san), critical=False)
    if eku is not None:
        builder = builder.add_extension(x509.ExtendedKeyUsage(eku), critical=False)
    return builder.sign(issuer_key, hashes.SHA256())


def main() -> None:
    # 1. CA
    ca_key = gen_key()
    ca_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "ua_hda Test CA")])
    ca_cert = build_cert(ca_name, ca_key.public_key(), ca_name, ca_key, True, [], None)
    dump_key(BASE / "ca_key.pem", ca_key)
    dump_cert(BASE / "ca_cert.pem", ca_cert)
    dump_cert(TRUST / "ca_cert.pem", ca_cert)
    print("CA 生成:", BASE / "ca_cert.pem")

    # 2. 服务端证书
    server_key = gen_key()
    server_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "ua_hda server")])
    server_cert = build_cert(
        server_name,
        server_key.public_key(),
        ca_name,
        ca_key,
        False,
        [
            x509.DNSName("localhost"),
            x509.IPAddress(ipaddress.ip_address("127.0.0.1")),
            x509.IPAddress(ipaddress.ip_address("0.0.0.0")),
            x509.UniformResourceIdentifier("urn:freeopcua:python:server"),
        ],
        [ExtendedKeyUsageOID.SERVER_AUTH],
    )
    dump_key(BASE / "server_key.pem", server_key)
    dump_cert(BASE / "server_cert.pem", server_cert)
    print("服务端证书生成:", BASE / "server_cert.pem")

    # 3. 客户端证书
    client_key = gen_key()
    client_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "ua_hda client")])
    client_cert = build_cert(
        client_name,
        client_key.public_key(),
        ca_name,
        ca_key,
        False,
        [x509.UniformResourceIdentifier("urn:example.org:FreeOpcUa:opcua-asyncio")],
        [ExtendedKeyUsageOID.CLIENT_AUTH],
    )
    dump_key(BASE / "client_key.pem", client_key)
    dump_cert(BASE / "client_cert.pem", client_cert)
    print("客户端证书生成:", BASE / "client_cert.pem")


if __name__ == "__main__":
    main()
