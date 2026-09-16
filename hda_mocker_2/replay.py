"""Fixed Parquet replay source: <config directory>/data/*.parquet."""
from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import pyarrow as pa
import pyarrow.parquet as pq


@dataclass(frozen=True)
class ReplayPoint:
    timestamp_us: int
    value: Optional[float]
    quality: int


@dataclass(frozen=True)
class ReplayTag:
    node_id: str
    points: tuple[ReplayPoint, ...]


_REQUIRED = {"tag", "timestamp", "value", "value_kind", "quality"}


def _timestamp_us(value: datetime) -> int:
    if value.tzinfo is None:
        raise ValueError("timestamp must have a timezone")
    delta = value.astimezone(timezone.utc) - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return delta.days * 86_400_000_000 + delta.seconds * 1_000_000 + delta.microseconds


def load_replay(directory: Path) -> list[ReplayTag]:
    """Read direct children in filename order; duplicate tags are an error."""
    paths = sorted(directory.glob("*.parquet")) if directory.is_dir() else []
    tags: dict[str, list[ReplayPoint]] = {}
    owners: dict[str, Path] = {}
    for path in paths:
        schema = pq.read_schema(path)
        missing = _REQUIRED - set(schema.names)
        timestamp_type = schema.field("timestamp").type if "timestamp" in schema.names else None
        if missing or timestamp_type is None or not pa.types.is_timestamp(timestamp_type) or timestamp_type.unit != "us":
            raise ValueError(f"invalid replay parquet {path}: missing {sorted(missing)} or invalid timestamp")
        table = pq.read_table(path, columns=["tag", "timestamp", "value", "value_kind", "quality"])
        local: dict[str, list[ReplayPoint]] = {}
        for row in table.to_pylist():
            tag, kind = row["tag"], row["value_kind"]
            if not isinstance(tag, str) or not tag:
                raise ValueError(f"invalid replay parquet {path}: empty tag")
            if kind not in {"finite", "nan", "pos_inf", "neg_inf", "null"}:
                raise ValueError(f"invalid replay parquet {path}: invalid value_kind")
            value = None if kind == "null" else row["value"]
            if kind != "null" and value is None:
                raise ValueError(f"invalid replay parquet {path}: null value without value_kind=null")
            if value is not None and not isinstance(value, float):
                raise ValueError(f"invalid replay parquet {path}: value must be double")
            quality = int(row["quality"])
            if quality < -0x80000000 or quality > 0xFFFFFFFF:
                raise ValueError(f"invalid replay parquet {path}: quality out of uint32 range")
            local.setdefault(tag, []).append(ReplayPoint(_timestamp_us(row["timestamp"]), value, quality & 0xFFFFFFFF))
        for tag, points in local.items():
            if tag in owners:
                raise ValueError(f"duplicate replay tag {tag!r}: {owners[tag]} and {path}")
            owners[tag] = path
            tags[tag] = sorted(points, key=lambda point: point.timestamp_us)
    return [ReplayTag(tag, tuple(tags[tag])) for tag in sorted(tags)]
