# UA Auth Lab 授权矩阵交接说明

更新时间：2026-09-23

## 用途与现状

本工具用于验证 OPC UA 客户端对安全端点、应用证书和用户身份的处理。它是测试服务，不是生产授权中心。每个端口只发布一种 SecurityPolicy/MessageSecurityMode 组合和一种用户身份方式，便于明确判断连接在哪一层成功或被拒绝。

当前矩阵共 77 个端口：48730–48762 为 33 个严格校验端口（11 种端点组合 × Anonymous、UserName、X.509 用户身份）；48770–48813 为 44 个开放接入端口（11 种端点组合 × Anonymous、UserName × `basic`、`none` 两档应用证书校验）。`configs/matrix.yaml` 是配置真源，`tools/gen_matrix_configs.py` 生成各端口 YAML 和 `configs/matrix/manifest.json`。

认证分为两层：安全通道与 CreateSession 处理客户端应用证书，ActivateSession 处理 Anonymous、UserName 或 X.509 用户身份。`trusted` 要求应用证书受信且有效；`basic` 不查信任目录，但检查有效期及 ApplicationUri；`none` 不校验客户端应用证书。三档都不会关闭用户身份验证。X.509 用户证书由独立白名单校验，不能用应用证书代替。

## 启动与检查

在 `ua_auth_lab` 目录执行。建议使用 Python 3.11 和项目指定的 asyncua 2.0.1。首次在测试环境使用时，按实际访问服务端的 DNS/IP 生成含对应 SAN 的测试证书；生成器会更新测试证书和密钥，不要在已有进程使用这些文件时重生成。`test_material/` 中的密钥只用于测试。

```powershell
python test_material/gen_certs.py --server-dns host.example --server-ip 10.30.70.77
```

把示例 DNS/IP 换成实际访问地址。

把 `configs/matrix.yaml` 的 `client_host` 改为客户端可访问的服务端地址，再生成矩阵并启动：

```powershell
python tools/gen_matrix_configs.py
python tools/matrix_ctl.py start --allow-insecure
python tools/matrix_ctl.py status
```

`--allow-insecure` 是启动全部 77 端口时必须显式给出的开关，因为其中包含完全不校验应用证书的 `none` 端口。只启动严格端口可用 `python tools/matrix_ctl.py start --only 48748`。测试完执行 `python tools/matrix_ctl.py stop`，再用 `status` 确认端口释放。启停脚本会核对进程记录，遇到被其他进程占用的端口会报错；不要按端口号盲目结束进程。

常用验证命令：

```powershell
python tests/unit_safety_tests.py
python tests/matrix_port_tests.py
python tests/client_cert_validation_tests.py
python tests/auth_negative_tests.py
python tests/auth_concurrent_tests.py
python tests/authorization_tests.py
python tests/session_lifecycle_tests.py
python client/auth_matrix_client.py --negative
```

## 已验证范围

2026-09-23 在本机隔离副本中，将客户端地址临时改为 `127.0.0.1` 后完成 77 端口验证：矩阵断言 385/385、矩阵客户端正向 77/77 和负向 154/154、证书校验 11/11、认证负向 11/11、并发认证 9/9、授权边界 9/9、会话生命周期 3/3、离线安全测试 7/7，全部通过。临时服务已全部停止。测试环境、矩阵哈希和限制见 [完整报告](reports/full_test_result_20260923.md)。

自动化客户端和服务端均使用 asyncua 2.0.1。本轮授权脚本实际验证了 Read/Write，未单独执行 Browse；客户端还报告过测试证书缺少当前 Windows 主机名的 SAN。这些结果不能证明 Browse 授权、严格主机名校验或第三方 UA 客户端兼容性。

## 尚需实机验收

在实际访问地址和重新生成的证书下，至少使用 UaExpert 和一种非 Python 协议栈验证代表性端口。检查 GetEndpoints 展示的端点和 UserTokenPolicy、正确与错误身份的结果，以及 Browse、Read、Write 和订阅。详细组合与记录项见 [第三方客户端验收清单](tests/THIRD_PARTY_ACCEPTANCE.md)。若某客户端不支持旧安全策略，应记录为客户端能力限制，不能算服务端认证失败。

项目中的应用证书文件以 PEM 保存；OPC UA 报文传输证书时使用 DER。若客户端产品只接受 DER 证书文件，可将同一张证书转换格式，例如 `openssl x509 -in cert.pem -outform DER -out cert.der`。转换后仍需配套使用对应私钥，并确认选择的是客户端应用证书还是 X.509 用户证书。
