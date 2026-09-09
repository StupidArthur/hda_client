# -*- coding: utf-8 -*-
"""
独立客户端测试：模拟真实第三方客户端。
使用 CA 为其签发的证书(client2) + Basic256Sha256_SignAndEncrypt 连接，
读取多个节点并写入一个可写节点后再读回，验证读写全链路。
"""
import asyncio
from pathlib import Path

from asyncua import Client, ua
from asyncua.crypto.security_policies import SecurityPolicyBasic256Sha256

URL = "opc.tcp://127.0.0.1:48620/ua_mocker/"

# The repository keeps test-only credentials outside the client package.
# A real delivery contains only this client's certificate, private key and
# the server certificate in a customer-controlled directory.
TEST_CERTS = Path(__file__).resolve().parents[1] / "test_material" / "certs"
SERVER_CERT = TEST_CERTS / "server_cert.pem"
CLIENT_CERT = TEST_CERTS / "client2_cert.pem"
CLIENT_KEY = TEST_CERTS / "client2_key.pem"


async def main() -> None:
    client = Client(URL)
    client.application_uri = "urn:example.org:FreeOpcUa:opcua-asyncio"
    await client.set_security(
        SecurityPolicyBasic256Sha256,
        CLIENT_CERT,
        CLIENT_KEY,
        server_certificate=SERVER_CERT,
        mode=ua.MessageSecurityMode.SignAndEncrypt,
    )
    await client.connect()
    print("连接成功:", URL)

    for node_str in ["ns=1;s=int32_ch_1", "ns=1;s=float_ch_1", "ns=1;s=string_ch_2"]:
        print(f"  读 {node_str} = {await client.get_node(node_str).read_value()!r}")

    write_node = client.get_node("ns=1;s=double_wr_1")
    await write_node.write_value(123.45, varianttype=ua.VariantType.Double)
    print("  写 double_wr_1 = 123.45")
    print(f"  回读 double_wr_1 = {await write_node.read_value()!r}")

    await client.disconnect()
    print("已断开，测试通过")


if __name__ == "__main__":
    asyncio.run(main())
