# hda_mocker_4

中文详细文档：

- [功能说明](docs/HDA_MOCKER_4_FUNCTIONS.md)
- [数据集提交规范](docs/HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md)
- [使用手册](docs/HDA_MOCKER_4_MANUAL.md)

This preset is a small, file-based OPC UA Historical Access mocker. Go owns the
DuckDB history database, import, playback, UA Read/Subscribe and raw
HistoryRead. Python is only an example source adapter and writes Parquet; it
never opens the runtime database.

## Value and status columns

HDA Parquet starts with `Timestamp`, followed by required numeric `tag` columns
and optional integer `tag.__status` columns. DA Parquet has required `tag`
columns plus optional `tag.__status`, `tag.__hda_value`, and
`tag.__hda_status` columns. Status values are complete OPC UA StatusCodes
(`UInt32`, 0..4294967295).
A missing status column defaults to 0 (`StatusGood`); a missing `__hda_value` uses
`tag`. A NULL status cell is rejected, while NULL/NaN/Inf values intentionally
produce no value with `BadWaitingForInitialData`.

During playback, `tag`/`__status` is returned by DA and
`__hda_value`/`__hda_status` is written to HDA. This permits DA errors and
normal historical backfill in the same row. Auxiliary columns never create UA
nodes. SourceTimestamp and ServerTimestamp are both the sample timestamp; Read
and subscriptions honor `TimestampsToReturn`, and HistoryRead rejects
`NEITHER`.

## Run

```powershell
go build -o hda_mocker_4.exe .
.\hda_mocker_4.exe --config presets\demo\config.yaml
```

The config directory is the preset root. Only the first-level `hda/*.parquet`
and `da/*.parquet` files are read. `hda` requires a first `Timestamp` column
(`timestamp[us, tz=UTC]`) followed by numeric tag columns. `da` contains only
numeric tag columns and is played in row order. A source without timezone must
be converted explicitly by `prepare.py` before it is placed in `hda`.

`server.ns` configures the actual numeric namespace index used in NodeIds;
`server.namespace` independently configures its URI. For example, `ns: 3`
produces string NodeIds such as `ns=3;s=AI1303A5.PV`.

Each DA file has its own playback period, keyed by its exact file name:

```yaml
playback:
  files:
    hda_all.parquet:
      period_ms: 30000
    dynamic_data_all.parquet:
      period_ms: 60000
```

Every DA file must have exactly one entry, and entries for missing files are
rejected. Files keep independent row progress and schedules.

The current Go DuckDB binding is `github.com/marcboeker/go-duckdb v1.8.5`.
It is a CGO binding and therefore the Windows build needs a C compiler and the
binding's native runtime; do not use `CGO_ENABLED=0`. A single executable is
the distribution target when built with a compatible static-capable toolchain.
`runtime/history.duckdb` and its WAL
files are owned by the process. Stop the process and back up the whole
preset/runtime directory before rebuilding history.

For the complete 34,364,236-sample acceptance dataset, the current UNIQUE
`(tag_id, ts)` layout produced an observed DuckDB file of about 1.43 GB on the
test machine. This is an observation, not a performance guarantee.

HistoryRead supports `ReadRawModifiedDetails` with `IsReadModified=false`,
forward and reverse ranges, per-node `NumValuesPerNode`, and continuation
points. Modified, bounds, processed, event and aggregate requests return the
corresponding unsupported status. Continuation points are session- and
node-bound, capped by `server.max_page_size`, and expire after five minutes.
The gopcua v0.9.1 server does not expose a convenient session-close hook in
this integration, so abandoned points are reclaimed by explicit
`ReleaseContinuationPoints` or the five-minute TTL; session close is not
claimed to synchronously clear them.

Every due playback group uses one UTC source timestamp and commits its live values
before updating the in-memory values used by UA subscriptions. Failed writes
do not publish. DA progress is persisted in DuckDB and wraps to row zero at
EOF; changed files restart at row zero.

The gopcua `server.NewVariableNode` getter is used for each variable. After a
successful playback commit, the process calls the namespace
`ChangeNotification` API once for every changed node; the server then reads
the getter and emits monitored-item data changes. The getter is not a second
history writer.

The checked Windows build uses WinLibs GCC 16.1 because the older TDM-GCC 10.3
cannot link the bundled DuckDB archive. The resulting `hda_mocker_4.exe` is a
single 69.5 MB executable whose imported DLLs are Windows system/UCRT DLLs;
the target machine does not need Go, Python, DuckDB, GCC, or a separate
database service.
