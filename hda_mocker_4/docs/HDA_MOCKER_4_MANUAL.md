# HDA Mocker 4 CLI 手册

HDA Mocker 4 v1.4.2 是纯命令行程序。启动时必须通过 `--config` 指定配置文件；进程在当前控制台显示启动阶段、运行状态和错误，同时把应用日志写入 exe 同级的 `logs/hda_mocker_4.log`。

## 启动

```powershell
.\hda_mocker_4.exe --version
.\hda_mocker_4.exe --config D:\preset\case-a\config.yaml
```

配置文件所在目录是预置集根目录，需包含 `hda/`、`da/`；数据目录可为空。`runtime/` 和 DuckDB 历史库会在启动时创建。

启动输出会显示配置校验、HDA 预存进度、UA 服务启动和实时轮播状态。启动失败时会在控制台打印错误，并返回非零退出码。运行中按 Ctrl+C 可等待当前事务结束并正常关闭服务。

不传参数时程序只打印用法并退出。

## 日志

```text
logs/
├── hda_mocker_4.log
└── hda_mocker_4_crash.log
```

常规日志同时输出到当前控制台与文件。单个日志文件达到 20 MB 后滚动，最多保留 5 份旧文件。CLI 启动失败的具体阶段和错误也会写入日志。Go 运行时崩溃堆栈保留在控制台，并另外写入 `hda_mocker_4_crash.log`。

## 独立诊断端口

可在配置中启用独立 HTTP 诊断端口，它与 OPC UA 业务端口分开；同机多实例需使用不同诊断端口。

```yaml
diagnostic:
  listen: 0.0.0.0:4841
```

- `GET /v1/diag`：返回扁平诊断状态。
- `GET /v1/detail`：返回包含 `info` 与 `diag` 的详细信息。

诊断接口只读，不返回证书或私钥。生产防火墙应只向管理端开放该端口。

## 运行时说明

- Go CLI 统一负责配置校验、HDA 预存、OPC UA 服务、DA 轮播和 HDA 实时入库。
- 日志用于观察和排错，不参与轮播调度。
- Windows 命令行版不依赖 WebView2。
