# 新 preset 空白模板

本目录是模板，没有HDA/DA Parquet，不计入门户的数据集/Mock数量，也不能直接启动成可用Mock。

## 制作步骤

1. 复制本目录并换成真实的preset目录名。
2. 准备标准HDA/DA Parquet，放到hda/、da/第一层；至少一侧有文件。
3. 修改config.yaml：UA实际IP和端口、Namespace、每份DA文件名及period_ms。不要照抄占位的hda_all.parquet。
4. 如需网站诊断，在config中启用diagnostic.listen；确认端口不与其他UA/诊断监听冲突。
5. 重写本summary.md，列清数据来源、位号、HDA时间范围及点数、DA周期、质量/空值含义和已知限制。

## summary.md 建议结构

- 数据概览：名称、来源、用途、实际位号数和观测数。
- HDA：文件、UTC区间、时间精度、等间隔/不等间隔和各位号点数。
- DA：文件、固定周期/源时间轴、period_ms、循环时长及初始化/重启行为。
- 数据特征：零值、负值、稀疏、恒值、尖峰、Good/Uncertain/Bad和空值含义。
- 配置说明：解释config字段，说明以config当前内容为准，不另维护一套连接信息。
- 位号清单及使用限制。

旧数据NULL表示Bad时必须写明确Bad；新数据NULL表示无观测时保持空并注明，不能一律转换。BASF稀疏场景可作为无观测规则的参考。详情见 ../../docs/HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md。
