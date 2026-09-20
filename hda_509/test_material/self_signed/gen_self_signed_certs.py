#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Generate the SELF-SIGNED test PKI for OPC UA SecureChannel testing.

This is a second, fully self-signed test PKI — independent of the Test Root
CA in `test_material/certs/`. It is used by the `config_self_signed.yaml`
scenario, where the mock server directly trusts a self-signed CLIENT
Application Certificate (no CA involved):

    Client self-signed certificate
            |
            v
    Server Trust Store   (test_material/self_signed/trust/)
            |
            v
    Certificate validation -> SecureChannel established

Run from anywhere:

    python test_material/self_signed/gen_self_signed_certs.py

Re-running regenerates the whole set. All paths are relative to this script
(pathlib), pure `cryptography` (no `openssl`), works identically on macOS and
Windows.

Certificates (all BasicConstraints ca=False; self-signed app certificates set
KeyUsage.keyCertSign=True so each can act as its own trust anchor):

  server/    self-signed Server Application Certificate
             EKU serverAuth, SAN = {ApplicationUri, localhost, 127.0.0.1}
  client/    self-signed Client Application Certificates
             EKU clientAuth, SAN = {Client ApplicationUri}
             - client_self_signed_cert.pem  (TRUSTED by the server)
             - client_self_signed_untrusted_cert.pem   (NOT in trust store)
             - client_self_signed_expired_cert.pem     (expired)
             - client_self_signed_wrong_uri_cert.pem   (SAN URI mismatch)
             - client_self_signed_mismatch_key.pem     (key not matching the cert)
  user/      self-signed User Certificates (X509IdentityToken)
             EKU clientAuth, SAN = {User ApplicationUri}
             - user_self_signed_cert.pem  (REGISTERED in the user-manager whitelist)
             - user_self_signed_unregistered_cert.pem (NOT registered)
             - user_self_signed_expired_cert.pem      (expired)
             - user_self_signed_wrong_key.pem         (key not matching the cert)
  trust/     server Application trust store: the single trusted self-signed
             client Application cert

Two trust domains are kept strictly separate:
  Application Trust  = server Application trust store  (trust/) -> CreateSession
  User Authentication = user-manager direct whitelist  (user/)  -> ActivateSession
A User Certificate must never be placed in the Application trust store, and
the Client Application Certificate must never be registered as a user.

TEST ONLY, never use as a production PKI.
"""

import datetime
import ipaddress
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

# ---------------------------------------------------------------------------
# Paths (relative to this script)
# ---------------------------------------------------------------------------
BASE = Path(__file__).resolve().parent
SERVER_DIR = BASE / "server"
CLIENT_DIR = BASE / "client"
USER_DIR = BASE / "user"
TRUST_DIR = BASE / "trust"
for d in (SERVER_DIR, CLIENT_DIR, USER_DIR, TRUST_DIR):
    d.mkdir(parents=True, exist_ok=True)

# ---------------------------------------------------------------------------
# Identity constants (must match config_self_signed.yaml / config_self_signed_x509_user.yaml)
# ---------------------------------------------------------------------------
SERVER_APP_URI = "urn:freeopcua:python:server"
CLIENT_SELF_SIGNED_URI = "urn:example.org:FreeOpcUa:selfsigned-client"
WRONG_URI = "urn:example.org:FreeOpcUa:selfsigned-wrong-uri"
USER_SELF_SIGNED_URI = "urn:example.org:FreeOpcUa:selfsigned-user"

ORG = "ua_hda test"
VALID_DAYS = 365 * 5
NOW = datetime.datetime.now(datetime.timezone.utc)

# ---------------------------------------------------------------------------
# Helpers
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


def build_self_signed_app_cert(
    cn: str,
    key: rsa.RSAPrivateKey,
    *,
    san: list[x509.GeneralName],
    eku: x509.ObjectIdentifier,
    not_valid_before: datetime.datetime | None = None,
    not_valid_after: datetime.datetime | None = None,
) -> x509.Certificate:
    """Self-signed OPC UA Application Certificate.

    BasicConstraints.ca=False, but KeyUsage.keyCertSign=True (self-signed
    application certificates must be able to vouch for themselves; it is the
    trust anchor here). Includes SKI + AKI (self).
    """
    return _build_self_signed(
        cn, key, san=san, eku=eku, key_cert_sign=True,
        not_valid_before=not_valid_before, not_valid_after=not_valid_after,
    )


def build_self_signed_user_cert(
    cn: str,
    key: rsa.RSAPrivateKey,
    *,
    san: list[x509.GeneralName],
    not_valid_before: datetime.datetime | None = None,
    not_valid_after: datetime.datetime | None = None,
) -> x509.Certificate:
    """Self-signed OPC UA User Certificate (X509IdentityToken).

    A USER identity certificate — deliberately NOT an Application Certificate:
    BasicConstraints.ca=False and KeyUsage.keyCertSign=False (it is not a
    trust anchor, it is only used to sign the UserTokenSignature during
    ActivateSession). Same reasonable profile otherwise (EKU clientAuth,
    SAN, SKI, AKI, validity).
    """
    return _build_self_signed(
        cn, key, san=san, eku=ExtendedKeyUsageOID.CLIENT_AUTH, key_cert_sign=False,
        not_valid_before=not_valid_before, not_valid_after=not_valid_after,
    )


def _build_self_signed(
    cn: str,
    key: rsa.RSAPrivateKey,
    *,
    san: list[x509.GeneralName],
    eku: x509.ObjectIdentifier,
    key_cert_sign: bool,
    not_valid_before: datetime.datetime | None = None,
    not_valid_after: datetime.datetime | None = None,
) -> x509.Certificate:
    """Self-signed cert builder shared by app certificates and user certificates."""
    name = x509.Name([
        x509.NameAttribute(NameOID.COMMON_NAME, cn),
        x509.NameAttribute(NameOID.ORGANIZATION_NAME, ORG),
    ])
    builder = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(not_valid_before or NOW)
        .not_valid_after(not_valid_after or (NOW + datetime.timedelta(days=VALID_DAYS)))
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(
            x509.KeyUsage(
                digital_signature=True,
                content_commitment=True,
                key_encipherment=True,
                data_encipherment=True,
                key_agreement=False,
                key_cert_sign=key_cert_sign,
                crl_sign=False,
                encipher_only=False,
                decipher_only=False,
            ),
            critical=True,
        )
        .add_extension(
            x509.SubjectAlternativeName(san), critical=False
        )
        .add_extension(
            x509.ExtendedKeyUsage([eku]), critical=False
        )
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(key.public_key()), critical=False
        )
        .add_extension(
            x509.AuthorityKeyIdentifier.from_issuer_public_key(key.public_key()), critical=False
        )
    )
    return builder.sign(key, hashes.SHA256())


def client_san(uri: str) -> list[x509.GeneralName]:
    return [x509.UniformResourceIdentifier(uri)]


def server_san() -> list[x509.GeneralName]:
    return [
        x509.UniformResourceIdentifier(SERVER_APP_URI),
        x509.DNSName("localhost"),
        x509.IPAddress(ipaddress.ip_address("127.0.0.1")),
    ]


def main() -> None:
    # ---- self-signed Server Application Certificate ------------------------
    server_key = gen_key()
    server_cert = build_self_signed_app_cert(
        "ua_hda X509 Mocker Server (self-signed PKI)",
        server_key,
        san=server_san(),
        eku=ExtendedKeyUsageOID.SERVER_AUTH,
    )
    dump_key(SERVER_DIR / "server_self_signed_key.pem", server_key)
    dump_cert(SERVER_DIR / "server_self_signed_cert.pem", server_cert)
    print(f"  [Server]            {SERVER_DIR / 'server_self_signed_cert.pem'}")

    # ---- TRUSTED self-signed Client Application Certificate ----------------
    client_key = gen_key()
    client_cert = build_self_signed_app_cert(
        "ua_hda X509 Mocker Client (self-signed PKI)",
        client_key,
        san=client_san(CLIENT_SELF_SIGNED_URI),
        eku=ExtendedKeyUsageOID.CLIENT_AUTH,
    )
    dump_key(CLIENT_DIR / "client_self_signed_key.pem", client_key)
    dump_cert(CLIENT_DIR / "client_self_signed_cert.pem", client_cert)
    # The server trust store directly trusts this exact self-signed client cert.
    dump_cert(TRUST_DIR / "client_self_signed_cert.pem", client_cert)
    print(f"  [Client (trusted)]  {CLIENT_DIR / 'client_self_signed_cert.pem'}")

    # ---- Untrusted self-signed client (NOT in trust store) ------------------
    untrusted_key = gen_key()
    untrusted_cert = build_self_signed_app_cert(
        "ua_hda X509 Mocker Client Untrusted (self-signed)",
        untrusted_key,
        san=client_san(CLIENT_SELF_SIGNED_URI),
        eku=ExtendedKeyUsageOID.CLIENT_AUTH,
    )
    dump_key(CLIENT_DIR / "client_self_signed_untrusted_key.pem", untrusted_key)
    dump_cert(CLIENT_DIR / "client_self_signed_untrusted_cert.pem", untrusted_cert)
    print(f"  [Client untrusted]  {CLIENT_DIR / 'client_self_signed_untrusted_cert.pem'}")

    # ---- Expired self-signed client ----------------------------------------
    expired_key = gen_key()
    expired_cert = build_self_signed_app_cert(
        "ua_hda X509 Mocker Client Expired (self-signed)",
        expired_key,
        san=client_san(CLIENT_SELF_SIGNED_URI),
        eku=ExtendedKeyUsageOID.CLIENT_AUTH,
        not_valid_before=NOW - datetime.timedelta(days=30),
        not_valid_after=NOW - datetime.timedelta(days=1),
    )
    dump_key(CLIENT_DIR / "client_self_signed_expired_key.pem", expired_key)
    dump_cert(CLIENT_DIR / "client_self_signed_expired_cert.pem", expired_cert)
    print(f"  [Client expired]    {CLIENT_DIR / 'client_self_signed_expired_cert.pem'}")

    # ---- Wrong ApplicationUri self-signed client ----------------------------
    wrong_uri_key = gen_key()
    wrong_uri_cert = build_self_signed_app_cert(
        "ua_hda X509 Mocker Client Wrong URI (self-signed)",
        wrong_uri_key,
        san=client_san(WRONG_URI),
        eku=ExtendedKeyUsageOID.CLIENT_AUTH,
    )
    dump_key(CLIENT_DIR / "client_self_signed_wrong_uri_key.pem", wrong_uri_key)
    dump_cert(CLIENT_DIR / "client_self_signed_wrong_uri_cert.pem", wrong_uri_cert)
    print(f"  [Client wrong-uri]  {CLIENT_DIR / 'client_self_signed_wrong_uri_cert.pem'}")

    # ---- Mismatched key (does not match client_self_signed_cert.pem) --------
    mismatch_key = gen_key()
    dump_key(CLIENT_DIR / "client_self_signed_mismatch_key.pem", mismatch_key)
    print(f"  [Client key mismatch] {CLIENT_DIR / 'client_self_signed_mismatch_key.pem'}")

    # ---- REGISTERED self-signed User Certificate ----------------------------
    # Independent from the Client Application Certificate (different key pair).
    # Used by config_self_signed_x509_user.yaml as the registered X.509 user.
    user_key = gen_key()
    user_cert = build_self_signed_user_cert(
        "ua_hda X509 Mocker User (self-signed)",
        user_key,
        san=client_san(USER_SELF_SIGNED_URI),
    )
    dump_key(USER_DIR / "user_self_signed_key.pem", user_key)
    dump_cert(USER_DIR / "user_self_signed_cert.pem", user_cert)
    print(f"  [User (registered)] {USER_DIR / 'user_self_signed_cert.pem'}")

    # ---- Unregistered self-signed User Certificate ---------------------------
    # Not in the user-manager whitelist -> ActivateSession rejected.
    user_unreg_key = gen_key()
    user_unreg_cert = build_self_signed_user_cert(
        "ua_hda X509 Mocker User Unregistered (self-signed)",
        user_unreg_key,
        san=client_san(USER_SELF_SIGNED_URI),
    )
    dump_key(USER_DIR / "user_self_signed_unregistered_key.pem", user_unreg_key)
    dump_cert(USER_DIR / "user_self_signed_unregistered_cert.pem", user_unreg_cert)
    print(f"  [User unregistered] {USER_DIR / 'user_self_signed_unregistered_cert.pem'}")

    # ---- Expired self-signed User Certificate --------------------------------
    # Registered certs are checked for validity too -> expired user is rejected.
    user_expired_key = gen_key()
    user_expired_cert = build_self_signed_user_cert(
        "ua_hda X509 Mocker User Expired (self-signed)",
        user_expired_key,
        san=client_san(USER_SELF_SIGNED_URI),
        not_valid_before=NOW - datetime.timedelta(days=30),
        not_valid_after=NOW - datetime.timedelta(days=1),
    )
    dump_key(USER_DIR / "user_self_signed_expired_key.pem", user_expired_key)
    dump_cert(USER_DIR / "user_self_signed_expired_cert.pem", user_expired_cert)
    print(f"  [User expired]      {USER_DIR / 'user_self_signed_expired_cert.pem'}")

    # ---- Wrong private key (does not match user_self_signed_cert.pem) --------
    user_wrong_key = gen_key()
    dump_key(USER_DIR / "user_self_signed_wrong_key.pem", user_wrong_key)
    print(f"  [User wrong key]    {USER_DIR / 'user_self_signed_wrong_key.pem'}")

    print("\nSelf-signed test PKI regenerated.")


if __name__ == "__main__":
    main()
