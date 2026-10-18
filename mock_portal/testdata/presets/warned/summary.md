# warned

本地验收夹具，仅用于验证门户状态显示；不要复制到生产 `presets/`。

| 项 | 值 |
|---|---|
| UA | opc.tcp://127.0.0.1:18981 |
| 诊断 | 127.0.0.1:18991（配合 `scripts/fake_diag -status warn`） |
| 期望状态 | 采集正常 / 源端告警（源端 status 原样保留） |
