# 局域网工具诊断中心

Go + Wails 桌面管理工具，轮询符合 `lan-tool-diagnostics` v1 协议的局域网工具。

- 管理器以诊断 `IP:PORT` 识别工具，名称和部署说明只保存在管理器中。
- 正常轮询 `GET /v1/diag`，点击工具时读取 `GET /v1/detail`。
- 展示 `ok / warn / error / offline`，状态变化写入历史并在界面通知。
- `lan_diag_manager.json` 与 `lan_diag_history.jsonl` 均位于 exe 同目录，可直接随 exe 备份或迁移。

首次启动会自动生成空配置，也可以直接编辑 JSON 后重启。生产部署目录必须允许当前用户写入。
