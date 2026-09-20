# -*- coding: utf-8 -*-
"""
组态加载：从 YAML 文件读取 OPC UA X.509 Compatibility Mocker 配置并做基本校验。

支持两种风格：

* 新版结构（推荐）：

    scenario: normal
    server: "0.0.0.0"
    port: 48620
    endpoint_path: "/ua_mocker/"
    application:
      name: "..."
      uri: "urn:freeopcua:python:server"
    security:
      policies: [ ... ]
      application_certificate:
        cert: "..."
        private_key: "..."
      trust_store: "..."
    user_auth:
      anonymous: true
      username: true
      users: [{username: test, password: test}]
      x509: true
      x509_user_cert: "..."

* 旧版结构（向后兼容，仍可用）：

    server: "0.0.0.0"
    port: 48620
    security_policy: ["Basic256Sha256_SignAndEncrypt"]
    cert: "..."
    private_key: "..."
    trust_store: "..."

所有证书/私钥/trust 路径都会解析为绝对路径（相对组态文件所在目录），
因此把组态拷贝到其它目录也能正常工作。
"""

import logging
from pathlib import Path
from typing import Any

import yaml

logger = logging.getLogger(__name__)

# 组态中必填顶层键
REQUIRED_TOP_KEYS = ("server", "port", "cycle", "namespace_index", "nodes")
# 节点项必填键
REQUIRED_NODE_KEYS = ("name", "type", "count", "change", "writable")

# 路径类键：加载后统一解析为绝对路径（相对组态文件所在目录）
_PATH_KEYS = ("cert", "private_key", "trust_store", "x509_user_cert")


def _to_abs(base: Path, value: Any) -> str:
    """把配置里的相对路径解析为基于组态文件目录的绝对路径字符串。"""
    candidate = Path(str(value))
    return str(candidate if candidate.is_absolute() else base / candidate)


def _resolve_paths(base: Path, cfg: dict[str, Any]) -> None:
    """就地解析路径键。支持新版 security.* 与旧版顶层键两种位置。"""
    for key in _PATH_KEYS:
        value = cfg.get(key)
        if value:
            cfg[key] = _to_abs(base, value)

    security = cfg.get("security")
    if isinstance(security, dict):
        app_cert = security.get("application_certificate")
        if isinstance(app_cert, dict):
            for key in ("cert", "private_key"):
                if app_cert.get(key):
                    app_cert[key] = _to_abs(base, app_cert[key])
        if security.get("trust_store"):
            security["trust_store"] = _to_abs(base, security["trust_store"])

    user_auth = cfg.get("user_auth")
    if isinstance(user_auth, dict):
        value = user_auth.get("x509_user_cert")
        if isinstance(value, list):
            user_auth["x509_user_cert"] = [_to_abs(base, v) for v in value]
        elif value:
            user_auth["x509_user_cert"] = _to_abs(base, value)


def load_config(config_path: str | Path) -> dict[str, Any]:
    """
    从 YAML 文件加载组态。

    :param config_path: 组态文件路径
    :return: 组态字典
    :raises FileNotFoundError: 文件不存在
    :raises ValueError: 格式或必填项缺失
    """
    path = Path(config_path)
    if not path.is_file():
        raise FileNotFoundError(f"组态文件不存在: {path}")

    raw = path.read_text(encoding="utf-8")
    try:
        cfg = yaml.safe_load(raw)
    except yaml.YAMLError as e:
        raise ValueError(f"YAML 解析失败: {e}") from e

    if not isinstance(cfg, dict):
        raise ValueError("组态根节点必须为键值对")

    for key in REQUIRED_TOP_KEYS:
        if key not in cfg:
            raise ValueError(f"组态缺少必填项: {key}")

    if not isinstance(cfg["nodes"], list):
        raise ValueError("组态中 nodes 必须为列表")

    for i, node in enumerate(cfg["nodes"]):
        if not isinstance(node, dict):
            raise ValueError(f"nodes[{i}] 必须为键值对")
        for k in REQUIRED_NODE_KEYS:
            if k not in node:
                raise ValueError(f"nodes[{i}] 缺少必填项: {k}")
        if node["change"] is False and "default" not in node:
            raise ValueError(f"nodes[{i}] change=false 时必须提供 default")

    _resolve_paths(path.parent, cfg)
    logger.info("组态加载成功: %s", path)
    return cfg
