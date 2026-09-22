#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 全拆矩阵端口启停控制。

按 configs/matrix/manifest.json 批量启动 / 停止 / 查询 33 个端口的服务器进程。

特性：

    * 每个端口独立进程，日志按端口隔离（环境变量 UA_MOCK_LOG_SUFFIX=_<port>）
    * start 幂等：已在监听的端口跳过，不会重复起进程
    * stop 按 pid 文件精确终止本工具启动的进程（不误杀其它 python）
    * pid 文件: configs/matrix/.pids.json（stop/status 共用）

用法：

    python tools/matrix_ctl.py start [--only 48730,48731] [--no-wait]
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


def load_manifest() -> dict:
    if not MANIFEST.exists():
        raise SystemExit(f"缺少清单: {MANIFEST}\n先运行: python tools/gen_matrix_configs.py")
    return json.loads(MANIFEST.read_text(encoding="utf-8"))


def _listening(port: int, timeout: float = 0.4) -> bool:
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
    pids = _load_pids()

    started: list[tuple[int, int]] = []
    skipped: list[int] = []
    for e in entries:
        port = e["port"]
        if _listening(port):
            skipped.append(port)
            continue
        config_path = BASE_DIR / e["config"]
        if not config_path.exists():
            print(f"[FAIL] {port}: 组态不存在 {e['config']}", file=sys.stderr)
            return 1
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
            return 1
        pids[str(port)] = p.pid
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
    up, down = [], []
    for e in manifest["entries"]:
        (up if _listening(e["port"]) else down).append(e["port"])
    print(f"监听中 {len(up)} / 共 {manifest['port_count']}")
    if down:
        print(f"未监听 {len(down)} 个: {down}")
    return 0 if not down else 1


def cmd_stop(_args) -> int:
    pids = _load_pids()
    if not pids:
        print("无 pid 记录（configs/matrix/.pids.json），未停止任何进程")
        return 0

    stopped, failed, not_running = [], [], []
    for port_str, pid in sorted(pids.items(), key=lambda kv: int(kv[0])):
        port = int(port_str)
        if not _listening(port) and not _pid_alive(pid):
            not_running.append(port)
            continue
        try:
            if sys.platform == "win32":
                subprocess.run(
                    ["taskkill", "/PID", str(pid), "/T", "/F"],
                    capture_output=True, check=False,
                )
            else:
                import os
                os.kill(pid, 15)
            stopped.append(port)
        except OSError as exc:
            failed.append((port, pid, str(exc)))
    _save_pids({})

    print(f"已停止 {len(stopped)} 个进程")
    if not_running:
        print(f"本就未运行 {len(not_running)} 个: {not_running}")
    if failed:
        for port, pid, err in failed:
            print(f"[FAIL] {port} (pid {pid}): {err}", file=sys.stderr)
        return 1
    return 0


def _pid_alive(pid: int) -> bool:
    if sys.platform == "win32":
        r = subprocess.run(["tasklist", "/PID", str(pid)], capture_output=True, text=True)
        return str(pid) in (r.stdout or "")
    import os
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def main() -> int:
    parser = argparse.ArgumentParser(description="矩阵端口启停控制")
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_start = sub.add_parser("start", help="启动全部端口")
    p_start.add_argument("--only", default=None, help="只启动指定端口(逗号分隔)，如 48730,48731")
    p_start.add_argument("--no-wait", action="store_true", help="不等待端口就绪")
    p_start.set_defaults(func=cmd_start)

    p_status = sub.add_parser("status", help="查看监听状态")
    p_status.set_defaults(func=cmd_status)

    p_stop = sub.add_parser("stop", help="停止全部端口")
    p_stop.set_defaults(func=cmd_stop)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
