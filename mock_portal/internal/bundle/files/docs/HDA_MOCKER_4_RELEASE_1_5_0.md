# v1.5.0 统一版本与迁移

`hda_mocker_4` 为统一代码主线，同一个 CLI 支持普通、宁波和 BASF preset。旧 `hda_mocker_4_basf` 保留作参考。本文优先于旧文档中的 NULL、DA 时间列及质量默认值约定。

## 统一观测规则

| 值 | 质量 | HDA | DA |
|---|---|---|---|
| 空且未明确 Bad | 默认 Good 或其他非 Bad | 不写点 | 保持前值及质量 |
| 任意，包括空 | 具体 Bad 码 | 写入原值及原质量 | 更新原值及原质量 |
| 有限数值，包括零 | 默认 Good 或显式质量 | 写入 | 更新 |

无状态列时默认 Good。有状态列时仍要求合法 UInt32 整数，拒绝 NULL 状态单元格。NaN/Inf 按无有效值处理。Uncertain 等质量在有值时原样保留。Bad 使用质量码最高位判断；旧 OPC DA 的192/0不是 OPC UA 的 Good/Bad。

HDA 仍为 UTC `Timestamp` + 位号 Double + 可选 `tag.__status`。宽表对齐空格不生成历史点，无需新增 present 列。

DA 保留 `.__status`、`.__hda_value`、`.__hda_status`。无 HDA 覆盖列时，历史跟随 DA 实际观测的值和质量，而不是保持值。覆盖列只改变对应的值或质量；列不存在才回退。覆盖值为空且未标 Bad 时不追加。

## 播放节奏

- DA 无 Timestamp：按每文件 `period_ms` 固定播放，支持多文件、持久化游标。
- DA 有 Timestamp：按源相对时间播放，每100ms检查；末行到下轮首行100ms；重启从首行。目前每套时间轴 preset 只支持一份 DA 文件，不支持与固定周期文件混用。配置仍声明 `period_ms: 100`。
- 两种模式初始化均向循环顺序前方查找最近真实观测及质量；固定周期从持久化游标前方查找。全列无观测时为 BadWaitingForInitialData。
- 历史事务成功后才更新 DA、发送通知。订阅仍按既有机制合并最新值，不保证中间通知全部送达。
- 当前 UTC 必须晚于已有历史末尾，否则拒绝启动。运行中写入失败时 DA 显示 BadOutOfService，诊断报告 error。

## 升级

1. 备份生产 Parquet、配置、exe 与 runtime；在独立目录验收。
2. 有值数据无需改。NULL 若表示无观测可保留；若用于模拟异常，补明确 Bad 码。若 DA 为 Bad 而 HDA 应为 Good，显式填 `.__hda_status=0`。
3. 必须使用新 runtime。非空旧数据库没有 v1.5 语义标记时会被拒绝，不自动删除或迁移历史。
4. 运行 `hda_mocker_4_v1.5.0.exe --config <preset>/config.yaml`。
5. 旧宁波192/0现场约定需确认业务含义后转换，引擎不猜测。当前 bundled preset 已转换为标准 UA 0/2147483648，不要再次映射；详见 [现有 preset 迁移记录](../presets/FORMAT_MIGRATION_1_5_0.md)。

## BASF 及验证

`presets/basf_dirty` 已合入。现有转换脚本及 manifest 标明源时间为 Asia/Shanghai，输出为 UTC；沿用这份已有修正，以当前 manifest/Parquet 为准。首次部署不带开发机 runtime。

测试覆盖固定周期质量及 HDA 覆盖值、空/Bad/Good、BASF 243248点、时间轴跨轮追加、未来时间拒绝及故障质量。100ms是检查周期而非网络送达保证。性能与72小时跨轮现场验收见 preset 的 FIELD_ACCEPTANCE.md，其旧版exe名称及旧runtime复用说明由本文替代。
