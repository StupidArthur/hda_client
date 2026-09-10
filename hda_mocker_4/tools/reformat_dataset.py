"""Convert a CSV HDA/DA pair to the hda_mocker_4 Parquet layout."""

from __future__ import annotations

import argparse
import csv
import datetime as dt
import hashlib
import json
import math
import os
import tempfile
from pathlib import Path

import pyarrow as pa
import pyarrow.parquet as pq


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def parse_number(raw: str, line: int, tag: str) -> float | None:
    value = raw.strip()
    if not value:
        return None
    try:
        number = float(value)
    except ValueError as exc:
        raise ValueError(f"line {line}: {tag} is not numeric: {raw!r}") from exc
    if not math.isfinite(number):
        raise ValueError(f"line {line}: {tag} is NaN/Inf")
    return number


def read_csv(path: Path) -> tuple[list[str], list[list[str]]]:
    with path.open("r", encoding="utf-8-sig", newline="") as stream:
        rows = list(csv.reader(stream))
    if len(rows) < 2 or not rows[0] or rows[0][0] != "timeStamp":
        raise ValueError(f"{path}: first column must be timeStamp and data is required")
    header = rows[0][1:]
    if not header or any(not tag.strip() for tag in header) or len(set(header)) != len(header):
        raise ValueError(f"{path}: tag columns must be non-empty and unique")
    width = len(rows[0])
    if any(len(row) != width for row in rows[1:]):
        raise ValueError(f"{path}: a data row has a different column count")
    return header, rows[1:]


def atomic_write(table: pa.Table, target: Path) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=target.stem + ".", suffix=".tmp", dir=target.parent)
    os.close(fd)
    temporary = Path(name)
    try:
        pq.write_table(table, temporary, compression="zstd")
        temporary.replace(target)
    finally:
        temporary.unlink(missing_ok=True)


def normalize(args: argparse.Namespace) -> dict:
    hda_tags, hda_rows = read_csv(args.hda)
    da_tags, da_rows = read_csv(args.da)
    if hda_tags != da_tags:
        raise ValueError("HDA and DA tag columns must match in name and order")
    end = dt.datetime.fromisoformat(args.hda_end.replace("Z", "+00:00"))
    if end.tzinfo is None:
        raise ValueError("--hda-end must include a timezone")
    end = end.astimezone(dt.timezone.utc)
    step = dt.timedelta(seconds=args.interval_sec)
    timestamps = [end - step * (len(hda_rows) - 1 - i) for i in range(len(hda_rows))]
    columns = {tag: [] for tag in hda_tags}
    for line, row in enumerate(hda_rows, 2):
        for tag, raw in zip(hda_tags, row[1:]):
            columns[tag].append(parse_number(raw, line, tag))
    da_columns = {tag: [] for tag in da_tags}
    for line, row in enumerate(da_rows, 2):
        for tag, raw in zip(da_tags, row[1:]):
            da_columns[tag].append(parse_number(raw, line, tag))
    output = args.output
    hda_path = output / "hda" / f"{args.dataset}.parquet"
    da_path = output / "da" / f"{args.dataset}.parquet"
    hda_table = pa.table({"Timestamp": pa.array(timestamps, type=pa.timestamp("us", tz="UTC")), **{tag: pa.array(values, type=pa.float64()) for tag, values in columns.items()}})
    da_table = pa.table({tag: pa.array(values, type=pa.float64()) for tag, values in da_columns.items()})
    atomic_write(hda_table, hda_path)
    atomic_write(da_table, da_path)
    manifest = {
        "dataset": args.dataset,
        "timezone": "UTC",
        "interval_sec": args.interval_sec,
        "playback_period_ms": args.period_ms,
        "tags": hda_tags,
        "hda": {"file": str(hda_path.relative_to(output)), "rows": len(hda_rows), "sha256": sha256(hda_path)},
        "da": {"file": str(da_path.relative_to(output)), "rows": len(da_rows), "sha256": sha256(da_path)},
    }
    (output / f"{args.dataset}.manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    return manifest


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hda", type=Path, required=True)
    parser.add_argument("--da", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--dataset", required=True)
    parser.add_argument("--hda-end", required=True, help="UTC/RFC3339 end timestamp")
    parser.add_argument("--interval-sec", type=int, required=True)
    parser.add_argument("--period-ms", type=int, required=True)
    print(json.dumps(normalize(parser.parse_args()), indent=2))


if __name__ == "__main__":
    main()
