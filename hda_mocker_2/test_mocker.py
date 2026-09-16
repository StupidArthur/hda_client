import unittest
import asyncio
from types import SimpleNamespace
from datetime import datetime, timedelta, timezone

from asyncua import Client, ua

from config import parse_duration
from model import build_specs, realtime_is_bad, sawtooth, sawtooth_stats
from history import AggregateHistoryManager, VirtualHistoryStorage
from replay import ReplayPoint, ReplayTag
from server import normalize_write_timestamp, realtime_value
from server import run
from config import load_settings


class MockerTests(unittest.IsolatedAsyncioTestCase):
    def test_sawtooth_never_zero(self):
        self.assertEqual([sawtooth(i) for i in range(102)], [float(i % 100 + 1) for i in range(102)])

    def test_quality_cycle(self):
        self.assertFalse(realtime_is_bad(539, 540, 60))
        self.assertTrue(realtime_is_bad(540, 540, 60))
        self.assertFalse(realtime_is_bad(600, 540, 60))

    def test_realtime_value_changes_with_timestamp(self):
        settings = type("SettingsStub", (), {"good_duration": 540, "bad_duration": 60})()
        spec = build_specs(0, 1, 0, 0)[0]
        first = realtime_value(spec, datetime.fromtimestamp(1, timezone.utc), settings)
        second = realtime_value(spec, datetime.fromtimestamp(2, timezone.utc), settings)
        self.assertNotEqual(first.Value.Value, second.Value.Value)

    def test_sawtooth_stats(self):
        values = [sawtooth(i) for i in range(37, 237)]
        count, total, minimum, maximum = sawtooth_stats(37, 236)
        self.assertEqual(count, len(values))
        self.assertEqual(total, sum(values))
        self.assertEqual(minimum, min(values))
        self.assertEqual(maximum, max(values))

    def test_node_counts(self):
        specs = build_specs(2, 1000, 1000, 1000)
        self.assertEqual(len(specs), 13 * 2 * 2 + 3000)
        self.assertEqual(specs[0].node_id, "inter_boolean_r_0001")

    def test_duration(self):
        self.assertEqual(parse_duration("24h"), 86400)
        self.assertEqual(parse_duration("7d"), 604800)

    def test_direct_aggregate(self):
        spec = build_specs(0, 1, 0, 0)
        storage = VirtualHistoryStorage(spec, 604800, 86400, 1200)
        end = datetime.now(timezone.utc).replace(microsecond=0)
        values = storage.aggregate(
            ua.NodeId("dynamic_0001", 2),
            end - timedelta(minutes=5),
            end,
            60000.0,
            ua.ObjectIds.AggregateFunction_Average,
        )
        self.assertEqual(len(values), 6)

    async def test_history_page_uses_smaller_client_limit(self):
        spec = build_specs(0, 1, 0, 0)
        storage = VirtualHistoryStorage(spec, 604800, 86400, 1200)
        end = datetime.now(timezone.utc).replace(microsecond=0)
        values, continuation = await storage.read_node_history(
            ua.NodeId("dynamic_0001", 2), end - timedelta(hours=1), end, 500
        )
        self.assertEqual(len(values), 500)
        self.assertIsNotNone(continuation)

    async def test_history_page_enforces_smaller_server_limit(self):
        spec = build_specs(0, 1, 0, 0)
        storage = VirtualHistoryStorage(spec, 604800, 86400, 1200)
        end = datetime.now(timezone.utc).replace(microsecond=0)
        values, continuation = await storage.read_node_history(
            ua.NodeId("dynamic_0001", 2), end - timedelta(hours=1), end, 5000
        )
        self.assertEqual(len(values), 1200)
        self.assertIsNotNone(continuation)

    async def test_replay_continuation_uses_record_index_for_same_timestamp(self):
        tag = ReplayTag("replay_1", tuple(ReplayPoint(1_000_000, float(i), 0) for i in range(3)))
        storage = VirtualHistoryStorage([], 1, 1, 2, replay=[tag])
        manager = AggregateHistoryManager.__new__(AggregateHistoryManager)
        manager.storage, manager._continuations = storage, {}
        details = SimpleNamespace(NumValuesPerNode=2, StartTime=datetime.fromtimestamp(1, timezone.utc), EndTime=datetime.fromtimestamp(1, timezone.utc))
        rv = SimpleNamespace(NodeId=ua.NodeId("replay_1", 2), ContinuationPoint=None)
        first, token = await manager._read_datavalue_history(rv, details)
        self.assertEqual([v.Value.Value for v in first], [0.0, 1.0])
        self.assertIsNotNone(token)
        rv.ContinuationPoint = token
        second, token = await manager._read_datavalue_history(rv, details)
        self.assertEqual([v.Value.Value for v in second], [2.0])
        self.assertIsNone(token)

    def test_replay_reverse_page_keeps_equal_timestamp_records(self):
        tag = ReplayTag("replay_1", tuple(ReplayPoint(1_000_000, float(i), 0) for i in range(3)))
        storage = VirtualHistoryStorage([], 1, 1, 2, replay=[tag])
        start = datetime.fromtimestamp(1, timezone.utc)
        first, offset = storage.replay_page(ua.NodeId("replay_1", 2), start, start, 2)
        self.assertEqual([v.Value.Value for v in first], [0.0, 1.0])
        # A reversed request returns every duplicate in reverse stable order.
        last, offset = storage.replay_page(ua.NodeId("replay_1", 2), start + timedelta(microseconds=1), start, 2)
        self.assertEqual([v.Value.Value for v in last], [2.0, 1.0])

    def test_generated_raw_page_keeps_grid_across_pages(self):
        storage = VirtualHistoryStorage(build_specs(0, 1, 0, 0), 3600, 3600, 2, interval=10)
        end = datetime.now(timezone.utc).replace(microsecond=0)
        start = end - timedelta(seconds=30)
        first, offset = storage.raw_page(ua.NodeId("dynamic_0001", 2), start, end, 2)
        second, offset = storage.raw_page(ua.NodeId("dynamic_0001", 2), start, end, 2, offset)
        stamps = [value.SourceTimestamp for value in first + second]
        self.assertEqual([int((stamp - stamps[0]).total_seconds()) for stamp in stamps], [0, 10, 20, 30])

    async def test_qa_protocol_registers_and_pages_replay(self):
        settings = load_settings("qa")
        task = asyncio.create_task(run(settings))
        try:
            for _ in range(20):
                await asyncio.sleep(0.1)
                client = Client(f"opc.tcp://127.0.0.1:{settings.port}/hda-mocker/")
                try:
                    await client.connect()
                    break
                except Exception:
                    await client.disconnect()
            else:
                self.fail("QA server did not start")
            try:
                namespace = await client.get_namespace_index(settings.namespace_uri)
                node = client.get_node(ua.NodeId("qa_ind_0001", namespace))
                start = datetime(2026, 9, 6, tzinfo=timezone.utc)
                end = start + timedelta(minutes=1)
                values = await node.read_raw_history(start, end, numvalues=2)
                self.assertEqual(len(values), 2)
                self.assertEqual(values[0].SourceTimestamp, start)
            finally:
                await client.disconnect()
        finally:
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task

    def test_missing_source_timestamp_uses_server_timestamp(self):
        server_time = datetime.now(timezone.utc)
        value = ua.DataValue(
            ua.Variant(42.0),
            ServerTimestamp=server_time,
        )
        normalized = normalize_write_timestamp(value)
        self.assertEqual(normalized.SourceTimestamp, server_time)

    def test_client_source_timestamp_is_preserved(self):
        source_time = datetime(2026, 1, 1, tzinfo=timezone.utc)
        value = ua.DataValue(
            ua.Variant(42.0),
            SourceTimestamp=source_time,
            ServerTimestamp=datetime.now(timezone.utc),
        )
        normalized = normalize_write_timestamp(value)
        self.assertEqual(normalized.SourceTimestamp, source_time)


if __name__ == "__main__":
    unittest.main()
