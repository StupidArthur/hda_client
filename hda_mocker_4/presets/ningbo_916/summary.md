# 宁波 916 水处理数据

统计基线：2026-10-18；适用于 hda_mocker_4 v1.5.0。本文描述本目录的输入数据，不是运行状态、实时追加历史或性能实测报告。配置摘录为编写时快照；连接地址和播放配置以同目录 `config.yaml` 的当前内容为唯一来源。

## 数据概览

22份现场Excel归档于 source_data/916。源 App Time 按 Asia/Shanghai 解释，转换后统一UTC；按北京时间2026-09-11 00:00:00切分HDA与DA。

水处理指标包括COD、PH、氨氮、总氮/总磷、DO、ORP和MLSS等。已整理到统一5秒网格。HDA同时包含Good和明确Bad，适合质量码及异常历史验证；当前DA段全部Good。

| 项目 | 当前数据 |
|---|---|
| preset | `ningbo_916` |
| 位号数 | 22，均为 Double |
| HDA 实际点数 | 9,236,854 |
| HDA 起始时间（UTC） | `2026-08-17T08:51:55Z` |
| HDA 结束时间（UTC） | `2026-09-10T15:59:55Z` |
| HDA Good / Uncertain / Bad | 6,789,228 / 0 / 2,447,626 |
| HDA 空值 Bad 点 | 0 |
| HDA 无观测空格 | 0 |
| HDA 有限数值为0 / 负数的点数 | 3,014,892 / 435,392（包含任意质量） |

以上实际点数按“有限值或明确Bad”统计；Good/Uncertain/Bad按完整UA状态码最高两位分组，不能仅用 status==0 判断Good。数值0与质量码0含义不同。

## HDA 文件与时间

| 文件 | 行数 | 实际历史点数 | 时间特征 |
|---|---:|---:|---|
| `hda/ningbo_916_hda.parquet` | 419,857 | 9,236,854 | 5秒 |

时间列统一为 `timestamp[us, tz=UTC]`，按文件严格递增。表中时间窗口包含首末观测，不代表每个位号在窗口内均匀有值。

## DA 轮播与实时 HDA

- 文件：`da/ningbo_916_da.parquet`，98,309行，22个位号，一轮含2,162,798个实际观测。
- 轮播模式：固定周期，按文件原始行序循环。
- `period_ms`：5000，即每行5秒。
- 一轮计划周期：136 小时 32 分 25 秒，按行数×period_ms计算。这是计划时间，不是对实际调度延迟的保证。
- 一轮 DA Good / Uncertain / Bad：2,162,798 / 0 / 0。

源质量码约定192=Good、0=Bad；本目录的运行Parquet已转换为UA Good(0)与Bad(2147483648)。不要把新文件的UA 0再映射为Bad。DA的.__hda_value和.__hda_status均显式提供，当前实时HDA值与质量跟随对应DA观测。

固定周期模式在新runtime中从初始位置开始，后续使用同一runtime可恢复持久化游标。DA初始当前值沿循环顺序查找最近真实观测，不作为新历史点写入；播放的实际观测成功写入实时HDA后才发布DA。

## 质量、空值与数据特征

旧preset中已有的NULL/NaN/Inf按Bad观测保留：已有明确Bad则保留，否则需补BadWaitingForInitialData；不能在新版中因未标Bad而被误跳过。本目录已完成相应迁移。真正没有采样事件的时间间隙不凭空补点。

当前HDA和DA均没有无效数值单元格。Bad为明确质量码，仍可能携带有限数值；不要仅根据value是否为空判断质量。

## 当前配置快照

```yaml
server:
  endpoint: opc.tcp://0.0.0.0:4840
  ns: 3
  namespace: urn:hda:mocker4:ningbo
  max_page_size: 5000
playback:
  files:
    ningbo_916_da.parquet:
      period_ms: 5000
history:
  retention_days: 0
```

- 当前UA监听：`opc.tcp://0.0.0.0:4840`；Namespace URI：`urn:hda:mocker4:ningbo`；配置索引：3。
- 节点位于 `Objects/HDA_Mocker`；例如 `ns=3;s=2_AB.ERQUDIPEIAB.A1_ORP_SF`。客户端宜通过Namespace URI解析实际索引。
- 历史单页上限：5000；`retention_days: 0`，0表示不自动清理实时历史。
- HTTP诊断：当前config未配置diagnostic.listen，即未启用HTTP诊断；网站应显示未监控，不能假定一个诊断端口。

当前地址仍使用 `0.0.0.0` 监听占位，并非可复制给用户的连接地址。实际部署时由管理员在本 `config.yaml` 中填入属于运行机器、用户可访问的IP；网站只读取它，不再另维护连接地址。各preset现有示例端口存在复用，不能原样全部同时启动，部署时需在各自config中安排不冲突的UA/诊断监听端口。本文不替代或覆盖当前config。

## 位号清单与 HDA 点数

| 位号 | 历史点数 | Good | Uncertain | Bad |
|---|---:|---:|---:|---:|
| `2_AB.ERQUDIPEIAB.A1_ORP_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.A2_DO_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.A2_ORP_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.A3_DO_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.A_MLSS_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.B1_DO_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.B1_ORP_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.B2_DO_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.B2_ORP_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.ERQUDIPEIAB.B3_DO_SF` | 419,857 | 230,054 | 0 | 189,803 |
| `2_AB.JSGS.COD` | 419,857 | 419,731 | 0 | 126 |
| `2_AB.JSGS.N_NH3` | 419,857 | 419,731 | 0 | 126 |
| `2_AB.JSGS.PH` | 419,857 | 419,731 | 0 | 126 |
| `2_AB.JSGS.PT` | 419,857 | 419,731 | 0 | 126 |
| `2_AB.JSGS.TN` | 419,857 | 419,731 | 0 | 126 |
| `2_AB.JSGS.TP` | 419,857 | 61,457 | 0 | 358,400 |
| `2_MB.CS.COD` | 419,857 | 419,701 | 0 | 156 |
| `2_MB.CS.NH3-N` | 419,857 | 419,701 | 0 | 156 |
| `2_MB.CS.PH` | 419,857 | 419,701 | 0 | 156 |
| `2_MB.CS.TN` | 419,857 | 419,701 | 0 | 156 |
| `2_MB.CS.TP` | 419,857 | 419,701 | 0 | 156 |
| `2_PLC3.PLC3.DO_A_SF` | 419,857 | 230,071 | 0 | 189,786 |

辅助列 `.__status`、`.__hda_value`、`.__hda_status` 不生成独立UA位号，不计入以上位号数。

## 使用与下载

可下载本目录的 `hda/*.parquet`、`da/*.parquet`（有则提供）、`config.yaml` 和 `summary.md`。`runtime/`、DuckDB/WAL、日志和原始现场文件不作为门户默认下载内容。网站只展示说明与文件，不在线扫描或改写这些Parquet。

Raw HistoryRead应设置 `ReturnBounds=false`；正常分页时 `ReleaseContinuationPoints=false`。所有时间使用UTC，保留原微秒/毫秒精度。换用迁移后的preset应新建runtime，不能仅替换文件而复用已导入旧质量码的历史库。本文的诊断、周期和数据特征不构成长期运行性能验收结论。
