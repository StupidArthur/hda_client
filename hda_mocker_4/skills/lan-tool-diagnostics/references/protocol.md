# Protocol v1

The manager stores a tool's display name, deployment summary, diagnostic host, and diagnostic port. Tools do not register and do not know which managers poll them. Multiple managers may query the same endpoints.

## `GET /v1/diag`

Return a flat JSON object. Required keys:

```json
{
  "status": "ok",
  "message": "DA上一轮播放 103/103；HDA上一轮入库 103/103；UA会话 2"
}
```

Additional product-specific scalar keys are allowed. Dotted names express grouping, for example `da.last_time`, `hda.tag_count`, or `opcua.session_count`.

## `GET /v1/detail`

Return exactly two flat maps:

```json
{
  "info": {
    "summary": "OPC UA HDA/DA service",
    "business.protocol": "opc.tcp",
    "business.port": 4840
  },
  "diag": {
    "status": "ok",
    "message": "DA上一轮播放 103/103；HDA上一轮入库 103/103；UA会话 2"
  }
}
```

`diag` must be produced by the same collector used by `/v1/diag`.

## HTTP behavior

- Respond with UTF-8 JSON and `Content-Type: application/json; charset=utf-8`.
- Use HTTP 200 whenever a valid diagnostic payload is produced, including `warn` and `error` states.
- Use HTTP 405 for non-GET methods and HTTP 404 for other paths.
- Bound collection time. A collection failure should normally produce `status=error` with a useful message; use HTTP 500 only when no valid payload can be encoded.
- Do not expose credentials, private keys, tokens, connection strings containing secrets, or environment dumps.

## Status meaning

- `ok`: monitored business work is current and usable.
- `warn`: still usable, but delayed, incomplete, starting, or partially impaired.
- `error`: a required business function is unavailable or stopped.

The manager treats request timeout/refusal as `offline` and handles polling, history, notification deduplication, and recovery notifications.
