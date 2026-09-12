# HDA Mocker 4 TODO

## DataValue 双时间戳

- [x] DA Read/订阅与 HDA HistoryRead 按 `TimestampsToReturn` 返回时间戳。
- [x] `HistoryRead(NEITHER)` 返回 `BadTimestampsToReturnInvalid`。
- [x] 当前模拟约定：`ServerTimestamp` 与 `SourceTimestamp` 相同；不增加 Parquet 或 DuckDB 字段。
