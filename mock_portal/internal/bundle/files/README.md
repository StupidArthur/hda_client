# HDA / DA 标准化工具包

这个压缩包与门户「标准化指南」页面配套，取自当前 `hda_mocker_4` 仓库的固定版本。

目录结构：

```text
skills/hda-data-import/SKILL.md   Agent 入口约束
docs/                             规则与升级说明
tools/reformat_dataset.py         参考整理脚本
examples/                         列结构与示例说明
```

`skills/hda-data-import/SKILL.md` 中的 `../../docs/...` 相对链接在这个目录结构下
可以直接打开。请把整个包交给负责整理的 Agent 或人员，不要只转发 SKILL.md。

当前入口适用于 `hda_mocker_4 v1.5.2`。按 `SKILL.md` 准备数据；包内包含
v1.5.0 统一观测规则及 v1.5.1/v1.5.2 优化说明。性能优化没有改变数据格式，
跨旧语义迁移与同语义 exe 升级的 runtime 要求不同，不要一律重建数据库。
参考整理脚本只适用于重建固定时间轴，不能直接用于保留原始不等间隔时间的场景。
门户只读，不接收上传，也不在线修改配置或启动 Mock。

给人阅读的入门说明是 `docs/HDA_DA_FORMAT_AND_PLAYBACK_GUIDE.md`，包含3个位号的小型示例表、HDA入库与DA播放流程、空值/Bad行为及查询方法。工具包包含文档，不包含示例exe和Parquet；可运行示例另在仓库 `presets/format_demo/`。
