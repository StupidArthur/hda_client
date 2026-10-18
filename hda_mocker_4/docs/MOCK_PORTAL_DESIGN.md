# Mock 门户：设计与开发交接文档

状态：第一版需求及实现约束，2026-10-18。供下一位实现 agent 直接使用。现有页面为审核原型，正式网站尚未实现；不把静态HTML原型当成已实现的shadcn应用。

## 1. 目标和不可变边界

实现一个轻量、独立、只读的网站，帮助使用者查看有哪些Mock、如何连接、当前诊断、数据说明，并下载HDA/DA Parquet及标准化工具。

必须遵守：

1. 不修改现有 hda_mocker_4 CLI 的Go代码、go.mod、gopcua第三方代码或运行行为，也不要求新增诊断接口。
2. 不启动、停止、重启或调度Mock；不连接或读取正在运行的DuckDB；不在线解析Parquet、CSV或Excel。
3. 不上传数据、不创建preset/实例、不在线编辑配置；这些属于下一版。
4. 一份preset对应一个Mock，最多启动一个实例。所有数量按preset统计，不再区分“服务数量”和“数据集数量”。
5. 详情只有三个页签：**诊断 / 说明 / 下载**，默认诊断。连接地址显示在列表和详情顶部，不单设“连接”页。
6. 使用 shadcn/ui 通用组件，macOS系统工具视觉；不要换成Radix Themes或重新手写全部控件。
7. 不引入第二份实例登记表，不重复维护IP、诊断地址、Namespace或播放周期。

本任务是门户开发，现有preset数据迁移属于另一个已完成但尚有未提交文件的工作。实现agent必须保留既有修改，不能覆盖或顺手提交无关改动。

## 2. 唯一信息来源

| 信息 | 唯一来源 | 处理方式 |
|---|---|---|
| Mock身份、总数 | presets第一层目录 | 目录名为稳定ID |
| UA连接地址 | 本preset/config.yaml的server.endpoint | 直接解析和展示 |
| Namespace及索引 | server.namespace / server.ns | 来自同一config |
| 诊断监听 | diagnostic.listen | 从同一config推导HTTP访问地址 |
| 播放文件和周期 | playback.files | 当前配置始终优先 |
| 运行诊断 | 现有/v1/diag、/v1/detail | 保留源字段、消息和采集时间 |
| 数据/位号/时间区间/特征说明 | 本preset/summary.md | 渲染Markdown正文，不抽取结构化业务字段 |
| 下载文件 | 本preset的hda/、da/、config.yaml、summary.md | 列举文件元信息，不读取Parquet内容 |
| 标准化工具与规范 | 现有skills/hda-data-import及引用文档 | 发布固定工具包 |

summary.md内的配置示例是编写时快照；页面连接地址和诊断地址只能来自当前config。不要从Markdown里的示例反向解析IP或播放周期，也不要为了列表再新增metadata.yaml或数据清单数据库。

门户自身只需监听配置和preset根目录，例如：

```yaml
listen: 0.0.0.0:8080
presets_dir: G:/cc_demos/hda/hda_mocker_4/presets
```

这份门户配置不包含逐preset地址或实例映射。未提供真实生产IP，开发agent不得将本机网卡地址或示例IP擅自写入现有preset。

## 3. preset发现和数量

默认扫描presets下第一层子目录：具有config.yaml的目录作为一个条目，明确排除demo空模板、隐藏目录。即使配置解析失败或缺少数据文件，已发现条目也保留并标明问题，不能静默消失。无config的普通资料目录不当作preset。

当前应有5个可用Mock：

| ID | 用途 | 说明文件 |
|---|---|---|
| hda_all | 76位号、30秒工业工艺历史 | presets/hda_all/summary.md |
| dynamic_data_all | 27位号、60秒工业动态历史 | presets/dynamic_data_all/summary.md |
| ningbo_916 | 22位号水处理数据，5秒轮播 | presets/ningbo_916/summary.md |
| basf_dirty | 25位号稀疏不等间隔观测 | presets/basf_dirty/summary.md |
| qa_v2 | 12位号、仅HDA的构造验收夹具 | presets/qa_v2/summary.md |

demo/summary.md是新数据说明模板，不计数。运行中、告警、不可达、未监控都是同一批条目的状态分布，不生成另一个服务总数。只有根目录读取成功才更新发现清单；读取失败显示错误和旧清单过期状态，不谎报“0个Mock”。

## 4. 页面与视觉

### 4.1 主导航和列表

左侧只有“Mock 列表”和“标准化指南”。

首页列表：preset目录名、可复制UA地址、诊断状态、最近成功采集时间、“查看”。名称先使用目录名即可，不为中文别名再造一份登记表。若展示程序版本，使用诊断detail返回值，尚未采到显示未知。位号数等可选摘要只能来自已有诊断字段，缺失不猜、不扫描Parquet；完整数据特征在说明页看。

列表数量从发现结果生成，不把README/summary数量、Parquet数量或诊断正常数量当作Mock总数。刷新按钮只刷新网站缓存和文件读取，不操作CLI。

### 4.2 详情：固定三个页签

顶部始终显示preset名、诊断状态、从config读取的UA地址和复制按钮。必要时显示Namespace，或置于配置查看区域，不能增加第四个连接页签。

- **诊断**：展示源status、message、runtime.phase、采集时间、版本、位号、历史窗口、连接/会话/订阅/监控项、最近DA/HDA处理和通知合并情况。只展示已提供字段，未知不填0。可在底部用只读折叠区域展示config.yaml；这是配置内容，不是在线编辑器。
- **说明**：原样渲染summary.md，支持标题、GFM表格、列表、代码块、站内安全相对链接。不要强制变成一批统计卡片或按标题拆页。缺失时显示“缺少summary.md”，不回退读别的数据源。
- **下载**：列出hda/与da/第一层的.parquet文件，以及config.yaml和summary.md；显示文件名、类别、大小和下载按钮。无DA显示“仅HDA，无DA文件”。整包zip不是第一版必需项，优先保证单文件流式下载。

### 4.3 标准化指南

解释如何下载skill、告诉Agent源时区、HDA/DA用途、空值含义和质量码制式，并通过现有人工渠道交付。工具包包含SKILL.md实际引用的规范与示例，不能只放一个失效链接的SKILL.md。不出现可操作的上传、创建或启停按钮。

### 4.4 视觉规范

macOS系统设置/Finder列表的克制风格：浅灰侧栏、系统字体、蓝色选中态、细分隔线、小圆角、轻阴影；深色模式可跟随系统。地址、NodeId与配置用等宽字体。状态同时有文字和颜色，不用大屏、重图表、大面积渐变或假的红黄绿窗口按钮。

使用shadcn/ui的Sidebar、Table、Tabs、Button、Badge、Tooltip、Collapsible、Alert、Skeleton等，按需引入。Markdown用成熟React渲染组件及GFM插件，禁用不受信任的原始HTML。桌面列表在小屏重排，不允许页面整体横向溢出。

## 5. 地址规则与当前部署待办

### 5.1 UA地址

server.endpoint是唯一来源。生产preset应填写属于运行主机、客户端可访问的真实IP。不要另外维护advertise_ip、connection_url、服务登记表，或根据浏览器访问网站的主机名替换IP。

当前仓库各preset仍为0.0.0.0样例：网站必须标“监听占位地址，请完善config”，禁用“复制为可连接地址”的操作，但仍允许看说明及下载。配置缺失或格式错误也应明确显示，不能自行替换为127.0.0.1。

### 5.2 诊断地址

diagnostic.listen是host:port，不是完整HTTP URL。

- 具体IP/主机名：使用该host和port组成http://host:port。
- 0.0.0.0或[::]：仅当同一config的server.endpoint有具体有效host时，用该host与诊断port组合。这是从同一文件推导，不保存第二份地址。
- 两边都是通配地址：显示配置待完善，不自动探测某个猜测地址。
- diagnostic.listen未配置/为空：显示未监控，不尝试UA端口或预设诊断端口。
- 环回地址按仅本机处理。第一版默认门户与CLI部署在同机或有可访问诊断地址；跨主机共享配置但只监听远端环回时应标无法访问，不能误连到门户本机进程。
- 使用标准URL和net.JoinHostPort处理IPv6；拒绝异常scheme、缺失/零端口，不跟随HTTP重定向去任意新目标。

不要向用户展示诊断HTTP地址作为opc.tcp连接地址。

### 5.3 当前事实（不自动改配置）

- hda_all、dynamic_data_all、qa_v2的UA样例端口都是18980；需要一起运行时由管理员在各自config中分配不冲突的端口。
- hda_all与dynamic_data_all的诊断样例均为0.0.0.0:4841，demo也有同样样例；BASF UA占4841。不要假定所有preset可以原样同机并行。
- BASF和宁波当前未配置diagnostic.listen；网站应显示未监控，而非离线或正常。
- qa_v2诊断配置为0.0.0.0:18981；它没有DA，不能因无DA推进而误报冻结。
- 实施前由部署人员在原config中完成IP、端口和诊断开关安排；不需要修改CLI代码。

## 6. 诊断采集与兼容

复用现有协议语义，但门户为独立模块，不直接耦合Wails或CLI main包。

- 默认每10秒后端采集一次/v1/diag，单次超时3秒；限制总并发、同preset不重叠。
- /v1/detail由详情页按需获取，后端短缓存（例如10秒）并合并并发请求，避免每个访客或每次列表刷新都执行重查询。
- 浏览器只请求门户API，共享同一缓存，不能自己探测Mock。
- config和summary按修改时间更新；读取失败保留明确错误。文件路径更改或诊断地址变更时废弃对应旧采集结果，不能让旧IP的健康状态套在新地址上。
- 页面同时展示最近尝试与最后成功时间；连续3次失败显示不可达，超过30秒无成功快照标过期。首次采集显示待检查。
- 采集失败不代表进程已停；只有管理员或明确源信息才能显示已停止。只读门户默认没有进程状态来源。
- 源端status=ok/warn/error原样保留；门户采集状态与源端运行状态分开。未知字段容忍并可在折叠区域显示，旧版本缺少字段显示未知。
- 配置地址与源business.endpoint矛盾时显示不一致提示，不覆盖原config；页面的连接来源仍为config。
- 稀疏数据短时间无更新、仅HDA无DA、通知合并不能直接判定历史丢点。
- 现有仅HDA实例可能返回“等待首轮DA”的warn：保留原消息并说明不适用，不修改CLI、不伪造ok。
- 当前接口没有实际100ms调度延迟，不展示调度达标、p95/p99等编造指标。

不要增加数据库或持久化采集历史作为第一版必做范围；只需当前缓存和必要的门户自身日志。

## 7. 现有字段映射

字段为JSON里的带点字符串键，不能误当成深层对象；/v1/detail返回info与diag两张对象。

| 展示 | 来源 |
|---|---|
| 程序版本 | detail.info["version"] |
| 源服务状态与原因 | diag["status"]、diag["message"] |
| 运行阶段 | diag["runtime.phase"] |
| 位号数 | detail.info["tag.count"] |
| 导入历史点数 | detail.info["hda.imported_samples"] |
| 原始历史窗口 | detail.info["hda.native.start_time"] / ["hda.native.end_time"] |
| 实时追加窗口 | detail.info["hda.live.start_time"] / ["hda.live.end_time"] |
| 最近DA/HDA处理 | diag["da.last_time"] / ["hda.last_time"] |
| 上轮处理数量 | diag["da.tag_count"]、["da.expected_count"]、["hda.tag_count"]、["hda.expected_count"] |
| 连接/会话/订阅/监控项 | diag["opcua.connection_count"]、["opcua.session_count"]、["opcua.subscription_count"]、["opcua.monitored_item_count"] |
| 待发与合并 | diag["opcua.notification_pending"]、["opcua.notification_coalesced_total"] |
| 合并最近时间 | diag["opcua.notification_coalesced_last_time"] |

以上来自diagnostic.go。hda.imported_samples可能只代表本次启动导入进度/计数，不能当作数据库当前全部历史点数或实时增长量。说明页中的输入数据点数来自summary，不强制与运行库实时点数相等。

所有时间默认UTC并明确标注；可选本地切换不改变数据。未知、空值和0必须区分。

## 8. 后端接口和目录建议

建议新建仓库同级独立模块mock_portal/，不要在hda_mocker_4/main.go塞Web功能。

```text
mock_portal/
  cmd/mock_portal/
  internal/catalog/      # 只读发现、config、summary和允许文件
  internal/diagnostics/  # HTTP采集与缓存
  internal/httpapi/
  frontend/              # React/TS + shadcn
  portal.yaml.example
  README.md
```

后端使用Go标准HTTP能力与YAML解析；前端构建后go:embed打入独立exe。运行时无需Node、Python、DuckDB或额外数据库。使用与项目环境兼容的依赖版本并锁定，不为门户升级主CLI依赖。

最少API：

| API | 返回 |
|---|---|
| GET /api/mocks | 从preset发现的清单、config连接摘要、缓存诊断和错误 |
| GET /api/mocks/{id} | 同一preset的连接摘要、配置文本、最新诊断详情及采集时间 |
| GET /api/mocks/{id}/summary | Markdown正文与最后修改时间，不用HTML替代原文件 |
| GET /api/mocks/{id}/files | 允许下载的文件ID、相对名称、类别及字节大小 |
| GET /api/mocks/{id}/files/{fileId} | 下载一个允许文件，使用Content-Disposition和流式/Range支持 |
| GET /api/standardization | 指南和工具包下载信息 |
| GET /api/standardization/bundle | 预备好的标准化工具包 |

config文本可随详情返回，不需要额外连接页。返回字段允许局部error：summary缺失不妨碍诊断，诊断不可达不妨碍下载。文件下载错误返回可理解的404/409，不返回某个替代文件。配置解析错误不把整站变500。

不提供POST/PUT/DELETE业务API，不保留隐蔽的启动/上传能力。文件ID由服务端列举结果生成，不允许任意磁盘路径、目录穿越、编码分隔符或逃逸根目录的符号链接。Markdown禁用危险URL和原始HTML；相对链接只映射到允许文档/下载，不暴露任意路径。

仅扫描hda/da第一层Parquet；不递归发布source_data、archive、runtime或日志。大文件不做请求时哈希/解析；如无已有校验值可不显示校验值，不能添加在线全量分析。

第一版面向可信内网，远程网络访问由部署层控制；不要为此新增账号权限平台。门户不得绕过已有文件访问边界或公开证书私钥。

## 9. 交付物和验收

交付独立门户源码、前端构建脚本、exe构建说明、portal.yaml.example、操作README和必要测试。不必重新编译Mock CLI。

验收至少包含：

1. 发现5个preset，demo不计；Mock总数始终等于发现条目数。
2. 每个详情恰好3个页签，默认诊断，顶部连接信息直接取config。
3. 修改测试副本的config端点后，门户读取更新；没有第二份地址登记需要同步，summary里的旧快照不影响当前连接。
4. 通配地址明确提示；无diagnostic.listen显示未监控；超时、无效JSON、旧字段缺失均能降级。
5. 五份summary按Markdown/GFM正确渲染，完整位号表和BASF稀疏规则可读；缺少summary只影响说明页。
6. 下载字节与磁盘文件一致，支持大文件流式及断点续传；无法通过文件ID下载runtime、源数据或任意路径。
7. 多浏览器不增加探测频率；门户启动、刷新、关闭均不影响UA服务。
8. 移动端不横向溢出；浅/深色文字可读，键盘可操作，状态有文字。
9. 无数据上传、创建、启停、在线特征扫描、DuckDB读取或CLI源码改动。

原型位于prototypes/mock-portal-review.html，浏览器预览为prototypes/mock-portal-preview.html。它们是视觉参考；原型的状态/地址为演示，不能被复制为生产连接配置或写死在正式前端。

## 10. 实施顺序

先做目录发现、config解析和只读API；再做统一Mock列表与三页签；接入现有诊断缓存；渲染summary和提供白名单下载；最后打包标准化工具、验证样例与边界。不要因为缺少实际生产IP而猜测配置，可在单独的测试夹具中模拟成功、告警、未监控和不可达。
