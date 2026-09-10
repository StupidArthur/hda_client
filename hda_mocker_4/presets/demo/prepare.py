"""Example external-to-standard adapter.

The source timestamps in this example are deliberately naive. SOURCE_TZ is a
required argument: guessing a timezone would silently corrupt HDA history.
Requires pandas and pyarrow. It writes only temporary Parquet files followed
by an atomic rename, and refuses to replace an existing valid output.
"""
from __future__ import annotations
import argparse
from pathlib import Path
from zoneinfo import ZoneInfo
import pandas as pd
import pyarrow.parquet as pq

def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("input", type=Path, help="CSV with Timestamp and tag columns")
    ap.add_argument("output", type=Path)
    ap.add_argument("--source-timezone", required=True)
    ap.add_argument("--mode", choices=("hda", "da"), required=True)
    a = ap.parse_args()
    if a.output.exists():
        raise SystemExit(f"refusing to overwrite existing output: {a.output}")
    with a.input.open("rb") as fh:
        magic = fh.read(4)
    if magic == b"PAR1" or a.input.suffix.lower() == ".parquet" or a.input.suffix == "":
        try:
            df = pq.read_table(a.input).to_pandas()
        except Exception:
            if a.input.suffix.lower() not in (".csv", ".tsv"):
                raise SystemExit("input is not a readable Parquet file; use an explicit .csv/.tsv suffix for text")
            df = pd.read_csv(a.input, sep="\t" if a.input.suffix.lower()==".tsv" else ",")
    elif a.input.suffix.lower() in (".csv", ".tsv"):
        df = pd.read_csv(a.input, sep="\t" if a.input.suffix.lower()==".tsv" else ",")
    else:
        raise SystemExit("input must be Parquet or explicitly named .csv/.tsv")
    if a.mode == "hda":
        if "Timestamp" not in df.columns:
            raise SystemExit("HDA input needs Timestamp")
        t = pd.to_datetime(df.pop("Timestamp"), errors="raise")
        if t.dt.tz is not None:
            raise SystemExit("source Timestamp must be timezone-naive; timezone is supplied explicitly")
        t = t.dt.tz_localize(ZoneInfo(a.source_timezone), ambiguous="raise", nonexistent="raise").dt.tz_convert("UTC").dt.floor("us")
        if t.isna().any() or not t.is_monotonic_increasing or t.duplicated().any():
            raise SystemExit("HDA Timestamp must be non-null, strictly increasing after UTC microsecond normalization")
        df.insert(0, "Timestamp", t)
    else:
        if "Timestamp" in df.columns:
            df = df.drop(columns=["Timestamp"])
    if df.empty or len(df.columns) == 0 or len(df) == 0:
        raise SystemExit("output must contain at least one tag and one row")
    a.output.parent.mkdir(parents=True, exist_ok=True)
    tmp = a.output.with_name(a.output.name + ".tmp")
    df.to_parquet(tmp, index=False)
    tmp.replace(a.output)

if __name__ == "__main__":
    main()
