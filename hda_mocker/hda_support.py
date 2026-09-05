# -*- coding: utf-8 -*-
"""
HDA 增强支持：
  - HdaHistoryManager: 扩展 asyncua 的 HistoryManager, 在 ReadProcessedDetails(聚合历史)
    分支上用原始历史数据实时计算聚合结果(Average/Minimum/Maximum/Count/TimeAverage)。
  - parse_period: 把 "7d"/"24h"/"30m"/"1w" 解析为 timedelta。
"""

import re
from datetime import datetime, timedelta, timezone
from statistics import fmean
from typing import Iterable

from asyncua import ua
from asyncua.server.history import HistoryManager

# 聚合函数 NodeId(Identifier) -> 计算方式
_AGG_FNS = {
    "Average": "average",
    "TimeAverage": "average",
    "Minimum": "minimum",
    "Maximum": "maximum",
    "Count": "count",
}

_UNITS = {"s": 1, "m": 60, "h": 3600, "d": 86400, "w": 604800}


def parse_period(text: str) -> timedelta:
    """把 "7d"/"24h"/"30m"/"1w"/"3600s" 解析为 timedelta。"""
    m = re.fullmatch(r"(\d+)([smhdw])", str(text).strip().lower())
    if not m:
        raise ValueError(f"无法解析周期: {text!r}, 支持 30m/24h/7d/1w 等")
    return timedelta(seconds=int(m.group(1)) * _UNITS[m.group(2)])


def _agg_identifier(agg_type: ua.NodeId) -> int | None:
    ident = agg_type.Identifier
    for name in _AGG_FNS:
        if ident == getattr(ua.ObjectIds, f"AggregateFunction_{name}"):
            return _AGG_FNS[name]
    return None


def _bucket_start(ts: datetime, interval_ms: float) -> datetime:
    ms = ts.timestamp() * 1000.0
    b = int(ms // interval_ms)
    tz = ts.tzinfo if ts.tzinfo is not None else timezone.utc
    return datetime.fromtimestamp(b * interval_ms / 1000.0, tz=tz)


def aggregate_values(dvs: Iterable[ua.DataValue], agg_type: ua.NodeId, interval_ms: float) -> list[ua.DataValue]:
    """
    按 ProcessingInterval 分桶聚合原始历史数据。
    返回每桶一个 DataValue, SourceTimestamp=桶起点, 值=聚合结果。
    """
    fn = _agg_identifier(agg_type)
    if fn is None or interval_ms <= 0:
        return []

    buckets: dict[int, list] = {}
    for dv in dvs:
        ts = dv.SourceTimestamp or dv.ServerTimestamp
        if ts is None or dv.Value is None:
            continue
        if dv.StatusCode is not None and dv.StatusCode.value != 0:
            continue
        ms = ts.timestamp() * 1000.0
        buckets.setdefault(int(ms // interval_ms), []).append((ts, dv.Value.Value))

    out: list[ua.DataValue] = []
    for key in sorted(buckets):
        items = buckets[key]
        start = _bucket_start(items[0][0], interval_ms)
        if fn == "count":
            out.append(ua.DataValue(ua.Variant(len(items)), SourceTimestamp=start))
            continue
        values = [v for _, v in items]
        if fn == "minimum":
            v = min(values)
        elif fn == "maximum":
            v = max(values)
        else:
            v = fmean(values)
        out.append(ua.DataValue(ua.Variant(v), SourceTimestamp=start))
    return out


class HdaHistoryManager(HistoryManager):
    """
    支持聚合历史(ReadProcessedDetails)的 HistoryManager。
    其它行为与 asyncua 内置 HistoryManager 一致。
    """

    async def _read_history(self, details, rv):
        if isinstance(details, ua.ReadProcessedDetails):
            result = ua.HistoryReadResult()
            result.HistoryData = ua.HistoryData()
            try:
                raw, _ = await self.storage.read_node_history(
                    rv.NodeId, details.StartTime, details.EndTime, 0
                )
                if details.AggregateType:
                    result.HistoryData.DataValues = aggregate_values(
                        raw, details.AggregateType[0], details.ProcessingInterval
                    )
                else:
                    result.HistoryData.DataValues = raw
            except Exception as e:
                result.StatusCode = ua.StatusCode(ua.StatusCodes.BadNoData)
            return result
        return await super()._read_history(details, rv)
