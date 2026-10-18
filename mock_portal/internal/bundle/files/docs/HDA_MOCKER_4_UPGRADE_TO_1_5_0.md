# 旧版组态升级到 v1.5.0

适用范围：`hda_mocker_4` v1.4.3 及之前的 CLI 组态，以及 v1.4.3-basf 系列 preset。更早的定制界面版本需要先整理为本文的 CLI preset 结构，不是直接替换界面程序。

## 1. 升级结论

v1.5.0 将固定周期轮播和 BASF 时间轴轮播合并为一个程序。Parquet 仍然是宽表，不需要改成长表，也不需要新增 `__present`。

本次不能只换 exe 后继续使用旧 runtime。建议新建部署目录，复制并调整 preset，让新程序重新导入历史；旧程序、旧数据、旧配置和旧 runtime 整套保留用于回退。

必须检查的两个数据语义变化：

1. 空值不再自动表示 Bad。没有明确 Bad 状态的 NULL/NaN/Inf 表示“这一时刻没有观测”。
2. DA 追加 HDA 时，没有 `.__hda_status` 列就继承 DA 的质量，不再独立默认 Good。

已有有效数值、完整 OPC UA 质量码及固定周期配置通常可以保留，但必须检查以上两项。旧 OPC DA 的 192/0 质量约定还需单独转换。

## 2. 升级前收集与备份

先记录旧 exe 的 `--version` 输出、启动命令、服务或计划任务启动方式，以及实际使用的 `config.yaml` 路径。

停止旧服务后，完整备份以下内容，避免只复制正在写入的数据库单文件：

- 旧 exe、config.yaml、HDA/DA Parquet、manifest 和源数据转换脚本。
- preset 下整个 `runtime` 目录，包括存在的 WAL 文件。
- 客户端连接地址、Namespace URI、命名空间索引、位号名、订阅参数和历史查询参数。

旧 runtime 中的实时追加历史不一定存在于 Parquet 中。新 runtime 只会从提交的 Parquet 重建初始历史，不会自动迁移旧库追加点或播放游标。若必须连续保留全部生产历史，应先安排独立导出、转换和校验方案；不要把“重导入 Parquet”当成完整数据库迁移。

## 3. Parquet 数据如何调整

### 3.1 空、Bad、Good 的统一规则

下表适用于 HDA 导入和 DA 实际观测；DA 的 HDA 覆盖列按第 3.3 节单独计算。

| 值 | 对应质量 | HDA | DA 当前值 |
|---|---|---|---|
| NULL/NaN/Inf | 无状态列，或显式非 Bad | 不生成历史点 | 保持前一个实际观测的值和质量 |
| 空值 | 显式 Bad | 写入空值和指定 Bad | 更新为空值和指定 Bad |
| 有限数值，包括 0 | 无状态列 | 写入，默认 Good | 更新，默认 Good |
| 有限数值 | 显式质量码 | 原样写入值和质量 | 原样更新值和质量 |

Uncertain 且有值时保留 Uncertain；Uncertain 且无值时按无观测处理。Bad 根据 OPC UA StatusCode 最高位判断。

状态列不存在时默认 `Good(0)`；如果存在，所有单元格必须是合法 UInt32 整数，不能用 NULL 表示默认状态。

宽表为了对齐其他位号而补的 NULL，保持为空即可，不要填前值到 HDA 中。各位号的实际 HDA 时间点无需相同。

### 3.2 旧 NULL 如何迁移

逐列确认旧 NULL 的业务含义，不要统一填成 0，也不要统一变成 Bad：

- 表示没有采样/对齐空格：保留 NULL，对应状态可为 0。
- 表示通信故障等异常：保留 NULL，增加或修正 `位号.__status` 为明确的 Bad。
- 希望维持旧版“NULL 自动产生 BadWaitingForInitialData”效果：明确写入该质量码 `2150760448`（`0x80320000`）。

例如位号 `A` 原有三行 `12.5 / NULL / 13.0`，其中中间一行确实是故障，可转换为：

| A | A.__status |
|---|---|
| 12.5 | 0 |
| NULL | 2150760448 |
| 13.0 | 0 |

如果中间一行只是没有观测，则不必新增状态列；新版本该行不产生 A 的历史点，DA 继续保持 12.5。

旧版 NULL 可能覆盖原有质量码并变成 BadWaitingForInitialData；新版明确 Bad 会原样保留。因此质量码对账也应按迁移后的数据定义进行。

### 3.3 DA 的 HDA 覆盖列

| DA 文件列 | 不存在时的规则 |
|---|---|
| `A.__status` | DA 质量默认为 Good |
| `A.__hda_value` | 追加历史使用当前行原始 A 值，不是 DA 保持的前值 |
| `A.__hda_status` | 追加历史继承当前行 DA 质量 |

如果旧组态希望“DA 为 Bad，但 HDA 回填正常值且 Good”，必须显式保留/补齐 `A.__hda_status=0`，不能再依赖缺省 Good。例如：

| A | A.__status | A.__hda_value | A.__hda_status |
|---|---|---|---|
| NULL | 2150694912 | 12.5 | 0 |

该行 DA 为 BadNoCommunication，HDA 为 12.5/Good。覆盖列分别控制值和质量；只有列不存在才回退，已存在的空单元格不会自动回退。

### 3.4 更早版本与宁波数据的质量码

v1.5.0 使用完整 OPC UA StatusCode，不使用 OPC DA 的质量字节。必须先确认源约定，不能仅凭数字自动猜测。

- 源定义 `192=Good` 时，转换为 UA `0`。
- 源定义 `0=Bad` 时，按故障含义转换为 UA Bad，例如通用 Bad `2147483648`，或明确的 BadNoCommunication `2150694912`。
- 如果源已经是 OPC UA，`0` 就是 Good，绝不能再转成 Bad。

宁波历史 preset 中的 192/0 约定应按上述原则审核。程序不会自动识别或修复这类业务约定。

### 3.5 时间列

HDA 首列仍为 `Timestamp`，类型为 `timestamp[us, tz=UTC]`。保留原始毫秒/微秒精度，不要整秒取整或人为补齐每个位号的历史点。

已经是 UTC 的 Parquet 不要再加减 8 小时。只有重新从无时区源文件生成数据时，才需要确认源时间所属时区并转换一次。当前 BASF 转换脚本/manifest 定义源时间为 Asia/Shanghai、输出为 UTC，以实际部署数据的 manifest 为准，不能把此规则套到所有旧数据集上。

## 4. config.yaml 与播放模式

v1.4.3 CLI 的配置结构保持不变。尽量保留原有 `server.endpoint`、`server.ns`、`server.namespace` 和位号列名，以保持客户端连接及 NodeId 组态稳定。更早版本如有额外配置字段，需按当前结构整理：未知字段会被拒绝。

以下仅为固定周期模式结构示例，地址和周期必须替换成现场原值：

```yaml
server:
  endpoint: opc.tcp://0.0.0.0:18980
  ns: 3
  namespace: urn:hda:mocker4
  max_page_size: 5000
diagnostic:
  listen: 127.0.0.1:18441
playback:
  files:
    demo.parquet:
      period_ms: 30000
history:
  retention_days: 0
```

`diagnostic.listen` 是诊断 HTTP 地址，不是 OPC UA 地址，不能与 OPC UA 端口冲突。`retention_days: 0` 表示不清理，不代表可以复用旧 runtime。

两种 DA 模式通过文件是否含 Timestamp 区分，不需要额外模式开关：

| 项目 | 无 Timestamp 的旧 DA | 有 Timestamp 的 DA |
|---|---|---|
| 播放节奏 | 保留每文件 period_ms | 按源时间差映射，每 100ms 检查到期观测 |
| 文件数量 | 支持多文件 | 当前仅支持一份 DA 文件，不能混用固定周期文件 |
| 配置 | 保留原 period_ms | 仍需配置该文件，建议 period_ms: 100 |
| 重启位置 | 使用新版本 runtime 中持久化游标 | 从首行开始 |

文件名必须与 `playback.files` 键匹配。配置文件所在目录就是 preset 根目录，只读取 `hda` 和 `da` 下第一层 Parquet。不要把备份 Parquet 放进这些目录，避免被重复扫描。纯 HDA 配置可使用 `playback.files: {}` 并保留空 `da` 目录。

不要为了升级而给原固定周期 DA 增加 Timestamp，否则会改变播放模式。

## 5. 换包操作

推荐新目录部署，例如：

```text
D:\mock_v1.5.0\
  hda_mocker_4_v1.5.0.exe
  presets\production\
    config.yaml
    manifest.json          （有则随数据一起带上）
    hda\*.parquet
    da\*.parquet
    runtime\               （不从旧版复制，首次启动自动创建）
```

1. 在新目录放入 v1.5.0 exe、迁移后的数据和配置。旧部署目录保持完整。
2. 在独立端口先验收时，同时修改 OPC UA 和诊断监听端口；切换生产时恢复现场地址，避免新旧实例争抢端口。
3. 确认机器 UTC 时间晚于已有历史最大时间。若数据落在未来，应核查源时区/系统时钟或等待窗口结束；不要通过伪造系统时间绕过检查。
4. 按现场服务管理方式停止旧实例，确认端口释放，再启动新实例。
5. 更新服务/计划任务的 exe 和 `--config` 路径；仅换文件但启动项仍指向旧目录不算完成升级。

手动启动示例：

```powershell
& 'D:\mock_v1.5.0\hda_mocker_4_v1.5.0.exe' --version
& 'D:\mock_v1.5.0\hda_mocker_4_v1.5.0.exe' --config 'D:\mock_v1.5.0\presets\production\config.yaml'
```

第一条输出应为 v1.5.0。首次启动会重新导入 HDA，并建立新 runtime。旧的非空 runtime 缺少 v1.5 语义标记时会被拒绝；不要手动补标记绕过，也不要直接删掉唯一的旧库备份。

新 runtime 不含旧播放游标，固定周期从初始位置开始；后续使用同一新版 runtime 才可恢复其游标。DA 初始化会沿循环顺序向前找最近的实际观测，可能来自上一轮尾部，且保留其质量；整列无观测则为 BadWaitingForInitialData。初始化本身不追加历史。

## 6. 换包后验收

### 6.1 基础与 HDA

- 核对版本、实际配置路径、端口、Namespace 和位号数，确认没有导入/播放错误日志。
- 核对 HDA 时间、数值和质量：新期望点数为“有限值观测 + 空值且明确 Bad 的观测”，不再是所有行乘位号数。
- 抽查正常值、0、负值、对齐 NULL、空值 Bad、带值 Bad，以及 DA/HDA 覆盖质量不同的样本。
- 分页读完整历史，确认无重复和漏点。原始导入窗口与实时追加窗口分开统计。

使用 asyncua 时，`read_raw_history()` 必须显式指定 `return_bounds=False`。底层 HistoryRead 正常读取应为 `IsReadModified=false`、`ReturnBounds=false`、`ReleaseContinuationPoints=false`；仅释放续读点时才设最后一项为 true。

### 6.2 DA 和实时追加

- 不能只证明“能读到 Good”：至少覆盖多个预期变化点，验证值确实推进。
- 空行应保持前值及前质量，且不因保持而产生额外历史点；明确 Bad 行应更新质量并按规则追加 HDA。
- 核对 DA 当前值和最近实际观测，结合 HDA 覆盖列判断追加内容，不能一律要求 DA/HDA 相等。
- 时间轴模式检查相邻 SourceTimestamp 差值与源时间差匹配；固定模式检查原 period_ms 节奏。需要两个时间戳时，客户端请求 `TimestampsToReturn.Both`。
- 客户端与服务器先校时，记录客户端接收时刻减计划 SourceTimestamp 的延迟分布、最大延迟及长时间停顿。100ms 是检查周期，不是每个位号每 100ms 必有数据，也不是网络到达时限。
- SourceTimestamp/ServerTimestamp 为样本时间，不能单靠它们证明实际调度准时。订阅可能合并中间更新；通知数少于历史点数不直接等于历史丢点，应同时对账 HDA 和订阅参数。
- 检查跨轮和重启；长周期数据集应单独安排跨轮测试。BASF 详见 [现场验收说明](../presets/basf_dirty/FIELD_ACCEPTANCE.md)，其中旧版本包名/旧 runtime 复用建议以本文为准。

出现 `timestamp playback stopped before publishing`、诊断 error、写入失败或 DA BadOutOfService，应判定播放验收失败，不能将初始化值当作实时正常。

## 7. 回退

1. 停止新实例，保存新目录的日志和整个 runtime，避免丢失升级后的追加历史。
2. 恢复旧启动项，使用原备份中的旧 exe、旧配置、旧 Parquet 和旧 runtime 整套启动。
3. 核对旧版本号、端口和客户端连接。

不要混搭“旧 exe + 新 runtime”或只恢复旧 exe。升级后新产生的历史不会自动合并回旧库；如需要保留，必须另行安排导出与校验。

## 8. 交付确认清单

- [ ] 旧版本及完整部署已停机备份，可回退。
- [ ] NULL 含义、质量码制式及 HDA 覆盖列已确认并转换。
- [ ] UTC 时间及源时区已确认，未重复偏移。
- [ ] 原客户端地址、Namespace、位号和固定周期设置已保留或明确通知变更。
- [ ] 使用 v1.5.0 exe 和全新 runtime，未带开发机运行库。
- [ ] HDA 原始点对账通过，DA 更新/保持/Bad 和实时追加验证通过。
- [ ] 调度延迟、跨轮、重启及服务启动路径已验证。
- [ ] 旧实时历史不自动迁移这一边界已确认。

相关说明：[v1.5.0 规则与版本说明](HDA_MOCKER_4_RELEASE_1_5_0.md)。
