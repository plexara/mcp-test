---
title: Tools overview
description: Twelve test tools across four categories. The catalog and the determinism contract: same input, same output, every time.
---

# Tools overview

12 tools across 4 categories. Each is an intentionally simple test
fixture. The goal is predictable, controllable behavior, not useful
work.

| Category | Tool | What it tests |
| --- | --- | --- |
| [identity](identity.md) | `whoami` | Identity forwarding through the gateway. |
| | `echo` | Argument round-trip and any enrichment applied to args. |
| | `headers` | HTTP header pass-through and the gateway's redaction rules. |
| [data](data.md) | `fixed_response` | Deterministic dedup, caching, hash stability. |
| | `sized_response` | Response size limits and chunking. |
| | `lorem` | Seeded reproducibility, content-type handling. |
| [failure](failure.md) | `error` | Error propagation and audit categorization. |
| | `slow` | Latency, timeout policy, ctx cancellation. |
| | `flaky` | Retry policy, reproducibility under partial failure. |
| [streaming](streaming.md) | `progress` | Progress notification pass-through. |
| | `long_output` | Multi-block content handling. |
| | `chatty` | Block ordering, mixed content types. |

## Listing tools

The MCP `tools/list` method returns every registered tool with its
JSON schema. The portal also exposes the list as JSON:

```bash
curl -s -H "X-API-Key: $KEY" http://localhost:8080/api/v1/portal/tools | jq
```

Or in the portal at <http://localhost:8080/portal/tools>.

## Calling a tool

Three ways:

1. **Through an MCP client.** The "real" path. Anything the client
   does (initialization headers, session management, progress
   handling) goes through the gateway, which is what you usually
   want to test.
2. **Through the portal Try-It tab.** Per-tool form with sliders,
   dropdowns, and inline help. The proxy invokes the tool through
   an in-process MCP client so the SDK plumbing behaves the same as
   a real call. Audit rows are tagged `source=portal-tryit` so
   portal-driven calls can be filtered out of test runs.
3. **Through the admin API directly.** For scripting:

```bash
curl -s -X POST http://localhost:8080/api/v1/admin/tryit/fixed_response \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"arguments":{"key":"hello"}}'
```

## Disabling categories

Each toolkit has an enable flag in config. Disabling a toolkit means
its tools are not registered with the MCP server at all (they don't
appear in `tools/list`).

```yaml
tools:
  identity:  { enabled: true }
  data:      { enabled: true }
  failure:   { enabled: false }   # for example, no failure mode in this deployment
  streaming: { enabled: true }
```

## Determinism contract

Every "deterministic" claim below means: same input ⇒ same output,
across processes, across machines, across versions of the binary
(unless a major-version bump explicitly changes the algorithm). If
you observe a difference, treat it as a bug; assertions in your
gateway tests can rely on it.

Tools without seeds (`whoami`, `headers`, `progress`,
`long_output`, `chatty`) are deterministic in shape but not in
content (e.g. `whoami` returns whatever identity authenticated; the
*structure* is fixed).
