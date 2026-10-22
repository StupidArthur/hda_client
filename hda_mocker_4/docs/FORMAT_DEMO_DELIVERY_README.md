# HDA / DA 标准演示包（v1.5.2）

整目录带走即可。根目录是程序和工具入口；真正的 preset 只有 `presets/format_demo/`，不要把根目录当作 preset。

```text
format_demo_v1.5.2/
  hda_mocker_4_v1.5.2.exe
  start.cmd
  README.md
  presets/
    format_demo/
      config.yaml
      hda/format_demo.parquet
      da/format_demo.parquet
      summary.md
      manifest.json
  docs/
    HDA_DA_FORMAT_AND_PLAYBACK_GUIDE.md
    HDA_DA_VERIFICATION.md
    HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md
    HDA_MOCKER_4_RELEASE_1_5_0.md
    HDA_MOCKER_4_RELEASE_1_5_1.md
    HDA_MOCKER_4_RELEASE_1_5_2.md
  skills/
    hda-data-import/SKILL.md
  tools/
    reformat_dataset.py
    build_format_demo_preset.py
```

## 给人看

1. 阅读 [格式、入库与播放逻辑](docs/HDA_DA_FORMAT_AND_PLAYBACK_GUIDE.md)。
2. 双击根目录 `start.cmd`，保持服务窗口运行。
3. 按 [启动后验证方法](docs/HDA_DA_VERIFICATION.md) 核对预存历史、DA轮播及实时HDA追加。
4. 示例概况见 [preset说明](presets/format_demo/summary.md)。

也可在包根目录执行：

```powershell
.\hda_mocker_4_v1.5.2.exe --config .\presets\format_demo\config.yaml
```

## 给 Agent 用

入口是 [skills/hda-data-import/SKILL.md](skills/hda-data-import/SKILL.md)，内容与仓库最新版一致，适用v1.5.2。它引用的4份格式/版本文档均在本包 `docs/` 中，相对链接可直接使用。

参考转换器 `tools/reformat_dataset.py` 会重建固定时间轴，不要用它覆盖可信的不等间隔时间。按skill中的工具边界选择处理方法。
`tools/build_format_demo_preset.py --verify` 可只读检查当前示例；这些开发工具需要Python和PyArrow，**运行exe本身不需要它们**。

历史规则中提及其他现场preset的迁移审计，属于完整源仓库材料，本包不包含其数据或审计；实际使用以v1.5.2 skill对当前格式和runtime迁移的说明为准。

## 配置与交付边界

唯一运行配置为 `presets/format_demo/config.yaml`。默认本机UA地址 `opc.tcp://127.0.0.1:14860`，诊断 `http://127.0.0.1:14861/v1/diag`；跨机器使用修改该配置，不另建地址表。

示例为3个位号、HDA/DA各8行、预存历史15点、DA每15秒一轮，故意包含空、Bad、零、负数和Good子码。
首次运行自动在 `presets/format_demo/runtime/` 创建数据库。本包没有runtime、WAL、日志或测试结果。

只更新同语义v1.5 exe不要求重建runtime；改动已导入的Parquet时先备份旧库，再用新runtime验收。
已完成离线文件及实际数据库导入检查，尚未为此示例启动UA服务或完成现场性能测试。
