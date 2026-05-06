---
title: Audit log
description: The audit_events Postgres schema, retention policy, redaction rules, and the JSON shape returned by the portal API.
---

# Audit log

Every `tools/call` produces a row in the `audit_events` Postgres
table. Auth failures, the portal's Try-It proxy, and direct admin-API
invocations all show up there too, with different `source` tags so
you can filter by origin.

## Two-table layout

As of v1.1.0 audit data lives in two tables joined 1:1 by event ID:

- `audit_events`: indexed summary used for time-range, tool, user, success, and free-text queries. Small rows, hot path.
- `audit_payloads`: full request and response envelope (parameters, headers, result content blocks, captured notifications, error categories). Optional, written in the same transaction as the summary; absent when `audit.capture_payloads: false` or when the deployment predates v1.1.0.

Cascade delete on the foreign key keeps retention atomic: deleting an `audit_events` row drops its payload row in the same statement.

## What gets recorded

| Column | Source |
| --- | --- |
| `id` | UUID generated server-side. |
| `ts` | UTC timestamp at the start of the call. |
| `duration_ms` | End-to-end time the tool took, including the auth chain. |
| `request_id` | UUID generated per call; useful for correlating across logs. |
| `session_id` | The MCP session ID the SDK assigned at initialize. |
| `user_subject` / `user_email` / `auth_type` / `api_key_name` | Resolved identity from the auth chain. |
| `tool_name` / `tool_group` | Which tool, and which category. |
| `parameters` | Sanitized arguments (JSONB). Keys matching `audit.redact_keys` have their values replaced with `"[redacted]"`. |
| `success` / `error_message` / `error_category` | Outcome. `error_category` is a short label (`auth`, `tool`, `protocol`, etc.) for filtering. |
| `request_chars` / `response_chars` / `content_blocks` | Sizing of input args and the result. Useful for spotting size-cap issues. |
| `transport` | Always `"http"` today. |
| `source` | `"mcp"` for real client calls, `"portal-tryit"` for portal-driven invocations. |
| `remote_addr` / `user_agent` | From the inbound HTTP headers. |

See [Database & Migrations](../configuration/database.md#schema) for
the exact DDL and indexes.

## Browsing in the portal

The portal's **Audit** tab is the primary UI:

- Time range, tool, user, success/error, and free-text search.
- Pagination (50 rows per page).
- Per-row drawer expanding the full event including sanitized params.
- Title-attribute on the User cell shows the canonical
  `user_subject` (e.g. a Keycloak UUID) when the displayed value is
  email or API-key name.

The **Dashboard** tab is a 1-hour snapshot: total calls, error rate,
p50 / p95 latency, unique users / tools, and a recent-activity table.

## REST endpoints

| Endpoint | Returns |
| --- | --- |
| `GET /api/v1/portal/audit/events` | Paginated event list. Query params: `from`, `to` (RFC 3339), `tool`, `user`, `session`, `success` (`true`/`false`), `q` (free text), `limit`, `offset`. Plus the JSONB filters in the next section. |
| `GET /api/v1/portal/audit/events/{id}` | Single event with captured payload (when present). |
| `GET /api/v1/portal/audit/export` | NDJSON stream of summary rows. Same filter surface as `/events`. Capped at 100,000 rows per request. |
| `GET /api/v1/portal/audit/timeseries` | Bucketed counts. Query params: `from`, `to`, `bucket` (Go duration like `1m`, `5m`). Returns `[{time, count, errors, avg_duration_ms}]`. |
| `GET /api/v1/portal/audit/breakdown` | Group-by aggregations. Query param: `by` (one of `tool`, `user`, `success`, `auth_type`). Returns `[{key, count, errors}]`. |
| `GET /api/v1/portal/dashboard` | The 1-hour summary. |

### JSONB filters

`/events` and `/export` accept JSONB-aware filters that hit the
`audit_payloads` sibling row. Use them to narrow by parameter values,
response shape, request headers, or presence of a payload column.

```bash
# Calls where the request param user.id equals "alice"
curl -H "X-API-Key: $KEY" \
  "$BASE/api/v1/portal/audit/events?param.user.id=alice"

# Tool-error responses (response.isError matches JSON true)
curl -H "X-API-Key: $KEY" \
  "$BASE/api/v1/portal/audit/events?response.isError=true"

# Match a User-Agent in request headers
curl -H "X-API-Key: $KEY" \
  "$BASE/api/v1/portal/audit/events?header.User-Agent=curl/8.0"

# Only events that recorded notifications
curl -H "X-API-Key: $KEY" \
  "$BASE/api/v1/portal/audit/events?has=notifications"

# Combine: tool-errors that emitted notifications, last hour
curl -H "X-API-Key: $KEY" \
  "$BASE/api/v1/portal/audit/events?response.isError=true&has=notifications&from=$(date -u -v-1H +%FT%TZ)"
```

Values are type-detected: `true` / `false` become JSON booleans,
integers and floats become numbers, everything else is a string.
Force a literal string by quoting: `?param.code="200"` matches the
JSON string, `?param.code=200` matches the number.

Allowed `has=` columns: `request_params`, `request_headers`,
`response_result`, `response_error`, `notifications`, `replayed_from`.

### Replay a captured call

`POST /api/v1/portal/audit/events/{id}/replay` re-invokes the tool with the same arguments captured on the original event, through an in-process MCP client. The replay produces a new audit row tagged `source=portal-replay` with `replayed_from = {id}`; that row is fired with the portal-authenticated identity, not the original caller's, so an operator can see who triggered the replay.

```bash
# Find a tool error from the last hour that you want to reproduce.
curl -H "X-API-Key: $KEY" \
  "$BASE/api/v1/portal/audit/events?response.isError=true&from=$(date -u -v-1H +%FT%TZ)&limit=5" \
  | jq -r '.events[].id'

# Replay one. The response includes the new event's id so you can
# follow up with /events/{id}.
curl -X POST -H "X-API-Key: $KEY" -H "X-Requested-With: x" \
  "$BASE/api/v1/portal/audit/events/<id>/replay" | jq
```

The replay refuses (`400`) when:

- the original event has no captured payload (capture was disabled when it was written),
- any captured parameter value is the literal `[redacted]` (replaying with a placeholder would mislead about what the call did; re-stage manually via Try-It with the real value),
- the named tool is no longer registered.

A per-identity token bucket (5 burst, ~5/min sustained) protects against runaway replay loops; exhausted callers get `429 Too Many Requests` with a `Retry-After` header.

Replay re-runs the tool's side effects. If the original call wrote to a database, sent a notification, or charged a card, the replay does it again. There is no dry-run mode and no per-tool allow list; if the operator can hit `/replay`, every registered tool is replayable. Treat this like Try-It: a developer affordance for debugging, not a production self-service.

### Live tail

`GET /api/v1/portal/audit/stream` is an SSE endpoint that emits one `event: audit\ndata: <event JSON>` per newly-written audit event. Open the connection, fire calls, watch them flow:

```bash
# In one terminal:
curl -N -H "X-API-Key: $KEY" "$BASE/api/v1/portal/audit/stream"

# In another, fire some tool calls; the first terminal sees them
# arrive within ~200ms of each write.
```

The endpoint emits an opening `: connected` comment so the consumer can detect the connection is live before the first audit row arrives, and a `: keepalive` comment every 30 seconds to keep idle proxies from killing the connection. Subscribers see only events written AFTER they subscribe; for history use `/events` or `/export`.

Slow consumers drop events silently per-subscriber (the producer never blocks). The buffered channel default is 64 events; SSE clients should drain promptly to avoid drops during bursts.

### NDJSON export

`/api/v1/portal/audit/export?format=jsonl` streams summary rows as
newline-delimited JSON. One event per line, ordered as `/events`
returns them. Use for offline analysis or backups:

```bash
# All errors from the last 24h, piped through jq
curl -H "X-API-Key: $KEY" \
  "$BASE/api/v1/portal/audit/export?success=false&from=$(date -u -v-24H +%FT%TZ)" \
  | jq -r '.tool_name + "\t" + .error_message'
```

The export omits the captured `audit_payloads` row from each line; if
you need the full envelope, follow up with
`/audit/events/{id}` per event. The endpoint caps at 100,000 rows
per request; tighten the filter window for larger sets.

`limit` and `offset` behave differently from `/events`:
`limit` clamps the total exported row count (still bounded by
the 100,000 hard cap), and `offset` is ignored. Exports always
start from the head of the matching set; use `from` / `to` to
window in time instead.

All of these accept either the session cookie (browser) or
`X-API-Key` / `Authorization: Bearer`.

## Direct SQL

For analyses the portal doesn't cover:

```sql
-- Rate of calls per user per minute, last 24h
SELECT date_trunc('minute', ts) AS bucket, user_subject, count(*)
FROM audit_events
WHERE ts >= now() - interval '1 day'
GROUP BY bucket, user_subject
ORDER BY bucket, count DESC;

-- Slowest 50 calls in the last week, with error category
SELECT ts, tool_name, user_subject, duration_ms, success, error_category
FROM audit_events
WHERE ts >= now() - interval '7 days'
ORDER BY duration_ms DESC
LIMIT 50;

-- All failed flaky calls (for verifying retry behavior in your gateway)
SELECT ts, request_id, parameters, error_message
FROM audit_events
WHERE tool_name = 'flaky' AND NOT success
ORDER BY ts DESC;
```

## Sanitization

`audit.redact_keys` is a list of case-insensitive substrings.
Tool-call argument keys matching any substring have their *values*
replaced with `"[redacted]"` before the row is written. The default
list includes `password`, `token`, `secret`, `authorization`,
`cookie`, `api_key`, `credentials`.

The match is on argument *keys*, not values. So an argument like
`{"password": "hunter2"}` is redacted, but a free-text body that
happens to contain the word "password" is not.

Sanitization is recursive: nested objects and arrays are walked.

## Retention

mcp-test does not auto-prune. The `audit.retention_days` field
documents the deployment's retention target; enforce it with a cron
job:

```sql
DELETE FROM audit_events WHERE ts < now() - interval '30 days';
```

## Performance

Each row is ~500 bytes (variable based on parameter payload) plus
index overhead. At 100 calls/sec sustained, the table grows ~4GB/day
plus indexes. For higher rates, partition by month or move to a
dedicated audit-only Postgres instance.
