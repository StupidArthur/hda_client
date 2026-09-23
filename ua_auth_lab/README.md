# UA Auth Lab — OPC UA 认证方式验证台（全拆 · 方案 A）

> 专门用于验证 **OPC UA 全部用户认证方式**（Anonymous / UserName / X.509 User）
> 与**全部安全通道组合**（SecurityPolicy × MessageSecurityMode）的行为，
> 并额外验证**客户端应用证书校验强度**（`trusted` / `basic` / `none`）对
> "任意客户端能否接入"的影响。
>
> 设计原则（**一端口一场景**）：
> **每个端口只开放 1 种端点组合 + 1 种用户认证方式，该端口上其余一切组合必须验证失败。**
>
> 与 `hda_509`（聚焦 48627 单一产品组合认证链路）不同，本项目的目标是
> **把「端点组合 × 认证方式」做成可配置、可自动验证的全拆矩阵**。

---

## 1. 两个正交维度

### 1.1 通道层：SecurityPolicy × MessageSecurityMode

OPC UA Part 7 定义 **6 种 SecurityPolicy**、**3 种 MessageSecurityMode**。
但**不是 6×3=18**，有效组合是 **11**：

| SecurityPolicy | None | Sign | SignAndEncrypt |
|---|:---:|:---:|:---:|
| `None` | ✅ | — | — |
| `Basic128Rsa15` *(deprecated)* | — | ✅ | ✅ |
| `Basic256` *(deprecated)* | — | ✅ | ✅ |
| `Basic256Sha256` | — | ✅ | ✅ |
| `Aes128_Sha256_RsaOaep` | — | ✅ | ✅ |
| `Aes256_Sha256_RsaPss` | — | ✅ | ✅ |

> `None` 只能配 `None`；加密策略只能配 `Sign` / `SignAndEncrypt`。
> 这 11 个组合恰好等于 asyncua 的 `SecurityPolicyType` 枚举成员数。
>
> `deprecated` = OPC Foundation 在 spec 1.04 标记废弃（SHA-1 碰撞 / RSA PKCS#1 v1.5
> padding-oracle），**但仍是合法标准策略**，第三方客户端会列出，故纳入全量验证。
> asyncua 可正常运行，仅在日志打印 `DEPRECATED!` 告警。

### 1.2 用户层：UserIdentityToken

| 认证方式 | Token 类型 | 说明 |
|---|---|---|
| **Anonymous** | `AnonymousIdentityToken` | 匿名身份（`UserRole.Anonymous`，mock 授权允许读测试节点）；Null 令牌等价 |
| **UserName** | `UserNameIdentityToken` | 测试账号 `test / test` |
| **X.509 User** | `X509IdentityToken` | direct 模式：精确 DER 白名单 + 有效期检查 |

### 1.3 应用层：客户端 Application Certificate 校验模式

加密通道（`Sign` / `SignAndEncrypt`）下客户端**必须出示应用证书**；服务端是否
信任这张证书，由 `CertificateValidator` 决定。本项目把它做成每端口可配的三档：

| 模式 | 校验内容 | 覆盖端口 |
|---|---|---|
| `trusted` | 有效期 + URI + KeyUsage/EKU + **必须受信**（只认 `trust_store`） | 核心 33 端口（严格） |
| `basic` | 有效期 + URI（**不查信任目录**） | 开放端口块 22 个 |
| `none` | **完全不校验**（不挂校验器） | 开放端口块 22 个 |

> `basic` = asyncua `BASIC_VALIDATION | PEER_CLIENT`；`none` = 不调用
> `set_certificate_validator`（asyncua 默认 `certificate_validator=None`，
> CreateSession 时整段跳过）。
>
> **这一档只作用于「应用身份」，与「用户身份」（UserIdentityToken）正交。**
> 放宽它**不影响** X.509 **用户**证书的 DER 白名单（那走 `CombinedUserManager`，
> 是另一条独立链路）。典型用途：让 UaExpert / 产品端等未登记的自签证书能远程接入。

### 1.4 为什么是"正交"而不是"并行"

两者分属**不同协议阶段**，且认证方式是**端点级声明**：

```text
GetEndpoints      -> 看到 N 个端点，每个端点各自广告允许哪些 UserTokenPolicy
选择端点          -> OpenSecureChannel(policy, mode)      [通道层]
CreateSession     -> 校验客户端 Application Certificate   [应用层]
ActivateSession   -> 校验 UserIdentityToken               [用户层]
```

所以：**端点组合 × 认证方式 = 叉积矩阵**，但带约束（如 X.509 用户认证需要加密
通道或签名策略提供签名算法；本项目的 `None + x509` 格子见 §5 的已知处理）。

---

## 2. 矩阵规模

```text
核心端口块（应用证书 trusted 严格）: 11 端点 × 3 认证 = 33 端口   48730 .. 48762
开放端口块（应用证书 basic / none）: 11 端点 × 2 认证 × 2 模式 = 44 端口   48770 .. 48813
合计 77 端口（端口起点/步长可在 configs/matrix.yaml 里改）
每端口负向抽样 2 条 -> 预计 231 次连接
```

> 开放端口块只铺 `anon` / `username`：X.509 **用户**认证还要过 DER 白名单，
> 任意客户端即使过了应用证书这一关也照样进不去，铺了没意义。

每个端口的断言（`tests/matrix_port_tests.py`）：

| # | 断言 | 期望 |
|---|---|---|
| 1 | 正向 | 本端口 (policy, mode, auth) 组合成功读到节点 |
| 2 | 端点隔离 | `GetEndpoints` **只暴露这 1 个端点** |
| 3 | Token 隔离 | 该端点**只发布这 1 种** UserIdentityToken |
| 4 | 负向① | 同端点换认证方式 → 拒绝于 `ActivateSession` |
| 5 | 负向② | 换端点组合同认证方式 → 拒绝于 `Endpoint selection` |

共 **385 个断言/轮**（77 端口 × 5）。

应用证书校验强度另有独立测试 `tests/client_cert_validation_tests.py`（11 用例），
见 §6。

---

## 3. 目录结构

```text
ua_auth_lab/
  main.py                       入口: python main.py <config.yaml>
  server_main.py                节点构建与更新
  server_builder.py             公共 Server 构建（安全端点/证书/信任/用户认证/权限）
  user_manager.py               组合 UserManager（三种方式各有显式开关）
  user_token_tracking.py        asyncua 令牌类型跟踪（区分 anon/username/x509）
  config_loader.py              YAML 组态加载与路径解析
  log_util.py                   日志（UA_MOCK_LOG_SUFFIX 支持多端口日志隔离）
  type_mapping.py / change_engines.py

  configs/
    matrix.yaml                 ★ 矩阵唯一真源（端口起点/端点组合/认证方式/应用证书校验模式）
    matrix/                     ★ 自动生成的 77 个端口配置 + manifest.json

  tools/
    gen_matrix_configs.py       ★ 生成器：matrix.yaml -> 77 配置 + manifest
    matrix_ctl.py               ★ 启停控制：start / status / stop

  client/
    conn.py                     共享分阶段连接库（6 阶段，--auth anon/username/x509）
    auth_matrix_client.py       ★ manifest 驱动矩阵客户端（正向 + 负向抽样）
    reference_client.py / discovery_probe.py / endpoint_dump.py / probe.py

  tests/
    _auth_common.py             测试公共模块（含 manifest 选端口 pick_port）
    matrix_port_tests.py        ★ 全拆矩阵测试（385 断言/轮）
    client_cert_validation_tests.py ★ 应用证书校验模式测试（trusted/basic/none，11 用例）
    auth_negative_tests.py      ★ 负向 11 用例（凭证/证书材料类，manifest 驱动）
    auth_concurrent_tests.py    ★ 并发隔离 9 组（manifest 驱动）
    auth_mode_toggle_tests.py   单方式开关测试（48631-33，见 §7）

  test_material/
    gen_certs.py                CA PKI 生成
    certs/                      CA 签发证书（test-only）
  reports/                      测试报告
  lib/ + install_offline.bat    离线 wheel（Python 3.11）
```

---

## 4. 快速开始

```bash
# 1. 依赖（Python >= 3.11）
python -m pip install asyncua==2.0.1 cryptography pyyaml
# 或 Windows 离线
install_offline.bat

# 2. 生成测试 PKI（首次）
python test_material/gen_certs.py

# 3. 生成 77 个端口配置
python tools/gen_matrix_configs.py

# 4. 启动全部端口
python tools/matrix_ctl.py start --allow-insecure  # 启动全部（含 none 开放端口）

# 5. 验证
python tests/matrix_port_tests.py                 # 385 断言
python tests/client_cert_validation_tests.py      # 应用证书校验模式（trusted/basic/none）
python tests/auth_negative_tests.py               # 11 负向
python tests/auth_concurrent_tests.py             # 并发隔离
python client/auth_matrix_client.py --negative    # 交互式矩阵客户端（正向 + 负向抽样）
python client/auth_matrix_client.py --group open  # 只跑开放接入块（不查信任）
```

改矩阵（端口起点、增删策略、增删认证方式、应用证书校验模式）只需编辑
`configs/matrix.yaml` 后重跑第 3 步。

> 只需要"任意客户端能连进来"来验证时，可直接用开放端口块：
> `--validation none` 的端口完全不校验客户端应用证书（自签/过期都能建 Session），
> `--validation basic` 的端口不查信任但仍校验有效期与 ApplicationUri。
> 因此 `matrix_ctl.py` 默认拒绝启动 `none` 端口；只有显式传入
> `--allow-insecure` 才会启动它们。启动单个核心端口不需要该参数。

---

## 5. 已知实现约束与处理

### 5.1 `None/None` + X.509 用户认证

**现象**：纯 `policies: [NoSecurity]` 的端口上，X509 用户认证不可用
（`BadIdentityTokenInvalid`，端点 `UserIdentityTokens` 为空）。

**根因**：asyncua `_set_endpoints()` 在 `mode == None_` 时，必须从
`self._security_policy` 里找到一个**带签名能力**的策略，才能把
`X509IdentityToken` 加进端点；纯 NoSecurity 找不到 → 不 append。

**处理**（生成器自动做）：给 `None + x509`（以及 `None + username`，避免密码
明文）端口额外注入一个加密策略提供签名算法，再用 `security.only_none_endpoint: true`
把该加密端点和加密通道**从 GetEndpoints 与可建通道中同时摘除**：

```text
端点 2 -> 1, 通道策略 2 -> 1
```

对外仍只暴露 `None/None` 一个端点、只接受 None 通道，**隔离语义不变**；
`_policies` 一并过滤，堵住"绕过 GetEndpoints 直开加密通道"的漏洞。

### 5.2 其它

- Discovery 可无保护，加密端口的 Session 必须走加密端点（`require_secured_session: true`）
- `None` 端口则 `require_secured_session: false`，允许在无保护通道建 Session
- 身份认证与授权角色分开：Anonymous 仍是 `UserRole.Anonymous`，由 `MockerRoleRuleset` 授予用户级读权限
- App 证书/私钥不匹配时 asyncua 抛内部错误/超时而非干净 StatusCode（连接仍被拒绝，见负向 Case 10）

---

## 6. 测试与结果边界

自动化测试的服务端和参考客户端都使用 `asyncua 2.0.1`，因此结果只证明
asyncua 内部回归通过，不等价于第三方 OPC UA 客户端兼容性认证。异构验收见
`tests/THIRD_PARTY_ACCEPTANCE.md`。

`reports/full_test_result_20260923.md` 记录当前工作树的 77 端口完整验证。
验证在隔离副本中把客户端连接地址改为 `127.0.0.1`；报告注明了源码版本、
矩阵哈希与证书主机名警告。异构客户端验收仍按 `tests/THIRD_PARTY_ACCEPTANCE.md` 执行。

| 测试 | 2026-09-23 结果 |
|---|---|
| `matrix_port_tests.py` | **385/385 断言 PASS**（77 端口） |
| `client_cert_validation_tests.py`（trusted / basic / none） | **11/11 PASS** |
| `auth_negative_tests.py`（凭证/证书材料负向） | **11/11 PASS** |
| `auth_concurrent_tests.py`（三种身份并发） | **9/9 PASS** |
| `auth_mode_toggle_tests.py`（9 组：单方式开关，48631-33） | **9/9 PASS** |
| `auth_matrix_client.py --negative` | 正向 **77/77**，负向 **154/154 PASS** |
| `authorization_tests.py`（Read/Write；尚未单独执行 Browse） | **9/9 PASS** |
| `session_lifecycle_tests.py`（重复/并发 Session） | **3/3 PASS** |
| `unit_safety_tests.py`（配置 fail-closed、PID 记录保护） | **7/7 PASS** |

以上完整结果和限制见 [本轮测试报告](reports/full_test_result_20260923.md)。
`auth_mode_toggle_tests.py` 的 9/9 是旧端口配置的历史结果，不计入本轮 77 端口验证。

可单独运行的离线和代表性测试：

```bash
python tests/unit_safety_tests.py          # 无需启动服务：fail-closed 配置规则
python tests/authorization_tests.py        # Read/Write 授权边界
python tests/session_lifecycle_tests.py    # 重复和并发 Session 稳定性
```

> 本轮 UA 自动化测试均使用 asyncua 2.0.1；不能替代 UaExpert 或其他协议栈的实机验收。

应用证书校验模式的实证对照（`client_cert_validation_tests.py`；端点
`Basic256Sha256/SignAndEncrypt`，客户端出示**未受信**应用证书）：

| 客户端应用证书 | `trusted` 48748 | `basic` 48782 | `none` 48804 |
|---|:---:|:---:|:---:|
| 未受信但合规（URI 匹配） | ❌ `BadCertificateUntrusted` | ✅ 放行 | ✅ 放行 |
| 已过期 | ❌ | ❌ `BadCertificateTimeInvalid` | ✅ 放行 |
| ApplicationUri 不匹配 | ❌ | ❌ `BadCertificateUriInvalid` | ✅ 放行 |

> `basic` 与 `none` 的差别就在后两行：`basic` 仍守有效期与 URI，`none` 一律放行。
> 三档下**用户层不受影响**：`basic` / `none` 端口用错误密码仍被
> `ActivateSession -> BadUserAccessDenied` 拒绝。

---

## 7. 与 hda_509 的关系

本项目从 `hda_509` 抽取核心认证模块（`server_builder.py` / `user_manager.py` /
`user_token_tracking.py` / `config_loader.py` / `client/conn.py`）并独立成仓。

| | `hda_509` | `ua_auth_lab`（本项目） |
|---|---|---|
| 目标 | 48627 单一产品组合认证链路 | 端点组合 × 认证方式 全拆矩阵 |
| 隔离模型 | 单场景 | 一端口一场景（77 端口） |
| 匿名 | ❌ 关闭 | ✅ 核心/开放端口块各含匿名档 |
| 认证方式 | 仅 X.509 User | Anonymous + UserName + X.509 |
| 应用证书校验 | 固定严格（须受信） | trusted / basic / none 三档可配 |
| 安全策略 | 固定 Basic256Sha256+SignAndEncrypt | 全 6 种 policy × 有效 mode = 11 组合 |

> 本项目可独立运行、独立推送（Gitea `supcon/ua_auth_lab`）。

---

## 8. 已知限制

- `test_material/` 里的所有密钥**仅用于本地测试**，不是生产 PKI
- 绑定 `asyncua 2.0.x`（`server_builder.py` 有版本断言；`DiscoveryCleanServer`
  依赖其 `_setup_server_nodes` 行为）
- 77 端口 = 77 个 Python 进程（约 2 GB 内存），用 `matrix_ctl.py stop` 统一回收
- `client_cert_validation: none` 的端口**不校验客户端应用证书**，仅用于验证/演示，
  绝不可用于生产
