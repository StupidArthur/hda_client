# HDA Mocker 2

轻量、配置驱动的 OPC UA HDA 模拟服务器。

```powershell
python main.py config.yaml
```

内置节点由 `preset_nodes.yaml` 控制规模。动态实时值在读取时按时间计算，历史按页生成，聚合按桶直接计算，不预加载七天数据。

当前版本不包含用户认证、X.509、事件以及 CSV/Parquet 数据源。
