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
| `GET` | `/api/v1/portal/audit/events` | Paginated audit events. Query: `from`, `to` (RFC 3339), `tool`, `user`, `session`, `success`, `q`, `limit`, `offset`. |
| `GET` | `/api/v1/portal/audit/timeseries` | Bucketed counts. Query: `from`, `to`, `bucket` (Go duration). |
| `GET` | `/api/v1/portal/audit/breakdown` | Group-by aggregations. Query: `by` (`tool`/`user`/`success`/`auth_type`). |
| `GET` | `/api/v1/portal/dashboard` | 1-hour stats + recent activity. |
| `GET` | `/api/v1/portal/wellknown` | Pretty rendering of the protected-resource metadata. |

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
