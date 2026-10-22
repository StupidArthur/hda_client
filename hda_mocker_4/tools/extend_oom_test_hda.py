"""Prepend August to OOM HDA; retain every September observation and all DA bytes."""
from datetime import datetime, timezone
import json
from pathlib import Path
import numpy as np
import pyarrow as pa
import pyarrow.parquet as pq
from build_oom_test_preset import ROOT, SOURCE, BATCH, sha, table, schema

preset = ROOT / 'presets' / 'oom_test'
manifest_path = preset / 'manifest.json'
m = json.loads(manifest_path.read_text(encoding='utf-8'))
assert m['start_inclusive'] == '2026-09-01T00:00:00+00:00', 'Unexpected existing window'
start = datetime(2026, 8, 1, tzinfo=timezone.utc)
epoch = int(start.timestamp() * 1000000)
anchor = int(datetime.fromisoformat(m['start_inclusive']).timestamp() * 1000000)
duration = anchor - epoch
cycle = m['source_cycle_us']
assert sha(SOURCE) == m['source_sha256']
source = pq.read_table(SOURCE)
relative = source.column(0).cast(pa.int64()).to_numpy()
relative = relative - relative[0]
values = np.column_stack([source.column(name).to_numpy() for name in source.column_names[1:]])
stage = preset / '.hda-august-staging'
assert not stage.exists()
stage.mkdir()
records = []
for group, record in enumerate(m['files'][:-1]):
    path = preset / record['file']
    assert sha(path) == record['sha256']
    old = pq.ParquetFile(path)
    names = old.schema_arrow.names[1:]
    shifted = (relative + group * 197000 + anchor - epoch) % cycle
    order = np.argsort(shifted)
    shifted, samples = shifted[order], values[order]
    rows, counts, first, last = 0, np.zeros(25, dtype=np.int64), None, None
    temp = stage / path.name
    with pq.ParquetWriter(temp, schema(names), compression='zstd') as writer:
        for offset in range(0, duration, cycle):
            stop = np.searchsorted(shifted, duration - offset)
            for lo in range(0, stop, BATCH):
                hi = min(lo + BATCH, stop)
                t, v = shifted[lo:hi] + epoch + offset, samples[lo:hi]
                writer.write_table(table(t, v, names))
                rows += len(t)
                counts += np.sum(~np.isnan(v), axis=0)
                first = int(t[0]) if first is None else first
                last = int(t[-1])
        assert last < anchor
        # Copy September batches without changing a timestamp, value or null.
        for batch in old.iter_batches(batch_size=BATCH):
            writer.write_batch(batch)
            rows += batch.num_rows
            counts += np.array([batch.num_rows - batch.column(i).null_count for i in range(1, 26)])
            last = batch.column(0)[-1].as_py()
    old.close()
    records.append(dict(record, rows=rows, observations=int(counts.sum()),
                        per_tag=dict(zip(names, map(int, counts))), first_us=first,
                        sha256=sha(temp)))
    print(f'Staged {group+1}/12: {int(counts.sum()):,} points', flush=True)
# Preserve old files before publishing; no input is deleted.
backup = ROOT / 'test_artifacts' / 'oom_test_before_august'
assert not backup.exists()
(backup / 'hda').mkdir(parents=True)
(backup / 'manifest.json').write_text(json.dumps(m, ensure_ascii=False, indent=2), encoding='utf-8')
for record in records:
    path = preset / record['file']
    path.replace(backup / 'hda' / path.name)
    (stage / path.name).replace(path)
stage.rmdir()
m.update(start_inclusive=start.isoformat(), hda_anchor=datetime.fromtimestamp(anchor/1000000, timezone.utc).isoformat(),
         da_start_inclusive=m['start_inclusive'], files=records + [m['files'][-1]],
         hda_observations=sum(r['observations'] for r in records))
manifest_path.write_text(json.dumps(m, ensure_ascii=False, indent=2), encoding='utf-8')
print('HDA total:', m['hda_observations'], 'Backup:', backup, flush=True)
