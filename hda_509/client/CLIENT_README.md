# Client connection guide

Connect to the OPC UA server with `Basic256Sha256_SignAndEncrypt` and three
files supplied for that client:

1. the client's certificate;
2. the matching client private key;
3. the server certificate.

The server validates the client certificate against its trust store. The
client validates the server certificate, so all three files are required.

The repository demo is [client_demo.py](client_demo.py). For local testing it
reads the test-only `client2` credentials from `../test_material/certs/`.
When handing the demo to an actual client, replace those three paths with the
client-specific files supplied by the deployment owner and set the endpoint to
the deployed server address.

Do not distribute the CA private key, server private key, a second client's
private key, or the repository's `test_material/` directory.

Minimal asyncua configuration:

```python
await client.set_security(
    SecurityPolicyBasic256Sha256,
    client_certificate,
    client_private_key,
    server_certificate=server_certificate,
    mode=ua.MessageSecurityMode.SignAndEncrypt,
)
```
