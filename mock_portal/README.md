# Mock 门户（mock_portal）

只读的门户网站：列出 `presets/` 下的 Mock，展示连接地址、诊断状态、数据说明，并提供
HDA/DA Parquet、`config.yaml`、`summary.md` 和标准化工具包的下载。

设计与约束见 [MOCK_PORTAL_DESIGN.md](../hda_mocker_4/docs/MOCK_PORTAL_DESIGN.md)。门户与 `hda_mocker_4/` 同级，独立构建与部署。

## 边界

- 不修改 `hda_mocker_4` 的 Go 代码、`go.mod`、第三方代码或运行行为；不调用 CLI。
- 不启动、停止、重启或调度 Mock；不连接 DuckDB；不解析 Parquet / CSV / Excel。
- 不上传数据、不创建 preset、不在线编辑配置；所有接口都是 `GET`。
- 一份 preset 对应一个 Mock，数量按 preset 统计；连接地址只来自该 preset 的
  `config.yaml`，诊断地址只由同一份 config 推导，没有第二份登记表。

## 目录

```text
mock_portal/
  cmd/mock_portal/        # 程序入口（HTTP + 采集器）
  internal/catalog/       # 只读发现、config 解析、地址推导、白名单文件
  internal/diagnostics/   # /v1/diag 周期采集与 /v1/detail 短缓存
  internal/httpapi/       # 只读 API + 内嵌前端
  internal/bundle/        # 标准化工具包（打包时固定）
  internal/web/           # 前端构建产物（go:embed）
  frontend/               # React + TypeScript + shadcn/ui（Vite）
  scripts/build.ps1       # 前端 + exe 一键构建
  portal.yaml.example     # 门户配置示例
```

## 构建

需要 Node.js（构建前端）和 Go 1.25+。运行时只需要 exe，不需要 Node、Python、DuckDB
或额外数据库。

```powershell
# 一键构建（前端 + exe）
powershell -ExecutionPolicy Bypass -File mock_portal/scripts/build.ps1

# 或手动
cd mock_portal/frontend; npm ci; npm run build
cd mock_portal; go build -trimpath -o mock_portal.exe ./cmd/mock_portal
```

`internal/web/dist` 是 `go:embed` 的输入，仓库中保留了一份可直接编译的构建产物；
改前端后需要重新执行 `npm run build`（或在开发时用 `npm run dev`，Vite 会把 `/api`
代理到 `127.0.0.1:8080`）。

## 运行

```powershell
Copy-Item mock_portal/portal.yaml.example mock_portal/portal.yaml
# 按部署环境修改 listen 和 presets_dir
mock_portal/mock_portal.exe -config mock_portal/portal.yaml
```

门户监听 `listen`，默认 `0.0.0.0:8080`。属于可信内网，远程访问边界由部署层控制，
门户自身不提供账号或权限系统。

## 配置

```yaml
listen: 0.0.0.0:8080
presets_dir: G:/cc_demos/hda/hda_mocker_4/presets
# 可选，默认排除 demo 空模板
# exclude_presets: [demo]
```

## API

| API | 返回 |
|---|---|
| `GET /api/mocks` | preset 清单、config 连接摘要、缓存诊断与错误；`?refresh=1` 强制重扫目录 |
| `GET /api/mocks/{id}` | 连接摘要、`config.yaml` 文本、缓存的 `/v1/diag`、按需获取的 `/v1/detail` |
| `GET /api/mocks/{id}/summary` | `summary.md` 正文与修改时间 |
| `GET /api/mocks/{id}/files` | 允许下载的文件（ID、名称、类别、字节大小） |
| `GET /api/mocks/{id}/files/{fileId}` | 单文件流式下载，支持 `Range` 断点续传 |
| `GET /api/standardization` | 标准化指南与工具包信息 |
| `GET /api/standardization/bundle` | 标准化工具包 zip |
| `GET /api/health` | 存活检查 |

文件 ID 由服务端列举结果生成；不支持任意磁盘路径、目录穿越或逃逸 preset 根目录的
符号链接。只发布 `hda/`、`da/` 第一层的 `.parquet` 以及 `config.yaml`、`summary.md`。

## 行为约定

- 采集：默认每 10 秒拉取一次 `/v1/diag`，单次超时 3 秒，限制总并发且同一 preset 不重叠。
  `/v1/detail` 由详情页按需触发，后端 10 秒缓存并合并并发请求。
- 状态：连续 3 次失败显示不可达；超过 30 秒没有成功快照标记过期；首次采集显示待检查；
  源端 `status`（ok/warn/error）原样保留，与门户采集状态分开显示。
- 地址：`server.endpoint` 是 UA 连接地址的唯一来源；`diagnostic.listen` 是 host:port，
  通配地址时仅用同一 config 中具体的 UA 主机推导，两边都是通配则显示配置待完善。
  `0.0.0.0` 样例地址会明确提示“监听占位地址”，并禁用“复制为可连接地址”。
- 地址变化会作废旧采集结果，旧 IP 的健康状态不会套到新地址上。
- 未知字段容忍并可在“原始诊断字段”折叠区查看；未知、空值与 0 区分显示；时间默认 UTC
  并明确标注。

## 部署待办（由部署人员完成，无需改 CLI）

- 各 preset 的 `server.endpoint` 仍是 `0.0.0.0` 样例：需要填写运行主机的真实 IP。
- `hda_all`、`dynamic_data_all`、`qa_v2` 的 UA 样例端口都是 18980；如需同时运行，请分配不冲突端口。
- `hda_all`、`dynamic_data_all`、`demo` 的诊断样例都是 `0.0.0.0:4841`，BASF 的 UA 占用 4841。
- `basf_dirty`、`ningbo_916` 未配置 `diagnostic.listen`，门户会显示“未监控”而不是离线或正常。
- 未提供真实生产 IP 前，门户不会猜测或写入任何地址。

## 测试

```powershell
cd mock_portal
go test ./...
```

覆盖 preset 发现与排除、config 解析与地址推导、目录穿越防护、诊断缓存与失败/过期状态、
`/v1/detail` 缓存合并、下载字节一致与 `Range`、只读方法限制、工具包与仓库文件同步。

## 本地验收夹具

`testdata/presets/` 提供四份夹具 preset，`scripts/fake_diag` 提供假诊断源，用于在没有运行
Mock 时验证“成功 / 告警 / 未监控 / 不可达”四种状态。它们只是测试夹具，不要复制到生产
`presets/`，也不要据此填写真实 IP 或端口。用法见
[testdata/presets/README.md](testdata/presets/README.md)。
