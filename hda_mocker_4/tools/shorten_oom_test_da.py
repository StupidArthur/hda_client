"""One-time generated-data migration to a one-day DA; preserve HDA unchanged."""
from datetime import datetime
import json
import os
from pathlib import Path
import pyarrow as pa
import pyarrow.compute as pc
import pyarrow.parquet as pq
from build_oom_test_preset import sha

root = Path(__file__).resolve().parents[1] / 'presets' / 'oom_test'
manifest_path = root / 'manifest.json'
m = json.loads(manifest_path.read_text(encoding='utf-8'))
target = root / 'da' / 'oom_test.parquet'
paused = target.with_suffix('.parquet.smoke-paused')
source = paused if paused.exists() else target
assert source.exists()
if m.get('da_duration_us') == 86400000000:
    raise SystemExit('Already shortened; no files changed')
old_record = m['files'][-1]
assert sha(source) == old_record['sha256']
epoch = int(datetime.fromisoformat(m['start_inclusive']).timestamp() * 1000000)
cutoff = epoch + 86400000000
temp = target.with_suffix('.parquet.tmp')
reader = pq.ParquetFile(source)
counts = [0] * 300
rows, first, last = 0, None, None
with pq.ParquetWriter(temp, reader.schema_arrow, compression='zstd') as writer:
    for batch in reader.iter_batches(batch_size=25000):
        part = pa.Table.from_batches([batch])
        part = part.filter(pc.less(part.column(0).cast(pa.int64()), cutoff))
        if not part.num_rows:
            break
        writer.write_table(part)
        ts = part.column(0).cast(pa.int64())
        first = ts[0].as_py() if first is None else first
        last = ts[-1].as_py()
        rows += part.num_rows
        for i in range(300):
            counts[i] += part.num_rows - part.column(i + 1).null_count
assert rows > 0 and all(n > 0 for n in counts)
reader.close()
backup = root / 'runtime' / 'da_3day.backup.parquet'
assert not backup.exists()
backup.parent.mkdir(exist_ok=True)
os.replace(source, backup)
os.replace(temp, target)
m['da_duration_us'] = 86400000000
m['da_loop_us'] = last - first + 100000
m['files'][-1] = {'file': 'da/oom_test.parquet', 'rows': rows,
                  'observations': sum(counts), 'sha256': sha(target)}
manifest_path.write_text(json.dumps(m, ensure_ascii=False, indent=2), encoding='utf-8')
print(json.dumps(m['files'][-1], indent=2))
print('DA loop microseconds:', m['da_loop_us'])
print('Old DA preserved in runtime; all HDA files unchanged.')
