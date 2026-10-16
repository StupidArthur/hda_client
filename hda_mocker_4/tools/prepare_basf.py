"""Preserve BASF observations, interpreting source timestamps as Asia/Shanghai and storing UTC."""
from pathlib import Path
import hashlib
import json
import pandas as pd
import pyarrow as pa
import pyarrow.parquet as pq

# The source CSV timestamp column is naive Beijing local time. It is converted
# to UTC here (shifted by -8h); it must NOT be relabelled as UTC directly.
SOURCE_TZ = 'Asia/Shanghai'

root = Path(__file__).resolve().parents[1] / 'presets' / 'basf_dirty'
source = next(root.glob('*.csv'))
df = pd.read_csv(source)
df.rename(columns={'timestamp': 'Timestamp'}, inplace=True)
df['Timestamp'] = pd.to_datetime(df['Timestamp']).dt.tz_localize(SOURCE_TZ).dt.tz_convert('UTC')
assert df.Timestamp.is_monotonic_increasing and df.Timestamp.is_unique
schema = pa.schema([pa.field('Timestamp', pa.timestamp('us', tz='UTC'))] +
                   [pa.field(c, pa.float64()) for c in df.columns[1:]])
table = pa.Table.from_pandas(df, schema=schema, preserve_index=False)
for kind in ('hda', 'da'):
    out = root / kind
    out.mkdir(exist_ok=True)
    pq.write_table(table, out / 'basf_dirty.parquet')
(root / 'manifest.json').write_text(json.dumps({
    'source': source.name, 'source_sha256': hashlib.sha256(source.read_bytes()).hexdigest(),
    'source_timezone': SOURCE_TZ, 'timezone': 'UTC',
    'rows': len(df), 'tags': len(df.columns)-1,
    'observations': int(df.iloc[:, 1:].count().sum()),
    'start': df.Timestamp.min().isoformat(), 'end': df.Timestamp.max().isoformat(),
    'hda_nulls': 'skip', 'da_nulls': 'hold last, circular initialization',
    'tick_ms': 100, 'loop_gap_ms': 100,
}, ensure_ascii=False, indent=2), encoding='utf-8')
