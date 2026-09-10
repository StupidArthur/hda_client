# HDA Mocker 4 v1.0.2 生产包

这是当前唯一的生产交付目录。请整体复制本目录，或在已有生产目录中仅替换
`hda_mocker_4.exe`。

## 启动

```powershell
.\hda_mocker_4.exe --config .\config.yaml
```

启动日志第一行必须包含：

```text
HDA Mocker 4 version=v1.0.2
```

配置中的 `opc.tcp://0.0.0.0:18980` 表示监听本机所有网卡。客户端使用生产机
实际 IP 连接，例如 `opc.tcp://10.30.144.70:18980`。服务端会在发现和会话响应
中返回客户端实际使用的访问地址。

## 必需文件

- `hda_mocker_4.exe`
- `config.yaml`
- `hda/*.parquet`
- `da/*.parquet`
- `runtime/history.duckdb`
- `runtime/history.duckdb.wal`（若存在，必须和数据库一起携带）

## 客户端注意事项

- gopcua 手工构造历史请求时，`DataEncoding` 应设置为空 `QualifiedName`。
- asyncua `read_raw_history` 当前应传 `return_bounds=False`；默认值 `True` 请求的
  边界点语义暂未实现。
