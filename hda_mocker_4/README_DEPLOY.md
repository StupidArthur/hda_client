# HDA Mocker 4 v1.4.3 CLI 交付说明

在已有生产目录中替换 exe 时，请先停止旧进程，保留原有配置、Parquet 数据及 `runtime/` 数据库，再用 `hda_mocker_4_v1.4.3.exe --config <配置文件路径>` 启动。CLI 会在当前控制台打印运行信息，同时在 exe 同级创建 `logs/`。

## 启动

每套场景测试数据集是 `presets/` 下的一个独立 preset。选择要运行的场景目录：

```powershell
.\hda_mocker_4_v1.4.3.exe --version
.\hda_mocker_4_v1.4.3.exe --config presets\hda_all\config.yaml
.\hda_mocker_4_v1.4.3.exe --config presets\dynamic_data_all\config.yaml
```

数据集清单与场景说明见 `presets/README.md`。

启动日志第一行必须包含：

```text
HDA Mocker 4 v1.4.3 starting
```

配置中的 `opc.tcp://0.0.0.0:18980` 表示监听本机所有网卡。客户端使用生产机
实际 IP 连接，例如 `opc.tcp://10.30.144.70:18980`。服务端会在发现和会话响应
中返回客户端实际使用的访问地址。

## 必需文件

- `hda_mocker_4_v1.4.3.exe`
- `presets/<数据集>/config.yaml`
- `presets/<数据集>/hda/*.parquet`
- `presets/<数据集>/da/*.parquet`
- `presets/<数据集>/runtime/history.duckdb`（如需沿用已有历史库；首次部署时自动创建）
- `presets/<数据集>/runtime/history.duckdb.wal`（若已有且存在，必须和数据库一起携带）

每套数据集单独交付、单独运行；同一时刻只启动一个场景即可。

## 客户端注意事项

- gopcua 手工构造历史请求时，`DataEncoding` 应设置为空 `QualifiedName`。
- asyncua `read_raw_history` 当前应传 `return_bounds=False`；默认值 `True` 请求的
  边界点语义暂未实现。
