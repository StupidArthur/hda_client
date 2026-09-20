#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Generate the complete TEST PKI for the OPC UA X.509 Compatibility Mocker.

Run from anywhere:

    python test_material/gen_certs.py
    python test_material/gen_certs.py --server-dns my-mac.local --server-ip 192.168.1.50
    python test_material/gen_certs.py --server-dns host1,host2 --server-ip 10.0.0.5,192.168.1.50

The whole PKI is regenerated on every run (serial numbers are random).
All output paths are computed relative to this script (pathlib.Path), so the
script does not depend on the current working directory, on `openssl`, on
`chmod` or on any shell tool. It only needs `cryptography`, which is the same
library asyncua uses, keeping macOS and Windows behaviour identical.

LAN support: pass --server-dns / --server-ip to add the development machine's
LAN DNS name / IP to the Server Application Certificate SAN. Useful when the
mocker runs on one machine (e.g. macOS) and the real client runs on another
(e.g. Windows). Flags are repeatable and accept comma-separated lists.

OPC UA Certificate Profile compliance notes:
  * Root CA: BasicConstraints(ca=True), CA KeyUsage, SubjectKeyIdentifier,
    AuthorityKeyIdentifier (self).
  * Application Certificates: SAN URI == ApplicationUri, DNS/IP for the
    server, serverAuth/clientAuth EKU, correct KeyUsage, SKI + AKI, subject
    CN + OrganizationName.
  * Self-signed Server Application Certificate: BasicConstraints(ca=False)
    BUT KeyUsage.key_cert_sign=True (it must be able to vouch for itself),
    plus SKI + AKI (self).
  * User X.509 Certificate: a user identity, not an application certificate;
    still gets a sane subject, SKI, AKI, validity and signing KeyUsage.

IMPORTANT - concepts that must not be confused:

  Application Certificate (client_app_*.pem / server_cert.pem)
      Used to authenticate the *application* during SecureChannel /
      CreateSession (application authentication).

  User X.509 Certificate (user_*.pem)
      Used to authenticate the *user* during ActivateSession via the
      X509IdentityToken (user authentication).

A certificate/key pair must never be reused for both roles.

This material is DEVELOPMENT/TEST ONLY. It must never be used as a
production PKI.
"""

import argparse
import datetime
import ipaddress
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
BASE = Path(__file__).resolve().parent / "certs"
TRUST = BASE / "trust"
BASE.mkdir(parents=True, exist_ok=True)
TRUST.mkdir(parents=True, exist_ok=True)

# ---------------------------------------------------------------------------
# Identity constants. They are shared with the server / client code and the
# YAML configurations, so keep them consistent.
# ---------------------------------------------------------------------------
SERVER_APP_URI = "urn:freeopcua:python:server"
CUSTOM_SERVER_APP_URI = "urn:ua-hda:test:custom-server"
CLIENT_A_APP_URI = "urn:example.org:FreeOpcUa:opcua-asyncio"
CLIENT_B_APP_URI = "urn:example.org:FreeOpcUa:client-b"
WRONG_URI = "urn:example.org:FreeOpcUa:wrong-uri"
USER_CERT_URI = "urn:example.org:FreeOpcUa:user-x509"
UNTRUSTED_URI = "urn:example.org:FreeOpcUa:untrusted"

ORG = "ua_hda test"

VALID_DAYS = 365 * 5
NOW = datetime.datetime.now(datetime.timezone.utc)

# ---------------------------------------------------------------------------
# Small helpers
# ---------------------------------------------------------------------------


def gen_key(key_size: int = 2048) -> rsa.RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=key_size)


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


def subject(cn: str, org: str = ORG) -> x509.Name:
    attrs = [x509.NameAttribute(NameOID.COMMON_NAME, cn)]
    if org:
        attrs.append(x509.NameAttribute(NameOID.ORGANIZATION_NAME, org))
    return x509.Name(attrs)


def _key_usage(ca: bool = False, self_signed_app: bool = False) -> x509.KeyUsage:
    """OPC UA Application Certificates require these KeyUsage bits.

    self_signed_app: a self-signed OPC UA Application Certificate must set
    key_cert_sign=True even though BasicConstraints.ca is False, so that it
    can act as its own trust anchor (the requirement explicitly forbids
    deriving keyCertSign from the CA flag alone).
    """
    return x509.KeyUsage(
        digital_signature=True,
        content_commitment=True,
        key_encipherment=True,
        data_encipherment=True,
        key_agreement=False,
        key_cert_sign=ca or self_signed_app,
        crl_sign=ca,
        encipher_only=False,
        decipher_only=False,
    )


def build_cert(
    subject_name: x509.Name,
    public_key,
    issuer_name: x509.Name,
    issuer_key: rsa.RSAPrivateKey,
    *,
    san: list[x509.GeneralName],
    eku: list[x509.ObjectIdentifier],
    is_ca: bool = False,
    self_signed_app: bool = False,
    not_valid_before: datetime.datetime | None = None,
    not_valid_after: datetime.datetime | None = None,
) -> x509.Certificate:
    """Issue a certificate signed by issuer_key. Default validity: VALID_DAYS.

    Adds SubjectKeyIdentifier and AuthorityKeyIdentifier (from the issuer's
    public key) to every certificate.
    """
    builder = (
        x509.CertificateBuilder()
        .subject_name(subject_name)
        .issuer_name(issuer_name)
        .public_key(public_key)
        .serial_number(x509.random_serial_number())
        .not_valid_before(not_valid_before or NOW)
        .not_valid_after(not_valid_after or (NOW + datetime.timedelta(days=VALID_DAYS)))
        .add_extension(x509.BasicConstraints(ca=is_ca, path_length=None), critical=True)
        .add_extension(_key_usage(ca=is_ca, self_signed_app=self_signed_app), critical=True)
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(public_key), critical=False
        )
        .add_extension(
            x509.AuthorityKeyIdentifier.from_issuer_public_key(issuer_key.public_key()),
            critical=False,
        )
    )
    if san:
        builder = builder.add_extension(x509.SubjectAlternativeName(san), critical=False)
    if eku:
        builder = builder.add_extension(x509.ExtendedKeyUsage(eku), critical=False)
    return builder.sign(issuer_key, hashes.SHA256())


def build_self_signed_cert(
    cn: str,
    key: rsa.RSAPrivateKey,
    *,
    san: list[x509.GeneralName],
    eku: list[x509.ObjectIdentifier],
    self_signed_app: bool = True,
) -> x509.Certificate:
    name = subject(cn)
    return build_cert(
        name,
        key.public_key(),
        name,
        key,
        san=san,
        eku=eku,
        self_signed_app=self_signed_app,
    )


def parse_comma_list(values: list[str] | None) -> list[str]:
    """把 argparse 的重复参数和逗号列表拍平。"""
    out: list[str] = []
    for item in values or []:
        for part in str(item).split(","):
            part = part.strip()
            if part:
                out.append(part)
    return out


def server_san(application_uri: str, extra_dns: list[str], extra_ip: list[str]) -> list[x509.GeneralName]:
    """服务端证书 SAN：URI + localhost + 127.0.0.1 + 用户额外指定 DNS/IP。"""
    san: list[x509.GeneralName] = [x509.UniformResourceIdentifier(application_uri)]
    dns = ["localhost", *extra_dns]
    ips = ["127.0.0.1", *extra_ip]
    for name in dict.fromkeys(dns):
        san.append(x509.DNSName(name))
    for ip in dict.fromkeys(ips):
        san.append(x509.IPAddress(ipaddress.ip_address(ip)))
    return san


# ---------------------------------------------------------------------------
# Generation
# ---------------------------------------------------------------------------


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate the test PKI")
    parser.add_argument(
        "--server-dns", action="append", default=[],
        help="额外加入 Server 证书 SAN 的 DNS 名（可重复 / 逗号分隔），如 --server-dns my-mac.local",
    )
    parser.add_argument(
        "--server-ip", action="append", default=[],
        help="额外加入 Server 证书 SAN 的 IP（可重复 / 逗号分隔），如 --server-ip 192.168.1.50",
    )
    args = parser.parse_args()
    extra_dns = parse_comma_list(args.server_dns)
    extra_ip = parse_comma_list(args.server_ip)

    ca_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "ua_hda Test CA"),
                         x509.NameAttribute(NameOID.ORGANIZATION_NAME, ORG)])

    # ---- Test Root CA -----------------------------------------------------
    ca_key = gen_key()
    ca_cert = build_cert(
        ca_name, ca_key.public_key(), ca_name, ca_key,
        san=[], eku=[], is_ca=True,
    )
    dump_key(BASE / "ca_key.pem", ca_key)
    dump_cert(BASE / "ca_cert.pem", ca_cert)
    # The server trust store only contains the CA certificate; any client
    # certificate issued by this CA is therefore trusted by the server.
    dump_cert(TRUST / "ca_cert.pem", ca_cert)
    print(f"  [CA]                {BASE / 'ca_cert.pem'}")

    # ---- Server Application Certificate -----------------------------------
    # SAN URI must equal the server ApplicationUri; DNS/IP cover the addresses
    # a client actually uses (localhost / 127.0.0.1 / optional LAN entries).
    server_san_default = server_san(SERVER_APP_URI, extra_dns, extra_ip)
    server_key = gen_key()
    server_cert = build_cert(
        subject("ua_hda X509 Mocker Server"),
        server_key.public_key(),
        ca_name, ca_key,
        san=server_san_default,
        eku=[ExtendedKeyUsageOID.SERVER_AUTH],
    )
    dump_key(BASE / "server_key.pem", server_key)
    dump_cert(BASE / "server_cert.pem", server_cert)
    print(f"  [Server App Cert]   {BASE / 'server_cert.pem'} (DNS={server_san_default[1:]})")

    # ---- Server Application Certificate with a CUSTOM ApplicationUri -------
    # Used by the custom-application-uri scenario / test. Same SAN as the
    # default server cert except the URI.
    custom_key = gen_key()
    custom_cert = build_cert(
        subject("ua_hda X509 Mocker Server (custom uri)"),
        custom_key.public_key(),
        ca_name, ca_key,
        san=server_san(CUSTOM_SERVER_APP_URI, extra_dns, extra_ip),
        eku=[ExtendedKeyUsageOID.SERVER_AUTH],
    )
    dump_key(BASE / "server_custom_uri_key.pem", custom_key)
    dump_cert(BASE / "server_custom_uri_cert.pem", custom_cert)
    print(f"  [Server Custom URI] {BASE / 'server_custom_uri_cert.pem'} ({CUSTOM_SERVER_APP_URI})")

    # ---- Self-signed Server Certificate (scenario 48621) -------------------
    # Same SANs as above, but self-signed: simulates a server that did not
    # obtain a certificate from the test CA. keyCertSign=True, ca=False.
    ss_key = gen_key()
    ss_cert = build_self_signed_cert(
        "ua_hda X509 Mocker Server (self-signed)",
        ss_key,
        san=server_san(SERVER_APP_URI, extra_dns, extra_ip),
        eku=[ExtendedKeyUsageOID.SERVER_AUTH],
    )
    dump_key(BASE / "server_self_signed_key.pem", ss_key)
    dump_cert(BASE / "server_self_signed_cert.pem", ss_cert)
    print(f"  [Server Self-Signed] {BASE / 'server_self_signed_cert.pem'}")

    # ---- Client Application Certificate A (trusted) ------------------------
    client_a_key = gen_key()
    client_a_cert = build_cert(
        subject("ua_hda Client Application A"),
        client_a_key.public_key(),
        ca_name, ca_key,
        san=[x509.UniformResourceIdentifier(CLIENT_A_APP_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
    )
    dump_key(BASE / "client_app_a_key.pem", client_a_key)
    dump_cert(BASE / "client_app_a_cert.pem", client_a_cert)
    print(f"  [Client App A]      {BASE / 'client_app_a_cert.pem'}")

    # ---- Client Application Certificate B (trusted, second identity) -------
    client_b_key = gen_key()
    client_b_cert = build_cert(
        subject("ua_hda Client Application B"),
        client_b_key.public_key(),
        ca_name, ca_key,
        san=[x509.UniformResourceIdentifier(CLIENT_B_APP_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
    )
    dump_key(BASE / "client_app_b_key.pem", client_b_key)
    dump_cert(BASE / "client_app_b_cert.pem", client_b_cert)
    print(f"  [Client App B]      {BASE / 'client_app_b_cert.pem'}")

    # ---- Client App Certificate with WRONG Application URI -----------------
    # CA-issued and valid, but the SAN URI does not match the ApplicationUri
    # the client advertises -> server must answer BadCertificateUriInvalid.
    wrong_uri_key = gen_key()
    wrong_uri_cert = build_cert(
        subject("ua_hda Client Wrong URI"),
        wrong_uri_key.public_key(),
        ca_name, ca_key,
        san=[x509.UniformResourceIdentifier(WRONG_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
    )
    dump_key(BASE / "client_app_wrong_uri_key.pem", wrong_uri_key)
    dump_cert(BASE / "client_app_wrong_uri_cert.pem", wrong_uri_cert)
    print(f"  [Client Wrong URI]  {BASE / 'client_app_wrong_uri_cert.pem'}")

    # ---- Expired Client Application Certificate ----------------------------
    # CA-issued and trusted, but already expired -> BadCertificateTimeInvalid.
    expired_key = gen_key()
    expired_cert = build_cert(
        subject("ua_hda Client Expired"),
        expired_key.public_key(),
        ca_name, ca_key,
        san=[x509.UniformResourceIdentifier(CLIENT_A_APP_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
        not_valid_before=NOW - datetime.timedelta(days=30),
        not_valid_after=NOW - datetime.timedelta(days=1),
    )
    dump_key(BASE / "client_app_expired_key.pem", expired_key)
    dump_cert(BASE / "client_app_expired_cert.pem", expired_cert)
    print(f"  [Client Expired]    {BASE / 'client_app_expired_cert.pem'}")

    # ---- Untrusted (self-signed) Client Application Certificate ------------
    # NOT issued by the test CA -> server must answer BadCertificateUntrusted.
    untrusted_key = gen_key()
    untrusted_cert = build_self_signed_cert(
        "ua_hda Client Untrusted",
        untrusted_key,
        san=[x509.UniformResourceIdentifier(UNTRUSTED_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
    )
    dump_key(BASE / "client_app_untrusted_key.pem", untrusted_key)
    dump_cert(BASE / "client_app_untrusted_cert.pem", untrusted_cert)
    print(f"  [Client Untrusted]  {BASE / 'client_app_untrusted_cert.pem'}")

    # ---- Mismatched key for the certificate/key mismatch test --------------
    # This key intentionally does NOT match client_app_a_cert.pem.
    mismatch_key = gen_key()
    dump_key(BASE / "client_app_a_mismatch_key.pem", mismatch_key)
    print(f"  [Client Key Mismatch] {BASE / 'client_app_a_mismatch_key.pem'}")

    # ---- User X.509 Certificate (trusted, for X509IdentityToken) -----------
    # This is the USER identity, separate from any client APPLICATION
    # certificate. The server user manager whitelists this exact certificate
    # AND checks its validity (direct mode).
    user_key = gen_key()
    user_cert = build_cert(
        subject("ua_hda Test User"),
        user_key.public_key(),
        ca_name, ca_key,
        san=[x509.UniformResourceIdentifier(USER_CERT_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
    )
    dump_key(BASE / "user_key.pem", user_key)
    dump_cert(BASE / "user_cert.pem", user_cert)
    print(f"  [User Cert]         {BASE / 'user_cert.pem'}")

    # ---- Expired User X.509 Certificate ------------------------------------
    # REGISTERED in the whitelist but already expired -> user authentication
    # must reject it (validity check) even though the DER matches.
    user_expired_key = gen_key()
    user_expired_cert = build_cert(
        subject("ua_hda Test User (expired)"),
        user_expired_key.public_key(),
        ca_name, ca_key,
        san=[x509.UniformResourceIdentifier(USER_CERT_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
        not_valid_before=NOW - datetime.timedelta(days=30),
        not_valid_after=NOW - datetime.timedelta(days=1),
    )
    dump_key(BASE / "user_expired_key.pem", user_expired_key)
    dump_cert(BASE / "user_expired_cert.pem", user_expired_cert)
    print(f"  [User Expired]      {BASE / 'user_expired_cert.pem'}")

    # ---- Untrusted (self-signed) User Certificate --------------------------
    # Not in the server user-manager whitelist -> user authentication fails.
    user_untrusted_key = gen_key()
    user_untrusted_cert = build_self_signed_cert(
        "ua_hda Untrusted User",
        user_untrusted_key,
        san=[x509.UniformResourceIdentifier(UNTRUSTED_URI)],
        eku=[ExtendedKeyUsageOID.CLIENT_AUTH],
    )
    dump_key(BASE / "user_untrusted_key.pem", user_untrusted_key)
    dump_cert(BASE / "user_untrusted_cert.pem", user_untrusted_cert)
    print(f"  [User Untrusted]    {BASE / 'user_untrusted_cert.pem'}")

    # ---- Wrong private key for the user certificate ------------------------
    # Does not match user_cert.pem -> user token signature verification fails.
    user_wrong_key = gen_key()
    dump_key(BASE / "user_wrong_key.pem", user_wrong_key)
    print(f"  [User Wrong Key]    {BASE / 'user_wrong_key.pem'}")

    print("\nTest PKI regenerated.")


if __name__ == "__main__":
    main()
