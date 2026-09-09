from __future__ import annotations

import time
from datetime import datetime, timedelta, timezone
from asyncua import ua
from asyncua.server.history import HistoryManager

from model import NodeSpec, sawtooth_stats, value_at


class VirtualHistoryStorage:
    def __init__(self, specs: list[NodeSpec], history_length: int, query_duration: int, page_size: int, interval: int = 1, read_timeout: int = 20):
        self.specs = {spec.node_id: spec for spec in specs}
        self.history_length = history_length
        self.query_duration = query_duration
        self.page_size = page_size
        self.interval = max(1, int(interval))
        self.read_timeout = max(1.0, float(read_timeout))

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
        if forward:
            timestamps = range(first, last + 1, self.interval)
        else:
            timestamps = range(last, first - 1, -self.interval)

        max_items = limit or self.page_size
        selected = []
        continuation = None
        deadline = time.monotonic() + self.read_timeout
        check_every = 4096
        since_check = 0
        for ts in timestamps:
            if len(selected) >= max_items:
                continuation = datetime.fromtimestamp(ts, timezone.utc)
                break
            if since_check >= check_every:
                since_check = 0
                if time.monotonic() > deadline:
                    continuation = datetime.fromtimestamp(ts, timezone.utc)
                    break
            since_check += 1
            selected.append(ts)
        points = [
            ua.DataValue(
                ua.Variant(value_at(spec, ts), spec.data_type),
                StatusCode=ua.StatusCode(ua.StatusCodes.Good),
                SourceTimestamp=datetime.fromtimestamp(ts, timezone.utc),
            )
            for ts in selected
        ]
        if continuation is None and requested_end > end:
            continuation = end + timedelta(seconds=1)
        return points, continuation

    def aggregate(self, node_id, start, end, interval_ms: float, aggregate_id: int):
        spec = self.specs.get(str(node_id.Identifier))
        if spec is None:
            return []
        start, end, forward = self._range(start, end)
        if not forward:
            start, end = end, start
        if end - start > timedelta(seconds=self.query_duration):
            end = start + timedelta(seconds=self.query_duration)

        interval = interval_ms / 1000.0
        bucket_start = start.timestamp()
        output = []
        ids = ua.ObjectIds
        deadline = time.monotonic() + self.read_timeout
        while bucket_start <= end.timestamp():
            bucket_end = min(bucket_start + interval, end.timestamp() + 1)
            first = int(bucket_start)
            last = int(bucket_end - 1e-9)
            if first > last:
                bucket_start += interval
                continue
            count = (last - first) // self.interval + 1
            if count <= 0:
                bucket_start += interval
                continue

            if aggregate_id == ids.AggregateFunction_Count:
                result_value = count
            elif spec.mode == "constant":
                if not isinstance(spec.initial_value, (int, float, bool)):
                    bucket_start += interval
                    continue
                result_value = float(spec.initial_value)
            else:
                _, total, minimum, maximum = sawtooth_stats(first, last, self.interval)
                if aggregate_id in (ids.AggregateFunction_Average, ids.AggregateFunction_TimeAverage):
                    result_value = total / count
                elif aggregate_id == ids.AggregateFunction_Minimum:
                    result_value = minimum
                elif aggregate_id == ids.AggregateFunction_Maximum:
                    result_value = maximum
                else:
                    return None

            output.append(ua.DataValue(
                ua.Variant(result_value),
                StatusCode=ua.StatusCode(ua.StatusCodes.Good),
                SourceTimestamp=datetime.fromtimestamp(bucket_start, timezone.utc),
            ))
            bucket_start += interval
            if time.monotonic() > deadline:
                break
        if not forward:
            output.reverse()
        return output

    async def read_node_history(self, node_id, start, end, nb_values):
        limit = min(nb_values, self.page_size) if nb_values and nb_values > 0 else self.page_size
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

        aggregate_id = details.AggregateType[0].Identifier
        values = self.storage.aggregate(
            rv.NodeId, details.StartTime, details.EndTime, interval_ms, aggregate_id
        )
        if values is None:
            result.StatusCode = ua.StatusCode(ua.StatusCodes.BadAggregateNotSupported)
            return result
        result.HistoryData.DataValues = values
        result.StatusCode = ua.StatusCode(ua.StatusCodes.Good)
        return result
