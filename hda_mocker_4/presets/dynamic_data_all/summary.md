# 工业动态历史（60 秒）

统计基线：2026-10-18；适用于 hda_mocker_4 v1.5.0。本文描述本目录的输入数据，不是运行状态、实时追加历史或性能实测报告。配置摘录为编写时快照；连接地址和播放配置以同目录 `config.yaml` 的当前内容为唯一来源。

## 数据概览

现场 XA_LS 系列数据；原始CSV归档于 source_data/external/dynamic_data_all.csv，另有最后10,000行的CSV子集。

27个XA_LS位号，涉及FE、FT、PT、TT、HIC、SE等。按统一60秒网格采样，数值变化和范围因位号而异，适合分钟级多变量历史及轮播测试。与hda_all没有同名位号，也没有发现直接的数值序列包含关系；是否同一现场/交付批次未确认。

| 项目 | 当前数据 |
|---|---|
| preset | `dynamic_data_all` |
| 位号数 | 27，均为 Double |
| HDA 实际点数 | 7,037,253 |
| HDA 起始时间（UTC） | `2025-09-02T00:02:00Z` |
| HDA 结束时间（UTC） | `2026-03-02T00:00:00Z` |
| HDA Good / Uncertain / Bad | 7,037,253 / 0 / 0 |
| HDA 空值 Bad 点 | 0 |
| HDA 无观测空格 | 0 |
| HDA 有限数值为0 / 负数的点数 | 285,230 / 30,699（包含任意质量） |

以上实际点数按“有限值或明确Bad”统计；Good/Uncertain/Bad按完整UA状态码最高两位分组，不能仅用 status==0 判断Good。数值0与质量码0含义不同。

## HDA 文件与时间

| 文件 | 行数 | 实际历史点数 | 时间特征 |
|---|---:|---:|---|
| `hda/dynamic_data_all.parquet` | 260,639 | 7,037,253 | 60秒 |

时间列统一为 `timestamp[us, tz=UTC]`，按文件严格递增。表中时间窗口包含首末观测，不代表每个位号在窗口内均匀有值。

## DA 轮播与实时 HDA

- 文件：`da/dynamic_data_all.parquet`，10,000行，27个位号，一轮含270,000个实际观测。
- 轮播模式：固定周期，按文件原始行序循环。
- `period_ms`：60000，即每行60秒。
- 一轮计划周期：166 小时 40 分 0 秒，按行数×period_ms计算。这是计划时间，不是对实际调度延迟的保证。
- 一轮 DA Good / Uncertain / Bad：270,000 / 0 / 0。

DA 为10,000行轮播输入；一轮长于数天，短时订阅无变化不能直接判故障，应结合对应位号数据和播放周期。

固定周期模式在新runtime中从初始位置开始，后续使用同一runtime可恢复持久化游标。DA初始当前值沿循环顺序查找最近真实观测，不作为新历史点写入；播放的实际观测成功写入实时HDA后才发布DA。

## 质量、空值与数据特征

旧preset中已有的NULL/NaN/Inf按Bad观测保留：已有明确Bad则保留，否则需补BadWaitingForInitialData；不能在新版中因未标Bad而被误跳过。本目录已完成相应迁移。真正没有采样事件的时间间隙不凭空补点。

当前HDA和DA均没有无效数值单元格。没有状态列时采用默认Good；这只说明Mock的输入约定，不等同于源现场的质量证明。

## 当前配置快照

```yaml
server:
  endpoint: opc.tcp://0.0.0.0:18980
  ns: 3
  namespace: urn:hda:mocker4
  max_page_size: 5000
diagnostic:
  listen: 0.0.0.0:4841
playback:
  files:
    dynamic_data_all.parquet:
      period_ms: 60000
history:
  retention_days: 0
```

- 当前UA监听：`opc.tcp://0.0.0.0:18980`；Namespace URI：`urn:hda:mocker4`；配置索引：3。
- 节点位于 `Objects/HDA_Mocker`；例如 `ns=3;s=XA_LS_G_FE_12001.PV`。客户端宜通过Namespace URI解析实际索引。
- 历史单页上限：5000；`retention_days: 0`，0表示不自动清理实时历史。
- HTTP诊断：当前配置 `0.0.0.0:4841`，接口为 `/v1/diag`、`/v1/detail`。

当前地址仍使用 `0.0.0.0` 监听占位，并非可复制给用户的连接地址。实际部署时由管理员在本 `config.yaml` 中填入属于运行机器、用户可访问的IP；网站只读取它，不再另维护连接地址。各preset现有示例端口存在复用，不能原样全部同时启动，部署时需在各自config中安排不冲突的UA/诊断监听端口。本文不替代或覆盖当前config。

## 位号清单与 HDA 点数

| 位号 | 历史点数 | Good | Uncertain | Bad |
|---|---:|---:|---:|---:|
| `XA_LS_G_FE_12001.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_FT_11001.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_HIC12005A.MV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_HIC12005B.MV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_HIC12005C.MV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_HIC12005D.MV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_HIC12005E.MV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_HIC12005F.MV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_11010.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_12004.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_12025A.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_12025B.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_12025C.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_12025D.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_12025E.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_PT_12025F.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_SICP11001B.MV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_TT_12004.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_TT_12005.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_TT_12006A.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_TT_12006B.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_G_TT_12006C.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_PG_SE12091A.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_PG_SE12091B.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_PG_SE12091C.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_rG_PT12072A.PV` | 260,639 | 260,639 | 0 | 0 |
| `XA_LS_rG_TT12076.PV` | 260,639 | 260,639 | 0 | 0 |

辅助列 `.__status`、`.__hda_value`、`.__hda_status` 不生成独立UA位号，不计入以上位号数。

## 使用与下载

可下载本目录的 `hda/*.parquet`、`da/*.parquet`（有则提供）、`config.yaml` 和 `summary.md`。`runtime/`、DuckDB/WAL、日志和原始现场文件不作为门户默认下载内容。网站只展示说明与文件，不在线扫描或改写这些Parquet。

Raw HistoryRead应设置 `ReturnBounds=false`；正常分页时 `ReleaseContinuationPoints=false`。所有时间使用UTC，保留原微秒/毫秒精度。换用迁移后的preset应新建runtime，不能仅替换文件而复用已导入旧质量码的历史库。本文的诊断、周期和数据特征不构成长期运行性能验收结论。
