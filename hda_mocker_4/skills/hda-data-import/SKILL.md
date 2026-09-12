# HDA/DA 数据导入约束

## 当前列约定

HDA Parquet 使用 `Timestamp`、`tag` 和可选 `tag.__status`。DA Parquet 使用
`tag`、可选 `tag.__status`、可选 `tag.__hda_value` 和可选
`tag.__hda_status`。辅助列归属于基础位号，不会成为位号。状态值是完整 OPC UA
StatusCode（UInt32，0..4294967295）。状态列缺失时默认 0（StatusGood）；DA
`__hda_value` 缺失时使用 `tag`。
状态单元格为 NULL 会被拒绝；数值单元格为 NULL、NaN 或 Inf 表示无值，返回
`BadWaitingForInitialData`。

DA 的 `tag`/`__status` 控制实时发布，`__hda_*` 控制同一轮事务写入的历史点。
HDA 和 DA 的辅助列可以不同，但基础位号集合必须一致。

本 skill 约束把外部数据整理成 `hda_mocker_4` 可直接加载的文件。服务端只读取
`hda/` 和 `da/` 下的 Parquet；原始 CSV、临时脚本和 manifest 不参与运行。

## 目录和文件

每一套数据使用同一个文件名，分别放在：

```text
hda/<dataset>.parquet
da/<dataset>.parquet
```

HDA 与 DA 不允许出现重复位号；不同数据集之间也不允许出现重复位号。配置中的
`playback.files.<dataset>.period_ms` 必须存在，且按单份 DA 文件定义轮播周期。

## Parquet 结构

HDA 第一列必须是 `Timestamp`，类型为带 UTC 时区的 timestamp；后续每列是一个位号，
类型为数值型（服务端当前按 DOUBLE 读取）。时间必须升序、无重复，统一 UTC。

DA 不包含 `Timestamp` 列；基础位号集合和 HDA 必须完全一致，列类型必须是数值型。
DA 行顺序就是轮播顺序，通常取 HDA 的尾部或指定窗口。DA 行数可以比 HDA 少。

HDA 每个值列可带 `tag.__status`；DA 可带 `tag.__status`、`tag.__hda_value` 和
`tag.__hda_status`。状态列使用完整 OPC UA StatusCode，范围为 0..4294967295。列缺失才使用默认
0（StatusGood）；状态单元格为 NULL 会被拒绝。DA 的 `tag`/`__status` 用于实时发布，
`__hda_value`/`__hda_status` 用于实时入库，两侧辅助列不要求一致。数值为空、NaN 或 Inf
时始终作为 BadWaitingForInitialData 返回，即使状态列是 Good。

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
