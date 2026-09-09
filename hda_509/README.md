# X.509 OPC UA Mock Server

`hda_509` is the repository's single X.509 OPC UA mocker package. Its YAML
file is the source of truth for the server configuration.

```text
hda_509/
  main.py, server_main.py, config_x509.yaml   server runtime and configuration
  client/                                     client handoff guide and demo
  test_material/                              development-only certificates and tests
  lib/                                        offline Python wheels
```

## Run locally

Install dependencies with `install_offline.bat`, then start the server from
this directory:

```bash
python main.py config_x509.yaml
```

The default endpoint is `opc.tcp://0.0.0.0:48620/ua_mocker/` and only accepts
`Basic256Sha256_SignAndEncrypt` connections with a certificate issued by the
test CA in `test_material/certs/trust/`.

## Certificate boundaries

`test_material/certs/` is a development and test PKI. It contains the CA key,
server key, two trusted client identities and one deliberately untrusted
identity. It must never be copied as a production delivery.

For each real client, issue and deliver only:

1. that client's certificate and private key;
2. the server certificate;
3. the endpoint and security-policy parameters.

Never distribute `ca_key.pem` or `server_key.pem`. `client2` and `untrusted`
exist solely for local positive and negative tests.

## Local verification

With the server running, use these commands from the indicated directories:

```bash
python client/client_demo.py
python test_material/test_client.py
python test_material/test_negative.py
```

`test_material/gen_certs.py` creates a fresh test PKI; `gen_client_cert.py`
adds the second trusted client identity.

See [client/CLIENT_README.md](client/CLIENT_README.md) for the customer-facing
connection guide.
