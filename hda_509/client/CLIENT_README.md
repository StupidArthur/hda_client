# Client connection guide

The OPC UA X.509 Compatibility Mocker requires a client **Application
certificate** issued by the test CA in `test_material/certs/trust/`. The
server validates that certificate at CreateSession.

## Files a client needs

For a secured connection you need four files:

1. the client's Application certificate;
2. the matching client Application private key;
3. the server certificate (to pin it);
4. the endpoint URL and the `SecurityPolicy / MessageSecurityMode` to use.

For **X.509 user authentication** you additionally need a **separate** user
certificate/key pair (see `../README.md` §1 — Application Certificate and
User Certificate are two different things and must not be mixed).

## Minimal asyncua configuration

```python
from asyncua import Client, ua
from asyncua.crypto.security_policies import SecurityPolicyBasic256Sha256

client = Client("opc.tcp://127.0.0.1:48620/ua_mocker/")
client.application_uri = "urn:example.org:FreeOpcUa:opcua-asyncio"  # must match the cert SAN URI
await client.set_security(
    SecurityPolicyBasic256Sha256,
    "client_app_a_cert.pem",      # client Application certificate
    "client_app_a_key.pem",       # client Application private key
    server_certificate="server_cert.pem",
    mode=ua.MessageSecurityMode.SignAndEncrypt,
)
await client.connect()
```

User identity (optional, choose one):

```python
# Anonymous (default): nothing to do.

# UserName:
client.set_user("test")
client.set_password("test")

# X.509 user certificate (different pair from the application certificate):
await client.load_client_certificate("user_cert.pem")
await client.load_private_key("user_key.pem")
```

Then `await client.connect()`.

## Discovery

A third-party client usually does NOT know the server certificate in advance.
The standard flow works against this mocker:

```bash
python client/discovery_probe.py   # unsecured GetEndpoints -> list everything
```

The mocker publishes a discovery-only `None/None` endpoint (`security.discovery:
true`). Unsecured `GetEndpoints / FindServers` are allowed, but **any Session
over an unsecured channel is rejected** — Sessions must use a secure endpoint
(`security.require_secured_session: true`).

## Ready-made reference clients

The repository provides ready-made probes and clients — see `../README.md`
§8–§10:

```bash
python client/discovery_probe.py       # unsecured discovery probe
python client/probe.py                 # secured GetEndpoints probe
python client/app_cert_client.py       # Application Certificate + Anonymous
python client/username_client.py       # Application Certificate + UserName
python client/x509_user_client.py      # Application Certificate + X.509 User
python tests/positive_tests.py         # full positive suite
python tests/negative_tests.py         # full negative suite
```

## X.509 User authentication semantics

The mocker's X.509 user validation is **direct mode**: the presented user
certificate must exactly match a registered certificate (the user-manager
whitelist) AND be within its validity period. It is NOT a full user PKI
(no CA-chain trust for user certs, no CRL). Application certificates, by
contrast, are validated against the server trust store (Test CA) at
CreateSession.

## Security notes

Do not distribute the CA private key, the server private key, or the
repository's `test_material/` directory to anyone. `test_material/certs/`
is **development/test only** and must never be used as a production PKI.
