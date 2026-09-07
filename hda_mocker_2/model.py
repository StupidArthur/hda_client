from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

from asyncua import ua


@dataclass(frozen=True)
class NodeSpec:
    node_id: str
    data_type: ua.VariantType
    initial_value: Any
    writable: bool
    mode: str


TYPE_DEFAULTS: dict[str, tuple[ua.VariantType, Any]] = {
    "boolean": (ua.VariantType.Boolean, True),
    "sbyte": (ua.VariantType.SByte, -1),
    "byte": (ua.VariantType.Byte, 1),
    "int16": (ua.VariantType.Int16, -16),
    "uint16": (ua.VariantType.UInt16, 16),
    "int32": (ua.VariantType.Int32, -32),
    "uint32": (ua.VariantType.UInt32, 32),
    "int64": (ua.VariantType.Int64, -64),
    "uint64": (ua.VariantType.UInt64, 64),
    "float": (ua.VariantType.Float, 1.25),
    "double": (ua.VariantType.Double, 2.5),
    "string": (ua.VariantType.String, "mock"),
    "datetime": (ua.VariantType.DateTime, datetime(2026, 1, 1, tzinfo=timezone.utc)),
}


def sawtooth(timestamp: float) -> float:
    """绝对时间对齐的 1~100 锯齿波，永不产生 0。"""
    return float(int(timestamp) % 100 + 1)


def sawtooth_stats(first: int, last: int) -> tuple[int, float, float, float]:
    """Return count, sum, minimum and maximum for inclusive epoch seconds."""
    if last < first:
        return 0, 0.0, 0.0, 0.0

    def prefix(seconds: int) -> int:
        cycles, remainder = divmod(seconds, 100)
        return cycles * 5050 + remainder * (remainder + 1) // 2

    count = last - first + 1
    total = float(prefix(last + 1) - prefix(first))
    if count >= 100:
        return count, total, 1.0, 100.0
    values = [float(ts % 100 + 1) for ts in range(first, last + 1)]
    return count, total, min(values), max(values)


def build_specs(type_groups: int, dynamic_count: int, static_count: int, bad_count: int) -> list[NodeSpec]:
    specs: list[NodeSpec] = []
    for type_name, (variant_type, default) in TYPE_DEFAULTS.items():
        for index in range(1, type_groups + 1):
            specs.append(NodeSpec(f"inter_{type_name}_r_{index:04d}", variant_type, default, False, "constant"))
            specs.append(NodeSpec(f"inter_{type_name}_w_{index:04d}", variant_type, default, True, "constant"))
    for index in range(1, dynamic_count + 1):
        specs.append(NodeSpec(f"dynamic_{index:04d}", ua.VariantType.Double, 1.0, False, "sawtooth"))
    for index in range(1, static_count + 1):
        specs.append(NodeSpec(f"static_{index:04d}", ua.VariantType.Double, 42.0, True, "constant"))
    for index in range(1, bad_count + 1):
        specs.append(NodeSpec(f"bad_realtime_{index:04d}", ua.VariantType.Double, 1.0, False, "bad_realtime"))
    return specs


def value_at(spec: NodeSpec, timestamp: float) -> Any:
    if spec.mode in {"sawtooth", "bad_realtime"}:
        return sawtooth(timestamp)
    return spec.initial_value


def realtime_is_bad(timestamp: float, good_duration: int, bad_duration: int) -> bool:
    cycle = good_duration + bad_duration
    return cycle > 0 and int(timestamp) % cycle >= good_duration
