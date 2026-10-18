# 本地验收夹具（不参与运行时）

这些 preset 只用于在没有运行 Mock 时验证门户的四种状态。它们**不是**生产配置，
不能被复制去填写真实 IP 或端口。

| 目录 | 模拟状态 | 诊断地址 |
|---|---|---|
| `healthy` | 采集正常 + 源端 ok | `127.0.0.1:18990` |
| `warned` | 采集正常 + 源端 warn | `127.0.0.1:18991` |
| `unmonitored` | 未监控（无 diagnostic.listen） | — |
| `unavailable` | 不可达（端口未监听，连续 3 次失败） | `127.0.0.1:18999` |

使用方式：

```powershell
# 终端 A / B：两个假诊断源
cd mock_portal
go run ./scripts/fake_diag -listen 127.0.0.1:18990 -status ok
go run ./scripts/fake_diag -listen 127.0.0.1:18991 -status warn

# 终端 C：门户指向夹具目录
@"
listen: 127.0.0.1:8900
presets_dir: <仓库>/mock_portal/testdata/presets
exclude_presets: []
"@ | Set-Content fixtures.yaml
.\mock_portal.exe -config fixtures.yaml
```
