from __future__ import annotations

import argparse
import asyncio
import sys

from config import load_settings
from server import run


def main() -> None:
    if sys.platform == "win32":
        # Windows IOCP(Proactor) 事件循环在客户端闪断时 accept 协程会抛
        # WinError 64 导致 accept 循环退出, 服务器此后无法接受新连接。
        # 切换 Selector 事件循环规避(并发连接数 < 64 的场景适用)。
        asyncio.set_event_loop_policy(asyncio.WindowsSelectorEventLoopPolicy())
    parser = argparse.ArgumentParser(description="启动 OPC UA HDA Mocker")
    parser.add_argument("preset", nargs="?", default="basf_long",
                        help="预设名(如 default/simple/basf_long)或 config.yaml 文件路径")
    args = parser.parse_args()
    asyncio.run(run(load_settings(args.preset)))


if __name__ == "__main__":
    main()
