# HDA Mocker 4 v1.4.1 / v1.4.2

两个版本均为 Windows 纯 CLI，沿用 v1.4.0 的配置、Parquet 和 DuckDB 结构。升级时先停止旧进程，备份整个预置集的 `runtime/` 目录，然后替换 exe；不需要删库或重新预存 HDA。

| 版本 | 改动 |
| --- | --- |
| v1.4.1 | 修复订阅 ID 复用、监控项增删锁顺序和异步删除竞态；显式关闭会话时清理关联订阅，异常断线后允许在会话超时前重连。 |
| v1.4.2 | 继承 v1.4.1；慢客户端通知按 ClientHandle 保留最新待发送值，发送响应设置超时；诊断增加订阅、监控项和通知积压指标。 |

v1.4.2 的 `/v1/diag` 和 `/v1/detail` 中 `diag` 新增：

- `opcua.subscription_count`：当前订阅数。
- `opcua.monitored_item_count`：当前监控项数。
- `opcua.notification_pending`：当前等待 Publish 的最新通知数。
- `opcua.notification_coalesced_total`：进程启动以来，待发送的旧值被同一 ClientHandle 新值取代的次数。
- `opcua.notification_coalesced_last_time`：最近一次合并的 UTC 时间；最近一分钟内发生合并时诊断状态为 `warn`。

通知合并遵循当前服务端修订的队列大小 1：慢客户端可能错过中间变化，但能收到待发送的最新值。DA 轮播和 HDA 入库的提交逻辑未改。异常断线时，绑定在断开连接上的 UA Session 会保留到协商的会话超时时间；期间成功重新绑定到新连接则取消过期清理，未重连的 Session 与订阅在超时后清理。
