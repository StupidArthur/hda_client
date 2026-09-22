# UA Auth Lab — OPC UA 认证方式验证台（全拆 · 方案 A）

> 专门用于验证 **OPC UA 全部用户认证方式**（Anonymous / UserName / X.509 User）
> 与**全部安全通道组合**（SecurityPolicy × MessageSecurityMode）的行为。
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

### 1.3 为什么是"正交"而不是"并行"

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
11 个端点组合 × 3 种认证方式 = 33 个端口
端口 48730 .. 48762（可在 configs/matrix.yaml 里改）
每端口负向抽样 2 条 -> 预计 99 次连接
```

每个端口的断言（`tests/matrix_port_tests.py`）：

| # | 断言 | 期望 |
|---|---|---|
| 1 | 正向 | 本端口 (policy, mode, auth) 组合成功读到节点 |
| 2 | 端点隔离 | `GetEndpoints` **只暴露这 1 个端点** |
| 3 | Token 隔离 | 该端点**只发布这 1 种** UserIdentityToken |
| 4 | 负向① | 同端点换认证方式 → 拒绝于 `ActivateSession` |
| 5 | 负向② | 换端点组合同认证方式 → 拒绝于 `Endpoint selection` |

共 **165 个断言/轮**。

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
    matrix.yaml                 ★ 矩阵唯一真源（端口起点/端点组合/认证方式）
    matrix/                     ★ 自动生成的 33 个端口配置 + manifest.json

  tools/
    gen_matrix_configs.py       ★ 生成器：matrix.yaml -> 33 配置 + manifest
    matrix_ctl.py               ★ 启停控制：start / status / stop

  client/
    conn.py                     共享分阶段连接库（6 阶段，--auth anon/username/x509）
    auth_matrix_client.py       ★ manifest 驱动矩阵客户端（正向 + 负向抽样）
    reference_client.py / discovery_probe.py / endpoint_dump.py / probe.py

  tests/
    _auth_common.py             测试公共模块（含 manifest 选端口 pick_port）
    matrix_port_tests.py        ★ 全拆矩阵测试（165 断言/轮）
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

# 3. 生成 33 个端口配置
python tools/gen_matrix_configs.py

# 4. 启动全部端口
python tools/matrix_ctl.py start        # status / stop 同理

# 5. 验证
python tests/matrix_port_tests.py       # 165 断言
python tests/auth_negative_tests.py     # 11 负向
python tests/auth_concurrent_tests.py   # 并发隔离
python client/auth_matrix_client.py --negative   # 交互式矩阵客户端
```

改矩阵（端口起点、增删策略、增删认证方式）只需编辑 `configs/matrix.yaml`
后重跑第 3 步。

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

## 6. 测试结果

最新完整报告：`reports/full_test_result_20260922.txt`

| 测试 | 结果 |
|---|---|
| `matrix_port_tests.py`（165 断言：33 正向 + 33 端点隔离 + 33 Token 隔离 + 66 负向） | **165/165 PASS** |
| `auth_negative_tests.py`（11 用例：凭证/证书材料类） | **11/11 PASS** |
| `auth_concurrent_tests.py`（9 组：三端口 × 有效/无效并发） | **9/9 PASS** |
| `auth_matrix_client.py --negative` | **33/33 正向 + 66/66 负向 PASS** |

---

## 7. 与 hda_509 的关系

本项目从 `hda_509` 抽取核心认证模块（`server_builder.py` / `user_manager.py` /
`user_token_tracking.py` / `config_loader.py` / `client/conn.py`）并独立成仓。

| | `hda_509` | `ua_auth_lab`（本项目） |
|---|---|---|
| 目标 | 48627 单一产品组合认证链路 | 端点组合 × 认证方式 全拆矩阵 |
| 隔离模型 | 单场景 | 一端口一场景（33 端口） |
| 匿名 | ❌ 关闭 | ✅ 11 个端口各含匿名档 |
| 认证方式 | 仅 X.509 User | Anonymous + UserName + X.509 |
| 安全策略 | 固定 Basic256Sha256+SignAndEncrypt | 全 6 种 policy × 有效 mode = 11 组合 |

> 本项目可独立运行、独立推送（Gitea `supcon/ua_auth_lab`）。

---

## 8. 已知限制

- `test_material/` 里的所有密钥**仅用于本地测试**，不是生产 PKI
- 绑定 `asyncua 2.0.x`（`server_builder.py` 有版本断言；`DiscoveryCleanServer`
  依赖其 `_setup_server_nodes` 行为）
- 33 端口 = 33 个 Python 进程，注意内存占用；用 `matrix_ctl.py stop` 统一回收
