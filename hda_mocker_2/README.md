# HDA Mocker 2

轻量、配置驱动的 OPC UA HDA 模拟服务器。

```powershell
python main.py config.yaml
```

内置节点由 `preset_nodes.yaml` 控制规模。当前版本不包含用户认证、X.509、事件以及 CSV/Parquet 数据源。
