---
title: Environment variables
description: MCPTEST_* environment variables and how they map onto the YAML config keys via Loader interpolation.
---

# Environment variables

mcp-test prefers YAML for configuration but every value can be
overridden via an environment variable thanks to `${VAR}` and
`${VAR:-default}` interpolation in the loaded config.

## Convention

The convention is `MCPTEST_<UPPER_SNAKE>` for project-specific values,
plus a few un-prefixed standard names (`LOG_LEVEL`).

## Common overrides

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">LOG_LEVEL</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">used by</span><span class="config-key__chip-value">binary</span></span>
</div>
</div>
<div class="config-key__body" markdown>
`debug`, `info` (default), `warn`, `error`. Sets the slog log level for the whole process.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_PORT</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">server.address</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">8080</code></span>
</div>
</div>
<div class="config-key__body" markdown>
The live config interpolates as `:${MCPTEST_PORT:-8080}`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_BASE_URL</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">server.base_url</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">https://mcp-test.example.com</code></span>
</div>
</div>
<div class="config-key__body" markdown>
The public origin clients reach the server at. Used in OIDC redirect URIs and the protected-resource metadata document.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_DATABASE_URL</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">database.url</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">postgres://mcp:mcp@localhost:5432/mcp_test?sslmode=disable</code></span>
</div>
</div>
<div class="config-key__body" markdown>
A pgx-compatible Postgres DSN.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_DEV_KEY</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">api_keys.file[0].key</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">devkey-please-change</code></span>
</div>
</div>
<div class="config-key__body" markdown>
The bootstrap file API key. Sent as `X-API-Key`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_OIDC_ISSUER</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">oidc.issuer</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">http://localhost:8081/realms/mcp-test</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Required when OIDC is enabled.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_OIDC_AUDIENCE</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">oidc.audience</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">mcp-test</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Required `aud` claim value.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_OIDC_CLIENT_ID</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">oidc.client_id</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">mcp-test-portal</code></span>
</div>
</div>
<div class="config-key__body" markdown>
The OIDC client ID used by the browser PKCE login flow.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_OIDC_CLIENT_SECRET</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">oidc.client_secret</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Confidential clients only; public PKCE clients leave it empty.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_COOKIE_SECRET</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">maps to</span><code class="config-key__chip-value">portal.cookie_secret</code></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><span class="config-key__chip-value">32+ bytes, base64</span></span>
</div>
</div>
<div class="config-key__body" markdown>
HMAC key for the portal session cookie. Generate with `openssl rand -base64 32`.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">MCPTEST_INSECURE</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">used by</span><span class="config-key__chip-value">OIDC skip-signature gate</span></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">1</code></span>
</div>
</div>
<div class="config-key__body" markdown>
Set to `1` to allow `oidc.skip_signature_verification` (refused otherwise).
</div>
</div>

</div>

## Interpolation rules

- `${VAR}` is replaced with the env value, or empty string if unset.
- `${VAR:-default}` is replaced with the env value, or `default` if
  unset or empty.
- Plain `$VAR` (no braces) is **not** interpolated. This is
  intentional so DSNs and other shell-shaped values round-trip.
- The substitution happens on the raw YAML string before parsing, so
  it works for any field type.

## Composing in compose

The bundled `docker-compose.dev.yml` shows the typical pattern: the
binary container's `environment:` block sets the variables and the
config file references them.

```yaml
# docker-compose.dev.yml
mcp-test:
  environment:
    MCPTEST_DATABASE_URL: postgres://mcp:mcp@postgres:5432/mcp_test?sslmode=disable
    MCPTEST_BASE_URL: http://localhost:8080
    MCPTEST_OIDC_ISSUER: http://keycloak:8080/realms/mcp-test
    MCPTEST_OIDC_AUDIENCE: mcp-test
    MCPTEST_OIDC_CLIENT_ID: mcp-test-portal
    MCPTEST_COOKIE_SECRET: dev-cookie-secret-not-for-production-use-dev
    MCPTEST_DEV_KEY: devkey-please-change
  command: ["--config", "/app/configs/mcp-test.dev.yaml"]
```

## Worth knowing

- The binary doesn't read `.env` files. Use your shell, container
  runtime, or systemd unit to set variables.
- Validation runs *after* interpolation, so a missing required
  variable surfaces as a config error rather than a YAML parse error.
- Secrets in env vars show up in process listings on shared hosts.
  For production, prefer a config file mounted from a secret manager.
