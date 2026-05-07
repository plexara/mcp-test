---
title: HTTP API reference
description: Portal read endpoints (/api/v1/portal/*) and admin write endpoints (/api/v1/admin/*). Request and response shapes.
---

# HTTP API reference

Every HTTP route mcp-test exposes, beyond the MCP transport itself.

## Public routes

These are reachable without auth.

<div class="api-endpoints" markdown>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/healthz</code>
</div>
<div class="api-endpoint__body" markdown>
`200 OK` text body. Liveness.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/readyz</code>
</div>
<div class="api-endpoint__body" markdown>
`200 OK` while ready, `503` during shutdown drain.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/.well-known/oauth-protected-resource</code>
</div>
<div class="api-endpoint__body" markdown>
RFC 9728 metadata: resource identifier, authorization servers, supported bearer methods.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/.well-known/oauth-authorization-server</code>
</div>
<div class="api-endpoint__body" markdown>
Lightweight stub pointing at the upstream OIDC issuer's metadata URL.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/portal/</code>
</div>
<div class="api-endpoint__body" markdown>
The embedded React SPA (or a placeholder if `make ui` wasn't run). Subpaths route to the SPA's client-side router.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/portal/auth/login</code>
</div>
<div class="api-endpoint__body" markdown>
Starts the OIDC PKCE flow. Redirects to the IdP.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/portal/auth/callback</code>
</div>
<div class="api-endpoint__body" markdown>
OIDC redirect URI. Exchanges the auth code for tokens, validates the id_token, sets the session cookie.
</div>
</div>

<div class="api-endpoint api-endpoint--post" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--post">POST</span>
<code class="api-endpoint__path">/portal/auth/logout</code>
</div>
<div class="api-endpoint__body" markdown>
Clears the session cookie.
</div>
</div>

</div>

## MCP endpoint

Mounted at `/`. Browser GETs hitting `/` get a 302 to `/portal/`; MCP
clients (which send `Accept: application/json` or
`text/event-stream`) pass through.

<div class="api-endpoints" markdown>

<div class="api-endpoint api-endpoint--post" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--post">POST</span>
<code class="api-endpoint__path">/</code>
</div>
<div class="api-endpoint__body" markdown>
JSON-RPC requests (`initialize`, `tools/list`, `tools/call`, etc.).
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/</code>
</div>
<div class="api-endpoint__body" markdown>
SSE stream for server-initiated messages (after a session is established).
</div>
</div>

<div class="api-endpoint api-endpoint--delete" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--delete">DELETE</span>
<code class="api-endpoint__path">/</code>
</div>
<div class="api-endpoint__body" markdown>
Tear down the MCP session.
</div>
</div>

</div>

All require `X-API-Key` or `Authorization: Bearer <jwt>` unless
`auth.allow_anonymous` is true. 401s carry a `WWW-Authenticate: Bearer
realm="mcp-test", resource_metadata="..."` header.

## Portal API (read-only)

Behind the cookie or `X-API-Key` / `Authorization: Bearer`.

<div class="api-endpoints" markdown>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/me</code>
</div>
<div class="api-endpoint__body" markdown>
Resolved Identity object.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/server</code>
</div>
<div class="api-endpoint__body" markdown>
Build version + sanitized config (secrets redacted).
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/instructions</code>
</div>
<div class="api-endpoint__body" markdown>
The `server.instructions` text the MCP server hands to clients at initialize time.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/tools</code>
</div>
<div class="api-endpoint__body" markdown>
List of `{name, group, description, input_schema}` for every registered tool.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/tools/{name}</code>
</div>
<div class="api-endpoint__body" markdown>
Same shape, single tool.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/audit/meta</code>
</div>
<div class="api-endpoint__body" markdown>
Filter contract surface: `{has_keys, json_sources, replay: {burst, refill_secs, sustained_min}, export: {max_rows}}`. Lets a UI build its filter editor against the server's source of truth without duplicating allow-lists.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/audit/events</code>
</div>
<div class="api-endpoint__body" markdown>
Paginated audit events. Query: `from`, `to` (RFC 3339), `tool`, `user`, `session`, `success`, `q`, `limit`, `offset`, plus the JSONB filters described below.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/audit/events/{id}</code>
</div>
<div class="api-endpoint__body" markdown>
Single event by id (UUID); includes the captured payload row when present. `400` on a non-UUID id, `404` when the event isn't recorded.
</div>
</div>

<div class="api-endpoint api-endpoint--post" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--post">POST</span>
<code class="api-endpoint__path">/api/v1/portal/audit/events/{id}/replay</code>
</div>
<div class="api-endpoint__body" markdown>
Re-invokes the captured tool call through an in-process MCP client. Writes a new audit event tagged `source=portal-replay` with `replayed_from` pointing at `{id}`. Per-identity rate limited (5 burst, 1 token / 12s); returns `429 Too Many Requests` with `Retry-After` when exhausted. Tokens are consumed *after* validation passes, so a click on a non-replayable row (no payload, redacted params, missing tool) returns `400` without burning the operator's budget. CSRF-gated via `X-Requested-With`.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/audit/export</code>
</div>
<div class="api-endpoint__body" markdown>
NDJSON stream of summary rows for a filter. `format=jsonl` (default) is the only supported format. Same filter surface as `/events`. Capped at 100,000 rows per request.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/audit/stream</code>
</div>
<div class="api-endpoint__body" markdown>
SSE live tail of new audit events. One `event: audit\ndata: <event JSON>` per write; opening comment `: connected` confirms the connection; `: keepalive` every 30 seconds. Sets `X-Accel-Buffering: no` for nginx-fronted deployments.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/audit/timeseries</code>
</div>
<div class="api-endpoint__body" markdown>
Bucketed counts. Query: `from`, `to`, `bucket` (Go duration).
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/audit/breakdown</code>
</div>
<div class="api-endpoint__body" markdown>
Group-by aggregations. Query: `by` (`tool` / `user` / `success` / `auth_type`).
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/dashboard</code>
</div>
<div class="api-endpoint__body" markdown>
1-hour stats + recent activity.
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/portal/wellknown</code>
</div>
<div class="api-endpoint__body" markdown>
Pretty rendering of the protected-resource metadata.
</div>
</div>

</div>

### JSONB path filters

`/audit/events` and `/audit/export` accept additional query parameters that compile to JSONB containment predicates against the `audit_payloads` sibling row. Filters are AND-combined with each other and with the indexed-column filters above.

<div class="def-cards" markdown>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">param.&lt;dotted.path&gt;=&lt;value&gt;</code></div>
<div class="def-card__body" markdown>
Compiles to `audit_payloads.request_params @> {"<path>": <value>}`.

Example: `?param.user.id=alice`
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">response.&lt;dotted.path&gt;=&lt;value&gt;</code></div>
<div class="def-card__body" markdown>
Compiles to `audit_payloads.response_result @> {"<path>": <value>}`.

Example: `?response.isError=true`
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">header.&lt;name&gt;=&lt;value&gt;</code></div>
<div class="def-card__body" markdown>
Compiles to `audit_payloads.request_headers @> {"<name>": ["<value>"]}` (single-segment name only).

Example: `?header.User-Agent=curl/8.0`
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">has=&lt;column&gt;</code></div>
<div class="def-card__body" markdown>
`audit_payloads.<column>` is `IS NOT NULL` and the column's text representation is not one of `'{}'`, `'[]'`, `'null'`, or `''`.

Example: `?has=response_error`
</div>
</div>

</div>

Allowed `has=` columns: `request_params`, `request_headers`, `response_result`, `response_error`, `notifications`, `replayed_from`. Anything else is silently dropped. Note: a JSONB column literally storing the JSON string `""` (rendered as `'""'::text`, four characters) does pass the filter; the exclusion list is canonical empty containers and an empty TEXT column, not "all logically empty values."

**Value type detection.** Bare values on `param.*` and `response.*` filters are type-detected before the containment doc is built: `true` / `false` become JSON booleans, integers and floats become numbers, everything else is a string. Force a literal string with quotes: `?param.code="200"` matches the JSON string `"200"`, while `?param.code=200` matches the JSON number `200`. **Header values** (`header.*`) are always treated as strings since HTTP header values are strings on the wire; type detection does not apply there.

**Header name canonicalization.** `header.<name>` canonicalizes the header name to the standard Go form before matching, so `?header.user-agent=curl/8.0` matches the same row as `?header.User-Agent=curl/8.0`.

**Header paths must be a single segment.** `?header.X-Foo.bar=v` is silently dropped at parse time; HTTP headers are flat name → values, no nesting. Use `param.*` or `response.*` for nested-path matches.

**Empty-segment paths.** `?param.a..b=v` (a path with an empty segment) cannot match any real payload and is silently dropped at parse time.

**Index use.** The `request_params` and `response_result` columns carry `jsonb_path_ops` GIN indexes; the `@>` operator hits them directly. `request_headers` is unindexed today, so `header.*` filters scan the matching subset and are best paired with a time-range filter. The `has=` filter is a NOT-NULL plus non-empty-content check on a JSONB or TEXT column; the planner can use a partial index on the column when present, otherwise it scans.

## Admin API (mutating)

Same auth requirements. Per the project decision, any authenticated
caller can call these.

<div class="api-endpoints" markdown>

<div class="api-endpoint api-endpoint--post" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--post">POST</span>
<code class="api-endpoint__path">/api/v1/admin/keys</code>
</div>
<div class="api-endpoint__body" markdown>
Mint a new DB-backed API key.
<dl class="api-endpoint__meta">
<dt>Body</dt><dd><code>{ "name": "...", "description": "..." }</code></dd>
<dt>Returns</dt><dd><code>{ "key": {...}, "plaintext": "mt_..." }</code> (plaintext shown once).</dd>
</dl>
</div>
</div>

<div class="api-endpoint api-endpoint--get" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--get">GET</span>
<code class="api-endpoint__path">/api/v1/admin/keys</code>
</div>
<div class="api-endpoint__body" markdown>
List all DB-backed API keys.
<dl class="api-endpoint__meta">
<dt>Returns</dt><dd><code>{ "keys": [...] }</code> (no plaintext).</dd>
</dl>
</div>
</div>

<div class="api-endpoint api-endpoint--delete" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--delete">DELETE</span>
<code class="api-endpoint__path">/api/v1/admin/keys/{name}</code>
</div>
<div class="api-endpoint__body" markdown>
Revoke a DB-backed API key by name.
<dl class="api-endpoint__meta">
<dt>Returns</dt><dd><code>204 No Content</code>.</dd>
</dl>
</div>
</div>

<div class="api-endpoint api-endpoint--post" markdown>
<div class="api-endpoint__head">
<span class="api-endpoint__method api-endpoint__method--post">POST</span>
<code class="api-endpoint__path">/api/v1/admin/tryit/{name}</code>
</div>
<div class="api-endpoint__body" markdown>
Invoke a registered tool through the in-process MCP client; the call lands in the audit log tagged `source=portal-tryit`.
<dl class="api-endpoint__meta">
<dt>Body</dt><dd><code>{ "arguments": { ... } }</code></dd>
<dt>Returns</dt><dd>The MCP <code>CallToolResult</code> (content + structuredContent + isError).</dd>
</dl>
</div>
</div>

</div>

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
