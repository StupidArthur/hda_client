# -*- coding: utf-8 -*-
"""
验证客户端：使用 X.509 客户端证书 + Basic256Sha256 连接服务端，并读取节点值。
"""
import asyncio
from pathlib import Path

from asyncua import Client, ua
from asyncua.crypto.security_policies import SecurityPolicyBasic256Sha256

URL = "opc.tcp://127.0.0.1:48620/ua_mocker/"
CERTS = Path(__file__).resolve().parent / "certs"
SERVER_CERT = CERTS / "server_cert.pem"
CLIENT_CERT = CERTS / "client_cert.pem"
CLIENT_KEY = CERTS / "client_key.pem"
NODES = [
    "ns=1;s=int32_ch_1",
    "ns=1;s=double_wr_1",
    "ns=1;s=string_ch_1",
    "ns=1;s=bool_ch_1",
    "ns=1;s=datetime_ch_1",
]


async def main() -> None:
    client = Client(URL)
    await client.set_security(
        SecurityPolicyBasic256Sha256,
        CLIENT_CERT,
        CLIENT_KEY,
        server_certificate=SERVER_CERT,
        mode=ua.MessageSecurityMode.SignAndEncrypt,
    )
    await client.connect()
    print("X.509 连接成功:", URL)
    for node_str in NODES:
        node = client.get_node(node_str)
        val = await node.read_value()
        print(f"{node_str} = {val!r}")
    await client.disconnect()
    print("已断开")


if __name__ == "__main__":
    asyncio.run(main())
