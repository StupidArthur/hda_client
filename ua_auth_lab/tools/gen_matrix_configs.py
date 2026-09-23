#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 全拆（方案 A）端口配置生成器。

读取 configs/matrix.yaml（矩阵唯一真源），生成：

    configs/matrix/p<port>_<policy>_<mode>_<auth>.yaml   每端口一个组态
    configs/matrix/manifest.json                          端口清单（测试/启停用）

设计要点（对应"一端口一场景"）：

    * 每个端口只发布 1 种端点组合（SecurityPolicy × MessageSecurityMode）
    * 每个端口只开放 1 种用户认证方式（anonymous / username / x509）
    * 其余认证方式、其余端点组合在该端口必须验证失败

端口/策略/认证维度全部由 configs/matrix.yaml 驱动，改矩阵只需改它再重跑本脚本。

用法：

    python tools/gen_matrix_configs.py
    python tools/gen_matrix_configs.py --config configs/matrix.yaml --out configs/matrix

退出码：0 成功；1 校验或生成失败。
"""

import argparse
import hashlib
import json
import sys
from datetime import datetime
from pathlib import Path
from typing import Any

import yaml

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))
sys.path.insert(0, str(BASE_DIR / "client"))

# 服务端支持的策略名（含 11 个合法端点组合所需的全部名字）
from server_builder import POLICY_NAME_TO_TYPE  # noqa: E402

# 客户端支持的策略名 / 模式名
from conn import MODES, POLICY_CLASSES  # noqa: E402

AUTH_NAMES = ("anon", "username", "x509")

# 客户端应用证书校验模式（与服务端 server_builder.CLIENT_CERT_VALIDATION_MODES 对应）
VALIDATION_MODES = ("trusted", "basic", "none")

# 端点组合的"合法模式"约束：policy=None 只能配 mode=None；
# 加密 policy 只能配 Sign / SignAndEncrypt。
_VALID_MODES_FOR_POLICY = {
    "None": {"None"},
}


class MatrixError(Exception):
    """矩阵定义校验失败。"""


def cfg_auth_x509(auth: dict[str, Any]) -> bool:
    """该认证方式是否开放 x509。"""
    return bool((auth.get("user_auth") or {}).get("x509"))


def cfg_auth_username(auth: dict[str, Any]) -> bool:
    """该认证方式是否开放 username。"""
    return bool((auth.get("user_auth") or {}).get("username"))


# ---------------------------------------------------------------------------
# 校验
# ---------------------------------------------------------------------------

def _server_policy_name(policy: str, mode: str) -> str:
    """把 (policy, mode) 映射到服务端 POLICY_NAME_TO_TYPE 的键。"""
    if policy == "None" and mode == "None":
        return "NoSecurity"
    return f"{policy}_{mode}"


def validate(matrix: dict[str, Any]) -> list[dict[str, Any]]:
    """校验矩阵定义，返回规范化后的端点组合列表。"""
    errors: list[str] = []

    base_port = matrix.get("base_port")
    if not isinstance(base_port, int) or not (1 <= base_port <= 65535):
        errors.append(f"base_port 必须是 1..65535 的整数，得到: {base_port!r}")
    # 客户端 url 主机：远程验证不能用回环地址（对方机器上 127.0.0.1 是它自己）
    client_host = str(matrix.get("client_host", "127.0.0.1")).strip()
    if not client_host:
        errors.append("client_host 不能为空")
    elif client_host in ("127.0.0.1", "localhost", "::1"):
        # 允许，但明确警告：远程机器连不上
        import warnings
        warnings.warn(
            f"client_host={client_host!r} 是回环地址，远程机器无法用该 url 连接；"
            f"若需远程验证请改成真实 IP（服务端绑定不受影响）",
            stacklevel=2,
        )
    step = matrix.get("port_step", 1)
    if not isinstance(step, int) or step < 1:
        errors.append(f"port_step 必须是 >=1 的整数，得到: {step!r}")

    endpoints = matrix.get("endpoints")
    if not isinstance(endpoints, list) or not endpoints:
        errors.append("endpoints 必须是非空列表")
        endpoints = []

    normalized: list[dict[str, Any]] = []
    for i, ep in enumerate(endpoints):
        if not isinstance(ep, dict):
            errors.append(f"endpoints[{i}] 必须是映射，得到: {ep!r}")
            continue
        policy = str(ep.get("policy", ""))
        mode = str(ep.get("mode", ""))
        if not policy or not mode:
            errors.append(f"endpoints[{i}] 缺少 policy/mode: {ep!r}")
            continue

        # 模式合法性
        allowed = _VALID_MODES_FOR_POLICY.get(policy)
        if allowed is not None:
            if mode not in allowed:
                errors.append(
                    f"endpoints[{i}]: SecurityPolicy None 只能配 MessageSecurityMode None，"
                    f"得到 {policy}/{mode}"
                )
        elif mode not in ("Sign", "SignAndEncrypt"):
            errors.append(
                f"endpoints[{i}]: 加密策略只能配 Sign 或 SignAndEncrypt，得到 {policy}/{mode}"
            )

        # 服务端策略名支持
        srv_name = _server_policy_name(policy, mode)
        if srv_name not in POLICY_NAME_TO_TYPE:
            errors.append(
                f"endpoints[{i}]: 服务端不支持策略名 {srv_name!r}；"
                f"可选: {sorted(POLICY_NAME_TO_TYPE)}"
            )

        # 客户端策略类支持
        if policy not in POLICY_CLASSES:
            errors.append(
                f"endpoints[{i}]: 客户端 conn.POLICY_CLASSES 不含 {policy!r}；"
                f"可选: {sorted(POLICY_CLASSES)}"
            )
        if mode not in MODES:
            errors.append(f"endpoints[{i}]: 客户端 conn.MODES 不含 {mode!r}")

        normalized.append(
            {
                "policy": policy,
                "mode": mode,
                "server_policy": srv_name,
                "deprecated": bool(ep.get("deprecated", False)),
            }
        )

    auths = matrix.get("auths")
    if not isinstance(auths, list) or not auths:
        errors.append("auths 必须是非空列表")
        auths = []
    for i, a in enumerate(auths):
        if not isinstance(a, dict) or "name" not in a:
            errors.append(f"auths[{i}] 必须含 name: {a!r}")
            continue
        if a["name"] not in AUTH_NAMES:
            errors.append(f"auths[{i}].name 必须是 {AUTH_NAMES} 之一，得到 {a['name']!r}")
        ua = a.get("user_auth")
        if not isinstance(ua, dict):
            errors.append(f"auths[{i}] 缺少 user_auth 映射")
            continue
        enabled = [k for k in AUTH_NAMES if ua.get(
            {"anon": "anonymous", "username": "username", "x509": "x509"}[k]
        )]
        if len(enabled) != 1 or enabled[0] != a["name"]:
            errors.append(
                f"auths[{i}] ({a['name']}) 的 user_auth 必须且只能开放它自己，"
                f"当前开放: {enabled}（一端口一方式）"
            )

    # 开放端口块（可选）
    _auth_names = [a["name"] for a in auths if isinstance(a, dict) and "name" in a]
    _core_ports: set[int] = set()
    if isinstance(base_port, int) and isinstance(step, int) and step >= 1:
        _core_ports = {
            base_port + i * step for i in range(len(normalized) * len(_auth_names))
        }
    _validate_open_ports(matrix, errors, normalized, _auth_names, _core_ports, step)

    if errors:
        raise MatrixError("矩阵定义校验失败:\n  - " + "\n  - ".join(errors))

    return normalized


def _validate_open_ports(
    matrix: dict[str, Any],
    errors: list[str],
    endpoints: list[dict[str, Any]],
    auth_names: list[str],
    core_ports: set[int],
    step: int,
) -> None:
    """校验可选的 open_ports 块（宽松校验端口）。"""
    op = matrix.get("open_ports")
    if op is None:
        return
    if not isinstance(op, dict):
        errors.append(f"open_ports 必须是映射，得到: {op!r}")
        return

    base = op.get("base_port")
    if not isinstance(base, int) or not (1 <= base <= 65535):
        errors.append(f"open_ports.base_port 必须是 1..65535 的整数，得到: {base!r}")
        base = None

    vals = op.get("validations", ["basic", "none"])
    if not isinstance(vals, list) or not vals:
        errors.append("open_ports.validations 必须是非空列表")
        vals = []
    for v in vals:
        if v not in VALIDATION_MODES:
            errors.append(f"open_ports.validations 含非法值 {v!r}，可选: {VALIDATION_MODES}")
    if "trusted" in vals:
        errors.append(
            "open_ports.validations 不应包含 'trusted'（核心端口已覆盖严格模式，"
            "开放块只放 basic / none）"
        )

    op_auths = op.get("auths", ["anon", "username"])
    if not isinstance(op_auths, list) or not op_auths:
        errors.append("open_ports.auths 必须是非空列表")
        op_auths = []
    for a in op_auths:
        if a not in auth_names:
            errors.append(f"open_ports.auths 含未定义认证方式 {a!r}，可选: {auth_names}")

    # 端口重叠检查
    if base is not None and vals and op_auths:
        n = len(endpoints) * len([a for a in op_auths if a in auth_names]) * len(vals)
        span = {base + i * step for i in range(n)}
        clash = sorted(span & core_ports)
        if clash:
            errors.append(f"open_ports 与核心端口重叠: {clash}")


def open_ports_values(matrix: dict[str, Any]) -> dict[str, Any] | None:
    """取 open_ports 规范化值（validate 已校验；这里再兜一层默认）。"""
    op = matrix.get("open_ports")
    if not isinstance(op, dict):
        return None
    return {
        "base_port": int(op.get("base_port", 0)),
        "validations": [str(v) for v in op.get("validations", ["basic", "none"])],
        "auths": [str(a) for a in op.get("auths", ["anon", "username"])],
    }


# ---------------------------------------------------------------------------
# 生成
# ---------------------------------------------------------------------------

def _safe(s: str) -> str:
    """用于文件名（Windows 上策略名含大写/驼峰，直接可用）。"""
    return s.replace("/", "_").replace(" ", "")


def _build_config(matrix: dict[str, Any], ep: dict[str, Any], auth: dict[str, Any],
                  port: int, validation: str = "trusted") -> dict[str, Any]:
    """构造单个端口的组态字典（新版结构）。"""
    if validation not in VALIDATION_MODES:
        raise MatrixError(f"非法 client_cert_validation: {validation!r}")
    is_none = ep["policy"] == "None" and ep["mode"] == "None"

    security: dict[str, Any] = dict(matrix["security"])
    security["client_cert_validation"] = validation
    security["policies"] = [ep["server_policy"]]

    # "None/None 且开放 x509 / username" 需要额外注入一个带签名(和加密)能力的
    # 策略，否则 asyncua._set_endpoints() 在 mode==None_ 分支里：
    #   * x509    -> 找不到签名策略 -> 不把 X509IdentityToken 加进端点
    #                 -> UserIdentityTokens=[] -> X509 用户认证不可用
    #   * username -> 找不到加密策略 -> 密码明文传输（asyncua 会打 warning）
    # 注入后再用 only_none_endpoint 把该加密端点/通道从对外面摘掉，
    # 端口对外仍只暴露 None/None 一个端点、只接受 None 通道（隔离语义不变）。
    injected_policy: str | None = None
    if is_none and (cfg_auth_x509(auth) or cfg_auth_username(auth)):
        injected_policy = f"{matrix.get('signing_policy', 'Basic256Sha256')}_SignAndEncrypt"
        if injected_policy not in POLICY_NAME_TO_TYPE:
            raise MatrixError(
                f"matrix.yaml 的 signing_policy={injected_policy!r} 不被服务端支持；"
                f"可选: {sorted(POLICY_NAME_TO_TYPE)}"
            )
        security["policies"] = [ep["server_policy"], injected_policy]
        security["only_none_endpoint"] = True

    if is_none:
        # 不加密端口：
        #   * keep_none_endpoint=true —— 否则 DiscoveryCleanServer 会把
        #     None/None 端点从 GetEndpoints 移除，客户端选不到端点
        #   * discovery=false —— 本端口自身就是 None 端点，无需额外 discovery
        #   * require_secured_session=false —— 允许在无保护通道上建 Session
        security["keep_none_endpoint"] = True
        security["discovery"] = False
        security["require_secured_session"] = False
    else:
        # 加密端口：discovery 仍发布 None/None 供第三方做 unsecured GetEndpoints，
        # 但 Session 必须走加密端点；DiscoveryCleanServer 会把 None/None 从
        # GetEndpoints 的 Session 端点里清掉（keep_none_endpoint 缺省 false）。
        security["discovery"] = True
        security["require_secured_session"] = True

    cfg: dict[str, Any] = {
        "scenario": (
            f"matrix_{_safe(ep['policy'])}_{_safe(ep['mode'])}_{auth['name']}"
            + (f"_{validation}" if validation != "trusted" else "")
        ),
        "server": "0.0.0.0",
        "port": port,
        "endpoint_path": matrix.get("endpoint_path", "/ua_auth/"),
        "cycle": matrix.get("cycle", 1000),
        "namespace_index": matrix.get("namespace_index", 1),
        "application": dict(matrix.get("application", {})),
        "security": security,
        "user_auth": dict(auth["user_auth"]),
        "nodes": list(matrix.get("nodes", [])),
    }

    # 认证材料：只在对应方式开放时写入（保持配置自解释，虽然开关已显式隔离）
    if cfg["user_auth"].get("username"):
        cfg["user_auth"]["users"] = list(matrix.get("users", []))
    if cfg["user_auth"].get("x509"):
        x509 = matrix.get("x509", {})
        cfg["user_auth"]["x509_user_cert"] = list(x509.get("user_cert", []))
        cfg["user_auth"]["x509_user_name"] = x509.get("user_name", "x509_user")
        cfg["user_auth"]["x509_validation"] = "direct"

    return cfg


def _render_yaml(cfg: dict[str, Any], header: str) -> str:
    body = yaml.safe_dump(cfg, allow_unicode=True, sort_keys=False, default_flow_style=False)
    return header + body


def _neg_sample(target: dict[str, Any], matrix: dict[str, Any],
                endpoints: list[dict[str, Any]], auths: list[dict[str, Any]],
                kind: str) -> dict[str, Any]:
    """构造一条负向抽样期望。

    kind='auth'   : 同端点组合，换一种认证方式 -> 期望 ActivateSession 失败
    kind='endpoint': 换一个端点组合，同认证方式 -> 期望在选端点前/选端点时失败
    """
    if kind == "auth":
        # 取矩阵里下一个认证方式（循环），保证确定性
        idx = next(i for i, a in enumerate(auths) if a["name"] == target["auth"])
        other = auths[(idx + 1) % len(auths)]
        return {
            "policy": target["policy"],
            "mode": target["mode"],
            "auth": other["name"],
            "expect_fail_stage": "ActivateSession",
            "reason": "该端口未开放此认证方式",
        }
    # endpoint：取下一个端点组合（循环）
    idx = next(i for i, e in enumerate(endpoints)
               if e["policy"] == target["policy"] and e["mode"] == target["mode"])
    other = endpoints[(idx + 1) % len(endpoints)]
    return {
        "policy": other["policy"],
        "mode": other["mode"],
        "auth": target["auth"],
        "expect_fail_stage": "Endpoint selection",
        "reason": "该端口未发布此端点组合",
    }


def generate(matrix_path: Path, out_dir: Path) -> dict[str, Any]:
    with matrix_path.open("r", encoding="utf-8") as f:
        matrix = yaml.safe_load(f)
    if not isinstance(matrix, dict):
        raise MatrixError(f"矩阵定义必须是映射: {matrix_path}")

    endpoints = validate(matrix)
    auths = matrix["auths"]
    # validate() 已校验过 client_host；这里取值供 url 拼接与 manifest 使用
    client_host = str(matrix.get("client_host", "127.0.0.1")).strip()
    base_port = int(matrix["base_port"])
    step = int(matrix.get("port_step", 1))
    neg_count = int(matrix.get("negative_samples", 2))
    if neg_count not in (0, 1, 2):
        raise MatrixError(f"negative_samples 只支持 0/1/2，得到 {neg_count}")

    out_dir.mkdir(parents=True, exist_ok=True)
    # 清掉旧生成物，避免矩阵变更后残留过期端口配置
    for old in out_dir.glob("p*.yaml"):
        old.unlink()

    entries: list[dict[str, Any]] = []
    used_ports: set[int] = set()

    def emit(ep: dict[str, Any], auth: dict[str, Any], port: int,
             validation: str, group: str) -> None:
        """落盘一个端口组态 + 追加一条清单条目。"""
        if port in used_ports:
            raise MatrixError(f"端口冲突: {port}")
        if port > 65535:
            raise MatrixError(f"端口溢出: {port}")
        used_ports.add(port)

        cfg = _build_config(matrix, ep, auth, port, validation)
        suffix = f"_{validation}" if validation != "trusted" else ""
        fname = (
            f"p{port}_{_safe(ep['policy'])}_{_safe(ep['mode'])}"
            f"_{auth['name']}{suffix}.yaml"
        )
        mode_note = {
            "trusted": "应用证书: 严格（必须受信）",
            "basic": "应用证书: 宽松（不查信任，仍查有效期/URI）",
            "none": "应用证书: 不校验（任意客户端均可接入）",
        }[validation]
        header = (
            f"# UA Auth Lab 全拆矩阵端口（自动生成，勿手改）\n"
            f"# 端口: {port}  端点: {ep['policy']} / {ep['mode']}"
            f"{'  (deprecated)' if ep['deprecated'] else ''}\n"
            f"# 认证: {auth['name']}（该端口只开放这一种）\n"
            f"# {mode_note}\n"
            f"# 源: {matrix_path.name}  重新生成: python tools/gen_matrix_configs.py\n"
            f"# 启动: python main.py configs/matrix/{fname}\n"
            f"\n"
        )
        (out_dir / fname).write_text(_render_yaml(cfg, header), encoding="utf-8")

        target = {
            "port": port,
            "policy": ep["policy"],
            "mode": ep["mode"],
            "auth": auth["name"],
        }
        negs = []
        if neg_count >= 1:
            negs.append(_neg_sample(target, matrix, endpoints, auths, "auth"))
        if neg_count >= 2:
            negs.append(_neg_sample(target, matrix, endpoints, auths, "endpoint"))

        entries.append(
            {
                "port": port,
                "group": group,
                "validation": validation,
                "policy": ep["policy"],
                "mode": ep["mode"],
                "auth": auth["name"],
                "deprecated": ep["deprecated"],
                "server_policy": ep["server_policy"],
                "endpoint_path": matrix.get("endpoint_path", "/ua_auth/"),
                "url": (
                    f"opc.tcp://{client_host}:{port}"
                    f"{matrix.get('endpoint_path', '/ua_auth/')}"
                ),
                "config": f"configs/matrix/{fname}",
                "read_node": matrix.get("read_node", "ns=1;s=int32_ch_1"),
                "negatives": negs,
            }
        )

    # ---- 核心端口块（trusted，严格校验）-----------------------------------
    core_count = 0
    for ep in endpoints:
        for auth in auths:
            emit(ep, auth, base_port + core_count * step, "trusted", "core")
            core_count += 1

    # ---- 开放接入端口块（basic / none，不查信任）--------------------------
    # 编排：validation 优先（basic 块在前、none 块在后），块内按 端点 × 认证。
    open_block = open_ports_values(matrix)
    open_count = 0
    open_base: int | None = None
    if open_block:
        open_base = open_block["base_port"]
        for validation in open_block["validations"]:
            for ep in endpoints:
                for auth in auths:
                    if auth["name"] not in open_block["auths"]:
                        continue
                    emit(ep, auth, open_base + open_count * step, validation, "open")
                    open_count += 1

    manifest = {
        "generated_at": datetime.now().isoformat(timespec="seconds"),
        "source": str(matrix_path.relative_to(BASE_DIR)).replace("\\", "/"),
        "source_sha256": hashlib.sha256(matrix_path.read_bytes()).hexdigest(),
        "client_host": client_host,
        "base_port": base_port,
        "port_step": step,
        "endpoint_count": len(endpoints),
        "auth_count": len(auths),
        "core_port_count": core_count,
        "open_port_count": open_count,
        "open_base_port": open_base,
        "open_validation_modes": open_block["validations"] if open_block else [],
        "open_auths": open_block["auths"] if open_block else [],
        "port_count": len(entries),
        "negative_samples_per_port": neg_count,
        "total_connections": len(entries) * (1 + neg_count),
        "read_node": matrix.get("read_node", "ns=1;s=int32_ch_1"),
        "entries": entries,
    }
    (out_dir / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    return manifest


def main() -> int:
    parser = argparse.ArgumentParser(description="生成全拆矩阵端口配置")
    parser.add_argument("--config", default=str(BASE_DIR / "configs" / "matrix.yaml"))
    parser.add_argument("--out", default=str(BASE_DIR / "configs" / "matrix"))
    args = parser.parse_args()

    try:
        manifest = generate(Path(args.config), Path(args.out))
    except MatrixError as e:
        print(str(e), file=sys.stderr)
        return 1
    except FileNotFoundError as e:
        print(f"文件不存在: {e}", file=sys.stderr)
        return 1

    print(f"生成端口配置: {manifest['port_count']} 个")
    print(f"  端点组合 {manifest['endpoint_count']} × 认证方式 {manifest['auth_count']}")
    print(f"  核心端口 {manifest['core_port_count']} 个（应用证书: trusted 严格）")
    if manifest["open_port_count"]:
        print(
            f"  开放端口 {manifest['open_port_count']} 个"
            f"（应用证书: {'/'.join(manifest['open_validation_modes'])}，不查信任）"
            f"  起始 {manifest['open_base_port']}"
        )
    print(f"  端口 {manifest['base_port']} 起，步长 {manifest['port_step']}")
    print(f"  每端口负向抽样 {manifest['negative_samples_per_port']} 条")
    print(f"  预计连接次数: {manifest['total_connections']}")
    print(f"  输出: {Path(args.out)}")
    print(f"  清单: {Path(args.out) / 'manifest.json'}")
    first, last = manifest["entries"][0], manifest["entries"][-1]
    print(f"  范围: {first['port']} .. {last['port']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
