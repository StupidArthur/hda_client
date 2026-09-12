# HDA Mocker 4 使用手册

HDA Mocker 4 在不传 `--config` 时启动 Windows 桌面控制台；传入 `--config` 时保持原有无人值守模式。

```powershell
# Windows GUI
.\hda_mocker_4.exe

# 兼容既有自动化脚本；config 文件名可以不是 config.yaml
.\hda_mocker_4.exe --config D:\preset\case-a.yaml
```

## 操作流程

1. 双击程序，点击“选择目录”。选择的目录必须包含 `config.yaml`、`hda/` 和 `da/`；如果业务数据仅包含 HDA 或 DA，另一个目录仍须存在但可为空。
2. 程序以临时内存数据库校验 YAML、Parquet 结构、位号集合和播放周期，不会创建或改写 `runtime/history.duckdb`。
3. 在“配置与数据集”核对 endpoint、原始 YAML、文件行数、位号数、辅助列和 DA 周期。
4. 点击“启动服务”。阶段依次为：校验中、预存 HDA、启动中、运行中。HDA 导入完成后才会开放 OPC UA endpoint 并开始 DA 轮播。
5. “运行概览”显示 HDA 文件完成数量、新导入样本、跳过数量、位号质量分布、DA 文件游标与日志。“位号详情”可检索和按 DA 质量筛选，DA 当前 VQT 与最近入库 HDA VQT 并列显示。
6. 点击“停止”或关闭窗口。服务先停止轮播、等待/回滚正在进行的 HDA 文件事务，再关闭 OPC UA 和 DuckDB。

## 状态与安全边界

- 运行或切换阶段不能重新选择预置目录。停止完成后才允许切换。
- 导入进度按已完成文件显示；单个 Parquet 文件不伪造行级百分比。导入过的同内容文件计为“跳过”，仍计入完成文件数。
- 导入取消通过 Go `context` 传入 DuckDB 查询；一个 Parquet 文件仍是一个事务，取消时整文件回滚，不会留下半个文件的历史样本。
- 页面时间统一按 UTC 显示。质量码显示为 OPC UA `0xXXXXXXXX`，并附良好、不确定、异常或等待数据状态。
- 纯 HDA 数据集没有 DA 当前值。概览在这种情况下明确以最新 HDA 样本统计质量，而非把所有位号误标为等待。

## 技术结构

- Wails v2 包装 Go 服务核心，React 18 + TypeScript + Tailwind CSS v3 实现 UI。
- `RuntimeController` 是 CLI 与 GUI 共享的唯一运行入口，负责启动、停止、状态快照和关闭确认。
- GUI 每秒获取轻量快照；轮播本身不等待 GUI、日志或页面渲染。
- `Playback` 只在实时 HDA 写入与游标提交成功后更新 UI 用的文件状态，因此页面不会展示未提交的游标。

Windows 运行 GUI 需要 WebView2 Runtime。Windows 11 通常已包含；离线 Windows 10 环境请随交付包提供 Microsoft Edge WebView2 Evergreen Runtime。
