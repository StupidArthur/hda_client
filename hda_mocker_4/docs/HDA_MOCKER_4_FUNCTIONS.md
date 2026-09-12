# HDA Mocker 4 功能说明

本文对应 `hda_mocker_4 v1.2.1`，用于说明当前已经实现的能力、运行语义、配置方法和已知性能边界。

## 1. 用途与边界

HDA Mocker 4 是一个基于文件数据集的 OPC UA DA/HDA 模拟服务，主要用于：

1. 提供具有不同数值、质量码和时间分布的预存 HDA 数据，验证客户端的历史拉取、分页、采样和聚合算法。
2. 按配置周期轮播 DA 数据，并将同一轮数据实时写入 HDA，模拟持续运行的数据源。
3. 让同一位号的 DA 对外呈现异常，而实时写入 HDA 的数据保持正常，用于验证业务端发现 DA 异常后通过 HDA 回补数据的流程。

服务由一个 Windows exe 独立运行。Parquet 是输入，DuckDB 是运行时历史库；不需要单独部署 Python、Go 或数据库服务。直接运行 exe 可打开 Windows 管理界面，传入 `--config` 保持原有脚本启动方式。

## 2. 目录与启动

`--config` 指定的 `config.yaml` 所在目录就是一个完整预置集的根目录：

```text
preset/
├── config.yaml
├── hda/                 # 预存历史数据
│   └── *.parquet
├── da/                  # 轮播数据
│   └── *.parquet
└── runtime/             # 自动创建
    └── history.duckdb
```

只扫描 `hda/` 和 `da/` 第一层中的 `.parquet` 文件，不递归扫描子目录。允许只有 HDA 或只有 DA，但两者不能同时为空。

启动示例：

```powershell
.\hda_mocker_4_v1.2.1.exe --config .\preset\config.yaml
```

版本确认：

```powershell
.\hda_mocker_4_v1.2.1.exe --version
```

## 3. OPC UA 节点

所有数据位号均创建为 `Objects/HDA_Mocker` 下的 Variable 节点。NodeId 使用配置的数字命名空间和原始位号名，例如：

```text
Objects (ns=0;i=85)
└── HDA_Mocker (ns=3;s=HDA_Mocker)
    └── AI1303A5.PV (ns=3;s=AI1303A5.PV)
```

`.__status`、`.__hda_value` 和 `.__hda_status` 是数据控制列，不会创建为 UA 节点。

## 4. 预存 HDA

预存 HDA 来自 `hda/*.parquet`：

- 第一列固定为 `Timestamp`，表示每一行历史样本的时间。
- `tag` 列表示历史值。
- 可选的 `tag.__status` 表示该历史值的完整 OPC UA `StatusCode`（UInt32）；整列缺失时默认 `0`（StatusGood）。
- 数据启动时导入 `runtime/history.duckdb`，来源标记为 `import`。
- 文件按 SHA-256 识别。已完整导入且内容未变化的文件不会重复导入；同一路径文件在导入后被修改会拒绝启动，避免静默污染已有历史库。
- 相同位号、相同时间的既有记录如果值或质量不同，会作为冲突拒绝导入。

HistoryRead 当前支持 Raw HistoryRead：正向或反向时间范围、`NumValuesPerNode` 和 continuation point。单页上限为客户端请求数量和 `server.max_page_size` 的较小值；客户端填 0 时使用服务端上限。continuation point 与会话、节点和时间戳模式绑定，空闲 5 分钟后回收，总数上限为 1024。

当前不支持 Modified、Bounds、Processed、Event 和 Aggregate 历史请求，会返回对应的不支持状态。

## 5. DA 轮播

DA 来自 `da/*.parquet`，没有 `Timestamp` 列，严格按文件行顺序播放：

- 服务启动后，每个 DA 文件立即播放第一行。
- 每个文件有独立的播放周期、当前行和调度进度。
- 到达文件末尾后从第 0 行重新循环。
- 播放位置保存在 DuckDB 中；文件 SHA-256 未变化时重启续播，文件变化时从第 0 行开始。
- 同一时刻到期的多个文件组成同一轮，使用同一个 UTC 时间戳并在同一个事务中写入。
- 写入事务成功后才更新 DA 当前值并发送订阅通知；写入失败不会发布一份没有入库的数据。

一个 DA 位号在首次成功播放前读取时返回 `BadWaitingForInitialData`，不会用预存 HDA 的最新值冒充实时 DA 值。只有 HDA、没有 DA 的位号读取时可以返回其最新历史样本。

## 6. DA 展示与实时 HDA 入库解耦

DA 文件可为每个位号提供四类列：

| 列 | 用途 | 整列缺失时的默认值 |
|---|---|---|
| `tag` | DA Read/订阅对外展示值 | 必填 |
| `tag.__status` | DA Read/订阅质量码 | `0`（StatusGood） |
| `tag.__hda_value` | 本轮实时写入 HDA 的值 | 使用 `tag` |
| `tag.__hda_status` | 本轮实时写入 HDA 的质量码 | `0`（StatusGood） |

每轮播放先将 `tag.__hda_value`/`tag.__hda_status` 对应的数据写入 HDA，再用 `tag`/`tag.__status` 更新 DA 和发送订阅通知。因此可以在同一行表达：

```text
DA：值存在，但质量为 BadNoCommunication
HDA：写入正常值，质量为 Good
```

业务端看到 DA 异常后发起 HistoryRead，能够拉到该时刻已经成功入库的正常 HDA 数据进行回补。DA 异常不会自动污染 HDA；反过来，HDA 覆盖列异常也不会改变 DA 展示值。

## 7. 质量码与异常值

数据集中的 status 是完整的 32 位 OPC UA `StatusCode`，服务不再执行 OPC DA 质量字节映射，直接透传和存储。可用范围为 0～4294967295；推荐 Parquet `uint32`。常用示例：

这是 v1.2.1 的数据语义变更。旧数据中的 OPC DA 质量字节必须先转换；例如旧 Good `192` 改为 OPC UA Good `0`，不能直接复用旧 status 数字。

| OPC UA StatusCode | 含义 |
|---:|---|
| `0` (`0x00000000`) | Good（默认值） |
| `9830400` (`0x00960000`) | GoodLocalOverride |
| `2150694912` (`0x80310000`) | BadNoCommunication |
| `2156724224` (`0x808D0000`) | BadOutOfService |
| `1083179008` (`0x40900000`) | UncertainLastUsableValue |

值单元格为 `NULL`、`NaN`、`+Inf` 或 `-Inf` 时，服务返回/存储“没有 Value，质量为 `BadWaitingForInitialData`”。该规则优先于该行显式填写的 status，避免出现无有效数值但质量仍为 Good 的矛盾数据。

status 列如果存在，则任何单元格都不能为 NULL，且必须是 0～4294967295 的整数；不合规文件会在启动校验阶段被拒绝。

## 8. SourceTimestamp 与 ServerTimestamp

当前 mocker 只有一个样本时间源，因此约定：

```text
ServerTimestamp = SourceTimestamp = 样本时间
```

- 预存 HDA：二者均取 HDA Parquet 的 `Timestamp`。
- DA 和实时入库 HDA：二者均来自该轮播放开始时生成的 UTC 时间，同轮到期文件共享同一个时间源；写入 DuckDB 的 HDA 时间归一化为微秒精度，因此 DA 当前值可能比历史查询结果多出亚微秒精度。
- DuckDB 历史时间统一为 UTC 微秒精度，不为 ServerTimestamp 增加独立的 Parquet 列或数据库列。
- DA Read 和订阅按客户端的 `TimestampsToReturn` 返回 Source、Server、Both 或 Neither；非法枚举值返回 `BadTimestampsToReturnInvalid`。
- HistoryRead 支持 Source、Server 和 Both；HistoryRead 请求 Neither 或非法枚举值时返回 `BadTimestampsToReturnInvalid`。

## 9. 多数据集播放配置

每个 `da/*.parquet` 必须在 `playback.files` 中按精确文件名配置一次，且不能为不存在的 DA 文件配置周期：

```yaml
server:
  endpoint: opc.tcp://0.0.0.0:4840
  ns: 3
  namespace: urn:hda-mocker:demo
  max_page_size: 5000

playback:
  files:
    fast_loop.parquet:
      period_ms: 100
    slow_loop.parquet:
      period_ms: 30000

history:
  retention_days: 0
```

配置含义：

- `endpoint`：监听地址。
- `ns`：NodeId 使用的数字命名空间，范围 1～65535。
- `namespace`：命名空间 URI，与 `ns` 分别配置。
- `max_page_size`：HistoryRead 单节点单页硬上限，必须大于 0。
- `period_ms`：对应 DA 文件播放一行的周期，必须大于 0。
- `retention_days`：实时入库 HDA 的保留天数；0 表示不清理。清理只删除 `live` 数据，不删除预存 `import` 数据；启动时清理一次，之后在没有活动 continuation point 时每小时尝试一次。

不同文件可以使用不同周期，但同一个目录内的 DA 位号不能重复，否则无法确定由哪个播放游标负责。同理，HDA 位号也不能跨 HDA 文件重复。当 HDA 和 DA 同时存在时，两边的基础位号集合必须完全相同；只有 HDA 或只有 DA 的预置集不受此集合匹配规则限制。

## 10. 一致性与运行维护

- 实时 HDA 样本、播放游标和批次状态在同一 DuckDB 事务中提交。
- 实时样本时间必须晚于这些 DA 位号已有的最新历史时间，也必须晚于上一次实时批次时间；否则该轮不提交。
- `runtime/history.duckdb` 及 WAL 由进程独占管理。备份或替换输入数据前应先停止进程，并整体备份预置集的 `runtime` 目录。
- 更换已经导入的 HDA 数据集时，不应直接覆盖同名文件并沿用旧库；应停止服务，使用新的预置目录，或在明确接受重建历史的前提下移走旧 `runtime`。

## 11. 性能评估

### 已有实测

在现有验收机上，完整数据集共导入 `34,364,236` 个“位号 × 时间”样本，采用当前 `UNIQUE(tag_id, ts)` 的 DuckDB 布局后，数据库文件约为 `1.43 GB`。这是单次环境观测值，不是容量保证，也不能直接外推不同磁盘、内存、位号数和时间分布下的结果。

v1.1.0 已完成真实 exe 的端到端功能验收，覆盖 DA BadNoCommunication、DA/HDA 独立值与质量、预存 HDA 质量、订阅、实时入库和双时间戳。该验收用于证明语义正确，不等同于压力测试。

### 性能特性与风险点

- 首次启动成本主要取决于 HDA 样本总量、Parquet 读取速度、DuckDB 写入和唯一键维护；后续使用同一未变化数据集启动会跳过已完成导入。
- 每轮实时写入量约为本轮到期文件中的基础位号总数。周期越短、位号越多，DuckDB 提交和 UA 订阅通知压力越高。
- 每次轮播会校验 DA 文件 SHA-256，并按当前行读取 Parquet。超大 DA 文件或慢盘会增加轮播耗时。
- HistoryRead 通过分页限制单次返回规模，但总吞吐仍取决于查询时间范围、客户端并发、页大小、磁盘和数据库体积。
- 多个文件同一时刻到期时会合并为一轮事务；这保证时间和提交一致性，也意味着其中一次事务失败会使该轮不发布。

### 上线前建议的评估方法

用拟上线机器和实际规模数据分别测试“仅 HDA 查询”和“DA 轮播同时查询”，至少记录：

1. 首次导入耗时、DuckDB 最终体积和进程峰值内存。
2. 不同 `period_ms` 下每轮实际提交耗时、计划周期与实际周期的偏差、失败/落后次数。
3. 固定时间范围、页大小、并发客户端数后的 HistoryRead 吞吐和 P50/P95/P99 延迟。
4. Read/订阅的通知延迟、CPU、内存和磁盘写入速率。
5. 连续运行后的数据库增长速度，以及 `retention_days` 清理耗时。

当前没有足够数据给出“最大位号数、最短安全周期、最大并发或每秒历史点数”的通用承诺；这些阈值必须以上述目标环境压测结果为准。
