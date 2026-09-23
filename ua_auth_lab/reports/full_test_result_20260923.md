# UA Auth Lab 授权矩阵验证报告

- 日期：2026-09-23
- 基础提交：`bf9a07a`，并包含当时尚未提交的工作区改动。
- Python：3.11.9
- asyncua：2.0.1
矩阵规模：77 个端口，48730–48762 与 48770–48813。

## 测试配置

测试在临时副本中运行。该副本由当前 `ua_auth_lab` 工作区复制而来，仅将 `configs/matrix.yaml` 的客户端地址从 `10.30.70.77` 改为 `127.0.0.1`，并重新生成 77 端口配置；原工作区矩阵配置和数据未被测试覆盖。

- 工作区 `configs/matrix.yaml` SHA-256：`5B41EB4931B3CC868F27347EB10F5ED5AC9F0B362F2D917DEC7E71A1A759CDCC`
- 临时副本 `configs/matrix.yaml` SHA-256：`C3A91DDF6233E99B84273999C3ACA7F35578D5C06E38F70B37FDA1DF84478442`
- 临时副本生成的 `manifest.json` SHA-256：`BB8B04098D03317B51705AE30ECA6565353FEFE3B6578443D2152305776C2920`

## 结果

| 测试 | 结果 |
|---|---:|
| 配置安全和矩阵 PID 记录单元测试 | 7/7 PASS |
| `matrix_port_tests.py`：每端口正向、Endpoint/Token 隔离和负向抽样 | 385/385 PASS |
| `auth_matrix_client.py --negative` | 正向 77/77，负向 154/154 PASS |
| `client_cert_validation_tests.py`：trusted/basic/none 边界 | 11/11 PASS |
| `auth_negative_tests.py`：错误凭证及证书 | 11/11 PASS |
| `auth_concurrent_tests.py`：三种身份并发隔离 | 9/9 PASS |
| `authorization_tests.py`：Read/Write 权限（未单独执行 Browse） | 9/9 PASS |
| `session_lifecycle_tests.py`：重复及并发 Session | 3/3 PASS |
| Python `compileall` | PASS |

测试结束后矩阵控制脚本停止了 77 个服务进程；随后确认 77 个端口均未监听。

## 限制与注意事项

- 自动化测试的客户端和服务端均为 asyncua 2.0.1，不证明 UaExpert、Milo、open62541 或其他第三方客户端兼容性。第三方验收按 `tests/THIRD_PARTY_ACCEPTANCE.md` 完成后再认定。
- `authorization_tests.py` 中原有的“Browse/Read”输出标签实际只执行了 Read，故本报告不把 Browse 计入已验证权限；第三方验收需单独检查 Browse。
- 测试证书没有当前 Windows 主机名的 DNS SAN，asyncua 客户端输出了主机名不匹配警告。因此本轮结果没有验证严格的服务端主机名校验。部署到其他机器前，应按该机器实际 DNS/IP 重新生成证书并由第三方客户端验收。
- 错误 Application 私钥用例中，服务端日志确认签名校验为 `InvalidSignature`；asyncua 客户端观察到 `OpenSecureChannel` 阶段超时关闭，而不是一个明确 UA StatusCode。该项证明错误签名未能建立通道，不应解读为服务端返回了特定错误码。
- `auth_mode_toggle_tests.py` 的 48631–48633 是旧版独立组态端口，不属于本轮 77 端口矩阵；其历史 9/9 结果不计入本次通过数。
