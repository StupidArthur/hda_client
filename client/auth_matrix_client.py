#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
UA Auth Lab —— 认证矩阵客户端（manifest 驱动）。

按 configs/matrix/manifest.json 逐端口连接：每个端口只应接受它自己那一组
(policy, mode, auth)，输出 PASS/FAIL 清单。

默认跑全部端口的【正向】；加 --negative 时同时跑每端口 2 条负向抽样。

用法:

    python client/auth_matrix_client.py                       # 33 端口正向
    python client/auth_matrix_client.py --only 48730,48732     # 指定端口
    python client/auth_matrix_client.py --negative             # 正向 + 负向抽样
    python client/auth_matrix_client.py --auth x509            # 只跑某认证方式

退出码：全部 PASS 返回 0，否则返回 1。
"""

import argparse
import asyncio
import json
import sys
from pathlib import Path

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))
sys.path.insert(0, str(Path(__file__).resolve().parent))

from conn import (  # noqa: E402
    DEFAULT_APP_CERT,
    DEFAULT_APP_KEY,
    DEFAULT_PASSWORD,
    DEFAULT_SERVER_CERT,
    DEFAULT_USER_CERT,
    DEFAULT_USER_KEY,
    DEFAULT_USERNAME,
    StageFailed,
    read_node,
    setup_client,
    stage_connect,
)

MANIFEST = BASE_DIR / "configs" / "matrix" / "manifest.json"
APP_URI = "urn:example.org:FreeOpcUa:opcua-asyncio"


def load_manifest() -> dict:
    if not MANIFEST.exists():
        raise SystemExit(f"缺少清单: {MANIFEST}\n先运行: python tools/gen_matrix_configs.py")
    return json.loads(MANIFEST.read_text(encoding="utf-8"))


async def probe(url: str, policy: str, mode: str, auth: str, node: str, args) -> tuple[bool, str]:
    """跑一次分阶段连接 + 读节点，返回 (ok, detail)。"""
    client = await setup_client(
        url,
        app_cert=Path(args.app_cert),
        app_key=Path(args.app_key),
        server_cert=Path(args.server_cert),
        policy_name=policy,
        mode_name=mode,
        application_uri=args.app_uri or APP_URI,
    )
    try:
        client = await stage_connect(
            client,
            auth=auth,
            username=args.username,
            password=args.password,
            user_cert=Path(args.user_cert),
            user_key=Path(args.user_key),
            policy_name=policy,
            mode_name=mode,
            print_steps=False,
        )
        val = await read_node(client, node)
        return True, f"read {node}={val!r}"
    except StageFailed as e:
        return False, f"{e.stage}: {type(e.cause).__name__}: {e.cause}"
    except Exception as e:  # noqa: BLE001
        return False, f"{type(e).__name__}: {e}"
    finally:
        try:
            await client.disconnect()
        except Exception:  # noqa: BLE001
            pass


def _label(e: dict) -> str:
    dep = " (废弃)" if e["deprecated"] else ""
    return f"p{e['port']} {e['policy']}/{e['mode']}{dep} {e['auth']}"


async def run(args) -> int:
    manifest = load_manifest()
    entries = manifest["entries"]

    if args.only:
        wanted = {int(x) for x in args.only.replace(" ", "").split(",") if x}
        entries = [e for e in entries if e["port"] in wanted]
        missing = wanted - {e["port"] for e in entries}
        if missing:
            print(f"[FAIL] 清单中不存在端口: {sorted(missing)}", file=sys.stderr)
            return 1
    if args.auth:
        entries = [e for e in entries if e["auth"] == args.auth]
    if args.policy:
        entries = [e for e in entries if e["policy"] == args.policy]
    if not entries:
        print("[FAIL] 过滤后无端口", file=sys.stderr)
        return 1

    total = passed = 0
    neg_total = neg_passed = 0

    print(f"端口数: {len(entries)}   负向抽样: {'开' if args.negative else '关'}")
    print("=" * 76)

    # ---- 正向 ----
    for e in entries:
        total += 1
        ok, detail = await probe(e["url"], e["policy"], e["mode"], e["auth"],
                                 e["read_node"], args)
        flag = "PASS" if ok else "FAIL"
        if ok:
            passed += 1
        print(f"  [{flag}] [正向] {_label(e)}  {detail}")

    # ---- 负向抽样 ----
    if args.negative:
        print("-" * 76)
        for e in entries:
            for i, neg in enumerate(e.get("negatives", []), start=1):
                neg_total += 1
                ok, detail = await probe(e["url"], neg["policy"], neg["mode"],
                                         neg["auth"], e["read_node"], args)
                # 期望：失败
                expected_fail = not ok
                if expected_fail:
                    neg_passed += 1
                    flag = "PASS"
                else:
                    flag = "FAIL"
                want = f"期望 {neg['expect_fail_stage']} 拒绝"
                print(f"  [{flag}] [负向{i}] {_label(e)} -> "
                      f"{neg['policy']}/{neg['mode']}/{neg['auth']}  "
                      f"{detail if not expected_fail else '被拒绝(' + want + ')'}")

    print("=" * 76)
    print(f"正向: {passed}/{total} PASS")
    if neg_total:
        print(f"负向: {neg_passed}/{neg_total} PASS（失败即被正确拒绝）")

    bad = (total - passed) + (neg_total - neg_passed)
    if bad:
        print(f"\n{bad} 项不符合预期。")
        return 1
    print("\n全部符合预期。")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="UA Auth Lab 认证矩阵客户端")
    parser.add_argument("--only", default=None, help="只跑指定端口(逗号分隔)")
    parser.add_argument("--auth", default=None, choices=["anon", "username", "x509"],
                        help="只跑某种认证方式")
    parser.add_argument("--policy", default=None, help="只跑某 SecurityPolicy")
    parser.add_argument("--negative", action="store_true", help="同时跑每端口负向抽样")
    parser.add_argument("--app-cert", default=str(DEFAULT_APP_CERT))
    parser.add_argument("--app-key", default=str(DEFAULT_APP_KEY))
    parser.add_argument("--app-uri", default=None, help="ApplicationUri（默认取固定值）")
    parser.add_argument("--server-cert", default=str(DEFAULT_SERVER_CERT))
    parser.add_argument("--user-cert", default=str(DEFAULT_USER_CERT))
    parser.add_argument("--user-key", default=str(DEFAULT_USER_KEY))
    parser.add_argument("--username", default=DEFAULT_USERNAME)
    parser.add_argument("--password", default=DEFAULT_PASSWORD)
    args = parser.parse_args()
    return asyncio.run(run(args))


if __name__ == "__main__":
    sys.exit(main())
