---
title: YAML reference
description: Every YAML key in mcp-test config: server, oidc, api_keys, auth, database, audit, portal, tools. Defaults and environment overrides.
---

# YAML reference

Every config key with its type, default, and environment-variable
override. The binary loads `--config <path>` (default
`configs/mcp-test.yaml`), expands `${VAR}` and `${VAR:-default}`
forms against the process environment, applies defaults, and
validates. Plain `$VAR` is intentionally left alone so DSNs and other
shell-shaped values round-trip.

## server

Top-level HTTP listener and lifecycle.

```yaml
server:
  name: mcp-test
  address: ":8080"
  base_url: "http://localhost:8080"
  instructions: "..."        # see configuration/instructions.md
  read_header_timeout: 10s
  shutdown:
    grace_period: 25s
    pre_shutdown_delay: 2s
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
  streamable:
    session_timeout: 30m
    stateless: false
    json_response: false
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `server.name` | string | `mcp-test` | Reported in the MCP `initialize` response and in the audit log. |
| `server.address` | string | `:8080` | Listen address; `MCPTEST_PORT` env interpolation common. |
| `server.base_url` | string | `http://localhost:<port>` | Public origin used for the protected-resource metadata document and OIDC redirect URIs. Set this to your real hostname behind TLS terminators. |
| `server.instructions` | string | (project default, see [Server Instructions](instructions.md)) | Returned to MCP clients via `initialize.result.instructions`. Most clients pass this to the LLM as system context. |
| `server.read_header_timeout` | duration | `10s` | Standard `http.Server.ReadHeaderTimeout`. |
| `server.shutdown.grace_period` | duration | `25s` | Maximum time `http.Server.Shutdown` waits for in-flight requests during drain. |
| `server.shutdown.pre_shutdown_delay` | duration | `2s` | After SIGINT/SIGTERM, the server flips `/readyz` to 503 and sleeps this long before starting the shutdown so load balancers notice. |
| `server.tls.enabled` | bool | `false` | If true, listens with TLS using the cert/key files below. Most deployments terminate TLS upstream and leave this false. |
| `server.tls.cert_file` | string | `""` | PEM-encoded certificate path. |
| `server.tls.key_file` | string | `""` | PEM-encoded private key path. |
| `server.streamable.session_timeout` | duration | `30m` | Idle MCP session timeout passed to `mcp.StreamableHTTPOptions`. |
| `server.streamable.stateless` | bool | `false` | If true, the SDK does not validate `Mcp-Session-Id` and uses ephemeral sessions. Useful behind external session stores; we don't ship one. |
| `server.streamable.json_response` | bool | `false` | If true, responses are `application/json` instead of `text/event-stream`. |

## oidc

External OIDC delegation. When enabled, mcp-test validates incoming
bearer tokens against the issuer's JWKS.

```yaml
oidc:
  enabled: true
  issuer: "http://localhost:8081/realms/mcp-test"
  audience: "mcp-test"
  client_id: "mcp-test-portal"
  client_secret: ""
  allowed_clients: []
  clock_skew_seconds: 30
  jwks_cache_ttl: 1h
  skip_signature_verification: false
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `oidc.enabled` | bool | `false` | Master toggle. With it off, only API keys (file/DB) and anonymous mode (if enabled) authenticate. |
| `oidc.issuer` | string | `""` | Required when enabled. The IdP's issuer URL; mcp-test fetches `<issuer>/.well-known/openid-configuration` to find the JWKS and authorization endpoints. |
| `oidc.audience` | string | `""` | Required `aud` claim value. Tokens that don't carry this audience are rejected. |
| `oidc.client_id` | string | `""` | The OIDC client ID used by the browser PKCE login flow. |
| `oidc.client_secret` | string | `""` | Optional. Confidential clients should set this; public PKCE clients leave it empty. |
| `oidc.allowed_clients` | []string | `[]` | Optional `azp` / `client_id` allowlist. Empty means any client of the issuer is accepted. |
| `oidc.clock_skew_seconds` | int | `30` | Leeway applied when validating `exp` / `iat` / `nbf`. |
| `oidc.jwks_cache_ttl` | duration | `1h` | How long fetched JWKS keys are cached. The cache transparently refreshes on any unknown `kid`. |
| `oidc.skip_signature_verification` | bool | `false` | Trust the IdP's TLS without verifying JWT signatures. Refused unless `MCPTEST_INSECURE=1` is set in the environment. |

See [Authentication](auth.md) for the full identity model and
auth-chain semantics.

## api_keys

Two API-key sources, used in series (file first, DB on miss).

```yaml
api_keys:
  file:
    - name: dev-local
      key: "${MCPTEST_DEV_KEY:-}"
      description: "local dev only"
  db:
    enabled: true
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `api_keys.file` | []entry | `[]` | List of `{name, key, description}` entries. The plaintext `key` is constant-time compared against the inbound `X-API-Key` header. Empty `key` values are skipped (so an unset env var doesn't enable an empty credential). |
| `api_keys.db.enabled` | bool | `false` | If true, the binary opens the bcrypt-hashed `api_keys` Postgres table for read+write. The portal's API Keys page manages entries. |

Both sources contribute to the same auth chain: an inbound API key is
matched against file entries first (cheap O(N) constant-time compare),
then DB entries (bcrypt scan) on miss.

## auth

Top-level auth toggles applied across MCP and portal routes.

```yaml
auth:
  allow_anonymous: false
  require_for_mcp: true
  require_for_portal: true
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.allow_anonymous` | bool | `false` | If true, missing credentials on `/mcp` resolve to a synthetic Anonymous identity. Useful for some gateway tests where you want to validate header pass-through without auth in the way. The portal still requires a credential. |
| `auth.require_for_mcp` | bool | `true` | Gate the `/` endpoint. Currently the auth gateway checks for credential *presence* and 401s without one (unless anonymous is allowed). |
| `auth.require_for_portal` | bool | `true` | Gate every `/portal/*` and `/api/v1/*` route. The portal auth middleware is independent of `allow_anonymous`. |

## database

Postgres connection used for migrations, audit, and DB-backed API
keys.

```yaml
database:
  url: "postgres://mcp:mcp@localhost:5432/mcp_test?sslmode=disable"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 1h
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `database.url` | string | (required) | A PostgreSQL DSN that the pgx driver understands. The migration runner rewrites `postgres://` to `pgx5://` internally so the golang-migrate driver picks it up. |
| `database.max_open_conns` | int | `25` | Max active connections in the pgxpool. |
| `database.max_idle_conns` | int | `5` | Min idle connections held open. |
| `database.conn_max_lifetime` | duration | `1h` | Recycle connections older than this. |

See [Database & Migrations](database.md) for the schema details.

## audit

Audit-log behavior.

```yaml
audit:
  enabled: true
  retention_days: 30
  redact_keys: [password, token, secret, authorization, cookie, api_key, credentials]
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `audit.enabled` | bool | `true` | Disables the audit pipeline entirely when false (no rows written, no portal data). |
| `audit.retention_days` | int | `30` | Documented retention target. mcp-test does not currently auto-prune; deploy a cron job against the `audit_events` table if you need it. |
| `audit.redact_keys` | []string | `[password, token, secret, authorization, api_key, credentials]` | Case-insensitive substring match. Any tool-call argument key matching one of these gets its value replaced with `[redacted]` before the row is written. |

## portal

The embedded React 19 SPA and its session cookie.

```yaml
portal:
  enabled: true
  cookie_name: mcp_test_session
  cookie_secret: "${MCPTEST_COOKIE_SECRET}"
  cookie_secure: true
  oidc_redirect_path: /portal/auth/callback
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `portal.enabled` | bool | `false` | Master toggle. Disabling skips loading the session store and mounting the portal/admin APIs and SPA. |
| `portal.cookie_name` | string | `mcp_test_session` | Name of the HMAC-signed session cookie. |
| `portal.cookie_secret` | string | (required when enabled) | At least 16 bytes, 32+ recommended. HMAC key for cookie signing. |
| `portal.cookie_secure` | bool | `true` | Sets the `Secure` cookie attribute. Leave on in production; turn off for local HTTP-only dev. |
| `portal.oidc_redirect_path` | string | `/portal/auth/callback` | OIDC redirect URI path; whatever you set must match what the IdP has registered for the portal client. |

## tools

Per-toolkit enable flags.

```yaml
tools:
  identity:  { enabled: true }
  data:      { enabled: true }
  failure:   { enabled: true }
  streaming: { enabled: true }
```

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `tools.identity.enabled` | bool | `false` | `whoami`, `echo`, `headers`. |
| `tools.data.enabled` | bool | `false` | `fixed_response`, `sized_response`, `lorem`. |
| `tools.failure.enabled` | bool | `false` | `error`, `slow`, `flaky`. |
| `tools.streaming.enabled` | bool | `false` | `progress`, `long_output`, `chatty`. |

A toolkit must be enabled in config for its tools to be registered
with the MCP server. The example and dev configs enable all four.

## Validation

`config.Validate()` fails fast at startup on impossible setups:

- `database.url` must be set.
- `portal.cookie_secret` is required when `portal.enabled=true`.
- `oidc.issuer` is required when `oidc.enabled=true`.
- `oidc.skip_signature_verification` requires `MCPTEST_INSECURE=1` in
  the environment.
- At least one auth method must be enabled (OIDC, file API keys, DB
  API keys, or anonymous).

The binary refuses to start if any of these checks fail and prints a
human-readable summary listing every problem.
