# OPC UA X.509 Compatibility Mocker

`hda_509` 是一个独立的 OPC UA X.509 兼容性测试工具（Mocker），用于：

* 提供 OPC UA Server，暴露多种 `SecurityPolicy / MessageSecurityMode` 安全端点
* 提供测试 PKI（CA / 服务端证书 / 客户端证书 / 用户证书）生成工具
* 提供 Reference / Probe Client 与正向、负向测试
* 供第三方客户端做 X.509、安全 Endpoint、UserIdentity 兼容性验证

> 注意：这不是一个生产级 OPC UA Server，也不是一个生产级 PKI。
> `test_material/` 里的所有密钥都只能用于本地开发测试。

---

## 1. 两个概念，千万不要混

* **Application Certificate（应用证书）**
  * 用于 **SecureChannel / application authentication**
  * 客户端在 OpenSecureChannel / CreateSession 时提交，Server 用
    `trust_store`（信任的 Test CA）校验它
  * 文件：`client_app_a_cert.pem`、`client_app_b_cert.pem`、`server_cert.pem`

* **User X.509 Certificate（用户证书）**
  * 用于 **ActivateSession / X509IdentityToken（user authentication）**
  * 客户端在 ActivateSession 时把用户证书放进 `X509IdentityToken`，Server 的
    UserManager 用**精确匹配注册的用户证书**校验
  * 文件：`user_cert.pem`

两者是**两套完全独立的链路**。测试 PKI 里也明确分开（`client_app_*.pem`
对应用认证，`user_*.pem` 对用户认证），**同一对证书/私钥不允许同时冒充两种身份**。

---

## 2. 当前实际支持的能力

### SecurityPolicy / MessageSecurityMode

asyncua 2.0.1 实际支持并启用（见 `asyncua.crypto.security_policies.SECURITY_POLICY_TYPE_MAP`）：

| SecurityPolicy        | Sign | SignAndEncrypt |
|-----------------------|:----:|:--------------:|
| Basic256Sha256        | ✅   | ✅             |
| Aes128Sha256RsaOaep   | ✅   | ✅             |
| Aes256Sha256RsaPss    | ✅   | ✅             |

Normal Server（48620）默认发布以上全部 **6 个** Endpoint。
**不发布 `None / None`**（NoSecurity）。如确有测试需要，可在组态里打开：

```yaml
security:
  no_security: true
```

> asyncua 2.0.1 不支持的组合（如 Basic128Rsa15 之外的遗留策略等）我们
> 没有伪造支持，只启用上面列表里的组合。

### 用户身份认证（UserIdentityToken）

Normal Server 同时发布：

* Anonymous
* UserName（测试账号 `test / test`）
* Certificate（X.509 User，用 `user_cert.pem`）

### 服务端 / 客户端证书校验

* Server 证书：Test CA 签发（`server_cert.pem`），SAN URI 与服务端
  ApplicationUri 一致，含 `localhost` 与 `127.0.0.1`（不用 `0.0.0.0` 作为客户端访问地址），`serverAuth` EKU，正确 KeyUsage
* 客户端 Application 证书：Test CA 签发，SAN URI 与客户端 ApplicationUri
  一致，`clientAuth` EKU，正确 KeyUsage
* 服务端信任存储：`test_material/certs/trust/`（只放 Test CA）

---

## 3. 端口与场景

| 端口 | 场景 | 组态 | 说明 |
|------|------|------|------|
| 48620 | normal | `config_x509.yaml` | CA 签发证书，发布全部 6 个加密端点 |
| 48621 | self-signed | `config_scenarios/self_signed_48621.yaml` | 服务端用自签名证书，模拟未向 CA 申请证书的部署 |

场景差异全部由 YAML 配置表达（端口、证书路径、策略列表），代码共用
`server_builder.py` 一个构建入口，方便以后增加更多场景（例如
48622 不受信 CA、48623 过期服务端证书 —— 只需加一个组态 + 在
`test_material/gen_certs.py` 里加对应证书）。

客户端统一用 `127.0.0.1` 或 `localhost` 访问，不用 `0.0.0.0`。

---

## 4. 目录结构

```text
hda_509/
  main.py                      入口: python main.py <config.yaml>
  server_main.py               节点构建与更新逻辑
  server_builder.py            公共 Server 构建（安全端点/证书/信任/用户认证）
  user_manager.py              组合 UserManager (Anonymous/UserName/X509 User)
  user_token_tracking.py       asyncua 限制的解决（跟踪 UserIdentityToken 类型）
  config_loader.py             YAML 组态加载与路径解析
  config_x509.yaml             正常场景组态
  config_scenarios/            特殊场景组态（self-signed 48621）
  change_engines.py            change=true 节点的值变化引擎
  type_mapping.py              OPC UA 类型映射
  log_util.py                  日志工具
  install_offline.bat          Windows 离线安装依赖
  lib/                         Windows 离线 wheel（Python 3.11）
  client/
    probe.py                   GetEndpoints 探针，打印全部端点
    reference_client.py        分阶段参考客户端（三种身份认证）
    app_cert_client.py         Application Certificate + Anonymous
    username_client.py         Application Certificate + UserName
    x509_user_client.py        Application Certificate + X.509 User
    conn.py                    共享连接/分阶段输出逻辑
  tests/
    positive_tests.py          正向测试
    negative_tests.py          负向测试
  test_material/
    gen_certs.py               测试 PKI 生成（CA/服务端/客户端/用户证书）
    certs/                     生成的测试证书（test-only）
```

---

## 5. 准备依赖

### macOS

```bash
python -m pip install asyncua==2.0.1 cryptography pyyaml
```

（`asyncua 2.0.1` 会同时装上 `pyopenssl`、`cryptography` 等依赖。）

### Windows

如果目标机没有网络，使用仓库自带的离线 wheel（Python 3.11，x64）：

```bat
install_offline.bat
```

或者联网安装：

```bat
python -m pip install asyncua==2.0.1 cryptography pyyaml
```

要求 `Python >= 3.11`。代码只用 `pathlib.Path` 处理路径、UTF-8 编码，
不依赖 `openssl`、`chmod`、bash、symlink，macOS / Windows 行为一致。

---

## 6. 生成测试证书

```bash
python test_material/gen_certs.py
```

每次运行都会**重新生成整套测试 PKI**（证书序列号随机），输出到
`test_material/certs/`：

```text
ca_cert.pem / ca_key.pem                       Test Root CA
server_cert.pem / server_key.pem               服务端 Application 证书（CA 签发）
server_self_signed_cert.pem / _key.pem         自签名服务端证书（48621 场景）
client_app_a_cert.pem / _key.pem               客户端 Application 证书 A
client_app_b_cert.pem / _key.pem               客户端 Application 证书 B
client_app_wrong_uri_cert.pem / _key.pem       客户端证书（SAN URI 错误，负向测试）
client_app_expired_cert.pem / _key.pem         客户端证书（已过期，负向测试）
client_app_untrusted_cert.pem / _key.pem       客户端证书（自签名，未受信，负向测试）
client_app_a_mismatch_key.pem                  与证书不匹配的私钥（负向测试）
user_cert.pem / user_key.pem                   X.509 用户证书（用户认证）
user_untrusted_cert.pem / _key.pem             未受信用户证书（负向测试）
user_wrong_key.pem                             与用户证书不匹配的私钥（负向测试）
trust/ca_cert.pem                              服务端信任存储（Test CA）
```

生成脚本是纯 Python（只用 `cryptography`），不依赖系统 OpenSSL CLI，
可在 macOS / Windows 上重复执行，路径都基于脚本自身位置计算。

> **这些证书只用于测试，不能作为生产 PKI。**

---

## 7. 启动 Server

### Normal Server（48620）

```bash
python main.py config_x509.yaml
```

### Self-signed Server（48621）

```bash
python main.py config_scenarios/self_signed_48621.yaml
```

可以同时跑多个场景（不同端口互不冲突）。

---

## 8. 运行 Probe（查看端点）

```bash
python client/probe.py
```

输出每个 Endpoint 的 URL、SecurityPolicy、SecurityMode、Server 证书详情
（Subject / Issuer / Serial / NotBefore / NotAfter / Application URI /
DNS / IP / Public Key / Key Size）以及 UserIdentityTokens。

```bash
# 查看 self-signed 场景
python client/probe.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ \
    --server-cert test_material/certs/server_self_signed_cert.pem
```

---

## 9. 运行三种正常认证测试

### 方式一：分别运行三个客户端

```bash
# A. Application Certificate + Anonymous
python client/app_cert_client.py

# B. Application Certificate + UserName (test/test)
python client/username_client.py

# C. Application Certificate + X.509 User Certificate
python client/x509_user_client.py
```

### 方式二：参考客户端 + 参数

```bash
python client/reference_client.py --auth anon
python client/reference_client.py --auth username
python client/reference_client.py --auth x509
python client/reference_client.py --auth x509 --policy Aes256Sha256RsaPss --mode SignAndEncrypt
```

参考客户端输出分阶段结果：

```text
[1] GetEndpoints          OK (6 endpoint(s))
[2] Endpoint selection    OK http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256
[3] OpenSecureChannel     OK
[4] CreateSession         OK
[5] ActivateSession       OK X509 User Certificate
[6] Read test node        OK ns=1;s=int32_ch_1 = 24
```

`x509_user_client.py` 同时使用**两对不同的证书**：
`client_app_a_cert.pem`（应用认证）+ `user_cert.pem`（用户认证）。

---

## 10. 运行测试套件

### 正向测试

```bash
python tests/positive_tests.py                      # Normal 48620
python tests/positive_tests.py --with-self-signed   # 加测 48621
```

### 负向测试

```bash
python tests/negative_tests.py
```

覆盖：

| 用例 | 期望结果 |
|------|----------|
| NoSecurity | 拒绝（Normal Server 不发布 None/None） |
| 未受信客户端 Application 证书 | `BadCertificateUntrusted` |
| 错误客户端 Application URI | `BadCertificateUriInvalid` |
| 过期客户端 Application 证书 | `BadCertificateTimeInvalid` |
| 客户端证书/私钥不匹配 | 连接失败（OpenSecureChannel 阶段） |
| 未受信 User 证书 | `BadUserAccessDenied`（ActivateSession 阶段） |
| 错误 User 私钥 | `BadIdentityTokenInvalid`（ActivateSession 阶段） |

测试会区分 **SecureChannel/Application 认证失败**（CreateSession 及之前）
与 **ActivateSession/User 认证失败**，并保留原始 StatusCode。

---

## 11. 手工验证建议

```bash
# 终端 1
python main.py config_x509.yaml

# 终端 2
python test_material/gen_certs.py      # 重新生成证书后请重启 Server
python client/probe.py
python client/x509_user_client.py
python tests/negative_tests.py
```

---

## 12. asyncua 2.0.1 的已知限制与处理

1. **UserManager 无法区分 Anonymous 与 X.509 用户令牌**
   `get_user()` 的 `certificate` 参数在"受保护通道 + Anonymous"时会传入客户端
   应用证书，在 `X509IdentityToken` 时才传入用户证书。仅凭该参数无法区分。
   处理：见 `user_token_tracking.py` —— 通过 asyncua 提供的
   `Server(iserver=...)` 扩展点，在自定义 `InternalSession` 中记录本次
   `UserIdentityToken` 类型（同步窗口内无竞争），UserManager 据此精确判断。
2. **客户端证书/私钥不匹配时的错误信息**
   asyncua 客户端在 OpenSecureChannel 阶段会因服务端无法解密而抛出内部
   `AttributeError`，而不是一个干净的 OPC UA StatusCode。连接仍然被拒绝
   （测试通过），只是错误文案不够友好。
3. **X.509 用户认证 = 精确匹配注册证书**
   asyncua 的 `CertificateUserManager` 采用精确 DER 匹配，我们保持一致。
   用户证书不受 CA 链信任约束（只做精确匹配），这与 Application Certificate
   的 CA 链校验是两条不同的链路。

---

## 13. 设计要点

* 场景差异全在 YAML 配置里，代码只有一份 `server_builder.py` 构建入口
* 所有证书生成/路径处理用 `pathlib.Path` + `cryptography`，macOS/Windows 一致
* 证书安全相关代码有注释，明确区分 Application Certificate / User Certificate /
  Server trust store / User certificate trust
* 不为不支持的组合伪造"看似支持"
