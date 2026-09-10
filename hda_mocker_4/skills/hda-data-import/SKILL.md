# HDA/DA 数据导入约束

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

DA 不包含 `Timestamp` 列；列名和 HDA 的位号集合必须完全一致，列类型必须是数值型。
DA 行顺序就是轮播顺序，通常取 HDA 的尾部或指定窗口。DA 可以比 HDA 少，但不能多出
HDA 中不存在的位号。

## 导入前验证

必须验证：文件存在、列名非空且唯一、HDA/DA 位号集合一致、无空文件、HDA 时间单调、
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
