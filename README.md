# UA Auth Lab — OPC UA 认证方式验证台

> 专门用于验证 **OPC UA 全部用户认证方式**（Anonymous / UserName / X.509 User）
> 以及它们与安全通道（SecurityPolicy / MessageSecurityMode）的组合行为。
>
> 与 `hda_509`（聚焦 48627 单一产品组合认证链路）不同，本项目的目标是
> **把认证方式做成可配置、可自动验证的矩阵**。

---

## 1. 支持的认证方式

| 用户认证方式 | UserIdentityToken | 说明 |
|---|---|---|
| **Anonymous** | `AnonymousIdentityToken` | 匿名身份（`UserRole.Anonymous`，mock 授权规则允许读测试节点） |
| **UserName** | `UserNameIdentityToken` | 测试账号 `test / test` |
| **X.509 User** | `X509IdentityToken` | direct 模式：精确 DER 白名单 + 有效期检查 |

三种方式可**单独开放**，也可**同时开放**（主场景 `all_auth`）。

> 概念区分：**Application Certificate**（SecureChannel/CreateSession，应用身份）
> 与 **User Certificate**（ActivateSession，用户身份）是两条独立链路，绝不能混用。

---

## 2. 场景与端口

| 端口 | 场景 | 配置 | 开放方式 |
|---|---|---|---|
| **48630** | `all_auth` | `configs/all_auth.yaml` | **Anonymous + UserName + X.509（全开）** |
| 48631 | `anon_only` | `configs/anon_only.yaml` | 仅 Anonymous |
| 48632 | `username_only` | `configs/username_only.yaml` | 仅 UserName |
| 48633 | `x509_only` | `configs/x509_only.yaml` | 仅 X.509 User |

主场景 `all_auth` 发布 3 种 SecurityPolicy × Sign/SignAndEncrypt = **6 个安全端点**，
外加一个 None/None 的 **discovery-only** 端点（仅 `GetEndpoints` / `FindServers`，
不暴露 None/None Session endpoint）。

---

## 3. 目录结构

```text
ua_auth_lab/
  main.py                       入口: python main.py <config.yaml>
  server_main.py                节点构建与更新
  server_builder.py             公共 Server 构建（安全端点/证书/信任/用户认证/权限）
  user_manager.py               组合 UserManager（Anonymous/UserName/X.509 direct）
  user_token_tracking.py        asyncua 令牌类型跟踪（区分 anon/username/x509）
  config_loader.py              YAML 组态加载与路径解析
  log_util.py / type_mapping.py / change_engines.py
  configs/
    all_auth.yaml               ★ 三认证全开（48630）
    anon_only.yaml              仅匿名（48631）
    username_only.yaml          仅用户名（48632）
    x509_only.yaml              仅证书（48633）
  client/
    conn.py                     共享分阶段连接库（--auth anon/username/x509）
    auth_matrix_client.py       ★ 认证矩阵客户端（auth × policy × mode）
    reference_client.py         单次参考客户端
    discovery_probe.py / probe.py / endpoint_dump.py
  tests/
    _auth_common.py             测试公共模块
    auth_matrix_tests.py        ★ 正向矩阵（18 组合 + 端点断言）
    auth_negative_tests.py      ★ 负向（11 用例）
    auth_concurrent_tests.py    ★ 并发隔离（25 会话）
  test_material/
    gen_certs.py                CA PKI 生成（服务端/客户端/用户证书）
    certs/                      CA 签发证书（test-only）
  reports/                      测试报告输出
  lib/                          离线 wheel（Python 3.11）
  install_offline.bat           Windows 离线安装依赖
```

---

## 4. 快速开始

### 4.1 准备依赖

```bash
# 联网
python -m pip install asyncua==2.0.1 cryptography pyyaml

# 或 Windows 离线（使用 lib/ 里的 wheel）
install_offline.bat
```

要求 `Python >= 3.11`。

### 4.2 生成测试 PKI

```bash
python test_material/gen_certs.py
```

生成 `test_material/certs/`（CA + 服务端 + 客户端 + 用户证书）。局域网测试可加
`--server-dns` / `--server-ip`。

### 4.3 启动服务器

```bash
python main.py configs/all_auth.yaml          # 48630 三认证全开
python main.py configs/anon_only.yaml         # 48631
python main.py configs/username_only.yaml     # 48632
python main.py configs/x509_only.yaml         # 48633
```

不同端口可同时运行。

### 4.4 认证矩阵客户端

```bash
python client/auth_matrix_client.py
# auth × policy × mode = 3 × 3 × 2 = 18 组合

python client/auth_matrix_client.py --auth anon
python client/auth_matrix_client.py --policy Basic256Sha256 --mode SignAndEncrypt
# 手动验证 None/None（无保护通道，默认不在矩阵内）：
python client/auth_matrix_client.py --policy None --mode None --auth anon
```

### 4.5 测试套件

```bash
python tests/auth_matrix_tests.py        # 正向矩阵 + 端点/token 断言
python tests/auth_negative_tests.py      # 负向 11 用例
python tests/auth_concurrent_tests.py    # 并发隔离（25 会话）
```

---

## 5. 认证矩阵（主场景 all_auth）

用户认证 × 安全通道，全部应成功：

| 用户认证 | Basic256Sha256 Sign | Basic256Sha256 SignAndEncrypt | Aes128… | Aes256… |
|---|:---:|:---:|:---:|:---:|
| Anonymous | ✅ | ✅ | ✅ | ✅ |
| UserName | ✅ | ✅ | ✅ | ✅ |
| X.509 User | ✅ | ✅ | ✅ | ✅ |

（每种策略的 Sign / SignAndEncrypt 均已覆盖，共 18 组合。）

---

## 6. 测试覆盖

### 正向（`auth_matrix_tests.py`）
- 18 组合（3 认证 × 3 策略 × 2 模式）全部成功
- `GetEndpoints` 只返回 6 个 secure endpoint，**不暴露 None/None Session endpoint**
- 每个 secure endpoint 发布 Anonymous + UserName + Certificate 三种 token
- 3 种 SecurityPolicy 全部发布

### 负向（`auth_negative_tests.py`）

| 用例 | 失败阶段 / StatusCode |
|---|---|
| 错误用户名 / 错误密码 | ActivateSession `BadUserAccessDenied` |
| 未注册 User 证书 | ActivateSession `BadUserAccessDenied` |
| 过期 User 证书（已注册） | ActivateSession `BadUserAccessDenied` |
| 错误 User 私钥 | ActivateSession `BadIdentityTokenInvalid` |
| App 证书冒充 User 证书 | ActivateSession `BadUserAccessDenied`（身份隔离） |
| 未受信 App 证书 | CreateSession `BadCertificateUntrusted` |
| 过期 App 证书 | CreateSession `BadCertificateTimeInvalid` |
| App 证书 URI 不匹配 | CreateSession `BadCertificateUriInvalid` |
| App 证书/私钥不匹配 | OpenSecureChannel 阶段失败 |
| None/None Session | 无法建立（discovery-only，无 None Session endpoint） |

### 并发（`auth_concurrent_tests.py`）
- 有效身份（anon/username/x509）并发全部成功
- 无效身份（错误密码/未注册证书）并发全部拒绝，**不降级为 Anonymous**

---

## 7. 设计要点

- 场景差异全在 YAML 配置里，代码只有一份 `server_builder.py` 构建入口
- 三种认证方式的开放/关闭由 `user_auth.anonymous / username / x509` 三个开关表达
- X.509 User 校验 = direct（精确 DER 白名单 + 有效期），不是完整用户 PKI
- Discovery 可无保护，Session 必须走安全端点（`require_secured_session: true`）
- 身份认证（Anonymous 仍是 `UserRole.Anonymous`）与授权角色分开处理

## 8. 已知限制

- `test_material/` 里的所有密钥**仅用于本地测试**，不是生产 PKI
- 绑定 `asyncua 2.0.x`（`server_builder.py` 有版本断言；`DiscoveryCleanServer`
  依赖其 `_setup_server_nodes` 行为）
- 客户端证书/私钥不匹配时，asyncua 会抛出内部错误/超时而非干净的 StatusCode
  （连接仍被拒绝，见负向 Case 10）

---

## 9. 与 hda_509 的关系

本项目从 `hda_509` 抽取核心认证模块（`server_builder.py` / `user_manager.py` /
`user_token_tracking.py` / `config_loader.py` / `client/conn.py`）并独立成仓。

| | `hda_509` | `ua_auth_lab`（本项目） |
|---|---|---|
| 目标 | 48627 单一产品组合认证链路 | 全部认证方式矩阵化验证 |
| 匿名 | ❌ 关闭 | ✅ 全开（主场景） |
| 认证方式 | 仅 X.509 User | Anonymous + UserName + X.509 |
| 安全策略 | 固定 Basic256Sha256+SignAndEncrypt | 3 策略 × Sign/SignAndEncrypt |

> 本项目可独立运行、独立推送（Gitea `supcon/ua_auth_lab`）。
