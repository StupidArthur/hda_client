# HDA Mocker 4 v1.4.3

本版只补充 OPC UA `CreateMonitoredItems` 的诊断日志，不改监控项创建与返回逻辑。沿用 v1.4.2 的配置、Parquet 和 DuckDB 数据库；升级时停止旧进程并替换 exe，无须删库或重新预存 HDA。

每次创建监控项时，控制台和 exe 同级 `logs/hda_mocker_4.log` 会记录客户端地址、Session ID、请求和订阅 ID、监控项总数、前 5 个 NodeId，以及首项的采样间隔、队列大小和监控模式。成功时记录创建数量；失败时记录处理阶段、项目序号及原始错误。若处理过程 panic，先记录堆栈再保持原有崩溃行为。不会记录证书私钥或认证令牌。

排查客户端的 `Bad_UnexpectedError` 时，按客户端报错时间搜索 `CreateMonitoredItems`，再用 `request_id` 对齐请求与失败日志。若只看到请求而没有完成或失败记录，同时检查 `logs/hda_mocker_4_crash.log`。日志中 NodeId 只展示前 5 个，不能代表整批节点都正确。
