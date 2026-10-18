# HDA/DA 数据导入约束

## v1.5.0 优先规则

执行数据导入前读取 [统一版规则](../../docs/HDA_MOCKER_4_RELEASE_1_5_0.md)，与下文旧版描述冲突时以统一版为准：无值且未标 Bad 为无观测，跳过 HDA 并保持 DA；显式 Bad 保留原质量（可无值）。DA 可带 UTC Timestamp 进行100ms检查的时间轴播放。无 HDA 覆盖列时沿用 DA 实际观测值及质量。旧 runtime 必须备份后在新目录重建，不自动复用。

## 当前列约定

HDA Parquet 使用 `Timestamp`、`tag` 和可选 `tag.__status`。DA Parquet 使用
`tag`、可选 `tag.__status`、可选 `tag.__hda_value` 和可选
`tag.__hda_status`。辅助列归属于基础位号，不会成为位号。状态值是完整 OPC UA
StatusCode（UInt32，0..4294967295）。状态列缺失时默认 0（StatusGood）；DA
`__hda_value` 缺失时使用 `tag`。
状态单元格为 NULL 会被拒绝；数值单元格为 NULL、NaN 或 Inf 且未明确标 Bad 时表示无观测，HDA 跳过、DA 保持前值及前质量；明确 Bad 则保留为无值的异常观测。

DA 的 `tag`/`__status` 控制实时发布，`__hda_*` 控制同一轮事务写入的历史点。
HDA 和 DA 的辅助列可以不同，但基础位号集合必须一致。

本 skill 约束把外部数据整理成 `hda_mocker_4` 可直接加载的文件。服务端只读取
`hda/` 和 `da/` 下的 Parquet；原始 CSV、临时脚本和 manifest 不参与运行。

## 目录和文件

每一套数据分别放在以下目录；两侧文件名可以不同：

```text
hda/<dataset>.parquet
da/<dataset>.parquet
```

HDA 与 DA 不允许出现重复位号；不同数据集之间也不允许出现重复位号。配置中的
`playback.files.<dataset>.period_ms` 必须存在，且按单份 DA 文件定义轮播周期。

## Parquet 结构

HDA 第一列必须是 `Timestamp`，类型为带 UTC 时区的 timestamp；后续每列是一个位号，
类型为数值型（服务端当前按 DOUBLE 读取）。时间必须升序、无重复，统一 UTC。

DA 可不带 Timestamp 按固定 period_ms 播放，或使用单份带 UTC Timestamp 的文件按源时间差播放，每100ms检查。不能混用两种模式。HDA/DA 同时存在时基础位号集合必须一致，也允许纯 HDA 或纯 DA。DA 行数可以比 HDA 少。

HDA 每个值列可带 `tag.__status`；DA 可带 `tag.__status`、`tag.__hda_value` 和
`tag.__hda_status`。状态列使用完整 OPC UA StatusCode，范围为 0..4294967295。列缺失才使用默认
0（StatusGood）；但缺少 `.__hda_status` 时继承 DA 的实际观测质量。状态单元格为 NULL 会被拒绝。DA 的 `tag`/`__status` 用于实时发布，
`__hda_value`/`__hda_status` 用于实时入库，两侧辅助列不要求一致。数值为空、NaN 或 Inf
且未标 Bad 时不产生观测；需要异常点时必须显式写 Bad。不得把 BASF 对齐空格全部改为 Bad。宁波旧 192/0 只有在来源已明确其业务含义时才映射为 UA 0/2147483648，已经转换的数据不得再次映射。

## 导入前验证

必须验证：文件存在、列名非空且唯一、HDA/DA 基础位号一致、辅助列均有对应基础位号、
状态单元格非空且在 0..4294967295 范围、无空文件、HDA 时间单调、
采样间隔符合预期、时间为 UTC、数值可以转换为 float。空单元格应明确解释为缺失值，
不能把错误字符串静默转换为零。

## 时间策略

如果源 HDA 时间列可信，保留并转换为 UTC；如果源时间损坏或缺失，必须由任务说明明确
一个结束时间和固定采样间隔，再从结束时间向前生成时间轴。不能凭文件名猜测时间策略。

## 推荐流程

1. 保留原始文件，不覆盖原始数据。
2. 用 `tools/reformat_dataset.py` 或同等解释性脚本生成临时输出。
3. 检查脚本输出的 manifest 和统计信息。
4. 用服务端启动验证 Parquet schema，再进行历史读取和轮播测试。
5. 验证通过后才把输出复制到生产目录，并同步更新 `config.yaml`。
