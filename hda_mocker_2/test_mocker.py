import unittest

from config import parse_duration
from model import build_specs, realtime_is_bad, sawtooth


class MockerTests(unittest.TestCase):
    def test_sawtooth_never_zero(self):
        self.assertEqual([sawtooth(i) for i in range(102)], [float(i % 100 + 1) for i in range(102)])

    def test_quality_cycle(self):
        self.assertFalse(realtime_is_bad(539, 540, 60))
        self.assertTrue(realtime_is_bad(540, 540, 60))
        self.assertFalse(realtime_is_bad(600, 540, 60))

    def test_node_counts(self):
        specs = build_specs(2, 1000, 1000, 1000)
        self.assertEqual(len(specs), 13 * 2 * 2 + 3000)
        self.assertEqual(specs[0].node_id, "inter_boolean_r_0001")

    def test_duration(self):
        self.assertEqual(parse_duration("24h"), 86400)
        self.assertEqual(parse_duration("7d"), 604800)


if __name__ == "__main__":
    unittest.main()
