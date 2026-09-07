from __future__ import annotations

import argparse
import asyncio

from config import load_settings
from server import run


def main() -> None:
    parser = argparse.ArgumentParser(description="轻量 OPC UA HDA Mocker")
    parser.add_argument("config", nargs="?", default="config.yaml")
    args = parser.parse_args()
    asyncio.run(run(load_settings(args.config)))


if __name__ == "__main__":
    main()
