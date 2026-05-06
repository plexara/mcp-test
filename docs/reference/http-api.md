---
title: HTTP API reference
description: Portal read endpoints (/api/v1/portal/*) and admin write endpoints (/api/v1/admin/*). Request and response shapes.
---

# HTTP API reference

Every HTTP route mcp-test exposes, beyond the MCP transport itself.

## Public routes

These are reachable without auth.

| Method | Path | Returns |
| --- | --- | --- |
| `GET` | `/healthz` | `200 OK` text body. Liveness. |
| `GET` | `/readyz` | `200 OK` while ready, `503` during shutdown drain. |
| `GET` | `/.well-known/oauth-protected-resource` | RFC 9728 metadata: resource identifier, authorization servers, supported bearer methods. |
| `GET` | `/.well-known/oauth-authorization-server` | Lightweight stub pointing at the upstream OIDC issuer's metadata URL. |
| `GET` | `/portal/` and subpaths | The embedded React SPA (or a placeholder if `make ui` wasn't run). |
| `GET` | `/portal/auth/login` | Starts the OIDC PKCE flow. Redirects to the IdP. |
| `GET` | `/portal/auth/callback` | OIDC redirect URI. Exchanges the auth code for tokens, validates the id_token, sets the session cookie. |
| `POST` | `/portal/auth/logout` | Clears the session cookie. |

## MCP endpoint

Mounted at `/`. Browser GETs hitting `/` get a 302 to `/portal/`; MCP
clients (which send `Accept: application/json` or
`text/event-stream`) pass through.

| Method | Path | Returns |
| --- | --- | --- |
| `POST` | `/` | JSON-RPC requests (initialize, tools/list, tools/call, etc.). |
| `GET` | `/` | SSE stream for server-initiated messages (after a session is established). |
| `DELETE` | `/` | Tear down the MCP session. |

All require `X-API-Key` or `Authorization: Bearer <jwt>` unless
`auth.allow_anonymous` is true. 401s carry a `WWW-Authenticate: Bearer
realm="mcp-test", resource_metadata="..."` header.

## Portal API (read-only)

Behind the cookie or `X-API-Key` / `Authorization: Bearer`.

| Method | Path | Returns |
| --- | --- | --- |
| `GET` | `/api/v1/portal/me` | Resolved Identity object. |
| `GET` | `/api/v1/portal/server` | Build version + sanitized config (secrets redacted). |
| `GET` | `/api/v1/portal/instructions` | The `server.instructions` text the MCP server hands to clients at initialize time. |
| `GET` | `/api/v1/portal/tools` | List of `{name, group, description, input_schema}` for every registered tool. |
| `GET` | `/api/v1/portal/tools/{name}` | Same shape, single tool. |
| `GET` | `/api/v1/portal/audit/meta` | Filter contract surface: `{has_keys, json_sources, replay: {burst, refill_secs, sustained_min}, export: {max_rows}}`. Lets a UI build its filter editor against the server's source of truth without duplicating allow-lists. |
| `GET` | `/api/v1/portal/audit/events` | Paginated audit events. Query: `from`, `to` (RFC 3339), `tool`, `user`, `session`, `success`, `q`, `limit`, `offset`, plus the JSONB filters described below. |
| `GET` | `/api/v1/portal/audit/events/{id}` | Single event by id (UUID); includes the captured payload row when present. 400 on a non-UUID id, 404 when the event isn't recorded. |
| `POST` | `/api/v1/portal/audit/events/{id}/replay` | Re-invokes the captured tool call through an in-process MCP client. Writes a new audit event tagged `source=portal-replay` with `replayed_from` pointing at `{id}`. Per-identity rate limited (5 burst, 1 token / 12s); returns `429 Too Many Requests` with `Retry-After` when exhausted. Tokens are consumed *after* validation passes, so a click on a non-replayable row (no payload, redacted params, missing tool) returns `400` without burning the operator's budget. CSRF-gated via `X-Requested-With`. |
| `GET` | `/api/v1/portal/audit/export` | NDJSON stream of summary rows for a filter. `format=jsonl` (default) is the only supported format. Same filter surface as `/events`. Capped at 100,000 rows per request. |
| `GET` | `/api/v1/portal/audit/stream` | SSE live tail of new audit events. One `event: audit\ndata: <event JSON>` per write; opening comment `: connected` confirms the connection; `: keepalive` every 30 seconds. Sets `X-Accel-Buffering: no` for nginx-fronted deployments. |
| `GET` | `/api/v1/portal/audit/timeseries` | Bucketed counts. Query: `from`, `to`, `bucket` (Go duration). |
| `GET` | `/api/v1/portal/audit/breakdown` | Group-by aggregations. Query: `by` (`tool`/`user`/`success`/`auth_type`). |
| `GET` | `/api/v1/portal/dashboard` | 1-hour stats + recent activity. |
| `GET` | `/api/v1/portal/wellknown` | Pretty rendering of the protected-resource metadata. |

### JSONB path filters

`/audit/events` and `/audit/export` accept additional query parameters that compile to JSONB containment predicates against the `audit_payloads` sibling row. Filters are AND-combined with each other and with the indexed-column filters above.

| Syntax | Compiles to | Example |
| --- | --- | --- |
| `param.<dotted.path>=<value>` | `audit_payloads.request_params @> {"<path>": <value>}` | `?param.user.id=alice` |
| `response.<dotted.path>=<value>` | `audit_payloads.response_result @> {"<path>": <value>}` | `?response.isError=true` |
| `header.<name>=<value>` | `audit_payloads.request_headers @> {"<name>": ["<value>"]}` (single-segment name only) | `?header.User-Agent=curl/8.0` |
| `has=<column>` | `audit_payloads.<column>` is `IS NOT NULL` and the column's text representation is not one of `'{}'`, `'[]'`, `'null'`, or `''` | `?has=response_error` |

Allowed `has=` columns: `request_params`, `request_headers`, `response_result`, `response_error`, `notifications`, `replayed_from`. Anything else is silently dropped. Note: a JSONB column literally storing the JSON string `""` (rendered as `'""'::text`, four characters) does pass the filter; the exclusion list is canonical empty containers and an empty TEXT column, not "all logically empty values."

**Value type detection.** Bare values on `param.*` and `response.*` filters are type-detected before the containment doc is built: `true` / `false` become JSON booleans, integers and floats become numbers, everything else is a string. Force a literal string with quotes: `?param.code="200"` matches the JSON string `"200"`, while `?param.code=200` matches the JSON number `200`. **Header values** (`header.*`) are always treated as strings since HTTP header values are strings on the wire; type detection does not apply there.

**Header name canonicalization.** `header.<name>` canonicalizes the header name to the standard Go form before matching, so `?header.user-agent=curl/8.0` matches the same row as `?header.User-Agent=curl/8.0`.

**Header paths must be a single segment.** `?header.X-Foo.bar=v` is silently dropped at parse time; HTTP headers are flat name → values, no nesting. Use `param.*` or `response.*` for nested-path matches.

**Empty-segment paths.** `?param.a..b=v` (a path with an empty segment) cannot match any real payload and is silently dropped at parse time.

**Index use.** The `request_params` and `response_result` columns carry `jsonb_path_ops` GIN indexes; the `@>` operator hits them directly. `request_headers` is unindexed today, so `header.*` filters scan the matching subset and are best paired with a time-range filter. The `has=` filter is a NOT-NULL plus non-empty-content check on a JSONB or TEXT column; the planner can use a partial index on the column when present, otherwise it scans.

## Admin API (mutating)

Same auth requirements. Per the project decision, any authenticated
caller can call these.

| Method | Path | Body | Returns |
| --- | --- | --- | --- |
| `POST` | `/api/v1/admin/keys` | `{ "name": "...", "description": "..." }` | `{ "key": {...}, "plaintext": "mt_..." }` (plaintext shown once). |
| `GET` | `/api/v1/admin/keys` | — | `{ "keys": [...] }` (no plaintext). |
| `DELETE` | `/api/v1/admin/keys/{name}` | — | `204 No Content`. |
| `POST` | `/api/v1/admin/tryit/{name}` | `{ "arguments": { ... } }` | The MCP `CallToolResult` (content + structuredContent + isError). |

`/api/v1/admin/tryit/{name}` invokes the named tool through an
in-process MCP client connected to the running server. It writes its
own audit row tagged `source=portal-tryit` with the portal-
authenticated identity.

## Errors

All API endpoints return JSON errors:

```json
{ "error": "human-readable description" }
```

Standard HTTP codes:

- `400` — malformed body.
- `401` — missing or invalid credentials.
- `404` — unknown tool / key / resource.
- `503` — feature not enabled (e.g. DB API keys when
  `api_keys.db.enabled` is false).
- `500` — unexpected server error.

## CORS

The mux wraps everything in a permissive CORS handler that:

- Allows any origin (`*`).
- Allows `GET, POST, DELETE, OPTIONS`.
- Round-trips `Authorization`, `Content-Type`, `X-API-Key`,
  `Mcp-Session-Id`, `Mcp-Protocol-Version`, `Last-Event-ID`.
- Exposes `Mcp-Session-Id`, `Mcp-Protocol-Version`.

Tighten this in production by terminating CORS at your ingress.
