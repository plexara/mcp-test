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

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.name</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">mcp-test</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Reported in the MCP `initialize` response and in the audit log.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.address</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">:8080</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Listen address; `MCPTEST_PORT` env interpolation common.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.base_url</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">http://localhost:&lt;port&gt;</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Public origin used for the protected-resource metadata document and OIDC redirect URIs. Set this to your real hostname behind TLS terminators.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.instructions</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><span class="config-key__chip-value">project default</span></span>
</div>
</div>
<div class="config-key__body" markdown>
Returned to MCP clients via `initialize.result.instructions`. Most clients pass this to the LLM as system context. See [Server Instructions](instructions.md) for the shipped default.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.read_header_timeout</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">duration</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">10s</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Standard `http.Server.ReadHeaderTimeout`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.shutdown.grace_period</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">duration</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">25s</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Maximum time `http.Server.Shutdown` waits for in-flight requests during drain.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.shutdown.pre_shutdown_delay</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">duration</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">2s</code></span>
</div>
</div>
<div class="config-key__body" markdown>
After SIGINT / SIGTERM, the server flips `/readyz` to 503 and sleeps this long before starting the shutdown so load balancers notice.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.tls.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
If true, listens with TLS using the cert / key files below. Most deployments terminate TLS upstream and leave this false.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.tls.cert_file</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">""</code></span>
</div>
</div>
<div class="config-key__body" markdown>
PEM-encoded certificate path.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.tls.key_file</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">""</code></span>
</div>
</div>
<div class="config-key__body" markdown>
PEM-encoded private key path.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.streamable.session_timeout</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">duration</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">30m</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Idle MCP session timeout passed to `mcp.StreamableHTTPOptions`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.streamable.stateless</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
If true, the SDK does not validate `Mcp-Session-Id` and uses ephemeral sessions. Useful behind external session stores; we don't ship one.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">server.streamable.json_response</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
If true, responses are `application/json` instead of `text/event-stream`.
</div>
</div>

</div>

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

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Master toggle. With it off, only API keys (file / DB) and anonymous mode (if enabled) authenticate.
</div>
</div>

<div class="config-key config-key--required" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.issuer</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--required"><span class="config-key__chip-label">required when</span><span class="config-key__chip-value">oidc.enabled</span></span>
</div>
</div>
<div class="config-key__body" markdown>
The IdP's issuer URL. mcp-test fetches `<issuer>/.well-known/openid-configuration` to find the JWKS and authorization endpoints.
</div>
</div>

<div class="config-key config-key--required" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.audience</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--required"><span class="config-key__chip-label">required when</span><span class="config-key__chip-value">oidc.enabled</span></span>
</div>
</div>
<div class="config-key__body" markdown>
Required `aud` claim value. Tokens that don't carry this audience are rejected.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.client_id</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">""</code></span>
</div>
</div>
<div class="config-key__body" markdown>
The OIDC client ID used by the browser PKCE login flow.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.client_secret</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">""</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Optional. Confidential clients should set this; public PKCE clients leave it empty.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.allowed_clients</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">[]string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">[]</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Optional `azp` / `client_id` allowlist. Empty means any client of the issuer is accepted.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.clock_skew_seconds</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">int</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">30</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Leeway applied when validating `exp` / `iat` / `nbf`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.jwks_cache_ttl</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">duration</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">1h</code></span>
</div>
</div>
<div class="config-key__body" markdown>
How long fetched JWKS keys are cached. The cache transparently refreshes on any unknown `kid`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">oidc.skip_signature_verification</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Trust the IdP's TLS without verifying JWT signatures. Refused unless `MCPTEST_INSECURE=1` is set in the environment.
</div>
</div>

</div>

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

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">api_keys.file</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">[]entry</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">[]</code></span>
</div>
</div>
<div class="config-key__body" markdown>
List of `{name, key, description}` entries. The plaintext `key` is constant-time compared against the inbound `X-API-Key` header. Empty `key` values are skipped (so an unset env var doesn't enable an empty credential).
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">api_keys.db.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
If true, the binary opens the bcrypt-hashed `api_keys` Postgres table for read+write. The portal's API Keys page manages entries.
</div>
</div>

</div>

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

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">auth.allow_anonymous</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
If true, missing credentials on `/mcp` resolve to a synthetic Anonymous identity. Useful for some gateway tests where you want to validate header pass-through without auth in the way. The portal still requires a credential.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">auth.require_for_mcp</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">true</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Gate the `/` endpoint. Currently the auth gateway checks for credential *presence* and 401s without one (unless anonymous is allowed).
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">auth.require_for_portal</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">true</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Gate every `/portal/*` and `/api/v1/*` route. The portal auth middleware is independent of `allow_anonymous`.
</div>
</div>

</div>

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

<div class="config-keys" markdown>

<div class="config-key config-key--required" markdown>
<div class="config-key__head">
<code class="config-key__name">database.url</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--required"><span class="config-key__chip-label">required</span><span class="config-key__chip-value">always</span></span>
</div>
</div>
<div class="config-key__body" markdown>
A PostgreSQL DSN that the pgx driver understands. The migration runner rewrites `postgres://` to `pgx5://` internally so the golang-migrate driver picks it up.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">database.max_open_conns</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">int</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">25</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Max active connections in the pgxpool.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">database.max_idle_conns</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">int</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">5</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Min idle connections held open.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">database.conn_max_lifetime</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">duration</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">1h</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Recycle connections older than this.
</div>
</div>

</div>

See [Database & Migrations](database.md) for the schema details.

## audit

Audit-log behavior.

```yaml
audit:
  enabled: true
  retention_days: 30
  redact_keys: [password, token, secret, authorization, cookie, api_key, credentials]
```

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">audit.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">true</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Disables the audit pipeline entirely when false (no rows written, no portal data).
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">audit.retention_days</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">int</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">30</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Documented retention target. mcp-test does not currently auto-prune; deploy a cron job against the `audit_events` table if you need it.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">audit.redact_keys</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">[]string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">[password, token, secret, authorization, api_key, credentials]</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Case-insensitive substring match. Any tool-call argument key matching one of these gets its value replaced with `[redacted]` before the row is written.
</div>
</div>

</div>

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

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">portal.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Master toggle. Disabling skips loading the session store and mounting the portal / admin APIs and SPA.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">portal.cookie_name</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">mcp_test_session</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Name of the HMAC-signed session cookie.
</div>
</div>

<div class="config-key config-key--required" markdown>
<div class="config-key__head">
<code class="config-key__name">portal.cookie_secret</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--required"><span class="config-key__chip-label">required when</span><span class="config-key__chip-value">portal.enabled</span></span>
</div>
</div>
<div class="config-key__body" markdown>
At least 16 bytes, 32+ recommended. HMAC key for cookie signing.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">portal.cookie_secure</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">true</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Sets the `Secure` cookie attribute. Leave on in production; turn off for local HTTP-only dev.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">portal.oidc_redirect_path</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">string</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">/portal/auth/callback</code></span>
</div>
</div>
<div class="config-key__body" markdown>
OIDC redirect URI path. Whatever you set must match what the IdP has registered for the portal client.
</div>
</div>

</div>

## tools

Per-toolkit enable flags.

```yaml
tools:
  identity:  { enabled: true }
  data:      { enabled: true }
  failure:   { enabled: true }
  streaming: { enabled: true }
```

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">tools.identity.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
`whoami`, `echo`, `headers`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">tools.data.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
`fixed_response`, `sized_response`, `lorem`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">tools.failure.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
`error`, `slow`, `flaky`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">tools.streaming.enabled</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">type</span><span class="config-key__chip-value">bool</span></span>
<span class="config-key__chip config-key__chip--default"><span class="config-key__chip-label">default</span><code class="config-key__chip-value">false</code></span>
</div>
</div>
<div class="config-key__body" markdown>
`progress`, `long_output`, `chatty`.
</div>
</div>

</div>

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
