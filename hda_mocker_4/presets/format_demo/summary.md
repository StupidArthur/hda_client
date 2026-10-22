# HDA/DA 格式与播放教学示例

适用程序：包根目录的 `hda_mocker_4_v1.5.2.exe`。本目录是标准preset，只放配置、数据和说明，是很小的合成教学数据，不是性能测试集。

## 快速开始

双击包根目录 `start.cmd`，或在包根目录运行：

```powershell
.\hda_mocker_4_v1.5.2.exe --config .\presets\format_demo\config.yaml
```

UA：`opc.tcp://127.0.0.1:14860`；诊断：`http://127.0.0.1:14861/v1/diag`。实际地址以 `config.yaml` 为准，本机演示用；跨机器需调整同一配置并检查端口/防火墙。

完整人类说明见 [格式和逻辑](../../docs/HDA_DA_FORMAT_AND_PLAYBACK_GUIDE.md)，含格式、NULL/Bad逻辑、入库、初始化、轮播时间线及查询方法。
启动后的逐项操作与验收记录见 [验证方法](../../docs/HDA_DA_VERIFICATION.md)，带走前先看 [README.md](../../README.md)。

## 数据与配置

| 项目 | 内容 |
|---|---|
| 位号 | `sample_temperature`、`sample_pressure`、`sample_quality` |
| NodeId | `ns=3;s=<位号名>`，在 `Objects/HDA_Mocker` 下 |
| namespace | `urn:hda:mocker4:format-demo` |
| HDA / DA | 各一份 `format_demo.parquet`，各8行 |
| HDA有效点 | 15点：6 / 4 / 5，按上述位号顺序；其中2点Bad |
| 文件UTC范围 | `2026-09-01T00:00:00.125Z`～`2026-09-01T00:00:15.025Z` |
| 实际最后历史点 | `2026-09-01T00:00:10.125Z`；末行全空不入库 |
| 时间 / 值类型 | `timestamp[us, tz=UTC]`，原始毫秒；float64 |
| DA播放 | 不等间隔源时间轴，每100ms检查，一轮15秒 |
| 分页上限 / 保留 | `max_page_size: 5000`；`retention_days: 0`，后续实时历史不自动清理 |

temperature、pressure没有状态列，有值时默认Good；quality有uint32状态列：0=Good，9830400=GoodLocalOverride，2150694912=BadNoCommunication。
NULL且非Bad表示无观测：HDA跳过、DA保持前值与前质量；无值Bad会写入。零值、负值都保留。两侧不使用HDA覆盖列。

## 直观看点

- 首行20/100/1；1秒后只有temperature变21，另外两列不产生新历史点。
- 2.5秒时quality变为无值Bad；4秒时恢复2/Good。
- 6.25秒时temperature是真实0；8秒变-1，quality为3/GoodLocalOverride。
- 10秒时quality为4/Bad；14.9秒全空行继续保持4/Bad；15秒开始下一轮。
- 时间基准是实际播放起点B，不是原始9月日期。初始化会从上一轮找最近观测，不保证客户端能看到短暂初始化状态。

## 校验与交付

原始历史查询UTC `2026-09-01T00:00:00Z`～`2026-09-01T00:00:16Z`，三列应为6/4/5点；原始查询设 `ReturnBounds=false`，正常分页设 `ReleaseContinuationPoints=false`。
数据生成工具为组件下 `tools/build_format_demo_preset.py`，`--verify`只读校验。文件哈希见 `manifest.json`。
已完成离线schema/时间/值/质量/哈希与实际入库校验，不代表已做此示例的UA启动、订阅或性能验收。
初次交付不带runtime，首次启动自动创建；不要混用其他preset的runtime。已导入数据如需修改，先备份旧runtime，再用新库验证。
