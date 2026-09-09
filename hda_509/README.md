# ua_hda — X.509 认证 OPC UA Mock Server

一个支持 **X.509 客户端证书认证**的 OPC UA 模拟服务器。按组态生成节点树（默认 1052 个节点），要求客户端使用受信证书 + `Basic256Sha256_SignAndEncrypt` 安全策略连接。

本仓库供对方 agent 使用，只需照做以下四步：装环境 → 生成证书 → 启动服务 → 给客户端发证书。

## 1. 环境

- Python 3.11.x
- 依赖：`asyncua`、`pyyaml`、`cryptography`

```bash
python -m pip install asyncua pyyaml cryptography
```

## 2. 首次启动前：生成证书

```bash
python gen_certs.py
```

在 `certs/` 下生成：CA（`ca_cert.pem`/`ca_key.pem`）、服务端证书（`server_cert.pem`/`server_key.pem`）、客户端证书（`client_cert.pem`/`client_key.pem`），并把 CA 放入服务端受信目录 `certs/trust/`。

## 3. 启动服务

```bash
python main.py config_x509.yaml
```

启动成功的控制台输出：

```
开始构建节点树
构建完成
可写节点 setter 已设置(26 个,强制使用服务器时间戳)
服务启动成功 opc.tcp://0.0.0.0:48620/ua_mocker/
节点数量: 1052
designed by yzc
```

- 端点：`opc.tcp://<服务器IP>:48620/ua_mocker/`
- 端口 **48620**（避开 Windows 保留端口区间，勿改回 18950）
- 组态文件 `config_x509.yaml` 中的关键项：
  - `security_policy`：`Basic256Sha256_SignAndEncrypt`（本服务器唯一允许的策略，不提供无加密端点）
  - `cert` / `private_key`：服务端证书与私钥
  - `trust_store`：`certs/trust`，里面放谁签发的证书，谁就能连

## 4. 给客户端发证书

给生产 UA 客户端三个文件（第 4 步生成）与连接参数：

| 文件 | 用途 |
|------|------|
| `certs/client_cert.pem` | 客户端身份证书（登录凭证） |
| `certs/client_key.pem` | 客户端私钥（配合同一证书，只给该客户端） |
| `certs/server_cert.pem` | 服务端证书（客户端据此信任本服务器） |

客户端侧连接参数：

- 安全策略：`Basic256Sha256_SignAndEncrypt`
- 加载：客户端证书 + 私钥 + 服务端证书

**绝不外发**：`server_key.pem`、`ca_key.pem`（私钥只属于所有者）。

若客户端不在本机，把 `client_demo.py` 里的 URL 由 `127.0.0.1` 改为服务器 IP 后即可连通验证。

## 验证（可选）

本仓库自带一个模拟"真实第三方客户端"的接入示例（自己生成密钥 + CSR → 由本仓库 CA 签发 `client2` 证书 → 连接并读写）：

```bash
python gen_client_cert.py   # 生成 client2_cert.pem / client2_key.pem（CA 签发）
python client_demo.py       # 用 client2 证书连接、读写
```

通过时输出：

```
连接成功: opc.tcp://127.0.0.1:48620/ua_mocker/
  读 ns=1;s=int32_ch_1 = 43
  读 ns=1;s=float_ch_1 = 43.0
  读 ns=1;s=string_ch_2 = 't'
  写 double_wr_1 = 123.45
  回读 double_wr_1 = 123.45
已断开，测试通过
```

## 文件清单

| 文件 | 作用 |
|------|------|
| `main.py` | 入口，`python main.py <组态文件>` |
| `server_main.py` | 建节点树、X.509 安全配置、周期更新、写值回调 |
| `config_loader.py` | 组态 YAML 加载与校验 |
| `type_mapping.py` / `change_engines.py` | 类型映射 / 节点值变化引擎 |
| `log_util.py` | 日志输出（`ua_mocker_YYYYMMDD.log`） |
| `config_x509.yaml` | 本次启动使用的组态（含安全配置） |
| `gen_certs.py` | 生成 CA / 服务端 / 客户端证书 |
| `gen_client_cert.py` | 模拟新客户端：生成密钥 + CSR → CA 签发 |
| `client_demo.py` / `test_client.py` | 正向连接验证 |
| `test_negative.py` | 负向验证（无证书/未受信证书被拒） |
| `certs/` | 证书目录（含受信目录 `certs/trust/`） |
