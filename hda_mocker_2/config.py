from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import re

import yaml


_DURATION = re.compile(r"^(\d+)(s|m|h|d|y)$")
_SECONDS = {"s": 1, "m": 60, "h": 3600, "d": 86400, "y": 365 * 86400}


def parse_duration(value: str) -> int:
    match = _DURATION.fullmatch(str(value).strip().lower())
    if not match:
        raise ValueError(f"无效时长 {value!r}，支持 30s/9m/24h/7d/1y")
    seconds = int(match.group(1)) * _SECONDS[match.group(2)]
    if seconds <= 0:
        raise ValueError("时长必须大于 0")
    return seconds


def _positive_int(data: dict, key: str, *, allow_zero: bool = True) -> int:
    value = int(data[key])
    if value < 0 or (not allow_zero and value == 0):
        raise ValueError(f"{key} 必须为{'正整数' if not allow_zero else '非负整数'}")
    return value


@dataclass(frozen=True)
class Settings:
    host: str
    port: int
    namespace_uri: str
    interval: int
    query_duration: int
    history_length: int
    page_size: int
    read_timeout: int
    type_groups: int
    dynamic_count: int
    static_count: int
    bad_count: int
    good_duration: int
    bad_duration: int


def load_settings(path: str | Path) -> Settings:
    config_path = Path(path).resolve()
    if not config_path.is_file():
        # 视为预设名: presets/<name>/config.yaml
        config_path = (
            Path(__file__).resolve().parent / "presets" / str(path) / "config.yaml"
        ).resolve()
        if not config_path.is_file():
            raise FileNotFoundError(f"预设配置不存在: {path!r} (需为 config.yaml 路径或 presets/ 下的预设名)")
    root = yaml.safe_load(config_path.read_text(encoding="utf-8"))
    if not isinstance(root, dict):
        raise ValueError("config.yaml 顶层必须是对象")

    preset_path = config_path.parent / root.get("preset_nodes", "preset_nodes.yaml")
    preset = yaml.safe_load(preset_path.read_text(encoding="utf-8"))
    server = root["server"]
    history = root["history"]

    interval = parse_duration(history.get("interval", "1s"))
    if interval <= 0:
        raise ValueError("history.interval 必须大于 0")

    settings = Settings(
        host=str(server.get("host", "0.0.0.0")),
        port=int(server.get("port", 48630)),
        namespace_uri=str(server.get("namespace_uri", "urn:hda-mocker-2")),
        interval=interval,
        query_duration=parse_duration(history.get("duration", "24h")),
        history_length=parse_duration(history.get("default_hda_length", "7d")),
        page_size=int(history.get("num_values_per_node", 1200)),
        read_timeout=parse_duration(history.get("read_timeout", "20s")),
        type_groups=_positive_int(preset["type_nodes"], "groups"),
        dynamic_count=_positive_int(preset["dynamic_nodes"], "count"),
        static_count=_positive_int(preset["static_nodes"], "count"),
        bad_count=_positive_int(preset["bad_realtime_nodes"], "count"),
        good_duration=parse_duration(preset["bad_realtime_nodes"].get("good_duration", "9m")),
        bad_duration=parse_duration(preset["bad_realtime_nodes"].get("bad_duration", "1m")),
    )
    if not 1 <= settings.port <= 65535:
        raise ValueError("server.port 必须在 1~65535 之间")
    if settings.page_size <= 0:
        raise ValueError("num_values_per_node 必须为正整数")
    if settings.history_length < settings.query_duration:
        raise ValueError("default_hda_length 不能小于 duration")
    return settings
