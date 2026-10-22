"""Read-only, offline verification; does not start Mock or perform E2E."""
from datetime import datetime
import json
from pathlib import Path
import numpy as np
import pyarrow as pa
import pyarrow.parquet as pq
from build_oom_test_preset import sha

root = Path(__file__).resolve().parents[1]
preset = root / 'presets' / 'oom_test'
m = json.loads((preset / 'manifest.json').read_text(encoding='utf-8'))
source = pq.read_table(root / m['source'])
assert sha(root / m['source']) == m['source_sha256']
source_time = source.column(0).cast(pa.int64()).to_numpy()
relative = source_time - source_time[0]
epoch = int(datetime.fromisoformat(m['start_inclusive']).timestamp() * 1000000)
end = int(datetime.fromisoformat(m['end_exclusive']).timestamp() * 1000000)
cycle = m['source_cycle_us']
anchor = int(datetime.fromisoformat(m.get('hda_anchor', m['start_inclusive'])).timestamp() * 1000000)
da_epoch = int(datetime.fromisoformat(m.get('da_start_inclusive', m['start_inclusive'])).timestamp() * 1000000)
mapping = {row['tag']: row for row in m['mapping']}
seen = set()
for record in m['files']:
    path = preset / record['file']
    assert sha(path) == record['sha256'], path
    parquet = pq.ParquetFile(path)
    assert parquet.schema_arrow.field(0).type == pa.timestamp('us', tz='UTC')
    names = parquet.schema_arrow.names[1:]
    assert all(parquet.schema_arrow.field(name).type == pa.float64() for name in names)
    hda = Path(record['file']).parts[0] == 'hda'
    if hda:
        assert not seen.intersection(names)
        seen.update(names)
    else:
        assert set(names) == set(mapping)
    expected = {}
    for name in names:
        info = mapping[name]
        values = source.column(info['source_tag']).to_numpy()
        valid = np.isfinite(values)
        times = (relative[valid] + info['phase_ms'] * 1000) % cycle
        order = np.argsort(times)
        times, values = times[order], values[valid][order]
        if hda:
            times = (times + anchor - epoch) % cycle
            order = np.argsort(times)
            times, values = times[order], values[order]
            offsets = range(0, end - epoch, cycle)
            expanded = np.concatenate([times + offset + epoch for offset in offsets])
            v = np.tile(values, len(offsets))
            keep = expanded < end
            times, values = expanded[keep], v[keep]
        else:
            keep = times < m.get('da_duration_us', cycle)
            times, values = times[keep], values[keep]
            times = times + da_epoch
        expected[name] = (times, values)
    cursors = dict.fromkeys(names, 0)
    previous, rows, observations = None, 0, 0
    for batch in parquet.iter_batches(batch_size=25000):
        times = batch.column(0).cast(pa.int64()).to_numpy()
        assert len(times) and np.all(np.diff(times) > 0) and np.all(times % 1000 == 0)
        assert times[0] >= epoch and times[-1] < end
        assert previous is None or times[0] > previous
        previous = times[-1]
        rows += len(times)
        for i, name in enumerate(names, 1):
            values = batch.column(i).to_numpy(zero_copy_only=False)
            valid = np.isfinite(values)
            n = int(valid.sum())
            cursor = cursors[name]
            et, ev = expected[name]
            assert np.array_equal(times[valid], et[cursor:cursor + n]), (path, name, 'timestamps')
            assert np.array_equal(values[valid], ev[cursor:cursor + n]), (path, name, 'values')
            cursors[name] += n
            observations += n
    assert rows == record['rows'] and observations == record['observations']
    assert all(cursors[name] == len(expected[name][0]) for name in names)
    assert all(cursors[name] > 0 for name in names)
    print(f'PASS {record["file"]}: {rows:,} rows / {observations:,} observations', flush=True)
assert seen == {f'oom_test_{i:03d}' for i in range(300)}
print('PASS: all 300 tags, complete timestamp/value comparison, hashes, schema, NULL semantics and UTC range.')
