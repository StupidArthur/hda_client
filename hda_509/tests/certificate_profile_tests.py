#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Automated validation of the generated test PKI against the OPC UA
Certificate Profile requirements described in the README.

Run after regenerating the PKI:

    python test_material/gen_certs.py
    python tests/certificate_profile_tests.py

This does real structural checks (SAN, EKU, KeyUsage, SKI, AKI, issuer,
signature verification, validity), not just "cryptography can parse it".

No server is needed for this test.
"""

import sys
from datetime import datetime, timezone
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

BASE = Path(__file__).resolve().parents[1] / "test_material" / "certs"
SS_DIR = Path(__file__).resolve().parents[1] / "test_material" / "self_signed"

SERVER_APP_URI = "urn:freeopcua:python:server"
CUSTOM_SERVER_APP_URI = "urn:ua-hda:test:custom-server"
CLIENT_A_APP_URI = "urn:example.org:FreeOpcUa:opcua-asyncio"
CLIENT_B_APP_URI = "urn:example.org:FreeOpcUa:client-b"
WRONG_URI = "urn:example.org:FreeOpcUa:wrong-uri"
USER_CERT_URI = "urn:example.org:FreeOpcUa:user-x509"
UNTRUSTED_URI = "urn:example.org:FreeOpcUa:untrusted"
ORG = "ua_hda test"

RESULTS: list[tuple[str, str, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    outcome = "PASS" if ok else "FAIL"
    print(f"  [{outcome}] {name} {('-> ' + detail) if detail else ''}")
    RESULTS.append((name, outcome, detail))


def load(name: str) -> x509.Certificate:
    return x509.load_pem_x509_certificate((BASE / name).read_bytes())


def load_key(name: str) -> rsa.RSAPrivateKey:
    from cryptography.hazmat.primitives import serialization
    return serialization.load_pem_private_key((BASE / name).read_bytes(), password=None)


def san_uris(cert) -> list[str]:
    return cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value.get_values_for_type(
        x509.UniformResourceIdentifier
    )


def san_dns(cert) -> list[str]:
    return cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value.get_values_for_type(
        x509.DNSName
    )


def san_ips(cert) -> list[str]:
    return [str(ip) for ip in cert.extensions.get_extension_for_class(
        x509.SubjectAlternativeName).value.get_values_for_type(x509.IPAddress)]


def has_extension(cert, cls) -> bool:
    try:
        cert.extensions.get_extension_for_class(cls)
        return True
    except x509.ExtensionNotFound:
        return False


def verify_sig(cert: x509.Certificate, public_key) -> bool:
    """用签发者的公钥验证证书签名（RSA PKCS1v15 + SHA256）。"""
    try:
        public_key.verify(cert.signature, cert.tbs_certificate_bytes,
                          padding.PKCS1v15(), cert.signature_hash_algorithm)
        return True
    except Exception:  # noqa: BLE001
        return False


def key_usage(cert) -> x509.KeyUsage:
    return cert.extensions.get_extension_for_class(x509.KeyUsage).value


def eku(cert) -> list:
    return cert.extensions.get_extension_for_class(x509.ExtendedKeyUsage).value


def check_common_app_profile(name: str, cert, *, expected_uri: str, eku_oid, ca_issued_by=None):
    """Application Certificate 通用检查（SKI/AKI/Org/KeyUsage/EKU/URI）。"""
    check(f"{name}: Subject CN/Organization",
          cert.subject.get_attributes_for_oid(NameOID.COMMON_NAME) and
          any(a.oid == NameOID.ORGANIZATION_NAME and a.value == ORG for a in cert.subject),
          cert.subject.rfc4514_string())
    check(f"{name}: SAN ApplicationUri == {expected_uri}", expected_uri in san_uris(cert), str(san_uris(cert)))
    check(f"{name}: EKU 含 {eku_oid._name if hasattr(eku_oid,'_name') else eku_oid}",
          eku_oid in eku(cert), str(eku(cert)))
    ku = key_usage(cert)
    check(f"{name}: KeyUsage digital_signature/content_commitment/key_encipherment/data_encipherment",
          ku.digital_signature and ku.content_commitment and ku.key_encipherment and ku.data_encipherment)
    check(f"{name}: BasicConstraints ca=False", cert.extensions.get_extension_for_class(
        x509.BasicConstraints).value.ca is False)
    check(f"{name}: SKI 存在", has_extension(cert, x509.SubjectKeyIdentifier))
    check(f"{name}: AKI 存在", has_extension(cert, x509.AuthorityKeyIdentifier))
    if ca_issued_by is not None:
        check(f"{name}: Issuer == {ca_issued_by.subject.rfc4514_string()}",
              cert.issuer == ca_issued_by.subject, cert.issuer.rfc4514_string())
        check(f"{name}: AKI 与 CA SKI 一致",
              cert.extensions.get_extension_for_class(x509.AuthorityKeyIdentifier).value.key_identifier
              == ca_issued_by.extensions.get_extension_for_class(x509.SubjectKeyIdentifier).value.digest,
              )
        check(f"{name}: 签名可由 CA 公钥验证", verify_sig(cert, ca_issued_by.public_key()))


def check_validity(name: str, cert, *, expired: bool) -> None:
    now = datetime.now(timezone.utc)
    if expired:
        check(f"{name}: 已过期", cert.not_valid_after_utc < now,
              f"not_valid_after={cert.not_valid_after_utc}")
    else:
        check(f"{name}: 有效期覆盖当前时间",
              cert.not_valid_before_utc <= now <= cert.not_valid_after_utc,
              f"[{cert.not_valid_before_utc} .. {cert.not_valid_after_utc}]")


def main() -> int:
    print("证书 Profile 自动验证：\n")

    ca = load("ca_cert.pem")
    ca_key = load_key("ca_key.pem")
    check("CA: 可加载且 BasicConstraints ca=True",
          ca.extensions.get_extension_for_class(x509.BasicConstraints).value.ca is True)
    check("CA: KeyUsage key_cert_sign + crl_sign",
          key_usage(ca).key_cert_sign and key_usage(ca).crl_sign)
    check("CA: SKI 存在", has_extension(ca, x509.SubjectKeyIdentifier))
    aki = ca.extensions.get_extension_for_class(x509.AuthorityKeyIdentifier).value
    check("CA: AKI 存在且指向自身", aki.key_identifier is not None
          and aki.key_identifier == ca.extensions.get_extension_for_class(
              x509.SubjectKeyIdentifier).value.digest)
    check("CA: 自签名可验证", verify_sig(ca, ca.public_key()))
    check("CA: Organization", any(a.oid == NameOID.ORGANIZATION_NAME and a.value == ORG
                                  for a in ca.subject), ca.subject.rfc4514_string())

    # ---- Server Application Certificate -------------------------------------
    server = load("server_cert.pem")
    check_common_app_profile("server_cert", server, expected_uri=SERVER_APP_URI,
                             eku_oid=ExtendedKeyUsageOID.SERVER_AUTH, ca_issued_by=ca)
    check("server_cert: SAN DNS 含 localhost", "localhost" in san_dns(server), str(san_dns(server)))
    check("server_cert: SAN IP 含 127.0.0.1", "127.0.0.1" in san_ips(server), str(san_ips(server)))
    check("server_cert: KeyUsage key_cert_sign=False (CA 签发应用证书)",
          key_usage(server).key_cert_sign is False)
    check_validity("server_cert", server, expired=False)

    # ---- Custom-URI Server Certificate --------------------------------------
    custom = load("server_custom_uri_cert.pem")
    check_common_app_profile("server_custom_uri_cert", custom, expected_uri=CUSTOM_SERVER_APP_URI,
                             eku_oid=ExtendedKeyUsageOID.SERVER_AUTH, ca_issued_by=ca)
    check("server_custom_uri_cert: SAN DNS/IP 与默认一致",
          "localhost" in san_dns(custom) and "127.0.0.1" in san_ips(custom))

    # ---- Self-signed Server Certificate -------------------------------------
    ss = load("server_self_signed_cert.pem")
    ss_key = load_key("server_self_signed_key.pem")
    check_common_app_profile("server_self_signed_cert", ss, expected_uri=SERVER_APP_URI,
                             eku_oid=ExtendedKeyUsageOID.SERVER_AUTH)
    check("server_self_signed_cert: BasicConstraints ca=False 但 key_cert_sign=True",
          ss.extensions.get_extension_for_class(x509.BasicConstraints).value.ca is False
          and key_usage(ss).key_cert_sign is True)
    check("server_self_signed_cert: AKI 指向自身",
          ss.extensions.get_extension_for_class(x509.AuthorityKeyIdentifier).value.key_identifier
          == ss.extensions.get_extension_for_class(x509.SubjectKeyIdentifier).value.digest)
    check("server_self_signed_cert: 自签名可验证", verify_sig(ss, ss.public_key()))

    # ---- Client Application Certificates ------------------------------------
    for fname, uri, cn in [("client_app_a_cert.pem", CLIENT_A_APP_URI, "A"),
                           ("client_app_b_cert.pem", CLIENT_B_APP_URI, "B")]:
        cc = load(fname)
        check_common_app_profile(f"client_app_{cn.lower()}_cert", cc, expected_uri=uri,
                                 eku_oid=ExtendedKeyUsageOID.CLIENT_AUTH, ca_issued_by=ca)
        check_validity(f"client_app_{cn.lower()}_cert", cc, expired=False)

    # ---- Negative-profile certs ---------------------------------------------
    wrong = load("client_app_wrong_uri_cert.pem")
    check("client_app_wrong_uri_cert: SAN URI 确实是错误的", 
          WRONG_URI in san_uris(wrong) and CLIENT_A_APP_URI not in san_uris(wrong),
          str(san_uris(wrong)))
    check_common_app_profile("client_app_wrong_uri_cert(结构合法)", wrong,
                             expected_uri=WRONG_URI, eku_oid=ExtendedKeyUsageOID.CLIENT_AUTH,
                             ca_issued_by=ca)

    expired = load("client_app_expired_cert.pem")
    check_validity("client_app_expired_cert", expired, expired=True)
    check_common_app_profile("client_app_expired_cert(结构合法)", expired,
                             expected_uri=CLIENT_A_APP_URI, eku_oid=ExtendedKeyUsageOID.CLIENT_AUTH,
                             ca_issued_by=ca)

    untrusted = load("client_app_untrusted_cert.pem")
    check("client_app_untrusted_cert: 自签名（issuer==subject）",
          untrusted.issuer == untrusted.subject)
    check("client_app_untrusted_cert: 不可由 CA 验证（非 CA 签发）",
          not _verify_by_ca(untrusted, ca))

    # ---- User Certificates --------------------------------------------------
    user = load("user_cert.pem")
    check_common_app_profile("user_cert", user, expected_uri=USER_CERT_URI,
                             eku_oid=ExtendedKeyUsageOID.CLIENT_AUTH, ca_issued_by=ca)
    check("user_cert: 不是 Application/CA 证书（ca=False, key_cert_sign=False）",
          user.extensions.get_extension_for_class(x509.BasicConstraints).value.ca is False
          and key_usage(user).key_cert_sign is False)
    check_validity("user_cert", user, expired=False)

    user_expired = load("user_expired_cert.pem")
    check_validity("user_expired_cert", user_expired, expired=True)
    check_common_app_profile("user_expired_cert(结构合法)", user_expired,
                             expected_uri=USER_CERT_URI, eku_oid=ExtendedKeyUsageOID.CLIENT_AUTH,
                             ca_issued_by=ca)

    # 未注册用户证书：Test CA 签发、profile 合法、当前有效（测试只差"不在白名单"）
    user_unreg = load("user_unregistered_cert.pem")
    check_common_app_profile("user_unregistered_cert", user_unreg, expected_uri=USER_CERT_URI,
                             eku_oid=ExtendedKeyUsageOID.CLIENT_AUTH, ca_issued_by=ca)
    check_validity("user_unregistered_cert", user_unreg, expired=False)
    check("user_unregistered_cert: CA 签发（非自签名）", user_unreg.issuer != user_unreg.subject)

    user_untrusted = load("user_untrusted_cert.pem")
    check("user_untrusted_cert: 自签名", user_untrusted.issuer == user_untrusted.subject)

    # ---- Self-signed PKI (test_material/self_signed/) ------------------------
    print()
    print("Self-signed PKI (config_self_signed.yaml)：\n")
    ss_server = x509.load_pem_x509_certificate((SS_DIR / "server" / "server_self_signed_cert.pem").read_bytes())
    check("ss_server: 自签名(issuer==subject)", ss_server.issuer == ss_server.subject)
    check("ss_server: EKU serverAuth", ExtendedKeyUsageOID.SERVER_AUTH in eku(ss_server))
    check("ss_server: ca=False 且 keyCertSign=True",
          ss_server.extensions.get_extension_for_class(x509.BasicConstraints).value.ca is False
          and key_usage(ss_server).key_cert_sign is True)
    check("ss_server: SAN URI==ServerAppUri", SERVER_APP_URI in san_uris(ss_server))
    check("ss_server: SAN DNS localhost", "localhost" in san_dns(ss_server))
    check("ss_server: SAN IP 127.0.0.1", "127.0.0.1" in san_ips(ss_server))
    check("ss_server: SKI+AKI", has_extension(ss_server, x509.SubjectKeyIdentifier)
          and has_extension(ss_server, x509.AuthorityKeyIdentifier))
    check_validity("ss_server", ss_server, expired=False)

    ss_client = x509.load_pem_x509_certificate((SS_DIR / "client" / "client_self_signed_cert.pem").read_bytes())
    check("ss_client: 自签名(issuer==subject)", ss_client.issuer == ss_client.subject)
    check("ss_client: EKU clientAuth", ExtendedKeyUsageOID.CLIENT_AUTH in eku(ss_client))
    check("ss_client: ca=False 且 keyCertSign=True",
          ss_client.extensions.get_extension_for_class(x509.BasicConstraints).value.ca is False
          and key_usage(ss_client).key_cert_sign is True)
    check("ss_client: SAN URI", "urn:example.org:FreeOpcUa:selfsigned-client" in san_uris(ss_client))
    check("ss_client: SKI+AKI", has_extension(ss_client, x509.SubjectKeyIdentifier)
          and has_extension(ss_client, x509.AuthorityKeyIdentifier))
    check_validity("ss_client", ss_client, expired=False)

    ss_expired = x509.load_pem_x509_certificate((SS_DIR / "client" / "client_self_signed_expired_cert.pem").read_bytes())
    check_validity("ss_client_expired", ss_expired, expired=True)
    ss_wrong = x509.load_pem_x509_certificate((SS_DIR / "client" / "client_self_signed_wrong_uri_cert.pem").read_bytes())
    check("ss_client_wrong_uri: SAN URI 不是正确值",
          "urn:example.org:FreeOpcUa:selfsigned-wrong-uri" in san_uris(ss_wrong)
          and "urn:example.org:FreeOpcUa:selfsigned-client" not in san_uris(ss_wrong),
          str(san_uris(ss_wrong)))

    # ---- Self-signed User Certificates (user/, X509IdentityToken) -------------
    ss_user = x509.load_pem_x509_certificate((SS_DIR / "user" / "user_self_signed_cert.pem").read_bytes())
    check("ss_user: 自签名(issuer==subject)", ss_user.issuer == ss_user.subject)
    check("ss_user: EKU clientAuth", ExtendedKeyUsageOID.CLIENT_AUTH in eku(ss_user))
    check("ss_user: ca=False 且 keyCertSign=False（不是信任锚）",
          ss_user.extensions.get_extension_for_class(x509.BasicConstraints).value.ca is False
          and key_usage(ss_user).key_cert_sign is False)
    check("ss_user: SAN URI", "urn:example.org:FreeOpcUa:selfsigned-user" in san_uris(ss_user))
    check("ss_user: SKI+AKI", has_extension(ss_user, x509.SubjectKeyIdentifier)
          and has_extension(ss_user, x509.AuthorityKeyIdentifier))
    check_validity("ss_user", ss_user, expired=False)

    ss_user_expired = x509.load_pem_x509_certificate((SS_DIR / "user" / "user_self_signed_expired_cert.pem").read_bytes())
    check_validity("ss_user_expired", ss_user_expired, expired=True)

    # ---- Application Certificate 与 User Certificate 必须不同 -----------------
    from cryptography.hazmat.primitives import serialization as _ser
    ss_app_pub = ss_client.public_key().public_bytes(_ser.Encoding.DER, _ser.PublicFormat.SubjectPublicKeyInfo)
    ss_user_pub = ss_user.public_key().public_bytes(_ser.Encoding.DER, _ser.PublicFormat.SubjectPublicKeyInfo)
    check("ss App cert != ss User cert (DER + PublicKey 不同)",
          (SS_DIR / "client" / "client_self_signed_cert.pem").read_bytes()
          != (SS_DIR / "user" / "user_self_signed_cert.pem").read_bytes()
          and ss_app_pub != ss_user_pub)

    print("\n汇总：")
    failed = [n for n, o, _ in RESULTS if o.startswith("FAIL")]
    for name, outcome, detail in RESULTS:
        print(f"  {outcome:5} {name}")
    if failed:
        print(f"\n{len(failed)} 项失败。")
        return 1
    print(f"\n全部 {len(RESULTS)} 项通过。")
    return 0


def _verify_by_ca(cert: x509.Certificate, ca: x509.Certificate) -> bool:
    return verify_sig(cert, ca.public_key())


if __name__ == "__main__":
    sys.exit(main())
