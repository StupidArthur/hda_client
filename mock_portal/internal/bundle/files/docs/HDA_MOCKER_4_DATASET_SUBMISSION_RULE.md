# HDA Mocker 4 数据集提交规范

**v1.5.0 更新：** [统一版规则](HDA_MOCKER_4_RELEASE_1_5_0.md) 优先于下文旧约定。空且未标 Bad 表示无观测；显式 Bad 可无值；DA 可带 UTC Timestamp；无 HDA 质量覆盖时沿用 DA 质量。迁移须使用新 runtime。

本文对应 `hda_mocker_4 v1.5.0`，供数据集制作方和验收方使用。已有 bundled preset 的转换结果见 [格式迁移记录](../presets/FORMAT_MIGRATION_1_5_0.md)。提交物必须是 Parquet 文件以及对应的播放配置，不接受把 CSV、Excel 或 DuckDB 文件直接放入运行目录。

## 1. 提交目录

每个数据集作为一个独立预置目录提交：

```text
dataset_name/
├── config.yaml
├── hda/
│   └── one_or_more.parquet
└── da/
    └── one_or_more.parquet
```

- 预存历史文件放入 `hda/`。
- 需要轮播和实时入库的文件放入 `da/`。
- 只识别两个目录第一层、扩展名为 `.parquet` 的普通文件。
- 文件写入完成前使用其他后缀，完成后再原子重命名为 `.parquet`，避免服务读到半文件。
- 不要提交 `runtime/history.duckdb`、WAL、临时文件或旧运行结果；运行时目录由服务创建。
- 至少提交一份有效 HDA 或 DA 文件。

## 2. 通用列规则

- 表至少有 1 行、1 个基础位号列。
- 列名必须非空且在文件内唯一，大小写敏感。
- 位号名应直接使用 OPC UA 节点期望的完整名称，例如 `AI1303A5.PV`。
- `Timestamp` 是保留列名；HDA 必须使用，DA 可选用作时间轴。
- 以 `.__status`、`.__hda_value`、`.__hda_status` 结尾的列是辅助列，不是独立位号。
- 每个辅助列必须存在同名基础位号列。例如有 `AI1303A5.PV.__status` 时，必须同时有 `AI1303A5.PV`。
- 基础位号值和 `.__hda_value` 必须写成 Parquet `DOUBLE`/float64；不要依赖整数、字符串或 decimal 类型的隐式转换。
- status 列必须是 Parquet 整数类型，每个值在 0～4294967295 内且不得为 NULL。推荐使用 uint32，表示完整 OPC UA StatusCode。

## 3. HDA 文件格式

HDA 是带明确历史时间的宽表，列结构如下：

| 顺序 | 列 | 类型 | 必填 | 说明 |
|---:|---|---|---|---|
| 1 | `Timestamp` | `timestamp[us, tz=UTC]` | 是 | 必须是第一列 |
| 2... | `tag` | float64 | 是 | 历史值 |
| 任意后续 | `tag.__status` | 整数 0～4294967295 | 否 | 对应历史 StatusCode，缺列默认 0（StatusGood） |

示例：

```text
Timestamp                    AI1303A5.PV  AI1303A5.PV.__status
2026-09-12T00:00:00.000000Z  12.5         0
2026-09-12T00:00:01.000000Z  12.8         0
```

HDA 时间规则：

- `Timestamp` 不得为 NULL。
- 必须携带 UTC 时区，最终 Parquet 类型应为 `timestamp[us, tz=UTC]`。
- 必须按文件行严格递增；不要求等间隔。
- 服务按微秒保存。转换到 UTC 微秒后出现相同时间也视为重复并拒绝。
- SourceTimestamp 和 ServerTimestamp 均使用此 `Timestamp`，无需额外提交 ServerTimestamp 列。

HDA 只允许 `tag` 和可选 `tag.__status`；不得包含 `Timestamp` 以外的 DA 专用 `.__hda_value` 或 `.__hda_status` 列。

## 4. DA 文件格式

DA 是宽表，可无时间列按固定周期轮播，也可带 UTC Timestamp 按源时间差循环播放：

| 列 | 类型 | 必填 | 整列缺失时的行为 |
|---|---|---|---|
| `tag` | float64 | 是 | 无默认值 |
| `tag.__status` | 整数 0～4294967295 | 否 | DA StatusCode 使用 0（StatusGood） |
| `tag.__hda_value` | float64 | 否 | 实时入库值使用 `tag` |
| `tag.__hda_status` | 整数 0～4294967295 | 否 | 实时入库继承当前行 DA StatusCode |

无 Timestamp 的 DA 由 period_ms 决定播放节奏。带 Timestamp 的 DA 要求 UTC 微秒时间严格递增，按相对时间差映射到实时播放起点，每100ms检查；每个 preset 仅允许一份时间轴 DA，不能与无时间列 DA 混用。其配置仍需填写 period_ms，建议100。

完整示例：

```text
tag    tag.__status  tag.__hda_value  tag.__hda_status
999.0  2150694912    12.0             0
998.0  2150694912    13.0             0
```

上述两行的语义是：DA 客户端读到 999/998 且质量为 BadNoCommunication；同一轮实时 HDA 入库 12/13 且质量为 Good。

注意“缺列”和“空单元格”语义不同：

- 整个 `tag.__hda_value` 列不存在：每行都回退使用 `tag`。
- `tag.__hda_value` 列存在但某行为空：不回退到 `tag`；仅在 HDA 对应质量明确为 Bad 时写入空值异常点，否则不追加。
- `tag.__status` 整列不存在：默认 0（StatusGood）；`tag.__hda_status` 整列不存在：继承当前行 DA 质量。
- status 列存在但某个单元格为空：属于无效数据集，拒绝启动，不按默认值处理。

## 5. 异常值规则

基础值或 HDA 覆盖值允许使用 NULL、NaN、`+Inf`、`-Inf` 表达无有效数值。是否产生观测由对应质量决定：

```text
空值 + 非 Bad（含缺列默认 Good）：无观测，HDA 跳过，DA 保持前值及前质量
空值 + 明确 Bad：真实异常观测，HDA 保存空值和指定 Bad，DA 更新为空值和指定 Bad
有限值（包括0）+ 任意有效质量：真实观测，保存值和对应质量
```

例如 `tag=NULL` 且 `tag.__status=0` 是无观测，不自动改为 Bad。若要保留旧“空值异常”行为，应明确写 `2150760448`（BadWaitingForInitialData）或符合业务含义的其他 Bad。未标 Bad 的空值不会覆盖 DA 之前的异常状态。

如果目的是模拟“值仍然可见，但质量异常”，不要把 value 写空；应填写正常数值，并在 `tag.__status` 中写入完整 Bad StatusCode，例如 `2150694912`（BadNoCommunication）。

## 6. status 填写规则

status 填完整 OPC UA StatusCode，范围为 0～4294967295；不填十六进制字符串。推荐直接存为 uint32 整数，服务直接透传，不执行 OPC DA 质量字节映射。

从 v1.1.0/v1.2.0 数据升级时必须转换旧的 OPC DA 质量字节，不能原样沿用。例如旧值 `192` 应改为 OPC UA Good 的 `0`，旧值 `24` 应按原业务含义改为 BadNoCommunication 的 `2150694912`。v1.2.1 会把输入数字直接当作 OPC UA StatusCode，不再猜测或转换旧格式。转换后的 Parquet 应使用新的预置目录和新的 `runtime/`；旧 DuckDB 中已经按旧规则导入的质量码不会自动迁移。

常用值：

| 十进制 | 十六进制 | 用途 |
|---:|---:|---|
| 0 | `0x00000000` | Good，默认值 |
| 9830400 | `0x00960000` | GoodLocalOverride |
| 2150694912 | `0x80310000` | BadNoCommunication |
| 2156724224 | `0x808D0000` | BadOutOfService |
| 2156527616 | `0x808A0000` | BadNotConnected |
| 1083179008 | `0x40900000` | UncertainLastUsableValue |

数据提供方应在随附说明中列出实际使用过的 StatusCode 及期望含义，避免仅凭数字猜测测试场景。

## 7. 多文件与位号集合规则

- 同一个位号不能出现在两份 HDA 文件中。
- 同一个位号不能出现在两份 DA 文件中。
- 一份文件可包含多个位号，各位号共用该文件的行数、HDA 时间轴或 DA 播放游标。
- 需要不同播放周期的位号应拆到不同 DA 文件中。
- 同一 DA 文件中的所有位号每轮一起前进一行。
- 当预置集中 HDA 和 DA 同时存在时，两侧基础位号集合必须完全一致；辅助列不计入集合。
- 只有 HDA 或只有 DA 的专项数据集允许存在，无需伪造另一侧文件。

如果多个 DA 文件恰好同时到期，它们会共享同一个播放时间并合并为一次实时入库事务。

## 8. config.yaml 提交规则

每一份 DA 文件都必须有且只有一个同名播放配置；不允许漏配，也不允许配置不存在的 DA 文件：

```yaml
server:
  endpoint: opc.tcp://0.0.0.0:4840
  ns: 3
  namespace: urn:hda-mocker:submitted-dataset
  max_page_size: 5000

playback:
  files:
    fast_faults.parquet:
      period_ms: 100
    normal_process.parquet:
      period_ms: 1000

history:
  retention_days: 0
```

- 文件名必须精确匹配，包括扩展名和大小写。
- `period_ms` 必须大于 0；固定周期文件按此间隔播放，时间轴文件按 Timestamp 时间差播放，仍每100ms检查。
- 仅提交 HDA 时，`playback.files` 使用空映射 `{}`。
- `max_page_size` 必须大于 0。
- `retention_days` 必须大于等于 0；0 表示实时 HDA 不自动清理。
- 配置不接受未知字段，拼写错误会直接报错。

## 9. 推荐随数据集提供的说明

除 Parquet 和配置外，建议附一份简短清单：

- 数据集名称、版本、制作日期和联系人。
- 每个文件的行数、基础位号数、时间范围或播放周期。
- DA 每一段行范围对应的正常/异常场景。
- 使用过的 status 值及业务含义。
- 哪些位号使用了独立 `.__hda_value`/`.__hda_status`，预期 DA 与 HDA 分别是什么。
- 文件 SHA-256，便于传输和投产前核对。

## 10. 提交前验收清单

- [ ] 所有输入均为可读取的 Parquet，且至少有一行。
- [ ] HDA 第一列是带 UTC 时区的微秒 Timestamp，非空并严格递增。
- [ ] DA 已明确固定周期或时间轴模式；时间轴仅一份文件、UTC微秒严格递增，不与固定模式混用。
- [ ] 基础值和 `.__hda_value` 是 float64。
- [ ] status 是整数、无 NULL、全部在 0～4294967295。
- [ ] 所有辅助列都有对应基础位号。
- [ ] HDA 未使用 `.__hda_value` 或 `.__hda_status`。
- [ ] NULL/NaN/Inf 已区分无观测和显式 Bad 异常；旧故障空值已补明确 Bad，普通对齐空格保持无观测。
- [ ] HDA 内部、DA 内部均无跨文件重复位号。
- [ ] 同时提交 HDA 和 DA 时，二者基础位号集合完全一致。
- [ ] 每份 DA 文件都配置了准确的 `period_ms`，且没有多余配置项。
- [ ] 未包含 runtime 数据库、WAL 或未写完的 `.parquet` 文件。
- [ ] 已记录文件行数、位号数、场景说明和 SHA-256。
