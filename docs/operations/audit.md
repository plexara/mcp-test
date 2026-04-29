---
title: Audit log
description: The audit_events Postgres schema, retention policy, redaction rules, and the JSON shape returned by the portal API.
---

# Audit log

Every `tools/call` produces a row in the `audit_events` Postgres
table. Auth failures, the portal's Try-It proxy, and direct admin-API
invocations all show up there too, with different `source` tags so
you can filter by origin.

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
| `GET /api/v1/portal/audit/events` | Paginated event list. Query params: `from`, `to` (RFC 3339), `tool`, `user`, `session`, `success` (`true`/`false`), `q` (free text), `limit`, `offset`. |
| `GET /api/v1/portal/audit/timeseries` | Bucketed counts. Query params: `from`, `to`, `bucket` (Go duration like `1m`, `5m`). Returns `[{time, count, errors, avg_duration_ms}]`. |
| `GET /api/v1/portal/audit/breakdown` | Group-by aggregations. Query param: `by` (one of `tool`, `user`, `success`, `auth_type`). Returns `[{key, count, errors}]`. |
| `GET /api/v1/portal/dashboard` | The 1-hour summary. |

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
