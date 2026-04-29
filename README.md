# mcp-test

A Model Context Protocol (MCP) test server, written in Go. Built primarily as
a controllable fixture for exercising MCP gateways (notably Plexara's), and
secondarily as a small reference implementation of a best-practices Go MCP
server using the official [`modelcontextprotocol/go-sdk`][sdk].

[sdk]: https://github.com/modelcontextprotocol/go-sdk

## Features

- **HTTP Streamable transport** at `/` (no stdio). Browsers hitting the root
  are redirected to `/portal/`.
- **Auth**: file-based API keys (constant-time compare), bcrypt-hashed
  Postgres-backed API keys, and external OIDC delegation (JWKS-cached
  validator). RFC 9728 protected-resource metadata at
  `/.well-known/oauth-protected-resource`; 401s include
  `WWW-Authenticate: Bearer resource_metadata=...` so MCP clients can
  discover the issuer.
- **Postgres-backed audit log** of every tool call (sanitized parameters,
  identity, latency, response size, content blocks, transport, source).
- **Test tools** designed to exercise gateway behavior:
  - `identity`; `whoami`, `echo`, `headers`
  - `data`; `fixed_response`, `sized_response`, `lorem`
  - `failure`; `error`, `slow`, `flaky`
  - `streaming`; `progress`, `long_output`, `chatty`
- **Web portal**; React 19 + Vite + Tailwind 4 SPA embedded into the
  binary via `go:embed`. Pages: Dashboard, Tools (with Try-It), Audit,
  API Keys, Config, Discovery (`.well-known` viewer). Browser auth is OIDC
  PKCE; API auth is `X-API-Key` or bearer.

## Quickstart

```bash
make dev
```

That brings up Postgres + Keycloak in Docker, waits for both to be ready,
builds the SPA into the embed dir if it's missing, and runs the binary in
the foreground. When it's up:

| URL | What |
|---|---|
| http://localhost:8080/portal/ | Portal; sign in via OIDC (`dev`/`dev`) or paste API key |
| http://localhost:8080/         | MCP streamable HTTP endpoint (browsers redirect to portal) |
| http://localhost:8081/         | Keycloak (`admin`/`admin`) |

API key for testing: `devkey-please-change` (override with `MCPTEST_DEV_KEY=...`).

`make dev-anon` is a faster alternative that skips Keycloak and runs in
anonymous mode; good for exercising the gateway without auth in the way.

The bundled Keycloak realm (`dev/keycloak/mcp-test-realm.json`) is
pre-seeded with realm `mcp-test`, public PKCE client `mcp-test-portal`,
service client `mcp-test`, and user `dev` / `dev`.

### Connecting Claude Code (or another MCP client)

`.mcp.json` at the repo root tells Claude Code how to reach the running
server over streamable HTTP with the dev API key. After `make dev` is up,
restart Claude Code in this directory and approve the server when
prompted; all 12 tools become available.

For other clients, the endpoint is `http://localhost:8080/` with header
`X-API-Key: devkey-please-change` (or `Authorization: Bearer <jwt>` from
Keycloak).

## Configuration

See [`configs/mcp-test.example.yaml`](configs/mcp-test.example.yaml) for the
full surface. All values support `${VAR}` and `${VAR:-default}` interpolation
against the environment.

## Tests

```bash
go test ./...                       # unit + in-memory MCP + portal API
go test -tags integration ./...     # adds testcontainers Postgres + HTTP roundtrip (needs Docker)
make ui && go test ./tests/...      # includes SPA embed assertions
```

## Layout

```
cmd/mcp-test     # binary entry: flags, config, boot, shutdown
internal/server  # composes config -> DB -> MCP server -> HTTP mux
internal/ui      # go:embed of ui/dist
ui/              # React 19 + Vite SPA source
pkg/apikeys      # bcrypt-hashed Postgres API keys
pkg/audit        # event + Logger interface; Postgres + memory implementations
pkg/auth         # Identity, ctx helpers, file/DB API keys, JWKS-cached OIDC, chain
pkg/config       # YAML loader with env interpolation + validation
pkg/database     # pgxpool + golang-migrate
pkg/httpsrv      # mux pieces: auth gateway, portal API, admin API, browser auth, SPA
pkg/mcpmw        # MCP method middleware (audit + identity)
pkg/tools        # Toolkit interface + per-group toolkits (identity/data/failure/streaming)
tests/           # in-memory + HTTP + portal + SPA + integration (build-tagged)
```

## License

[Apache 2.0](LICENSE).

---

Open source by [Plexara](https://plexara.io), the commercial MCP server with
configurable enrichment built in. mcp-test is what Plexara uses to verify its
gateway behavior end-to-end; it ships as OSS so anyone building MCP
integrations can use the same fixture.
