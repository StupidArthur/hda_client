---
name: lan-tool-diagnostics
description: Add or review the lightweight HTTP diagnostic interface used by tools deployed on a LAN. Use when a tool must expose manager-polled health and business information through a separate configurable diagnostic port.
---

# LAN Tool Diagnostics

Implement the protocol in [references/protocol.md](references/protocol.md). Preserve these invariants:

- The diagnostic listener is independent of business listeners and its address is configurable per tool instance.
- The tool has no diagnostic identity or manager registration. A manager identifies it by diagnostic `IP:PORT` and owns its display name and deployment description.
- Expose only `GET /v1/diag` and `GET /v1/detail`.
- `/v1/diag` is the exact same flat map returned as `/v1/detail.diag`; generate it once and reuse it.
- `/v1/detail` contains exactly two maps named `info` and `diag`. Both maps are flat; use dotted keys for grouping.
- Every map value is a JSON scalar (`string`, `number`, `boolean`, or `null`). Do not put maps, arrays, or encoded JSON strings inside values.
- `diag.status` is `ok`, `warn`, or `error`. `diag.message` is a concise, human-readable summary containing real business facts, not a generic phrase such as "running normally".
- The tool owns status calculation. The manager must not infer health by parsing product-specific fields.
- Network timeout or refusal is `offline`, which is manager state rather than a tool-reported status.
- Diagnostic collection must be bounded, concurrency-safe, read-only, and must not delay business work.

Keep product-specific keys small and useful. Put stable business facts in `info`; put current operational observations and monitored conditions in `diag`.

When adding this protocol to an existing tool, add contract tests for response shape, flatness, route behavior, and equality between `/v1/diag` and `/v1/detail.diag`.
