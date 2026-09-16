"""Build a hda_mocker_4 preset from the external industrial_hda_v2 event fixture.

The external fixture is a long-event table (one row per OPC UA event) carrying
case-level problems. mocker_4 loads a wide HDA table where every tag shares the
same Timestamp rows, and its import materializes one sample per (row x tag)
cell - a NULL cell becomes a real "no value + Bad" sample. Because the twelve
tags have asynchronous timestamps, merging them into one wide file would invent
events for tags that had none. This tool therefore writes one HDA file per tag,
so each tag keeps exactly its own events.

Conversion rules (documented in the generated manifest and README):
  * value_kind finite -> the IEEE value; nan/pos_inf/neg_inf -> IEEE non-finite;
    null -> NULL. mocker_4 currently normalizes every non-finite/NULL cell to
    "no value + BadWaitingForInitialData", so those rows lose their original
    Good quality and their exact kind.
  * quality -> <tag>.__status (complete OPC UA StatusCode).
  * events are sorted by timestamp; an identical (tag, time, value, quality)
    duplicate is reduced to one; a conflicting duplicate (same tag/time, other
    value) cannot be represented and the extra row is dropped and recorded.
"""

from __future__ import annotations

import hashlib
import json
from collections import Counter
from pathlib import Path

import pyarrow as pa
import pyarrow.parquet as pq

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "docs" / "external" / "industrial_hda_v2.parquet"
OUTPUT = ROOT / "presets" / "qa_v2"
DATASET = "qa_v2"

CONFIG = """server:
  endpoint: opc.tcp://0.0.0.0:18980
  ns: 3
  namespace: urn:hda:mocker4
  max_page_size: 5000
playback:
  files: {}
history:
  retention_days: 0
"""


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def cell_value(kind: str, value):
    if kind == "null":
        return None
    if kind == "finite":
        return float(value)
    if kind == "nan":
        return float("nan")
    if kind == "pos_inf":
        return float("inf")
    if kind == "neg_inf":
        return float("-inf")
    raise ValueError(f"unknown value_kind {kind!r}")


def main() -> None:
    src = pq.read_table(SOURCE).to_pydict()
    rows = len(src["scenario"])

    per_tag: dict[str, list[tuple]] = {}
    for i in range(rows):
        per_tag.setdefault(src["tag"][i], []).append(
            (src["timestamp"][i], src["value_kind"][i], src["value"][i], int(src["quality"][i]))
        )

    (OUTPUT / "hda").mkdir(parents=True, exist_ok=True)
    (OUTPUT / "da").mkdir(parents=True, exist_ok=True)
    (OUTPUT / "runtime").mkdir(parents=True, exist_ok=True)
    (OUTPUT / "da" / ".gitkeep").write_text("", encoding="utf-8")
    (OUTPUT / "runtime" / ".gitkeep").write_text("", encoding="utf-8")
    (OUTPUT / "config.yaml").write_text(CONFIG, encoding="utf-8")

    summary = {
        "dataset": DATASET,
        "source": str(SOURCE.relative_to(ROOT)).replace("\\", "/"),
        "source_rows": rows,
        "tags": {},
        "totals": Counter(),
    }

    for tag in sorted(per_tag):
        events = sorted(per_tag[tag], key=lambda x: x[0])
        timestamps, values, statuses = [], [], []
        last_ts = None
        last_key = None
        for ts, kind, value, quality in events:
            key = (kind, None if kind != "finite" else float(value), quality)
            if ts == last_ts:
                if key == last_key:
                    summary["totals"]["identical_duplicates_dropped"] += 1
                    continue
                summary["totals"]["conflicting_duplicates_dropped"] += 1
                summary.setdefault("conflicts", []).append(
                    {"tag": tag, "timestamp": ts.isoformat(), "kept": last_key, "dropped": key}
                )
                continue
            timestamps.append(ts)
            values.append(cell_value(kind, value))
            statuses.append(quality)
            last_ts, last_key = ts, key
            if kind != "finite":
                summary["totals"][f"non_finite_{kind}"] += 1

        assert all(b > a for a, b in zip(timestamps, timestamps[1:])), f"{tag}: not strictly increasing"

        path = OUTPUT / "hda" / f"{tag}.parquet"
        pq.write_table(
            pa.table(
                {
                    "Timestamp": pa.array(timestamps, type=pa.timestamp("us", tz="UTC")),
                    tag: pa.array(values, type=pa.float64()),
                    f"{tag}.__status": pa.array(statuses, type=pa.uint32()),
                }
            ),
            path,
            compression="zstd",
        )
        summary["tags"][tag] = {"rows": len(timestamps), "sha256": sha256(path)}

    (OUTPUT / "manifest.json").write_text(
        json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8"
    )
    print(json.dumps(summary, indent=2, default=str))


if __name__ == "__main__":
    main()
