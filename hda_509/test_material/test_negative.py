# -*- coding: utf-8 -*-
"""
负面验证：确认服务端强制 X.509 认证。
1) 无安全连接(NoSecurity) -> 应失败
2) 使用未受信证书(自签,不在 trust 目录) -> 应失败(BadCertificateUntrusted)
"""
import asyncio
from pathlib import Path

from asyncua import Client, ua
from asyncua.crypto.security_policies import SecurityPolicyBasic256Sha256, SecurityPolicyNone

URL = "opc.tcp://127.0.0.1:48620/ua_mocker/"
CERTS = Path(__file__).resolve().parent / "certs"


async def try_no_security() -> None:
    client = Client(URL)
    try:
        await client.connect()
        print("[!] NoSecurity 连接成功(不应发生)")
    except Exception as e:
        print(f"[OK] NoSecurity 被拒绝: {type(e).__name__}")
    finally:
        try:
            await client.disconnect()
        except Exception:
            pass


async def try_untrusted() -> None:
    client = Client(URL)
    await client.set_security(
        SecurityPolicyBasic256Sha256,
        CERTS / "untrusted_cert.pem",
        CERTS / "untrusted_key.pem",
        server_certificate=CERTS / "server_cert.pem",
        mode=ua.MessageSecurityMode.SignAndEncrypt,
    )
    try:
        await client.connect()
        print("[!] 未受信证书连接成功(不应发生)")
    except Exception as e:
        print(f"[OK] 未受信证书被拒绝: {type(e).__name__}: {e}")
    finally:
        try:
            await client.disconnect()
        except Exception:
            pass


async def main() -> None:
    await try_no_security()
    await try_untrusted()


if __name__ == "__main__":
    asyncio.run(main())
