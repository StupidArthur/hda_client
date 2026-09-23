#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 全拆矩阵端口启停控制。

按 configs/matrix/manifest.json 批量启动 / 停止 / 查询矩阵端口的服务器进程。

特性：

    * 每个端口独立进程，日志按端口隔离（环境变量 UA_MOCK_LOG_SUFFIX=_<port>）
    * start 幂等：已在监听的端口跳过，不会重复起进程
    * stop 按 pid 文件精确终止本工具启动的进程（不误杀其它 python）
    * pid 文件: configs/matrix/.pids.json（stop/status 共用）

用法：

    python tools/matrix_ctl.py start [--only 48730,48731] [--no-wait] [--allow-insecure]
    python tools/matrix_ctl.py status
    python tools/matrix_ctl.py stop

退出码：0 成功；1 失败。
"""

import argparse
import json
import socket
import subprocess
import sys
import time
from pathlib import Path

BASE_DIR = Path(__file__).resolve().parents[1]
MANIFEST = BASE_DIR / "configs" / "matrix" / "manifest.json"
PID_FILE = BASE_DIR / "configs" / "matrix" / ".pids.json"
PYTHON = sys.executable


def _process_fingerprint(pid: int) -> str | None:
    """读取进程的可执行文件和创建时间，用于识别 PID 复用。"""
    try:
        if sys.platform == "win32":
            import ctypes
            from ctypes import wintypes

            PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
            kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
            kernel32.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
            kernel32.OpenProcess.restype = wintypes.HANDLE
            kernel32.QueryFullProcessImageNameW.argtypes = [
                wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR,
                ctypes.POINTER(wintypes.DWORD),
            ]
            kernel32.QueryFullProcessImageNameW.restype = wintypes.BOOL
            kernel32.GetProcessTimes.argtypes = [
                wintypes.HANDLE, ctypes.POINTER(wintypes.FILETIME),
                ctypes.POINTER(wintypes.FILETIME), ctypes.POINTER(wintypes.FILETIME),
                ctypes.POINTER(wintypes.FILETIME),
            ]
            kernel32.GetProcessTimes.restype = wintypes.BOOL
            kernel32.CloseHandle.argtypes = [wintypes.HANDLE]
            kernel32.CloseHandle.restype = wintypes.BOOL
            handle = kernel32.OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION, False, pid
            )
            if not handle:
                return None
            try:
                size = wintypes.DWORD(32768)
                buf = ctypes.create_unicode_buffer(size.value)
                if not kernel32.QueryFullProcessImageNameW(
                    handle, 0, buf, ctypes.byref(size)
                ):
                    return None
                created, exited, kernel, user = (wintypes.FILETIME() for _ in range(4))
                if not kernel32.GetProcessTimes(
                    handle, ctypes.byref(created), ctypes.byref(exited),
                    ctypes.byref(kernel), ctypes.byref(user),
                ):
                    return None
                ticks = (created.dwHighDateTime << 32) | created.dwLowDateTime
                return f"{buf.value.lower()}|{ticks}"
            finally:
                kernel32.CloseHandle(handle)
        exe = Path(f"/proc/{pid}/exe").resolve()
        stat = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8")
        # comm is parenthesized and may itself contain spaces; parse fields only
        # after its final ')' so field 22 (starttime) stays in a fixed position.
        fields = stat[stat.rfind(")") + 2:].split()
        return f"{exe}|{fields[19]}"
    except (OSError, ValueError):
        return None


def _record_matches(record: object, config_path: Path) -> bool:
    """确认 PID 仍是本工具以目标组态启动的 main.py。"""
    if (not isinstance(record, dict) or not isinstance(record.get("pid"), int)
            or isinstance(record.get("pid"), bool) or record["pid"] <= 0):
        return False
    expected_config = str(config_path.resolve()).lower()
    fingerprint = record.get("fingerprint")
    if not isinstance(fingerprint, str) or not fingerprint:
        return False
    config = record.get("config")
    return (
        isinstance(config, str) and config.lower() == expected_config
        and fingerprint == _process_fingerprint(record["pid"])
    )


def load_manifest() -> dict:
    if not MANIFEST.exists():
        raise SystemExit(f"缺少清单: {MANIFEST}\n先运行: python tools/gen_matrix_configs.py")
    return json.loads(MANIFEST.read_text(encoding="utf-8"))


def _listening(port: int, timeout: float = 0.4) -> bool:
    """本机健康检查：探自己起的进程，故意用回环地址。

    与 configs/matrix.yaml 的 client_host 无关 —— 那个只决定"客户端/清单
    url 里写什么地址"（远程验证用），服务端始终绑 0.0.0.0。本机自检走
    127.0.0.1 可避开网卡/防火墙/路由等无关因素。
    """
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=timeout):
            return True
    except OSError:
        return False


def _save_pids(data: dict) -> None:
    PID_FILE.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")


def _load_pids() -> dict:
    if PID_FILE.exists():
        try:
            return json.loads(PID_FILE.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError):
            return {}
    return {}


def select_entries(manifest: dict, only: str | None) -> list[dict]:
    if not only:
        return manifest["entries"]
    wanted = {int(x) for x in only.replace(" ", "").split(",") if x}
    picked = [e for e in manifest["entries"] if e["port"] in wanted]
    missing = wanted - {e["port"] for e in picked}
    if missing:
        raise SystemExit(f"清单中不存在端口: {sorted(missing)}")
    return picked


def cmd_start(args) -> int:
    manifest = load_manifest()
    entries = select_entries(manifest, args.only)
    insecure = [e["port"] for e in entries if e.get("validation") == "none"]
    if insecure and not args.allow_insecure:
        print(
            "[FAIL] 本次包含完全不校验客户端应用证书的端口: "
            f"{insecure}\n确认用于隔离实验环境后追加 --allow-insecure。",
            file=sys.stderr,
        )
        return 1
    pids = _load_pids()

    # Preflight the whole selection before creating any process, so a busy port
    # or missing config cannot leave a partially started matrix behind.
    for e in entries:
        config_path = BASE_DIR / e["config"]
        if not config_path.is_file():
            print(f"[FAIL] {e['port']}: 组态不存在 {e['config']}", file=sys.stderr)
            return 1
        if _listening(e["port"]) and not _record_matches(pids.get(str(e["port"])), config_path):
            print(
                f"[FAIL] {e['port']}: 端口已被非本次记录的进程占用，拒绝当作实验服务",
                file=sys.stderr,
            )
            return 1

    started: list[tuple[int, int]] = []
    skipped: list[int] = []
    for e in entries:
        port = e["port"]
        config_path = BASE_DIR / e["config"]
        if _listening(port):
            skipped.append(port)
            continue
        env = {"UA_MOCK_LOG_SUFFIX": f"_{port}"}
        import os
        proc_env = dict(os.environ)
        proc_env.update(env)
        creationflags = 0
        if sys.platform == "win32":
            # 新进程组 + 不弹窗；失败时回退为普通后台
            creationflags = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
            creationflags |= getattr(subprocess, "BELOW_NORMAL_PRIORITY_CLASS", 0)
        try:
            p = subprocess.Popen(
                [PYTHON, "main.py", e["config"]],
                cwd=str(BASE_DIR),
                env=proc_env,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                creationflags=creationflags,
            )
        except OSError as exc:
            print(f"[FAIL] {port}: 启动失败 {exc}", file=sys.stderr)
            for started_port, started_pid in started:
                if _process_fingerprint(started_pid) is None:
                    continue
                if sys.platform == "win32":
                    subprocess.run(
                        ["taskkill", "/PID", str(started_pid), "/T", "/F"],
                        capture_output=True, check=False,
                    )
                else:
                    import os
                    os.kill(started_pid, 15)
                pids.pop(str(started_port), None)
            _save_pids(pids)
            return 1
        pids[str(port)] = {
            "pid": p.pid,
            "config": str(config_path.resolve()),
            "fingerprint": _process_fingerprint(p.pid),
            "started_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        }
        started.append((port, p.pid))

    _save_pids(pids)

    if args.no_wait:
        print(f"已请求启动 {len(started)} 个端口（不等待就绪）")
        if skipped:
            print(f"已在监听，跳过 {len(skipped)} 个: {skipped}")
        return 0

    # 等待就绪
    deadline = time.time() + 30
    pending = {p for p, _ in started}
    while pending and time.time() < deadline:
        time.sleep(0.5)
        pending = {p for p in pending if not _listening(p)}

    ok = [p for p, _ in started if p not in pending]
    bad = sorted(pending)

    print(f"启动完成: 成功 {len(ok)} / 请求 {len(started)}")
    if skipped:
        print(f"已在监听，跳过 {len(skipped)} 个: {skipped}")
    if bad:
        print(f"[FAIL] 未就绪端口: {bad}", file=sys.stderr)
        for port in bad:
            log = BASE_DIR / f"ua_mocker_{time.strftime('%Y%m%d')}_{port}.log"
            if log.exists():
                tail = log.read_text(encoding="utf-8", errors="replace").splitlines()[-5:]
                print(f"  --- {log.name} 末尾 ---", file=sys.stderr)
                for line in tail:
                    print(f"    {line}", file=sys.stderr)
        return 1

    print(f"全部就绪: {manifest['base_port']}..{manifest['entries'][-1]['port']}"
          f"  ({manifest['port_count']} 端口)")
    return 0


def cmd_status(_args) -> int:
    manifest = load_manifest()
    pids = _load_pids()
    up, down, foreign = [], [], []
    for e in manifest["entries"]:
        port = e["port"]
        if not _listening(port):
            down.append(port)
        elif _record_matches(pids.get(str(port)), BASE_DIR / e["config"]):
            up.append(port)
        else:
            foreign.append(port)
    print(f"监听中 {len(up)} / 共 {manifest['port_count']}")
    if down:
        print(f"未监听 {len(down)} 个: {down}")
    if foreign:
        print(f"端口被未识别进程占用 {len(foreign)} 个: {foreign}")
    return 0 if not down and not foreign else 1


def cmd_stop(_args) -> int:
    pids = _load_pids()
    if not pids:
        print("无 pid 记录（configs/matrix/.pids.json），未停止任何进程")
        return 0

    stopped, failed, not_running = [], [], []
    remaining = dict(pids)
    manifest = load_manifest()
    for port_str, record in sorted(pids.items(), key=lambda kv: int(kv[0])):
        port = int(port_str)
        entry = next((e for e in manifest["entries"] if e["port"] == port), None)
        if entry is None:
            failed.append((port, 0, "端口不在当前矩阵清单中，拒绝终止"))
            continue
        config_path = BASE_DIR / entry["config"]
        if not isinstance(record, dict) or not isinstance(record.get("pid"), int):
            failed.append((port, 0, "旧版或损坏的 PID 记录；为避免误杀，请人工确认"))
            continue
        pid = record["pid"]
        if not _pid_alive(pid):
            not_running.append(port)
            remaining.pop(port_str, None)
            continue
        if not _record_matches(record, config_path):
            failed.append((port, pid, "PID 存在但命令行不匹配，拒绝终止"))
            continue
        try:
            if sys.platform == "win32":
                result = subprocess.run(
                    ["taskkill", "/PID", str(pid), "/T", "/F"],
                    capture_output=True, text=True, encoding="utf-8", errors="replace",
                    check=False,
                )
                if result.returncode != 0:
                    detail = (result.stderr or result.stdout or "taskkill failed").strip()
                    raise OSError(detail)
            else:
                import os
                os.kill(pid, 15)
            deadline = time.time() + 5
            while _pid_alive(pid) and time.time() < deadline:
                time.sleep(0.1)
            if _pid_alive(pid):
                raise OSError("进程在终止命令后仍存活")
            stopped.append(port)
            remaining.pop(port_str, None)
        except OSError as exc:
            failed.append((port, pid, str(exc)))
    _save_pids(remaining)

    print(f"已停止 {len(stopped)} 个进程")
    if not_running:
        print(f"本就未运行 {len(not_running)} 个: {not_running}")
    if failed:
        for port, pid, err in failed:
            print(f"[FAIL] {port} (pid {pid}): {err}", file=sys.stderr)
        return 1
    return 0


def _pid_alive(pid: int) -> bool:
    return _process_fingerprint(pid) is not None


def main() -> int:
    parser = argparse.ArgumentParser(description="矩阵端口启停控制")
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_start = sub.add_parser("start", help="启动全部端口")
    p_start.add_argument("--only", default=None, help="只启动指定端口(逗号分隔)，如 48730,48731")
    p_start.add_argument("--no-wait", action="store_true", help="不等待端口就绪")
    p_start.add_argument(
        "--allow-insecure", action="store_true",
        help="允许启动 client_cert_validation=none 的开放端口",
    )
    p_start.set_defaults(func=cmd_start)

    p_status = sub.add_parser("status", help="查看监听状态")
    p_status.set_defaults(func=cmd_status)

    p_stop = sub.add_parser("stop", help="停止全部端口")
    p_stop.set_defaults(func=cmd_stop)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
