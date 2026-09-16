# HDA Mocker 2

回放数据固定在配置文件同级的 `data/` 目录。启动时按文件名扫描第一层 `*.parquet`；目录缺失或为空即没有回放节点，新增文件后重启生效。文件必须含 `tag`、UTC `timestamp[us]`、`value`、`value_kind`、`quality` 字段。重复时间戳、Null、NaN/Inf/-0 和 uint32 状态码都会保留；重复位号或无效文件会阻止启动。

轻量、配置驱动的 OPC UA HDA 模拟服务器。

```powershell
python main.py config.yaml
```

内置节点由 `preset_nodes.yaml` 控制规模。动态实时值在读取时按时间计算，历史按页生成，聚合按桶直接计算，不预加载七天数据。

当前版本不包含用户认证、X.509 和事件能力；固定历史回放使用 Parquet 文件。

客户端写入未携带 `SourceTimestamp` 时，Mocker 使用收到写入时的服务器 UTC 时间补齐。
