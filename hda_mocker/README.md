# HDA Mocker — OPC UA 历史数据模拟服务器

一个支持 **HDA（Historical Data Access）** 的 OPC UA 模拟服务器，用于开发/验证客户端。

- 可生成任意数量的变量位号（默认配置 1 万个 `M0000~M9999.VALUE`）
- 历史数据三种后端：**惰性秒级生成（7 天任意窗口可查）/ SQLite 落盘 / 内存**
- 支持原始历史、聚合历史（Average/Min/Max/Count/TimeAverage）
- 可选 X.509 客户端证书认证

## 1. 环境

```bash
python -m pip install asyncua pyyaml cryptography
```

## 2. 启动

```bash
python main.py <组态文件.yaml>
```

服务器启动即阻塞运行。三种代表性组态：

| 组态 | 端口 | 用途 |
|------|------|------|
| `hda_10k.yaml` | 48630 | **1 万位号 + 7 天秒级历史**（惰性生成，推荐客户端开发用） |
| `hda_full.yaml` | 48622 | HDA 全能力：SQLite 持久化 + 聚合 + 事件历史 |
| `config_x509.yaml` | 48620 | X.509 证书认证（Basic256Sha256_SignAndEncrypt） |
| `config_example.yaml` | 18950 | 基本示例（各类型节点，无历史） |

启动成功标志（hda_10k）：

```
开始构建节点树
构建完成
惰性历史已启用(确定性秒级生成, 支持 7 天窗口)
服务启动成功 opc.tcp://0.0.0.0:48630/ua_mocker/
节点数量: 10000
designed by yzc
```

> 端口注意：Windows 上 `18950` 等落在系统保留端口区间，若报"拒绝访问"请换端口（48620~48630 等安全）。

## 3. 数据情况

### 3.1 历史后端（组态 `history.storage`）

| 模式 | 说明 | 适用 |
|------|------|------|
| `lazy` | **惰性确定性生成**：按位号+时间戳实时算秒级值，零存储，任意窗口（含 7 天）可查 | 客户端开发/验证（推荐） |
| `sqlite` | 真实落盘持久化：订阅节点值变化写入 SQLite，重启保留 | 需要"真实积累/写入历史"的场景 |
| `memory` | 进程内存存储，重启即清空 | 轻量临时 |
| `none` | 仅标记 Historizing，无历史数据 | 只测节点树 |

### 3.2 惰性模式数据模型（hda_10k.yaml）

- **位号**：`ns=1;s=M0000.VALUE` ~ `ns=1;s=M9999.VALUE`，共 1 万个，Double 类型
- **采样**：1 点/秒
- **值**：确定性公式 `value_at(index, 时间戳)` = 多周期正弦叠加 + 位号相位（平滑、每位号不同、**同一位号同一时间永远返回同一值**，跨请求/重启可复现）
- **窗口**：任意时间范围可查；单次读取最大限幅 7 天
- **响应分页**：单次响应最多 1 万条，超出通过 ContinuationPoint 续传

### 3.3 实测数据（hda_10k.yaml，本机回环）

| 查询 | 返回 | 耗时 |
|------|------|------|
| 单个位号最近 10 分钟 | 601 条 | 毫秒级 |
| 单个位号最近 1 天 | 86,401 条 | ~2.5s |
| 单个位号最近 7 天（全量） | 604,801 条 | ~83s |
| 聚合 Average(60s, 2h) | 121 条 | 毫秒级 |

> 7 天全量 83s 是一次性拉 60 万条的极限场景；客户端按段查（15 分钟/段，900 条）为毫秒级，日常使用无感。

### 3.4 聚合历史

惰性/SQLite 后端均支持 `ReadProcessedDetails` 聚合：Average、Minimum、Maximum、Count、TimeAverage，按 `ProcessingInterval` 分桶计算。

## 4. 组态说明

```yaml
server: "0.0.0.0"        # 监听地址
port: 48630              # 端口
cycle: 1000              # change 节点更新周期(ms)
namespace_index: 1       # 命名空间索引(客户端看到的 ns)

history:                 # HDA 配置(可选)
  storage: lazy          # memory | sqlite | lazy | none
  db_file: data/history.db   # sqlite 时
  period: 7d             # 保留周期: 30m/24h/7d/1w
  count: 0               # 每节点记录上限, 0=不限
  aggregates: true       # 启用聚合历史
  events:                # 事件历史(可选)
    period_ms: 3000
    message: "HDA demo event"
    severity: 500

nodes:                   # 节点定义
  - name_format: "M{idx:04d}.VALUE"   # 用模板批量命名(name 与 name_format 二选一)
    idx_start: 0
    type: Double
    count: 10000
    change: false        # true=按 cycle 周期变化
    writable: false
    default: 0.0         # change=false 时必填
    history: true        # 参与历史
```

节点历史能力：`history: true` 的节点会置 `Historizing=True` + `AccessLevel=Read|HistoryRead`，客户端可读历史。

## 5. 客户端使用

自带验证客户端 `hda_client.py`（支持原始/聚合/事件历史、X.509）：

```bash
# 读原始历史(最近 10 分钟)
python hda_client.py --url opc.tcp://127.0.0.1:48630/ua_mocker/ --no-security --raw --node "ns=1;s=M0000.VALUE"

# 聚合历史
python hda_client.py --url opc.tcp://127.0.0.1:48630/ua_mocker/ --no-security --processed --node "ns=1;s=M0000.VALUE" --agg Average --interval 60

# 列出所有 historizing 节点
python hda_client.py --url opc.tcp://127.0.0.1:48630/ua_mocker/ --no-security --list-history
```

GUI 工具 `../hda_client/`（Wails + Go）可直接连本服务器做交互式查询与导出。

## 6. X.509 认证（可选，统一目录 ../hda_509/）

X.509 证书认证统一由 `../hda_509/` 管理；测试证书、生成工具与客户端示例在该目录内，本服务器通过组态启用：

```bash
python ../hda_509/test_material/gen_certs.py  # 生成测试 CA/服务端/客户端证书
python main.py config_x509.yaml   # 用 X.509 认证启动
```

给客户端：仅交付其专属客户端证书与私钥，以及服务端证书。测试材料位于 `../hda_509/test_material/certs/`；绝不外发 `server_key.pem`、`ca_key.pem`。详见 `../hda_509/client/CLIENT_README.md`。

## 7. 文件清单

| 文件 | 作用 |
|------|------|
| `main.py` | 入口 |
| `server_main.py` | 节点构建、X.509、HDA 配置、周期更新 |
| `config_loader.py` / `type_mapping.py` / `change_engines.py` | 组态加载 / 类型映射 / 值变化引擎 |
| `log_util.py` | 日志（`ua_mocker_YYYYMMDD.log`） |
| `hda_support.py` | HdaHistoryManager：聚合历史实现 |
| `lazy_history.py` | 惰性秒级历史存储（1 万位号数据源） |
| `hda_client.py` | 命令行 HDA 客户端（验证用） |
| `bench_*.py` | 并发/耗时基准脚本 |
| `config_x509.yaml` | X.509 认证启动配置（测试证书工具在 `../hda_509/test_material/`） |
| `data/` | SQLite 历史库（`history.storage: sqlite` 时） |

