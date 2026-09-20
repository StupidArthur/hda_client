# OPC UA X.509 Compatibility Mocker

`hda_509` 是一个独立的 OPC UA X.509 兼容性测试工具（Mocker），用于：

* 提供 OPC UA Server，暴露多种 `SecurityPolicy / MessageSecurityMode` 安全端点
* 提供标准 **Discovery** 流程（unsecured GetEndpoints → 选择安全端点 → 建立安全 Session）
* 提供测试 PKI（CA / 服务端证书 / 客户端证书 / 用户证书）生成工具 + 证书 Profile 自动校验
* 提供 Reference / Probe / Discovery Client 与正向、负向、并发测试
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
    UserManager 做 `direct` 校验（精确 DER 白名单 + 有效期检查）
  * 文件：`user_cert.pem`（用户身份，不是应用身份）

两者是**两套完全独立的链路**。测试 PKI 里也明确分开（`client_app_*.pem`
对应用认证，`user_*.pem` 对用户认证），**同一对证书/私钥不允许同时冒充两种身份**。

---

## 2. 当前实际支持的能力

### Discovery（unsecured discovery + 安全 Session）

这是第三方客户端最常见的发现流程，mock 已支持：

```text
connect（None / unsecured discovery）
  -> GetEndpoints / FindServers
  -> 获取 ServerCertificate / SecurityPolicy / SecurityMode / UserTokens
  -> 选择安全 Endpoint
  -> 建立 SecureChannel（Basic256Sha256 / Aes128Sha256RsaOaep / Aes256Sha256RsaPss）
```

实现方式（`security.discovery: true`，默认开启）：
* 底层 BinaryServer **保留 `SecurityPolicyNone` policy factory**，因此客户端
  可以用 `MessageSecurityMode None` 建立临时 SecureChannel 走 unsecured
  `GetEndpoints / FindServers`；
* 但 `GetEndpoints` **只返回 6 个真正支持 Session 的 secure endpoints**，
  **不暴露 None/None Session Endpoint**（通过 asyncua 2.0.1 的 Server 子类
  `DiscoveryCleanServer` 清理，见 `server_builder.py`）；
* `security.require_secured_session: true`（默认）——**任何在无保护通道上
  建立的 Session 都会被拒绝**（客户端无法匹配到 None Session Endpoint，
  且 UserManager 也拒绝无保护通道激活）。
* 也就是说：**Discovery 可以无保护，Session 必须走安全端点**。

> asyncua 2.0.1 限制说明：
> asyncua 的 `Server` 把「None 通道能否建立」和「是否发布 None 端点」绑定在
> 一起（`_setup_server_nodes` 中二者同时发生）。它本身不区分
> "discovery-only None" 与 "完整 None Session"。我们通过绑定 asyncua 2.0.1 的
> `DiscoveryCleanServer` 子类在 `_setup_server_nodes()` 后保留
> `SecurityPolicyNone` factory、同时把 None/None EndpointDescription 从
> `iserver.endpoints` 移除，达到「unsecured Discovery 可用 + 不暴露 None
> Session Endpoint」的效果（见 `server_builder.py`），不修改 asyncua 源码。

### SecurityPolicy / MessageSecurityMode

asyncua 2.0.1 实际支持并启用（见 `asyncua.crypto.security_policies.SECURITY_POLICY_TYPE_MAP`）：

| SecurityPolicy        | Sign | SignAndEncrypt |
|-----------------------|:----:|:--------------:|
| Basic256Sha256        | ✅   | ✅             |
| Aes128Sha256RsaOaep   | ✅   | ✅             |
| Aes256Sha256RsaPss    | ✅   | ✅             |

Normal Server（48620）默认发布以上全部 **6 个**加密 Endpoint（GetEndpoints
只返回这 6 个 secure endpoints，**不暴露 None/None Session Endpoint**）。
通过 `security.policies` 配置可增减。

> asyncua 2.0.1 不支持的组合（如 Basic128Rsa15 之外的遗留策略等）我们
> 没有伪造支持，只启用上面列表里的组合。

### 用户身份认证（UserIdentityToken）

每个安全端点都发布三种 UserIdentityToken：

* Anonymous（身份 = `UserRole.Anonymous`，不是 User；读取测试节点由
  `MockerRoleRuleset` 权限规则授权，身份认证与授权角色分开处理）
* UserName（测试账号 `test / test`）
* Certificate（X.509 User，`direct` 模式：精确 DER 白名单 + 有效期检查）

### 服务端 / 客户端证书校验

* Server 证书：Test CA 签发（`server_cert.pem`），SAN URI 与服务端
  ApplicationUri 一致，含 `localhost`、`127.0.0.1`（及可选的局域网
  DNS/IP），`serverAuth` EKU，正确 KeyUsage
* 客户端 Application 证书：Test CA 签发，SAN URI 与客户端 ApplicationUri
  一致，`clientAuth` EKU，正确 KeyUsage
* 服务端信任存储：`test_material/certs/trust/`（只放 Test CA）

另外提供一套**完全 self-signed 的测试 PKI**（`test_material/self_signed/`），
用于 `config_self_signed.yaml`（48626）：服务端证书 self-signed，**信任存储
直接信任一个 self-signed 客户端 Application 证书**（不需要 Test CA）——
用于「自定义 self-signed 证书建立 OPC UA SecureChannel」的兼容性测试
（见 §6.2 / §7 / §10）。

证书都带 **SKI / AKI / OrganizationName**，Root CA 为
`BasicConstraints(ca=True)` + CA KeyUsage；自签名 Server 证书是
`BasicConstraints(ca=False)` 但 `KeyUsage.keyCertSign=True`（见 §6 与
`tests/certificate_profile_tests.py`）。

---

## 3. 端口与场景

| 端口 | 场景 | 组态 | 说明 |
|------|------|------|------|
| 48620 | normal | `config_x509.yaml` | CA 签发证书，GetEndpoints 返回全部 6 个加密端点 |
| 48621 | self-signed | `config_scenarios/self_signed_48621.yaml` | 服务端用自签名证书（client 必须 pin 该证书） |
| 48625 | custom-uri | `config_scenarios/custom_uri_48625.yaml` | 非默认 ApplicationUri（`urn:ua-hda:test:custom-server`） |
| 48626 | self-signed PKI | `config_self_signed.yaml` | **完全自签名 PKI**：服务端直接信任 self-signed 客户端 Application 证书（无 CA），Basic256Sha256 + SignAndEncrypt |

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
  server_builder.py            公共 Server 构建（安全端点/证书/信任/用户认证/权限）
  user_manager.py              组合 UserManager + X.509 User direct 校验
  user_token_tracking.py       asyncua 限制的解决（跟踪令牌类型 + 通道是否受保护）
  config_loader.py             YAML 组态加载与路径解析
  config_x509.yaml             正常场景组态
  config_self_signed.yaml      完全 self-signed PKI 场景组态（48626）
  config_scenarios/            self-signed 48621 / custom-uri 48625
  change_engines.py            change=true 节点的值变化引擎
  type_mapping.py              OPC UA 类型映射
  log_util.py                  日志工具
  install_offline.bat          Windows 离线安装依赖
  lib/                         Windows 离线 wheel（Python 3.11）
  client/
    discovery_probe.py         unsecured discovery 探针（无需预知 server 证书）
    probe.py                   受保护通道 GetEndpoints 探针
    endpoint_dump.py           端点/证书可读输出（两个 probe 共用）
    reference_client.py        分阶段参考客户端（三种身份认证）
    app_cert_client.py         Application Certificate + Anonymous
    username_client.py         Application Certificate + UserName
    x509_user_client.py        Application Certificate + X.509 User
    conn.py                    共享连接/分阶段输出逻辑
  tests/
    certificate_profile_tests.py  证书 Profile 自动校验（139 项）
    positive_tests.py             正向测试 + endpoint 断言
    negative_tests.py             负向测试（严格 PASS/FAIL，离线判定）
    concurrent_auth_tests.py      并发用户认证测试（30 会话）
    custom_application_uri_tests.py 自定义 ApplicationUri 测试
    self_signed_client_tests.py   self-signed 客户端信任测试（48626）
  test_material/
    gen_certs.py               测试 PKI 生成（CA/服务端/客户端/用户证书）
    certs/                     CA-signed 测试证书（test-only）
    self_signed/               self-signed 测试 PKI（server/client/trust）
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

局域网测试（mock 跑在 macOS，产品 Client 跑在 Windows 时很有用）：

```bash
python test_material/gen_certs.py \
  --server-dns my-mac.local \
  --server-ip 192.168.1.50
```

`--server-dns` / `--server-ip` 可重复、也接受逗号列表
（`--server-dns host1,host2`）。最终 Server 证书 SAN 包含：URI、`localhost`、
`127.0.0.1`、额外 DNS、额外 IP。**生成脚本是纯 Python（只用
`cryptography`），不依赖系统 OpenSSL CLI**，macOS / Windows 一致。

每次运行都会**重新生成整套测试 PKI**（证书序列号随机），输出到
`test_material/certs/`：

```text
ca_cert.pem / ca_key.pem                       Test Root CA（ca=True, SKI/AKI/CA KU）
server_cert.pem / server_key.pem               服务端 Application 证书（CA 签发）
server_custom_uri_cert.pem / _key.pem          服务端证书（SAN URI = 自定义 URI）
server_self_signed_cert.pem / _key.pem         自签名服务端证书（48621；ca=False 但 keyCertSign=True）
client_app_a_cert.pem / _key.pem               客户端 Application 证书 A
client_app_b_cert.pem / _key.pem               客户端 Application 证书 B
client_app_wrong_uri_cert.pem / _key.pem       客户端证书（SAN URI 错误，负向测试）
client_app_expired_cert.pem / _key.pem         客户端证书（已过期，负向测试）
client_app_untrusted_cert.pem / _key.pem       客户端证书（自签名，未受信，负向测试）
client_app_a_mismatch_key.pem                  与证书不匹配的私钥（负向测试）
user_cert.pem / user_key.pem                   X.509 用户证书（用户认证，在白名单）
user_unregistered_cert.pem / _key.pem          CA 签发、profile 合法但【不在白名单】（负向测试）
user_expired_cert.pem / _key.pem               已过期的用户证书（在白名单但过期 -> 拒绝）
user_untrusted_cert.pem / _key.pem             self-signed 用户证书（malformed/profile 不合规）
user_wrong_key.pem                             与用户证书不匹配的私钥（负向测试）
trust/ca_cert.pem                              服务端信任存储（Test CA）
```

所有 Application/User 证书都带：Subject CN + OrganizationName（`ua_hda test`）、
SAN URI、正确 EKU、正确 KeyUsage、**SKI + AKI**。

> **这些证书只用于测试，不能作为生产 PKI。**

### 6.2 生成 self-signed 测试 PKI（48626 场景）

```bash
python test_material/self_signed/gen_self_signed_certs.py
```

生成 `test_material/self_signed/`：

```text
self_signed/
  server/server_self_signed_cert.pem + _key.pem   自签名 Server Application 证书
  client/client_self_signed_cert.pem + _key.pem   受信任的 self-signed 客户端证书
  client/client_self_signed_untrusted_cert.pem…   未受信任（不在 trust）
  client/client_self_signed_expired_cert.pem…     已过期
  client/client_self_signed_wrong_uri_cert.pem…   SAN URI 错误
  client/client_self_signed_mismatch_key.pem      与证书不匹配的私钥
  trust/client_self_signed_cert.pem               服务端信任存储：直接信任该 self-signed 客户端证书
```

全部为 self-signed（`BasicConstraints ca=False`，但 `KeyUsage.keyCertSign=True`
以便证书自证为信任锚），带 SKI/AKI/OrganizationName。生成脚本纯 Python，
不依赖 Test Root CA、不依赖 OpenSSL CLI，macOS/Windows 一致。

---

## 7. 启动 Server

```bash
python main.py config_x509.yaml                        # Normal 48620
python main.py config_scenarios/self_signed_48621.yaml # Self-signed 48621
python main.py config_scenarios/custom_uri_48625.yaml  # Custom URI 48625
python main.py config_self_signed.yaml                 # Self-signed PKI 48626
```

可以同时跑多个场景（不同端口互不冲突）。

---

## 8. Discovery 流程怎么测

第三方客户端通常这样发现 endpoint，用 `discovery_probe.py` 模拟：

```bash
python client/discovery_probe.py
```

它**不需要预先知道 server 证书或安全参数**：走 unsecured discovery →
`GetEndpoints` → 打印全部端点（**6 个 secure endpoints**，不含 None/None）
与 Server 证书详情，并验证 unsecured Session 无法建立（不暴露 None Session
Endpoint）。

```bash
# 查看其它场景
python client/discovery_probe.py --url opc.tcp://127.0.0.1:48621/ua_mocker/
python client/discovery_probe.py --url opc.tcp://127.0.0.1:48625/ua_mocker/
```

### 受保护通道探针（已知道安全参数的场景）

```bash
python client/probe.py                                 # secured GetEndpoints
python client/probe.py --url opc.tcp://127.0.0.1:48621/ua_mocker/ \
    --server-cert test_material/certs/server_self_signed_cert.pem
```

---

## 9. 运行三种正常认证测试

```bash
# A. Application Certificate + Anonymous
python client/app_cert_client.py
# B. Application Certificate + UserName (test/test)
python client/username_client.py
# C. Application Certificate + X.509 User Certificate
python client/x509_user_client.py
```

或用参考客户端（可切换策略/模式）：

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

### 证书 Profile 自动校验（无需服务器）

```bash
python test_material/gen_certs.py
python tests/certificate_profile_tests.py     # 139 项：SAN/EKU/KeyUsage/SKI/AKI/签名/有效期...
```

### 正向测试

```bash
python tests/positive_tests.py                        # Normal 48620
python tests/positive_tests.py --with-self-signed     # 加测 48621
python tests/positive_tests.py --with-custom-uri      # 加测 48625
```

正向测试包含 **endpoint 发现断言**：逐个检查发布的安全端点
（`Basic256Sha256 / Aes128_Sha256_RsaOaep / Aes256_Sha256_RsaPss` ×
`Sign / SignAndEncrypt`）的 Policy URI、SecurityMode、UserIdentityTokens
（Anonymous+UserName+Certificate），并断言 GetEndpoints **不暴露 None/None
Session Endpoint**，而不是只看数量。

### 负向测试（严格 PASS/FAIL）

```bash
python tests/negative_tests.py                # Normal 48620
python tests/negative_tests.py --with-self-signed   # 加测 48621 错误 pin
```

规则：只有「连接失败 + 发生在预期阶段 + 包含预期 StatusCode」才算 **PASS**；
服务器离线 / ConnectionRefused / DNS 错误一律 **FAIL**；任一 FAIL 退出码非 0。
（没有 "PASS?" 这种假绿。）

| 用例 | 期望结果 |
|------|----------|
| NoSecurity | unsecured Session 被拒绝（服务器在线但不暴露 None Session Endpoint） |
| 未受信客户端 Application 证书 | CreateSession `BadCertificateUntrusted` |
| 错误客户端 Application URI | CreateSession `BadCertificateUriInvalid` |
| 过期客户端 Application 证书 | CreateSession `BadCertificateTimeInvalid` |
| 客户端证书/私钥不匹配 | OpenSecureChannel 阶段失败 |
| 未注册 User 证书（CA 签发, 白名单外） | ActivateSession `BadUserAccessDenied` |
| 错误 User 私钥 | ActivateSession **严格** `BadIdentityTokenInvalid` |
| 过期 User 证书（在白名单但过期） | ActivateSession 拒绝（CreateSession 正常，证明是用户链路） |
| Self-signed User 证书（profile 不合规） | ActivateSession `BadUserAccessDenied` |
| Self-signed 服务器错误 pin（48621） | OpenSecureChannel / CreateSession 阶段失败 |

测试区分 **SecureChannel/Application 认证失败**（CreateSession 及之前）
与 **ActivateSession/User 认证失败**，并保留原始 StatusCode。

### 并发用户认证测试

```bash
python tests/concurrent_auth_tests.py    # 30 个并发会话（含无效身份降级检测）
```

并发执行 6 类身份（各 5 个）：
`valid anon / valid username / valid x509` 必须全部成功，
`invalid username / unregistered x509 / expired x509` 必须全部失败。
如果 `user_token_tracking` 的令牌类型在并发下串成 `anon`，无效身份可能被
错误当作 Anonymous 放行——本测试能抓出来。它**只验证**“并发下令牌类型隔离 +
无效身份不会被降级放行”，不宣称验证其它能力。

### 自定义 ApplicationUri 测试

```bash
python main.py config_scenarios/custom_uri_48625.yaml
python tests/custom_application_uri_tests.py
```

验证非默认 ApplicationUri（`urn:ua-hda:test:custom-server`）确实生效于：
EndpointDescription.Server.ApplicationUri、ServerArray、NamespaceArray[1]、
Server 证书 SAN URI。

### Self-signed Client Trust 测试（48626）

```bash
python main.py config_self_signed.yaml
python tests/self_signed_client_tests.py
```

服务端直接信任一个 self-signed 客户端 Application 证书（无 Test CA）。
覆盖：

| 用例 | 期望结果 |
|------|----------|
| 受信任 self-signed 客户端证书 | 成功：OpenSecureChannel → CreateSession → ActivateSession(Anonymous) → Read |
| 未受信任 self-signed 客户端证书 | CreateSession `BadCertificateUntrusted` |
| 过期 self-signed 客户端证书 | CreateSession `BadCertificateTimeInvalid` |
| ApplicationUri 不匹配 | CreateSession `BadCertificateUriInvalid` |
| 证书/私钥不匹配 | OpenSecureChannel 阶段失败 |

手工验证：

```bash
python client/discovery_probe.py --url opc.tcp://127.0.0.1:48626/ua_mocker/
python client/reference_client.py --url opc.tcp://127.0.0.1:48626/ua_mocker/ \
    --app-cert test_material/self_signed/client/client_self_signed_cert.pem \
    --app-key test_material/self_signed/client/client_self_signed_key.pem \
    --server-cert test_material/self_signed/server/server_self_signed_cert.pem \
    --app-uri urn:example.org:FreeOpcUa:selfsigned-client
```

---

## 11. 手工验证建议

```bash
# 终端 1
python main.py config_x509.yaml

# 终端 2
python test_material/gen_certs.py       # 重新生成证书后请重启 Server
python client/discovery_probe.py        # 标准 discovery 流程
python client/probe.py                  # 受保护通道探针
python client/x509_user_client.py       # X.509 用户认证
python tests/positive_tests.py
python tests/negative_tests.py
python tests/concurrent_auth_tests.py
```

---

## 12. asyncua 2.0.1 的已知限制与处理

1. **UserManager 无法区分 Anonymous 与 X.509 用户令牌**
   `get_user()` 的 `certificate` 参数在"受保护通道 + Anonymous"时会传入客户端
   应用证书，在 `X509IdentityToken` 时才传入用户证书。仅凭该参数无法区分。
   处理：见 `user_token_tracking.py` —— 通过 asyncua 提供的
   `Server(iserver=...)` 扩展点，在自定义 `InternalSession` 中记录本次
   `UserIdentityToken` 类型与底层通道是否受保护（同步窗口内无 await，
   无跨会话竞争；`tests/concurrent_auth_tests.py` 实测 30 并发不串）。
2. **客户端证书/私钥不匹配时的错误信息**
   asyncua 客户端在 OpenSecureChannel 阶段会因服务端无法解密而抛出内部
   `AttributeError` 或超时，而不是一个干净的 OPC UA StatusCode。连接仍然被拒绝
   （测试通过），只是错误文案不够友好。
3. **X.509 User validation = direct 白名单 + 有效期**
   当前 `X509IdentityToken` 校验是「注册用户证书的精确 DER 白名单 + 有效期检查」，
   **不是**完整用户 PKI。本轮不实现：User Certificate CA-chain trust /
   Intermediate CA / CRL / CertificateGroup / TrustList。
   `user_unregistered_cert.pem`（CA 签发、profile 合法）用于单一变量测试
   「未注册进白名单」；self-signed 用户证书仅作为 malformed/profile 不合规材料。
4. **discovery-only None 的实现**
   asyncua 把「None 通道能否建立」与「是否发布 None 端点」绑定，无法原生区分
   discovery-only 与完整 None Session。处理：绑定 asyncua 2.0.1 的
   `DiscoveryCleanServer` 子类保留 `SecurityPolicyNone` factory 但把 None/None
   EndpointDescription 从 GetEndpoints 移除；Session 层再由 UserManager 拒绝
   无保护通道激活（双保险，见 `server_builder.py` / `user_manager.py`）。
5. **Anonymous 授权**
   asyncua 默认 `SimpleRoleRuleset` 给 `UserRole.Anonymous` 空权限。mock 通过
   `MockerRoleRuleset` 让 Anonymous 身份仍为 `UserRole.Anonymous`，但授予读取
   测试节点的用户级权限（身份认证与授权角色分开处理）。

---

## 13. 设计要点

* 场景差异全在 YAML 配置里，代码只有一份 `server_builder.py` 构建入口
* Discovery / 应用 URI / 权限规则 / X.509 User direct 语义都由配置或代码显式表达
* 所有证书生成/路径处理用 `pathlib.Path` + `cryptography`，macOS/Windows 一致
* 证书安全相关代码有注释，明确区分 Application Certificate / User Certificate /
  Server trust store / User certificate trust
* 不为不支持的组合伪造"看似支持"
