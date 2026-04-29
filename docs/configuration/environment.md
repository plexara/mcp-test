# Environment variables

mcp-test prefers YAML for configuration but every value can be
overridden via an environment variable thanks to `${VAR}` and
`${VAR:-default}` interpolation in the loaded config.

## Convention

The convention is `MCPTEST_<UPPER_SNAKE>` for project-specific values,
plus a few un-prefixed standard names (`LOG_LEVEL`).

## Common overrides

| Variable | Used by | Example |
| --- | --- | --- |
| `LOG_LEVEL` | Binary | `debug`, `info` (default), `warn`, `error`. |
| `MCPTEST_PORT` | `server.address` | `8080` |
| `MCPTEST_BASE_URL` | `server.base_url` | `https://mcp-test.example.com` |
| `MCPTEST_DATABASE_URL` | `database.url` | `postgres://mcp:mcp@localhost:5432/mcp_test?sslmode=disable` |
| `MCPTEST_DEV_KEY` | `api_keys.file[0].key` | `devkey-please-change` |
| `MCPTEST_OIDC_ISSUER` | `oidc.issuer` | `http://localhost:8081/realms/mcp-test` |
| `MCPTEST_OIDC_AUDIENCE` | `oidc.audience` | `mcp-test` |
| `MCPTEST_OIDC_CLIENT_ID` | `oidc.client_id` | `mcp-test-portal` |
| `MCPTEST_OIDC_CLIENT_SECRET` | `oidc.client_secret` | (confidential clients only) |
| `MCPTEST_COOKIE_SECRET` | `portal.cookie_secret` | 32+ bytes, base64 |
| `MCPTEST_INSECURE` | OIDC skip-signature gate | `1` to allow `oidc.skip_signature_verification` |

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
