# -*- coding: utf-8 -*-
"""
惰性 HDA 历史存储(LazyHistoryStorage)：
对 M0000~M9999.VALUE 这类位号, 按"位号索引 + 时间戳"实时确定性生成秒级历史值,
无需真实存储, 任意 7 天(或更长)窗口都能查到秒级数据, 用于客户端功能开发与验证。

值模型: 多周期正弦叠加 + 位号相关相位, 平滑、可复现、每位号不同。
"""

import math
import re
from datetime import datetime, timezone

from asyncua import ua

_MATCH = re.compile(r"^M(\d+)\.VALUE$")

_HASH_A = 2654435761  # 用于把位号索引打散成 0~1 相位
_HASH_B = 40503


def _parse_index(node_id) -> int | None:
    """从 NodeId 解析位号索引, 非 M####.VALUE 返回 None。"""
    ident = getattr(node_id, "Identifier", None)
    if ident is None:
        return None
    m = _MATCH.match(str(ident))
    if not m:
        return None
    return int(m.group(1))


def value_at(index: int, ts: float) -> float:
    """确定性秒级值: 多周期正弦 + 位号相位。"""
    a = (index * _HASH_A) % 10000 / 10000.0
    b = (index * _HASH_B) % 10000 / 10000.0
    v = (
        50.0
        + 30.0 * math.sin(2 * math.pi * ts / 3600.0 + a * 2 * math.pi)
        + 12.0 * math.sin(2 * math.pi * ts / 300.0 + b * 2 * math.pi)
        + 8.0 * math.sin(2 * math.pi * ts / 86400.0 + a * 5.0)
    )
    return round(v, 4)


class LazyHistoryStorage:
    """确定性惰性历史后端, 实现 asyncua 所需的存储接口。"""

    def __init__(self, max_history_data_response_size: int = 10000):
        self.max_history_data_response_size = max_history_data_response_size

    async def init(self):
        pass

    async def stop(self):
        pass

    async def new_historized_node(self, node_id, period, count=0):
        # 惰性: 无需注册
        pass

    async def save_node_value(self, node_id, datavalue):
        # 惰性: 不存储写入
        pass

    async def new_historized_event(self, source_id, evtypes, period, count=0):
        pass

    async def save_event(self, event):
        pass

    async def read_event_history(self, source_id, start, end, nb_values, evfilter):
        return [], None

    async def read_node_history(self, node_id, start, end, nb_values):
        """生成 start~end 的秒级历史, 支持分页(cont)与条数截断。"""
        index = _parse_index(node_id)
        if index is None:
            return [], None
        if start is None:
            start = ua.get_win_epoch()
        if end is None:
            end = ua.get_win_epoch()

        t0 = int(start.timestamp())
        t1 = int(end.timestamp())
        if t1 < t0:
            return [], None
        # 限幅: 单次最多生成 7 天, 防止超大窗口把客户端拖垮
        max_span = 7 * 86400
        if t1 - t0 > max_span:
            t0 = t1 - max_span

        count = t1 - t0 + 1
        if nb_values and count > nb_values:
            count = nb_values

        dvs: list[ua.DataValue] = []
        ts = t0
        while len(dvs) < count:
            dvs.append(
                ua.DataValue(
                    ua.Variant(value_at(index, ts), ua.VariantType.Double),
                    SourceTimestamp=datetime.fromtimestamp(ts, timezone.utc),
                )
            )
            ts += 1

        cont = None
        if len(dvs) > self.max_history_data_response_size:
            cont = dvs[self.max_history_data_response_size].SourceTimestamp
            dvs = dvs[: self.max_history_data_response_size]
        return dvs, cont
