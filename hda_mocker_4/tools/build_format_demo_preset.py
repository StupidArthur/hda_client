"""Create or offline-verify the tiny human-facing v1.5.2 format demo."""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
from pathlib import Path
import pyarrow as pa
import pyarrow.parquet as pq

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'presets' / 'format_demo'
TAGS = ['sample_temperature', 'sample_pressure', 'sample_quality']
BASE = datetime(2026, 9, 1, 0, 0, 0, 125000, tzinfo=timezone.utc)
OFFSETS_MS = [0, 1000, 2500, 4000, 6250, 8000, 10000, 14900]
VALUES = [
    [20.0, 21.0, None, 22.0, 0.0, -1.0, 23.0, None],
    [100.0, None, 101.0, None, 102.0, None, 103.0, None],
    [1.0, None, None, 2.0, None, 3.0, 4.0, None],
]
QUALITY = [0, 0, 2150694912, 0, 0, 9830400, 2150694912, 0]


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def expected_table():
    return pa.table({
        'Timestamp': pa.array([BASE + timedelta(milliseconds=n) for n in OFFSETS_MS], type=pa.timestamp('us', tz='UTC')),
        **{name: pa.array(values, type=pa.float64()) for name, values in zip(TAGS, VALUES)},
        'sample_quality.__status': pa.array(QUALITY, type=pa.uint32()),
    })


def main(verify_only):
    expected = expected_table()
    manifest_path = OUT / 'manifest.json'
    files = [OUT / kind / 'format_demo.parquet' for kind in ('hda', 'da')]
    counts = {name: sum(v is not None or (QUALITY[i] & 0x80000000 != 0 if name == 'sample_quality' else False)
                        for i, v in enumerate(values)) for name, values in zip(TAGS, VALUES)}
    assert counts == dict(zip(TAGS, [6, 4, 5]))
    if not verify_only:
        if manifest_path.exists() or any(path.exists() or path.with_suffix('.parquet.tmp').exists() for path in files):
            raise SystemExit('Refusing to overwrite generated data; use --verify for read-only checks')
        for path in files:
            path.parent.mkdir(parents=True, exist_ok=True)
            temp = path.with_suffix('.parquet.tmp')
            pq.write_table(expected, temp, compression='zstd')
            temp.replace(path)
        manifest = {
            'dataset': 'format_demo', 'version': 'v1.5.2', 'source': 'deterministic synthetic teaching example',
            'timezone': 'UTC', 'tags': TAGS, 'rows_per_file': 8,
            'file_start': BASE.isoformat(), 'file_end': (BASE + timedelta(milliseconds=OFFSETS_MS[-1])).isoformat(),
            'last_observation': (BASE + timedelta(milliseconds=10000)).isoformat(),
            'hda_observations': sum(counts.values()), 'hda_per_tag': counts,
            'bad_observations': 2, 'da_loop_ms': OFFSETS_MS[-1] + 100, 'tick_ms': 100,
            'files': [{'file': p.relative_to(OUT).as_posix(), 'sha256': sha(p)} for p in files],
        }
        manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding='utf-8')
    manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
    for record in manifest['files']:
        path = OUT / record['file']
        actual = pq.read_table(path)
        assert actual.equals(expected), f'timestamp/value/status/schema mismatch: {path}'
        assert sha(path) == record['sha256']
        ts = actual.column(0).to_pylist()
        assert all(a < b for a, b in zip(ts, ts[1:]))
        assert all(t.tzinfo is not None and t.microsecond % 1000 == 0 for t in ts)
        assert actual.column('sample_quality.__status').null_count == 0
    assert manifest['hda_observations'] == 15 and manifest['da_loop_ms'] == 15000
    print('PASS: two Parquet files, 3 tags, 8 rows each, 15 HDA points, 15-second DA loop; no server started.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verify', action='store_true')
    main(parser.parse_args().verify)
