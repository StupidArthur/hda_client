"""Build a reproducible 300-tag BASF-derived preset, without starting Mock.

HDA: 2026-08-01 <= Timestamp < 2026-10-01 UTC, 12 disjoint 25-tag files.
DA: the first UTC day of the merged timeline, containing all 300 tags.
"""
from pathlib import Path
from datetime import datetime, timezone
import hashlib
import json
import numpy as np
import pyarrow as pa
import pyarrow.parquet as pq

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'presets' / 'oom_test'
SOURCE = ROOT / 'presets' / 'basf_dirty' / 'hda' / 'basf_dirty.parquet'
START = datetime(2026, 8, 1, tzinfo=timezone.utc)
ANCHOR = datetime(2026, 9, 1, tzinfo=timezone.utc)
END = datetime(2026, 10, 1, tzinfo=timezone.utc)
BATCH = 25000
DA_DURATION_US = 86400 * 1000000


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def schema(names):
    return pa.schema([pa.field('Timestamp', pa.timestamp('us', tz='UTC'))] +
                     [pa.field(name, pa.float64()) for name in names])


def table(times, values, names):
    return pa.Table.from_arrays(
        [pa.array(times, type=pa.timestamp('us', tz='UTC'))] +
        [pa.array(values[:, i], mask=np.isnan(values[:, i])) for i in range(len(names))],
        schema=schema(names))


def main():
    if (OUT / 'manifest.json').exists() or any(OUT.glob('*/*.parquet*')):
        raise SystemExit(f'Refusing to overwrite existing generated data: {OUT}')
    source = pq.read_table(SOURCE)
    original_names = source.column_names[1:]
    assert len(original_names) == 25
    times = source.column(0).cast(pa.int64()).to_numpy()
    assert np.all(np.diff(times) > 0) and np.all(times % 1000 == 0)
    rel = times - times[0]
    cycle = int(rel[-1]) + 100000
    duration = int((END - START).total_seconds() * 1000000)
    epoch = int(START.timestamp() * 1000000)
    values = np.column_stack([source.column(name).to_numpy() for name in original_names])
    assert np.all(np.isfinite(values) | np.isnan(values))
    for folder in ('hda', 'da'):
        (OUT / folder).mkdir(parents=True, exist_ok=True)
    groups, records, mapping = [], [], []
    all_names = [f'oom_test_{i:03d}' for i in range(300)]
    for group in range(12):
        # A deterministic subsecond phase shift preserves each tag's circular
        # interval sequence, but prevents all twelve copies updating together.
        phase = group * 197000
        shifted = (rel + phase) % cycle
        order = np.argsort(shifted)
        shifted, samples = shifted[order], values[order]
        names = all_names[group * 25:(group + 1) * 25]
        groups.append((shifted, samples, names))
        hda_shifted = (shifted + int((ANCHOR - START).total_seconds() * 1000000)) % cycle
        hda_order = np.argsort(hda_shifted)
        hda_shifted, hda_samples = hda_shifted[hda_order], samples[hda_order]
        for i, name in enumerate(names):
            mapping.append({'tag': name, 'source_tag': original_names[i], 'phase_ms': phase // 1000})
        path = OUT / 'hda' / f'oom_test_{group:02d}.parquet'
        temp = path.with_suffix('.parquet.tmp')
        rows, counts = 0, np.zeros(25, dtype=np.int64)
        first, last = None, None
        with pq.ParquetWriter(temp, schema(names), compression='zstd') as writer:
            for offset in range(0, duration, cycle):
                stop = np.searchsorted(hda_shifted, duration - offset)
                for lo in range(0, stop, BATCH):
                    hi = min(lo + BATCH, stop)
                    t = hda_shifted[lo:hi] + offset + epoch
                    v = hda_samples[lo:hi]
                    writer.write_table(table(t, v, names))
                    rows += len(t)
                    counts += np.sum(~np.isnan(v), axis=0)
                    first = int(t[0]) if first is None else first
                    last = int(t[-1])
        temp.replace(path)
        records.append({'file': str(path.relative_to(OUT)), 'rows': rows,
                        'observations': int(counts.sum()), 'per_tag': dict(zip(names, map(int, counts))),
                        'first_us': first, 'last_us': last, 'sha256': sha(path)})
        print(f'HDA {group + 1}/12: {rows:,} rows, {int(counts.sum()):,} points', flush=True)
    timeline = np.unique(np.concatenate([g[0] for g in groups]))
    timeline = timeline[timeline < DA_DURATION_US]
    path = OUT / 'da' / 'oom_test.parquet'
    temp = path.with_suffix('.parquet.tmp')
    da_counts = np.zeros(300, dtype=np.int64)
    with pq.ParquetWriter(temp, schema(all_names), compression='zstd') as writer:
        for lo in range(0, len(timeline), BATCH):
            t = timeline[lo:lo + BATCH]
            v = np.full((len(t), 300), np.nan)
            for group, (shifted, samples, _) in enumerate(groups):
                a, b = np.searchsorted(shifted, [t[0], t[-1]], side='left')
                b = np.searchsorted(shifted, t[-1], side='right')
                positions = np.searchsorted(t, shifted[a:b])
                v[positions, group * 25:(group + 1) * 25] = samples[a:b]
            writer.write_table(table(t + int(ANCHOR.timestamp() * 1000000), v, all_names))
            da_counts += np.sum(~np.isnan(v), axis=0)
    temp.replace(path)
    records.append({'file': str(path.relative_to(OUT)), 'rows': len(timeline),
                    'observations': int(da_counts.sum()), 'sha256': sha(path)})
    manifest = {'source': str(SOURCE.relative_to(ROOT)), 'source_sha256': sha(SOURCE),
                'timezone': 'UTC', 'start_inclusive': START.isoformat(), 'end_exclusive': END.isoformat(),
                'hda_anchor': ANCHOR.isoformat(), 'da_start_inclusive': ANCHOR.isoformat(),
                'tags': 300, 'hda_observations': sum(r['observations'] for r in records[:-1]),
                'source_cycle_us': cycle, 'da_duration_us': DA_DURATION_US,
                'da_loop_us': int(timeline[-1] - timeline[0]) + 100000,
                'quality': 'No status columns; finite observations default Good(0); NULL means no observation',
                'mapping': mapping, 'files': records}
    (OUT / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding='utf-8')
    print('Generated; verify independently before deployment.', flush=True)


if __name__ == '__main__':
    main()
