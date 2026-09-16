# HDA Mocker 4 场景测试数据集

`presets/` 下的每个子目录就是一套独立的场景测试数据集（preset），自带
`config.yaml`、`hda/`、`da/` 和运行期 `runtime/`。每套数据集用于验证一类
客户端行为或业务场景，彼此独立，单独启停。

数据文件格式与提交要求见
[HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md](../docs/HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md)；
本文是数据集的唯一总览台账，新增数据集时在此登记。

## 目录约定

```text
presets/
├── README.md                     # 本文件：数据集总览
├── demo/                         # 空白模板，用于新建数据集
├── hda_all/
│   ├── config.yaml
│   ├── hda/hda_all.parquet
│   ├── da/hda_all.parquet
│   └── runtime/                  # 服务运行后生成
└── dynamic_data_all/
    ├── config.yaml
    ├── hda/dynamic_data_all.parquet
    ├── da/dynamic_data_all.parquet
    └── runtime/
```

Parquet 载荷不入 Git，仅 `config.yaml` 和目录占位符 `.gitkeep` 受版本管理。

## 启动

```powershell
.\hda_mocker_4.exe --config presets\hda_all\config.yaml
.\hda_mocker_4.exe --config presets\dynamic_data_all\config.yaml
```

同一时刻只运行一个场景：各数据集默认都监听 `opc.tcp://0.0.0.0:18980`，
顺序启停即可，不需要改端口。

## 数据集索引

| 数据集 | 目录 | 版本 | 位号数 | HDA 行数 | HDA 采样 | DA 行数 | DA 周期 |
|---|---|---|---:|---:|---:|---:|---:|
| `hda_all` | `presets/hda_all` | v1.0 | 76 | 452,161 | 30 秒 | 10,000 | 30,000 ms |
| `dynamic_data_all` | `presets/dynamic_data_all` | v1.0 | 27 | 260,639 | 60 秒 | 10,000 | 60,000 ms |
| `qa_v2` | `presets/qa_v2` | v1.0 | 12 | 169,129 | 事件驱动 | — | 无 DA |

## 公共约定

- 服务端：`opc.tcp://0.0.0.0:18980`，`ns=3`，`namespace=urn:hda:mocker4`，
  `max_page_size=5000`，`retention_days=0`。
- 节点：`Objects/HDA_Mocker` 下，NodeId 形如 `ns=3;s=<位号名>`。
- HDA 与 DA 使用相同文件名和完全一致的位号集合；DA 无时间列，按行循环播放。
- 运行期数据库在该数据集目录下的 `runtime/history.duckdb`，首次启动导入 HDA，
  之后 DA 轮播持续写入同一个库。
- StatusCode 采用完整 OPC UA StatusCode（UInt32）。`hda_all`、`dynamic_data_all`
  未使用辅助状态列，所有值为 `0`（StatusGood）；`qa_v2` 使用 `<tag>.__status`
  承载 Good/GoodClamped/Uncertain/Bad。

## hda_all

### 名称 / 目录 / 版本

- 名称：`hda_all`
- 目录：`presets/hda_all`
- 版本：v1.0

### 场景说明

稳态工艺量历史数据集。76 个仪表位号（FC/FI/PC/PI/TC/TI/AI 等），覆盖约 5 个月的
30 秒采样历史，DA 以 30 秒周期轮播。用于验证大位号数、长时间跨度下的 HDA 分页读取、
时间范围查询与 DA 轮播实时入库。

### 位号与规模

- 位号数：76
- HDA：`hda/hda_all.parquet`，452,161 行/位号，合计 34,364,236 个样本
- DA：`da/hda_all.parquet`，10,000 行，位号集合与 HDA 一致
- 数据类型：全部 Double，无空值

### 时间与周期

- HDA 时间范围：`2025-08-15 00:00:00 UTC` 至 `2026-01-19 00:00:00 UTC`（首尾包含）
- HDA 采样间隔：固定 30 秒
- DA 轮播周期：30,000 ms，到达尾部后回到第 0 行

### StatusCode 用法

- HDA：`0`（StatusGood）
- DA：`0`（StatusGood）

### 位号清单

```text
AI1303A5.PV   AI1303A1.PV   AC4101.PV     FC1207.PV     FC1208.PV
FC1223.PV     FC1301.PV     FC1314.PV     FC1315.PV     FC1318.PV
FC1319.PV     FC3150.PV     FC4101.PV     FC4102.PV     FC4104.PV
FC4120.PV     FI1224.PV     FI1303.PV     FI1312.PV     FI4135.PV
PC1201D.PV    PC1303.PV     PC1313B.PV    PC4101.PV     PC4103.PV
PI1308.PV     TC1111.PV     TC1201.PV     TC1206.PV     TC1220C.PV
TC1301.PV     TC4102.PV     TC4158.PV     TI1123B.PV    TI1206A.PV
TI1206B.PV    TI1210.PV     TI1215.PV     TI1219A.PV    TI1220A.PV
TI1221.PV     TI1234.PV     TI1302.PV     TI1304.PV     TI1306.PV
TI1307.PV     TI1308.PV     TI1309.PV     TI1312.PV     TI1313.PV
TI1314.PV     TI1315.PV     TI1317.PV     TI1318.PV     TI1321.PV
TI1324.PV     TI1328.PV     TI1331.PV     TI1333.PV     TI1335.PV
TI4105.PV     TI4107.PV     TI4108.PV     TI4109.PV     TI4110.PV
TI4111.PV     TI4112.PV     TI4128.PV     TI4130.PV     TI4131.PV
TI4132.PV     TI4133.PV     TI4152.PV     TI4155.PV     TI4156.PV
TI4157.PV
```

## dynamic_data_all

### 名称 / 目录 / 版本

- 名称：`dynamic_data_all`
- 目录：`presets/dynamic_data_all`
- 版本：v1.0

### 场景说明

动态多变量历史数据集。27 个 `XA_LS_*` 位号（FE/FT/PT/TT/HIC/SE 等），覆盖约 6 个月的
60 秒采样历史，DA 以 60 秒周期轮播。用于验证与 `hda_all` 不同的采样周期和位号规模下的
HDA/DA 行为，并可与 `hda_all` 对照测试不同周期的订阅更新节奏。

### 位号与规模

- 位号数：27
- HDA：`hda/dynamic_data_all.parquet`，260,639 行/位号，合计 7,037,253 个样本
- DA：`da/dynamic_data_all.parquet`，10,000 行，位号集合与 HDA 一致
- 数据类型：全部 Double，无空值

### 时间与周期

- HDA 时间范围：`2025-09-02 00:02:00 UTC` 至 `2026-03-02 00:00:00 UTC`（首尾包含）
- HDA 采样间隔：固定 60 秒
- DA 轮播周期：60,000 ms，到达尾部后回到第 0 行

### StatusCode 用法

- HDA：`0`（StatusGood）
- DA：`0`（StatusGood）

### 位号清单

```text
XA_LS_G_FE_12001.PV    XA_LS_G_FT_11001.PV    XA_LS_G_HIC12005A.MV   XA_LS_G_HIC12005B.MV
XA_LS_G_HIC12005C.MV   XA_LS_G_HIC12005D.MV   XA_LS_G_HIC12005E.MV   XA_LS_G_HIC12005F.MV
XA_LS_G_PT_11010.PV    XA_LS_G_PT_12004.PV    XA_LS_G_PT_12025A.PV   XA_LS_G_PT_12025B.PV
XA_LS_G_PT_12025C.PV   XA_LS_G_PT_12025D.PV   XA_LS_G_PT_12025E.PV   XA_LS_G_PT_12025F.PV
XA_LS_G_SICP11001B.MV  XA_LS_G_TT_12004.PV    XA_LS_G_TT_12005.PV    XA_LS_G_TT_12006A.PV
XA_LS_G_TT_12006B.PV   XA_LS_G_TT_12006C.PV   XA_LS_PG_SE12091A.PV   XA_LS_PG_SE12091B.PV
XA_LS_PG_SE12091C.PV   XA_LS_rG_PT12072A.PV   XA_LS_rG_TT12076.PV
```

## qa_v2

### 名称 / 目录 / 版本

- 名称：`qa_v2`
- 目录：`presets/qa_v2`
- 版本：v1.0

### 场景说明

由外部工业异常验收夹具 `docs/external/industrial_hda_v2.parquet` 转换而来，
共 20 个异常场景段（V01–V20，40 小时，169,143 个源事件）。用于验证客户端拉取
HDA 时的采样/对齐/聚合链路。仅 HDA，无 DA。

**每个位号一个 HDA 文件**（`hda/qa_v2_NNNN.parquet`）：源事件是逐位号异步的，
若强行拼成一张宽表，没有事件的位号会被 mocker 的导入落成"无值+Bad"样本，凭空
造出事件，所以按位号拆分以保真。

### 位号与规模

- 位号数：12（`qa_v2_0001` ~ `qa_v2_0012`），每位号一个文件
- HDA 合计：169,129 行（源 169,143，减去 13 条完全重复 + 1 条冲突）
- 数据类型：Double；每列附 `tag.__status`（UInt32）

### 时间与周期

- 时间范围：`2026-09-06 00:00:00 UTC` 至 `2026-09-07 15:59:50 UTC`
- 采样：事件驱动（10 秒网格 + 毫秒/微秒偏移），非等间隔
- 无 DA、无轮播

### StatusCode 用法

- 来自源的原始 OPC UA StatusCode：`0`（Good）、`0x00300000`（GoodClamped）、
  `0x40000000`（Uncertain）、`0x80000000`（Bad），写入 `<tag>.__status`。
- 源里值类型为 NaN/±Inf/Null 的事件（质量原本为 Good），在 mocker_4 中会被统一
  落成"无值 + BadWaitingForInitialData"；源里的重复/冲突时间与到达顺序无法表达。

### 生成与保真说明

- 生成工具：`tools/build_qa_v2_preset.py`；生成清单：`presets/qa_v2/manifest.json`。
- 逐 case 的适配结论见 `docs/external/case-mapping.md`。
- 已知失真：非有限/Null 事件的质量与类型（V04/V07/V16/V18）、重复时间（V14/V20）、
  冲突时间（V15）、到达顺序（V14/V20）。其中被丢弃的 1 条冲突记录在 manifest。

### 位号清单

```text
qa_v2_0001  qa_v2_0002  qa_v2_0003  qa_v2_0004  qa_v2_0005  qa_v2_0006
qa_v2_0007  qa_v2_0008  qa_v2_0009  qa_v2_0010  qa_v2_0011  qa_v2_0012
```
