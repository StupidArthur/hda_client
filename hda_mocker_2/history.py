from __future__ import annotations

from datetime import datetime, timedelta, timezone
from statistics import fmean

from asyncua import ua
from asyncua.server.history import HistoryManager

from model import NodeSpec, value_at


class VirtualHistoryStorage:
    def __init__(self, specs: list[NodeSpec], history_length: int, query_duration: int, page_size: int):
        self.specs = {spec.node_id: spec for spec in specs}
        self.history_length = history_length
        self.query_duration = query_duration
        self.page_size = page_size

    async def init(self):
        pass

    async def stop(self):
        pass

    async def new_historized_node(self, node_id, period, count=0):
        pass

    async def save_node_value(self, node_id, datavalue):
        pass

    async def new_historized_event(self, source_id, evtypes, period, count=0):
        pass

    async def save_event(self, event):
        pass

    async def read_event_history(self, source_id, start, end, nb_values, evfilter):
        return [], None

    def _range(self, start, end):
        now = datetime.now(timezone.utc)
        archive_start = now - timedelta(seconds=self.history_length)
        start = start if start and start > ua.get_win_epoch() else archive_start
        end = end if end and end > ua.get_win_epoch() else now
        start = max(start.astimezone(timezone.utc), archive_start)
        end = min(end.astimezone(timezone.utc), now)
        forward = start <= end
        return start, end, forward

    def generate(self, node_id, start, end, limit: int | None, apply_duration: bool = True):
        spec = self.specs.get(str(node_id.Identifier))
        if spec is None:
            return [], None
        start, end, forward = self._range(start, end)
        if not forward:
            start, end = end, start

        requested_end = end
        if apply_duration and end - start > timedelta(seconds=self.query_duration):
            end = start + timedelta(seconds=self.query_duration)

        first = int(start.timestamp())
        last = int(end.timestamp())
        timestamps = range(first, last + 1)
        if not forward:
            timestamps = reversed(range(first, last + 1))

        max_items = limit or self.page_size
        points = []
        continuation = None
        for ts in timestamps:
            if len(points) >= max_items:
                continuation = datetime.fromtimestamp(ts, timezone.utc)
                break
            points.append(ua.DataValue(
                ua.Variant(value_at(spec, ts), spec.data_type),
                StatusCode=ua.StatusCode(ua.StatusCodes.Good),
                SourceTimestamp=datetime.fromtimestamp(ts, timezone.utc),
            ))
        if continuation is None and requested_end > end:
            continuation = end + timedelta(seconds=1)
        return points, continuation

    async def read_node_history(self, node_id, start, end, nb_values):
        limit = nb_values if nb_values and nb_values > 0 else self.page_size
        return self.generate(node_id, start, end, limit)


class AggregateHistoryManager(HistoryManager):
    def __init__(self, iserver, storage: VirtualHistoryStorage):
        super().__init__(iserver)
        self.set_storage(storage)

    async def _read_history(self, details, rv):
        if not isinstance(details, ua.ReadProcessedDetails):
            return await super()._read_history(details, rv)

        result = ua.HistoryReadResult()
        result.HistoryData = ua.HistoryData()
        interval_ms = details.ProcessingInterval
        if interval_ms <= 0 or not details.AggregateType:
            result.StatusCode = ua.StatusCode(ua.StatusCodes.BadInvalidArgument)
            return result

        raw, _ = self.storage.generate(rv.NodeId, details.StartTime, details.EndTime, None, apply_duration=True)
        aggregate_id = details.AggregateType[0].Identifier
        buckets: dict[int, list[float]] = {}
        for item in raw:
            key = int(item.SourceTimestamp.timestamp() * 1000 // interval_ms)
            buckets.setdefault(key, []).append(item.Value.Value)

        ids = ua.ObjectIds
        for key in sorted(buckets):
            values = buckets[key]
            if aggregate_id in (ids.AggregateFunction_Average, ids.AggregateFunction_TimeAverage):
                value = fmean(values)
            elif aggregate_id == ids.AggregateFunction_Minimum:
                value = min(values)
            elif aggregate_id == ids.AggregateFunction_Maximum:
                value = max(values)
            elif aggregate_id == ids.AggregateFunction_Count:
                value = len(values)
            else:
                result.StatusCode = ua.StatusCode(ua.StatusCodes.BadAggregateNotSupported)
                return result
            ts = datetime.fromtimestamp(key * interval_ms / 1000, timezone.utc)
            result.HistoryData.DataValues.append(ua.DataValue(
                ua.Variant(value), StatusCode=ua.StatusCode(ua.StatusCodes.Good), SourceTimestamp=ts
            ))
        result.StatusCode = ua.StatusCode(ua.StatusCodes.Good)
        return result
