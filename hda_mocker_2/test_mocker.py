import unittest
from datetime import datetime, timedelta, timezone

from asyncua import ua

from config import parse_duration
from model import build_specs, realtime_is_bad, sawtooth, sawtooth_stats
from history import VirtualHistoryStorage
from server import normalize_write_timestamp


class MockerTests(unittest.TestCase):
    def test_sawtooth_never_zero(self):
        self.assertEqual([sawtooth(i) for i in range(102)], [float(i % 100 + 1) for i in range(102)])

    def test_quality_cycle(self):
        self.assertFalse(realtime_is_bad(539, 540, 60))
        self.assertTrue(realtime_is_bad(540, 540, 60))
        self.assertFalse(realtime_is_bad(600, 540, 60))

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
