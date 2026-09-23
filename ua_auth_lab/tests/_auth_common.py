# -*- coding: utf-8 -*-
"""
UA Auth Lab - shared test helpers.

所有认证测试共用：连接尝试、严格 PASS/FAIL 判定、服务器在线检查、
证书材料路径。

严格规则（与 hda_509 一致）：只有「连接失败 + 发生在预期阶段 + 包含预期
StatusCode」才算 PASS；服务器离线 / ConnectionRefused / DNS 错误一律 FAIL。
"""

import json
import socket
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "client"))

from conn import (  # noqa: E402
    StageFailed,
    read_node,
    setup_client,
    stage_connect,
)

# ---- 默认场景：全拆矩阵（manifest 驱动）---------------------------------
BASE = Path(__file__).resolve().parents[1]
MANIFEST = BASE / "configs" / "matrix" / "manifest.json"
DEFAULT_NODE = "ns=1;s=int32_ch_1"

# 兼容旧引用：默认指向矩阵首个端口（矩阵未生成时退回 48730）
DEFAULT_URL = "opc.tcp://10.30.70.77:48730/ua_auth/"


def load_manifest() -> dict:
    """读取 configs/matrix/manifest.json（生成器产出的端口清单）。"""
    if not MANIFEST.exists():
        raise SystemExit(f"缺少清单: {MANIFEST}\n先运行: python tools/gen_matrix_configs.py")
    return json.loads(MANIFEST.read_text(encoding="utf-8"))


def pick_port(
    auth: str | None = None,
    policy: str | None = None,
    mode: str | None = None,
    validation: str | None = None,
    group: str | None = None,
    *,
    require_encrypted: bool = False,
) -> dict:
    """从 manifest 挑一个满足条件的端口条目。

    require_encrypted=True 时排除 None/None 端点（需要加密通道的用例用）。
    validation: trusted / basic / none（客户端应用证书校验模式）。
    group:      core（核心严格块）/ open（开放接入块）。
    找不到会直接 SystemExit，避免测试"悄悄用了错误端口"。
    """
    manifest = load_manifest()
    for e in manifest["entries"]:
        if auth is not None and e["auth"] != auth:
            continue
        if policy is not None and e["policy"] != policy:
            continue
        if mode is not None and e["mode"] != mode:
            continue
        if validation is not None and e.get("validation") != validation:
            continue
        if group is not None and e.get("group") != group:
            continue
        if require_encrypted and e["policy"] == "None":
            continue
        return e
    raise SystemExit(
        "manifest 中找不到满足条件的端口: "
        f"auth={auth} policy={policy} mode={mode} "
        f"validation={validation} group={group} encrypted={require_encrypted}"
    )
CERTS = BASE / "test_material" / "certs"
SERVER_CERT = CERTS / "server_cert.pem"

# Application Certificate（客户端应用身份）
APP_CERT = CERTS / "client_app_a_cert.pem"
APP_KEY = CERTS / "client_app_a_key.pem"
APP_URI = "urn:example.org:FreeOpcUa:opcua-asyncio"

# User Certificate（用户身份，与 App 证书不同的一对）
USER_CERT = CERTS / "user_cert.pem"
USER_KEY = CERTS / "user_key.pem"

USERNAME = "test"
PASSWORD = "test"

RESULTS: list[tuple[str, str, str]] = []


def report(case: str, outcome: str, detail: str = "") -> None:
    print(f"  [{outcome}] {case} {('-> ' + detail) if detail else ''}")
    RESULTS.append((case, outcome, detail))


def summarize(title: str) -> int:
    """打印汇总并返回退出码（有 FAIL 返回 1）。"""
    print(f"\n{title}汇总：")
    failed = [c for c, o, _ in RESULTS if o.startswith("FAIL")]
    for case, outcome, detail in RESULTS:
        print(f"  {outcome:5} {case}")
    if failed:
        print(f"\n{len(failed)} 个用例失败。")
        return 1
    print(f"\n全部 {len(RESULTS)} 个用例通过。")
    return 0


def server_online(url: str = DEFAULT_URL) -> bool:
    from urllib.parse import urlparse

    parsed = urlparse(url)
    try:
        with socket.create_connection((parsed.hostname, parsed.port), timeout=3):
            return True
    except OSError:
        return False


def is_network_error(err) -> bool:
    text = f"{type(err).__name__}: {err}".lower()
    if isinstance(err, (ConnectionRefusedError, ConnectionResetError, socket.gaierror, TimeoutError, OSError)):
        return True
    return any(
        k in text
        for k in ("connection refused", "name or service not known", "timed out", "connect call failed")
    )


class Attempt:
    def __init__(self, ok: bool, stage: str = "", error: str = ""):
        self.ok, self.stage, self.error = ok, stage, error

    @property
    def network_error(self) -> bool:
        return is_network_error(self.error)


async def attempt(
    *,
    auth: str = "anon",
    policy: str = "Basic256Sha256",
    mode: str = "SignAndEncrypt",
    app_cert: Path = APP_CERT,
    app_key: Path = APP_KEY,
    app_uri: str | None = APP_URI,
    user_cert: Path = USER_CERT,
    user_key: Path = USER_KEY,
    username: str = USERNAME,
    password: str = PASSWORD,
    url: str = DEFAULT_URL,
    node: str = DEFAULT_NODE,
    read: bool = True,
    print_steps: bool = False,
    skip_endpoints: bool = False,
) -> Attempt:
    """尝试一次认证连接，返回 Attempt。"""
    try:
        client = await setup_client(
            url,
            app_cert=app_cert,
            app_key=app_key,
            server_cert=SERVER_CERT,
            policy_name=policy,
            mode_name=mode,
            application_uri=app_uri,
        )
        await stage_connect(
            client,
            auth=auth,
            username=username,
            password=password,
            user_cert=user_cert,
            user_key=user_key,
            policy_name=policy,
            mode_name=mode,
            print_steps=print_steps,
            skip_endpoints=skip_endpoints,
        )
        if read:
            val = await read_node(client, node)
            detail = f"read {node}={val!r}"
        else:
            detail = "activated"
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass
        return Attempt(ok=True, error=detail)
    except StageFailed as e:
        return Attempt(ok=False, stage=e.stage, error=f"{type(e.cause).__name__}: {e.cause}")
    except Exception as e:  # noqa: BLE001
        return Attempt(ok=False, error=f"{type(e).__name__}: {e}")
