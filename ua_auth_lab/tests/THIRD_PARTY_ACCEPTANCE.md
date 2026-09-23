# 异构 OPC UA 客户端验收清单

这份清单用于防止 `asyncua` 服务端与 `asyncua` 客户端“同源自证”。每次发布至少用 UaExpert 和一种非 Python 协议栈完成下列代表性组合。

| 场景 | 端口 | 预期 |
|---|---:|---|
| None/None + Anonymous | 48730 | 发现、登录、Browse、Read 成功 |
| None/None + UserName | 48731 | UserName Token 可被正确选择，正确密码成功 |
| Basic256Sha256/Sign + UserName | 48746 | 签名通道成功，错误密码被拒绝 |
| Basic256Sha256/SignAndEncrypt + Anonymous | 48748 | 加密通道和匿名成功 |
| Basic256Sha256/SignAndEncrypt + X.509 User | 48750 | 应用证书与用户证书分别校验 |
| Aes256Sha256RsaPss/SignAndEncrypt + UserName | 48761 | 现代策略成功 |
| Basic128Rsa15/Sign + UserName | 48734 | 客户端若支持废弃策略则成功，否则记录为客户端不支持 |

每个场景记录：客户端名称与版本、操作系统、服务端 Git commit、测试时间、GetEndpoints 实际内容、登录结果、Browse/Read/Write/Subscribe 结果、失败 StatusCode。

发布门禁：

- UaExpert 不得只验证“能连接”，必须检查 Endpoint 和 UserTokenPolicy 展示。
- 非 Python 协议栈至少选用 open62541、Milo、gopcua 之一。
- 任一客户端发现协议差异时，不得用 asyncua 自测通过覆盖该结论。
- 报告必须明确区分“服务端拒绝”与“客户端不支持该策略”。
- 在非 localhost 机器部署前，必须用实际 DNS/IP 重新生成服务端证书，例如：
  `python test_material/gen_certs.py --server-dns host.example --server-ip 10.30.70.77`。
  否则严格客户端应当报服务端证书主机名不匹配，不得将警告当作通过。
