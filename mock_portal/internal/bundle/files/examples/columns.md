# 列结构示例

以下示例只说明列名与辅助列的关系，实际数据以 `SKILL.md` 和
`docs/HDA_MOCKER_4_RELEASE_1_5_0.md` 为准。

## HDA

```text
Timestamp                 temperature  temperature.__status  pressure  pressure.__status
2026-01-01T00:00:00Z      21.5         0                     1.20      0
2026-01-01T00:05:00Z      21.7         0                     1.21      0
```

- `Timestamp` 必须是第一列，带 UTC 时区，升序且无重复。
- 每个位号列是一个基础位号；`<tag>.__status` 是同一基础位号的完整 OPC UA
  StatusCode（UInt32）。
- 状态列缺失时按 `0`（StatusGood）处理；状态单元格不能为 NULL。

## DA

```text
temperature  temperature.__status  temperature.__hda_value  temperature.__hda_status
21.5         0                     21.5                     0
```

- `temperature` / `.__status` 控制实时发布。
- `.__hda_value` / `.__hda_status` 控制同一轮写入历史库的值与质量。
- 基础位号集合必须与 HDA（若存在）一致。

## 空值

数值为空、NaN 或 Inf 且未明确标 Bad 时表示“无观测”：HDA 跳过该点，DA 保持前值
及前质量。需要保留异常观测时，必须显式写入 Bad 状态码，不能依赖空数值。
