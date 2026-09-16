from __future__ import annotations

import bisect
import secrets
import time
from contextvars import ContextVar
from datetime import datetime, timedelta, timezone
from asyncua import ua
from asyncua.server.history import HistoryManager

from model import NodeSpec, sawtooth_stats, value_at
from replay import ReplayPoint, ReplayTag

_EPOCH = datetime(1970, 1, 1, tzinfo=timezone.utc)
_SESSION_ID: ContextVar[object] = ContextVar("history_session_id", default=None)


def _from_us(timestamp_us: int) -> datetime:
    return _EPOCH + timedelta(microseconds=timestamp_us)


def _to_us(timestamp: datetime) -> int:
    delta = timestamp.astimezone(timezone.utc) - _EPOCH
    return delta.days * 86_400_000_000 + delta.seconds * 1_000_000 + delta.microseconds


class VirtualHistoryStorage:
    def __init__(self, specs: list[NodeSpec], history_length: int, query_duration: int, page_size: int, interval: int = 1, read_timeout: int = 20, replay: list[ReplayTag] | None = None):
        self.specs = {spec.node_id: spec for spec in specs}
        self.history_length = history_length
        self.query_duration = query_duration
        self.page_size = page_size
        self.interval = max(1, int(interval))
        self.read_timeout = max(1.0, float(read_timeout))
        self.replay = {t.node_id: t.points for t in replay} if replay else {}
        self.replay_times = {tag: tuple(point.timestamp_us for point in points) for tag, points in self.replay.items()}

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
        if str(node_id.Identifier) in self.replay:
            return self._generate_replay(node_id, start, end, limit)
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

    def _replay_window(self, start, end):
        """回放节点用绝对查询窗口∩数据窗口, 不依赖 now 夹取。返回 (s_ts, e_ts, forward)。"""
        data_min = min((p.timestamp_us for tags in self.replay.values() for p in tags[:1]), default=0)
        data_max = max((p.timestamp_us for tags in self.replay.values() for p in tags[-1:]), default=0)
        s = int(start.timestamp() * 1_000_000) if start and start > ua.get_win_epoch() else data_min
        e = int(end.timestamp() * 1_000_000) if end and end > ua.get_win_epoch() else data_max
        forward = s <= e
        return max(s, data_min), min(e, data_max), forward

    def _generate_replay(self, node_id, start, end, limit):
        points = self.replay.get(str(node_id.Identifier))
        if not points:
            return [], None
        s, e, forward = self._replay_window(start, end)
        if not forward:
            s, e = e, s
        timestamps = [point.timestamp_us for point in points]
        lo = bisect.bisect_left(timestamps, s)
        hi = bisect.bisect_right(timestamps, e)
        window = points[lo:hi]
        if not forward:
            window = list(reversed(window))
        max_items = limit or self.page_size
        window = window[:max_items]
        out = []
        for point in window:
            if point.value is None:
                dv = ua.DataValue(ua.Variant(None), StatusCode=ua.StatusCode(point.quality),
                                  SourceTimestamp=_from_us(point.timestamp_us))
            else:
                dv = ua.DataValue(ua.Variant(point.value, ua.VariantType.Double), StatusCode=ua.StatusCode(point.quality),
                                  SourceTimestamp=_from_us(point.timestamp_us))
            out.append(dv)
        return out, None

    def replay_page(self, node_id, start, end, limit, offset=0):
        """Return replay records by positional offset, so equal timestamps cannot skip."""
        points = self.replay.get(str(node_id.Identifier), ())
        if not points:
            return [], None
        timestamps = self.replay_times[str(node_id.Identifier)]
        data_min, data_max = timestamps[0], timestamps[-1]
        s = _to_us(start) if start and start > ua.get_win_epoch() else data_min
        e = _to_us(end) if end and end > ua.get_win_epoch() else data_max
        forward = s <= e
        if not forward:
            s, e = e, s
        lo, hi = bisect.bisect_left(timestamps, s), bisect.bisect_right(timestamps, e)
        if forward:
            page = points[lo + offset:min(lo + offset + limit, hi)]
            next_offset = offset + len(page)
            has_more = lo + next_offset < hi
        else:
            end_index = hi - offset
            page = tuple(reversed(points[max(lo, end_index - limit):end_index]))
            next_offset = offset + len(page)
            has_more = hi - next_offset > lo
        values = [ua.DataValue(ua.Variant(point.value, ua.VariantType.Double) if point.value is not None else ua.Variant(None), StatusCode=ua.StatusCode(point.quality), SourceTimestamp=_from_us(point.timestamp_us)) for point in page]
        return values, (next_offset if has_more else None)

    def raw_page(self, node_id, start, end, limit, offset=0):
        """One positional raw-query contract for generated and replay data."""
        if str(node_id.Identifier) in self.replay:
            return self.replay_page(node_id, start, end, limit, offset)
        spec = self.specs.get(str(node_id.Identifier))
        if spec is None:
            return [], None
        begin, finish = start.astimezone(timezone.utc), end.astimezone(timezone.utc)
        forward = begin <= finish
        if not forward:
            begin, finish = finish, begin
        first, last = int(begin.timestamp()), int(finish.timestamp())
        total = max(0, (last - first) // self.interval + 1)
        indexes = range(offset, min(offset + limit, total))
        values = []
        deadline = time.monotonic() + self.read_timeout
        for index in indexes:
            if index > offset and (index - offset) % 4096 == 0 and time.monotonic() > deadline:
                break
            timestamp = first + index * self.interval if forward else last - index * self.interval
            values.append(ua.DataValue(ua.Variant(value_at(spec, timestamp), spec.data_type), StatusCode=ua.StatusCode(ua.StatusCodes.Good), SourceTimestamp=datetime.fromtimestamp(timestamp, timezone.utc)))
        next_offset = offset + len(values)
        return values, (next_offset if next_offset < total else None)

    def raw_bounds(self, node_id, start, end):
        """Resolve defaults/retention once, before a positional cursor is issued."""
        if str(node_id.Identifier) in self.replay:
            times = self.replay_times[str(node_id.Identifier)]
            lower, upper = _from_us(times[0]), _from_us(times[-1])
            begin = start if start and start > ua.get_win_epoch() else lower
            finish = end if end and end > ua.get_win_epoch() else upper
            forward = begin <= finish
            return begin, finish, forward
        return self._range(start, end)

    def _aggregate_replay(self, node_id, start, end, interval_ms, aggregate_id):
        points = self.replay.get(str(node_id.Identifier))
        if not points:
            return []
        s, e, forward = self._replay_window(start, end)
        if not forward:
            s, e = e, s
        timestamps = [point.timestamp_us for point in points]
        lo = bisect.bisect_left(timestamps, s)
        hi = bisect.bisect_right(timestamps, e)
        window = points[lo:hi]
        if not window:
            return []

        interval = interval_ms / 1000.0
        output = []
        ids = ua.ObjectIds
        idx = 0
        bucket_start = window[0].timestamp_us
        while idx < len(window) and bucket_start <= e:
            bucket_end = bucket_start + interval * 1_000_000
            bucket_vals = []
            while idx < len(window) and window[idx].timestamp_us < bucket_end:
                point = window[idx]
                if point.value is not None and point.quality == 0:
                    bucket_vals.append(point.value)
                idx += 1
            if bucket_vals:
                if aggregate_id == ids.AggregateFunction_Count:
                    result_value = len(bucket_vals)
                elif aggregate_id in (ids.AggregateFunction_Average, ids.AggregateFunction_TimeAverage):
                    result_value = sum(bucket_vals) / len(bucket_vals)
                elif aggregate_id == ids.AggregateFunction_Minimum:
                    result_value = min(bucket_vals)
                elif aggregate_id == ids.AggregateFunction_Maximum:
                    result_value = max(bucket_vals)
                else:
                    return None
                output.append(ua.DataValue(
                    ua.Variant(result_value),
                    StatusCode=ua.StatusCode(ua.StatusCodes.Good),
                    SourceTimestamp=_from_us(int(bucket_start)),
                ))
            bucket_start = bucket_end
        if not forward:
            output.reverse()
        return output

    def aggregate(self, node_id, start, end, interval_ms: float, aggregate_id: int):
        if str(node_id.Identifier) in self.replay:
            return self._aggregate_replay(node_id, start, end, interval_ms, aggregate_id)
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
        self._continuations: dict[bytes, tuple[object, object, object, object, int]] = {}

    async def read_history(self, params, session_id=None):
        """HistoryManager is shared: bind every opaque token to its session."""
        results = []
        for rv in params.NodesToRead:
            if params.ReleaseContinuationPoints:
                token = bytes(rv.ContinuationPoint or b"")
                state = self._continuations.get(token)
                result = ua.HistoryReadResult()
                if state is None or state[0] != session_id or state[1] != rv.NodeId:
                    result.StatusCode = ua.StatusCode(ua.StatusCodes.BadContinuationPointInvalid)
                else:
                    del self._continuations[token]
                    result.StatusCode = ua.StatusCode(ua.StatusCodes.Good)
                results.append(result)
                continue
            marker = _SESSION_ID.set(session_id)
            try:
                results.append(await self._read_history(params.HistoryReadDetails, rv))
            finally:
                _SESSION_ID.reset(marker)
        return results

    def release_session(self, session_id):
        for token, state in list(self._continuations.items()):
            if state[0] == session_id:
                del self._continuations[token]

    async def _read_datavalue_history(self, rv, details):
        """All raw data uses opaque, session-bound positional continuation points."""
        limit = min(details.NumValuesPerNode, self.storage.page_size) if details.NumValuesPerNode else self.storage.page_size
        offset, start, end = 0, details.StartTime, details.EndTime
        if rv.ContinuationPoint:
            state = self._continuations.get(bytes(rv.ContinuationPoint))
            if state is None or state[0] != _SESSION_ID.get() or state[1] != rv.NodeId:
                raise ua.UaStatusCodeError(ua.StatusCodes.BadContinuationPointInvalid)
            del self._continuations[bytes(rv.ContinuationPoint)]
            _, node, start, end, offset = state
        else:
            start, end, _ = self.storage.raw_bounds(rv.NodeId, start, end)
        values, next_offset = self.storage.raw_page(rv.NodeId, start, end, limit, offset)
        token = None
        if next_offset is not None:
            token = secrets.token_bytes(24)
            self._continuations[token] = (_SESSION_ID.get(), rv.NodeId, start, end, next_offset)
        return values, token

    async def _read_history(self, details, rv):
        if isinstance(details, ua.ReadRawModifiedDetails):
            result = ua.HistoryReadResult()
            result.HistoryData = ua.HistoryModifiedData() if details.IsReadModified else ua.HistoryData()
            try:
                values, token = await self._read_datavalue_history(rv, details)
            except ua.UaStatusCodeError:
                result.StatusCode = ua.StatusCode(ua.StatusCodes.BadContinuationPointInvalid)
                return result
            result.HistoryData.DataValues = values
            result.ContinuationPoint = token
            result.StatusCode = ua.StatusCode(ua.StatusCodes.Good)
            return result
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
