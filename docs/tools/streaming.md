# Streaming tools

Three tools that exercise progress notifications, multi-block content,
and chunked output. The long-running side of MCP that gateways must
forward faithfully.

## progress

Emits N progress notifications spaced `step_ms` apart, then returns.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `steps` | int | Required. Default 5, max 100. |
| `step_ms` | int | Optional. Default 200, max 5000. |

The client must include a `progressToken` in `_meta` to receive
notifications. Without one, the tool runs to completion silently
(`notified=false` in the response).

**Returns:**

```json
{ "steps": 5, "notified": true, "done": true }
```

**Side effects:** N `notifications/progress` messages, each with:

```json
{
  "method": "notifications/progress",
  "params": {
    "progressToken": "<client-supplied>",
    "progress": 1,
    "total": 5,
    "message": "step 1/5"
  }
}
```

**What it tests:**

- **Notification pass-through.** Many gateways can break progress
  notifications by buffering the SSE stream. Set `step_ms=500`,
  `steps=10`, and verify the client receives 10 notifications spaced
  by ~500ms. If they all arrive at the end, the gateway is buffering.
- **Token round-trip.** The `progressToken` in the params should
  match exactly between client send and server receive. A gateway
  that synthesizes its own tokens breaks correlation.
- **Cancellation mid-stream.** Cancel after step 3 and verify the
  tool's audit row records `Done: false` with the correct partial
  step count.

## long_output

Returns a single `CallToolResult` containing M text content blocks of
K characters each.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `blocks` | int | Required. Default 3, max 50. |
| `chars` | int | Optional. Default 256, max 65536. |

**Returns:** a `CallToolResult` with `blocks` text content items.
Each block starts with `[block N] ` and is padded to exactly `chars`
characters.

**What it tests:**

- **Multi-block content.** Verify the gateway preserves block count
  and order. Some gateways merge blocks, some reorder, some drop
  empty ones; this tool exposes those.
- **Per-block size limits.** Bump `chars` until a block is dropped
  or truncated.
- **Total size limits.** `blocks * chars` total bytes; bump until you
  hit the gateway's response cap.

## chatty

Returns a `CallToolResult` with a fixed mix of varied text content
blocks.

**Arguments:** none.

**Returns:**

```json
{
  "content": [
    { "type": "text", "text": "first block: short" },
    { "type": "text", "text": "second block: a slightly longer string with multiple words" },
    { "type": "text", "text": "third block: numbers 1 2 3 4 5" },
    { "type": "text", "text": "fourth block: unicode; café résumé naïve" }
  ]
}
```

**What it tests:**

- **Unicode handling.** The fourth block contains accented characters;
  any gateway that mis-encodes will mangle them.
- **Block ordering.** The four blocks have a deterministic order; any
  reordering by the gateway shows up in a diff.
- **Mixed-shape content.** Useful as a smoke test for content-aware
  enrichment that operates per-block.

## Determinism guarantee

`long_output` and `chatty` are byte-deterministic across runs.
`progress` notifications are byte-deterministic in shape; the wall
clock between them is approximate (within a few ms of `step_ms`).
