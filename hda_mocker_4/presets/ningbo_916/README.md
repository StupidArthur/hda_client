# 宁波 916 preset

本目录是宁波 916 现场数据在 HDA Mocker 4 主版本中的自包含 mock 方案，只包含数据、配置和说明，不包含宁波定制版的 Wails 界面或旧版 Go 代码。

## 启动

在 `hda_mocker_4` 目录执行：

```powershell
.\hda_mocker_4.exe --config presets\ningbo_916\config.yaml
```

服务地址为 `opc.tcp://0.0.0.0:4840`，命名空间索引为 `3`，URI 为 `urn:hda:mocker4:ningbo`。DA 每 5 秒播放一行，同时将该轮对应值写入实时 HDA。

## 数据

- `hda/ningbo_916_hda.parquet`：切分点之前的历史数据。
- `da/ningbo_916_da.parquet`：切分点之后的循环播放数据。
- `manifest.json`：原始 Excel、工作表、位号、时间范围、行数、哈希和转换规则清单。

原始输入是主版本目录 `source_data/916/` 下的 22 个 Excel 文件。转换时仅采用 `App Time`，按 `Asia/Shanghai` 解释，并以 `2026-09-11 00:00:00 +08:00` 为切分点；数据统一到 5 秒时间网格。

## 宁波专用语义

源数据质量码按现场约定原样保留：`192=Good`、`0=Bad`。这不是标准 OPC UA StatusCode 语义；若测试对象严格按 OPC UA StatusCode 解读，使用前应先完成质量码映射。详细转换事实以 `manifest.json` 为准。

本 preset 同时保留 `.__hda_value` 和 `.__hda_status` 控制列，使 DA 播放与实时 HDA 入库行为保持明确、可复现。
