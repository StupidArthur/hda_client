# HDA Mocker 4 生产数据与验证说明

软件版本：`v1.0.2`

## 1. 服务信息

```text
Listen endpoint:  opc.tcp://0.0.0.0:18980
Client endpoint:  opc.tcp://10.30.144.70:18980
Namespace index: 3
Namespace: urn:hda:mocker4
安全策略: None
认证方式: Anonymous
服务端分页上限: 5000 点/位号/页
```

服务监听本机全部网卡，并需放通 TCP 端口 `18980`。客户端使用生产机实际 IP 连接。服务节点位于 Objects 下的 `HDA_Mocker` 文件夹，位号 NodeId 形式为 `ns=3;s=<位号名>`。

## 2. 数据文件布局

```text
preset/
├── config.yaml
├── hda/
│   ├── hda_all.parquet
│   └── dynamic_data_all.parquet
├── da/
│   ├── hda_all.parquet
│   └── dynamic_data_all.parquet
└── runtime/
    ├── history.duckdb
    └── history.duckdb.wal
```

同一数据集在 `hda` 和 `da` 中使用相同文件名。HDA 是启动时导入的已有历史，DA 是服务运行后循环播放并持续写入同一个历史库的数据。

## 3. 数据集 hda_all

### HDA

- 文件：`hda/hda_all.parquet`
- 位号数：76
- 历史行数：452,161 行/位号
- 历史点总数：34,364,236
- 时间范围：`2025-08-15 00:00:00 UTC` 至 `2026-01-19 00:00:00 UTC`，首尾均包含
- 采样间隔：固定 30 秒
- 数据类型：全部为 Double
- 空值数量：0
- 示例位号：`AI1303A5.PV`、`FC1207.PV`、`TI4157.PV`

以下完整位号列表同时适用于 `hda/hda_all.parquet` 和 `da/hda_all.parquet`：

```text
AI1303A5.PV
AI1303A1.PV
AC4101.PV
FC1207.PV
FC1208.PV
FC1223.PV
FC1301.PV
FC1314.PV
FC1315.PV
FC1318.PV
FC1319.PV
FC3150.PV
FC4101.PV
FC4102.PV
FC4104.PV
FC4120.PV
FI1224.PV
FI1303.PV
FI1312.PV
FI4135.PV
PC1201D.PV
PC1303.PV
PC1313B.PV
PC4101.PV
PC4103.PV
PI1308.PV
TC1111.PV
TC1201.PV
TC1206.PV
TC1220C.PV
TC1301.PV
TC4102.PV
TC4158.PV
TI1123B.PV
TI1206A.PV
TI1206B.PV
TI1210.PV
TI1215.PV
TI1219A.PV
TI1220A.PV
TI1221.PV
TI1234.PV
TI1302.PV
TI1304.PV
TI1306.PV
TI1307.PV
TI1308.PV
TI1309.PV
TI1312.PV
TI1313.PV
TI1314.PV
TI1315.PV
TI1317.PV
TI1318.PV
TI1321.PV
TI1324.PV
TI1328.PV
TI1331.PV
TI1333.PV
TI1335.PV
TI4105.PV
TI4107.PV
TI4108.PV
TI4109.PV
TI4110.PV
TI4111.PV
TI4112.PV
TI4128.PV
TI4130.PV
TI4131.PV
TI4132.PV
TI4133.PV
TI4152.PV
TI4155.PV
TI4156.PV
TI4157.PV
```

### DA

- 文件：`da/hda_all.parquet`
- 行数：10,000
- 位号数：76，与该数据集 HDA 完全一致
- 内容：HDA 前 10,000 行对应的数值，最终文件不含时间列
- 轮播周期：30,000ms，即每30秒播放下一行
- 到达文件尾部后回到第0行继续轮播

## 4. 数据集 dynamic_data_all

### HDA

- 文件：`hda/dynamic_data_all.parquet`
- 位号数：27
- 历史行数：260,639 行/位号
- 历史点总数：7,037,253
- 时间范围：`2025-09-02 00:02:00 UTC` 至 `2026-03-02 00:00:00 UTC`，首尾均包含
- 采样间隔：固定 60 秒
- 时间生成规则：以已确认的结束时间为锚点，按60秒向前生成
- 数据类型：全部为 Double
- 空值数量：0
- 示例位号：`XA_LS_G_FE_12001.PV`、`XA_LS_G_PT_12004.PV`、`XA_LS_rG_TT12076.PV`

以下完整位号列表同时适用于 `hda/dynamic_data_all.parquet` 和 `da/dynamic_data_all.parquet`：

```text
XA_LS_G_FE_12001.PV
XA_LS_G_FT_11001.PV
XA_LS_G_HIC12005A.MV
XA_LS_G_HIC12005B.MV
XA_LS_G_HIC12005C.MV
XA_LS_G_HIC12005D.MV
XA_LS_G_HIC12005E.MV
XA_LS_G_HIC12005F.MV
XA_LS_G_PT_11010.PV
XA_LS_G_PT_12004.PV
XA_LS_G_PT_12025A.PV
XA_LS_G_PT_12025B.PV
XA_LS_G_PT_12025C.PV
XA_LS_G_PT_12025D.PV
XA_LS_G_PT_12025E.PV
XA_LS_G_PT_12025F.PV
XA_LS_G_SICP11001B.MV
XA_LS_G_TT_12004.PV
XA_LS_G_TT_12005.PV
XA_LS_G_TT_12006A.PV
XA_LS_G_TT_12006B.PV
XA_LS_G_TT_12006C.PV
XA_LS_PG_SE12091A.PV
XA_LS_PG_SE12091B.PV
XA_LS_PG_SE12091C.PV
XA_LS_rG_PT12072A.PV
XA_LS_rG_TT12076.PV
```

### DA

- 文件：`da/dynamic_data_all.parquet`
- 行数：10,000
- 位号数：27，与该数据集 HDA 完全一致
- 内容：原始完整数据尾部 10,000 行对应的数值；CSV 精度差异不超过约 `5e-10`
- 最终文件不含时间列，原始 CSV 时间仅用于数据对应关系验证
- 轮播周期：60,000ms，即每60秒播放下一行
- 到达文件尾部后回到第0行继续轮播

## 5. 轮播行为

两份 DA 独立维护播放行号和调度时间：

```text
服务启动：两份 DA 都立即播放第一行
第30秒：  只播放 hda_all
第60秒：  hda_all 和 dynamic_data_all 同时播放
第90秒：  只播放 hda_all
第120秒： 两份同时播放
```

同一时刻到期的文件在一个数据库事务中写入。轮播生成的样本使用实际播放时刻的 UTC 时间，并追加到 `runtime/history.duckdb`，不会改写导入历史。

服务停机期间不补造数据；重启后从数据库记录的各文件 `next_row` 继续。文件内容变化后，该文件从第0行重新开始。

## 6. 生产 UA 客户端验证

### 连接和节点

1. 连接 `opc.tcp://10.30.144.70:18980`。
2. 使用 `SecurityPolicy=None`、匿名认证。
3. 在 Objects 下找到 `HDA_Mocker`。
4. 应能浏览到 103 个变量节点：`hda_all` 76个，`dynamic_data_all` 27个。

### 当前值和订阅

分别订阅以下两个代表位号：

```text
AI1303A5.PV
XA_LS_G_FE_12001.PV
```

预期：

- `AI1303A5.PV` 大约每30秒更新一次。
- `XA_LS_G_FE_12001.PV` 大约每60秒更新一次。
- 在第60秒的公共调度点，两类位号都会更新。
- SourceTimestamp 应为生产服务运行时的 UTC 时间。
- StatusCode 应为 Good。

### 历史查询

对 `AI1303A5.PV` 查询：

```text
StartTime: 2025-08-15 00:00:00 UTC
EndTime:   2026-01-19 00:00:00 UTC
```

完整结果应包含 452,161 个导入历史点。分页时客户端需要持续使用 continuation point，直到服务端不再返回 continuation point。

对 `XA_LS_G_FE_12001.PV` 查询：

```text
StartTime: 2025-09-02 00:02:00 UTC
EndTime:   2026-03-02 00:00:00 UTC
```

完整结果应包含 260,639 个导入历史点。

如果查询结束时间覆盖服务启动后的时间，还会额外看到 DA 轮播产生的实时历史点，这是正常行为。

### 客户端兼容性

- `GetEndpoints` 返回1个可用端点，asyncua 可以按标准发现流程连接。
- 标准 `Objects`（`ns=0;i=85`）下可浏览到 `HDA_Mocker`，其下为103个位号节点。
- gopcua 和 asyncua 的 Read、订阅与数据变化通知均已通过冒烟测试。
- `HistoryReadValueID.DataEncoding` 是 OPC UA 二进制结构中的必编码字段；不需要指定特殊编码，但客户端必须编码一个空 `QualifiedName`。gopcua 和 asyncua 会自动处理。手写二进制请求不得直接省略该字段。

## 7. 部署注意事项

- 复制文件前先停止服务。
- `runtime` 必须整体复制，不能遗漏 WAL 文件。
- 生产环境不需要安装 Go、Python、DuckDB 或 GCC。
- `hda_mocker_4.exe`、`config.yaml`、两个 `hda` 文件、两个 `da` 文件及整个 `runtime` 目录应作为一个部署包携带。
- 如果生产主机没有 `10.30.144.70` 这个本机地址，服务会因为无法绑定该 IP 而启动失败。
