"""Small live OPC UA smoke test; run only against an explicitly started OOM preset."""
import asyncio
import argparse
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import math
import pyarrow.parquet as pq
from asyncua import Client, ua

ROOT = Path(__file__).resolve().parents[1]
PRESET = ROOT / 'presets' / 'oom_test'
UTC = timezone.utc


async def history(client, node, start, end):
    detail = ua.ReadRawModifiedDetails(IsReadModified=False, StartTime=start,
                                      EndTime=end, NumValuesPerNode=5000, ReturnBounds=False)
    points, pages, cp = [], 0, None
    while True:
        params = ua.HistoryReadParameters(
            HistoryReadDetails=detail, TimestampsToReturn=ua.TimestampsToReturn.Both,
            ReleaseContinuationPoints=False,
            NodesToRead=[ua.HistoryReadValueId(NodeId=node.nodeid, ContinuationPoint=cp)])
        results = await client.uaclient.history_read(params)
        result = results[0]
        result.StatusCode.check()
        points.extend(result.HistoryData.DataValues)
        pages += 1
        cp = result.ContinuationPoint
        if not cp:
            return points, pages
        assert pages < 100, 'unexpected pagination loop'


async def main(hda_only=False, endpoint='opc.tcp://127.0.0.1:4850'):
    report = {'endpoint': endpoint, 'started_utc': datetime.now(UTC).isoformat(), 'history': [], 'hda_only': hda_only}
    async with Client(report['endpoint'], timeout=30) as client:
        ns = await client.get_namespace_index('urn:hda:mocker4:oom_test')
        objects = await client.nodes.objects.get_children()
        folders = [node for node in objects if (await node.read_browse_name()).Name == 'HDA_Mocker']
        assert len(folders) == 1
        nodes = await folders[0].get_children()
        names = {str(node.nodeid.Identifier) for node in nodes}
        expected_names = {f'oom_test_{i:03d}' for i in range(300)}
        assert names == expected_names, (len(names), len(expected_names))
        values = await client.read_attributes(nodes, ua.AttributeIds.Value)
        assert all(v.StatusCode.is_good() and v.Value is not None and math.isfinite(v.Value.Value) for v in values)
        report.update(namespace=ns, browsed_tags=len(nodes), good_current_values=len(values))
        print('PASS browse and read: 300 unique tags / 300 Good finite current values', flush=True)
        cases = [
            (0, '2026-08-01T00:00:00+00:00', '2026-08-01T00:01:00+00:00'),
            (25, '2026-08-31T23:59:30+00:00', '2026-09-01T00:00:30+00:00'),
            (0, '2026-09-01T00:00:00+00:00', '2026-09-02T00:00:00+00:00'),
            (25, '2026-09-01T00:00:00+00:00', '2026-09-01T00:01:00+00:00'),
            (14, '2026-09-01T00:00:00+00:00', '2026-09-02T00:00:00+00:00'),
            (152, '2026-09-15T12:00:00+00:00', '2026-09-15T12:05:00+00:00'),
            (299, '2026-09-30T23:59:00+00:00', '2026-10-01T00:00:00+00:00'),
        ]
        for index, a, b in cases:
            name = f'oom_test_{index:03d}'
            start, end = datetime.fromisoformat(a), datetime.fromisoformat(b)
            source = pq.read_table(PRESET / 'hda' / f'oom_test_{index // 25:02d}.parquet',
                                   columns=['Timestamp', name],
                                   filters=[('Timestamp', '>=', start), ('Timestamp', '<=', end)])
            expected = [(t, v) for t, v in zip(source.column(0).to_pylist(), source.column(1).to_pylist()) if v is not None]
            assert expected, f'empty test case: {name}'
            points, pages = await history(client, client.get_node(ua.NodeId(name, ns)), start, end)
            assert len(points) == len(expected), (name, len(points), len(expected))
            for point, (timestamp, value) in zip(points, expected):
                assert point.StatusCode.value == 0
                assert point.SourceTimestamp == timestamp, (name, point.SourceTimestamp, timestamp)
                assert point.Value.Value == value
            item = {'tag': name, 'start': a, 'end': b, 'points': len(points), 'pages': pages,
                    'first_timestamp': points[0].SourceTimestamp.isoformat(),
                    'first_value': points[0].Value.Value, 'exact_time_value_match': True}
            report['history'].append(item)
            print('PASS history:', item, flush=True)
        if not hda_only:
            node = client.get_node(ua.NodeId('oom_test_000', ns))
            before = await node.read_data_value()
            await asyncio.sleep(12)
            after = await node.read_data_value()
            assert after.StatusCode.is_good() and after.SourceTimestamp > before.SourceTimestamp
            points, pages = await history(client, node, before.SourceTimestamp, after.SourceTimestamp + timedelta(microseconds=1))
            match = [p for p in points if p.SourceTimestamp == after.SourceTimestamp and p.Value.Value == after.Value.Value]
            assert match, 'latest DA value was not present in appended HDA'
            report['live'] = {'tag': 'oom_test_000', 'before': before.SourceTimestamp.isoformat(),
                              'after': after.SourceTimestamp.isoformat(), 'points': len(points), 'current_matches_history': True}
            print('PASS DA advances and matches live appended HDA:', report['live'], flush=True)
    report['passed'] = True
    report['finished_utc'] = datetime.now(UTC).isoformat()
    output = PRESET / 'runtime' / 'smoke_result.json'
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    print(f'PASS all checks; result: {output}', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--hda-only', action='store_true')
    parser.add_argument('--endpoint', default='opc.tcp://127.0.0.1:4850')
    args = parser.parse_args()
    asyncio.run(main(args.hda_only, args.endpoint))
