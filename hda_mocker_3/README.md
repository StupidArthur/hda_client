# HDA Mocker 3

Pure-Go OPC UA Historical Access mocker. It is intended to load-test the
existing `hda_client` without a Python or Rust runtime.

```powershell
hda_mocker_3.exe default
hda_mocker_3.exe basf_long
hda_mocker_3.exe C:\configs\my-mocker.yaml
```

The endpoint is `opc.tcp://HOST:PORT/hda-mocker/`; nodes are string NodeIDs in
namespace 2, for example `ns=2;s=dynamic_0001`. Configuration is entirely in
files under `presets/`: its YAML shape is compatible with `hda_mocker_2`
(`server`, `history`, and `preset_nodes`). Copy a preset directory to make a
new scenario.

History samples are calculated only for the requested page, at `history.interval`:
dynamic and bad-realtime nodes yield the UTC-second-aligned 1..100 sawtooth;
static and type nodes retain their configured fixed value. No multi-year data is
kept in memory. The Raw HistoryRead implementation honours start/end time,
request `NumValuesPerNode`, the configured server page cap, pagination,
continuation-point release, and reverse ranges.

`history.continuation_point_mode` accepts:

- `rotating` (default): each page gets a new opaque token.
- `stable`: repeated pages use the same token while the server-side cursor
  advances. This intentionally exercises clients that incorrectly assume a
  continuation point must change.

Abandoned continuation points are bounded by the configurable
`history.continuation_point_ttl` (default `5m`). The bundled Go server does not
expose a public close-session callback, so a background TTL sweep owns cleanup;
HistoryRead never performs a whole-table cleanup scan.

Run verification with `go test ./...`. Build a standalone binary using
`go build -o hda_mocker_3.exe .`.
