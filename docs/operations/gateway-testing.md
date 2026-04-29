---
title: Testing a gateway
description: Patterns for asserting on a gateway's identity-forwarding, redaction, enrichment, and progress-passthrough using mcp-test as the predictable upstream.
---

# Testing a gateway

mcp-test is a fixture, not a goal. The point is what you can verify
about an MCP gateway sitting between your client and this server.
This page collects the patterns.

## The setup

```
┌────────┐      ┌─────────┐      ┌──────────┐
│ client │ ──▶  │ gateway │ ──▶  │ mcp-test │
└────────┘      └─────────┘      └──────────┘
                                       │
                                       ▼
                                  ┌─────────┐
                                  │ audit   │
                                  │ (Postgres) │
                                  └─────────┘
```

The client makes calls. The gateway transforms them (auth,
enrichment, header rewriting, redaction, rate limiting). mcp-test
records what arrived. Compare what the client sent against what
mcp-test received against what the client got back. Where they
differ is what the gateway did.

## Identity forwarding

**Question:** does the gateway forward the original caller's identity,
or does it re-authenticate and substitute its own?

```
client → gateway: Authorization: Bearer <user-jwt>
                  │
                  └─→ mcp-test: Authorization: Bearer <whichever>
```

Call `whoami`. The returned `subject` and `email` should match the
user who authenticated to the gateway. If they match the gateway's
service-account identity, the gateway is re-authenticating.

```bash
TOKEN=<user jwt from your IdP>
curl -X POST https://gateway.example.com/ \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"whoami"},"id":1}'
# Response should carry subject = your user, not the gateway's service account
```

## Header pass-through

**Question:** which headers does the gateway forward, and which does it
strip or rewrite?

Call `headers`. The response shows every header that arrived at
mcp-test. Diff against your client's request:

- New headers (gateway-injected) appear in the response but weren't
  in your request. Common: `X-Forwarded-For`, `X-Request-Id`,
  `X-Tenant-Id`, gateway-specific tracing headers.
- Stripped headers (from the request) are missing from the response.
  Common: `Authorization` (re-authenticated), `Cookie` (session
  scrubbed).
- Rewritten headers have different values. `User-Agent` is often
  rewritten to identify the gateway.

```bash
curl -X POST https://gateway.example.com/ \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-My-Header: client-value" \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"headers"},"id":1}'
```

The response's `Authorization` should be `[redacted]` (mcp-test
redacts; verify the gateway also redacted before logging if it logs
headers anywhere). `X-My-Header` should round-trip if the gateway is
allow-listing extras.

## Argument enrichment

**Question:** does the gateway inject extra arguments into tool calls?

Call `echo` with a known input:

```json
{ "name": "echo", "arguments": { "message": "client said this" } }
```

Compare the response. If `echo` returns extra fields (`tenant_id`,
`request_id`, etc.), the gateway is enriching args.

The mcp-test audit row's `parameters` column shows what reached the
upstream too — useful for confirming the enrichment happened
gateway-side and not via some client library.

## Response enrichment / shaping

**Question:** does the gateway rewrite responses?

Call `fixed_response` with a known key. The expected hash is
deterministic; if the body or hash differs, the gateway is rewriting.

```bash
# Direct mcp-test (bypass gateway): expected = sha256("hello")
echo -n "hello" | sha256sum
# 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824

# Through gateway: should match
curl -X POST https://gateway.example.com/ \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"fixed_response","arguments":{"key":"hello"}},"id":1}'
```

For text manipulation (a gateway that prepends `[STAGING]` to every
response, say), call `chatty` and verify each block is prefixed.

## Caching / dedup

**Question:** does the gateway cache identical calls?

Call `fixed_response` twice with the same key. Then check the
mcp-test audit log:

- Two rows: gateway is not caching, every call hits the upstream.
- One row: gateway cached the first response and served the second
  from cache.

```sql
SELECT count(*), max(ts), min(ts)
FROM audit_events
WHERE tool_name = 'fixed_response'
  AND parameters->>'key' = 'hello'
  AND ts >= now() - interval '1 minute';
```

## Timeout / cancellation

**Question:** does the gateway propagate cancellation to the upstream?

Call `slow` with `milliseconds=10000` and cancel the client request
after 1 second. Then check the audit log:

- `success=false`, `duration_ms ≈ 1000`, `error_message` mentioning
  context cancellation: gateway propagated the cancel.
- `success=true`, `duration_ms = 10000`: gateway did not propagate;
  mcp-test ran to completion despite the client giving up.

The latter is a leak — the upstream is doing work the client
abandoned. Important to catch.

## Retry behavior

**Question:** does the gateway retry on failure, and how does it
identify retries?

Use `flaky` with a fixed seed and call_id sequence:

```bash
for i in 1 2 3 4 5; do
  curl -X POST https://gateway.example.com/ \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"tools/call\",\"params\":{\"name\":\"flaky\",\"arguments\":{\"fail_rate\":0.5,\"seed\":\"test\",\"call_id\":$i}},\"id\":$i}"
done
```

Check the audit log for the same `(seed, call_id)` showing up
multiple times — that's the gateway retrying. With a fixed seed, you
know in advance which call_ids are deterministically failures, so you
can predict retry behavior.

## Progress notification pass-through

**Question:** does the gateway buffer SSE responses?

Call `progress` with `steps=10, step_ms=500`. Time when each
notification arrives at the client:

- Spaced ~500ms apart: gateway is streaming.
- All ~5s into the call (at the end): gateway is buffering.
- Some other pattern: gateway is doing something interesting,
  investigate.

The mcp-test audit row records when the call completed. Compare
against when the client received the final notification.

## Rate limiting

**Question:** does the gateway rate-limit per user / per tool / per
total?

Hammer `whoami` (cheap, low audit-row cost) at a target rate. Watch
the audit log:

- Steady stream of rows: no rate limit hitting.
- Bursts followed by gaps: token-bucket style rate limit.
- Hard cutoff: fixed-window rate limit.

The audit `remote_addr` column shows what the gateway is forwarding
as the client address. Useful for confirming the rate limiter is
keying on the right thing.

## Putting it together

A typical gateway-validation suite:

1. **Identity**: one `whoami` per auth path (anonymous, OIDC, API
   key) confirming each surface the right identity.
2. **Headers**: a `headers` call with a known custom header
   confirming pass-through and a known sensitive header confirming
   redaction.
3. **Enrichment**: an `echo` call confirming gateway-injected fields.
4. **Caching**: two `fixed_response` calls with the same key; assert
   audit row count.
5. **Cancellation**: a `slow` call with mid-flight cancel; assert
   `duration_ms` matches the cancel time.
6. **Retry**: a sequence of `flaky` calls; assert deterministic
   failure pattern matches expected retry count.
7. **Streaming**: a `progress` call; assert notification timing
   matches `step_ms`.

Wrap them in your test framework of choice. The audit log is your
ground truth.
