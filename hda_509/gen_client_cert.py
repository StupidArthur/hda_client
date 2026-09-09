# -*- coding: utf-8 -*-
"""
模拟"真实第三方客户端"接入：
客户端自己生成密钥对 + CSR，提交给 ua_hda 的 CA 签发证书。
私钥由客户端自持（不落服务端 trust 目录），证书可被服务端 trust 中的 CA 验证。

输出: certs/client2_cert.pem / certs/client2_key.pem
"""

import datetime
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

BASE = Path(__file__).resolve().parent / "certs"
CA_CERT = BASE / "ca_cert.pem"
CA_KEY = BASE / "ca_key.pem"
DAYS = 365 * 2
NOW = datetime.datetime.now(datetime.timezone.utc)


def main() -> None:
    ca_cert = x509.load_pem_x509_certificate(CA_CERT.read_bytes())
    ca_key = serialization.load_pem_private_key(CA_KEY.read_bytes(), password=None)

    client_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    csr = (
        x509.CertificateSigningRequestBuilder()
        .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "ua_hda demo client")]))
        .sign(client_key, hashes.SHA256())
    )

    client_cert = (
        x509.CertificateBuilder()
        .subject_name(csr.subject)
        .issuer_name(ca_cert.subject)
        .public_key(csr.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(NOW)
        .not_valid_after(NOW + datetime.timedelta(days=DAYS))
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(
            x509.SubjectAlternativeName(
                [x509.UniformResourceIdentifier("urn:example.org:FreeOpcUa:opcua-asyncio")]
            ),
            critical=False,
        )
        .add_extension(
            x509.ExtendedKeyUsage([ExtendedKeyUsageOID.CLIENT_AUTH]),
            critical=False,
        )
        .add_extension(
            x509.KeyUsage(
                digital_signature=True,
                content_commitment=True,
                key_encipherment=True,
                data_encipherment=True,
                key_agreement=False,
                key_cert_sign=False,
                crl_sign=False,
                encipher_only=False,
                decipher_only=False,
            ),
            critical=True,
        )
        .sign(ca_key, hashes.SHA256())
    )

    (BASE / "client2_key.pem").write_bytes(
        client_key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.TraditionalOpenSSL,
            serialization.NoEncryption(),
        )
    )
    (BASE / "client2_cert.pem").write_bytes(
        client_cert.public_bytes(serialization.Encoding.PEM)
    )
    print("新客户端证书已由 CA 签发: client2_cert.pem / client2_key.pem")


if __name__ == "__main__":
    main()
