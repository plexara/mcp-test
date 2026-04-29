# Architecture

A short tour of how the binary is wired together. Useful if you're
contributing or building something similar.

## Package layout

```
cmd/mcp-test/      # binary entry point: flags, config, boot, shutdown
internal/server/   # composes config → DB → MCP server → HTTP mux
internal/ui/       # //go:embed all:dist scaffold for the SPA
ui/                # React 19 + Vite SPA source
pkg/apikeys/       # bcrypt-hashed Postgres API key store
pkg/audit/         # event model, Logger interface, memory + Postgres
pkg/auth/          # Identity, ctx helpers, file/DB keys, OIDC, chain
pkg/build/         # build-time stamped Version/Commit/Date
pkg/config/        # YAML loader with env interpolation + validation
pkg/database/      # pgxpool + golang-migrate
pkg/httpsrv/       # mux pieces: auth gate, sessions, well-known, portal/admin API, SPA
pkg/mcpmw/         # MCP-side middleware (audit + identity)
pkg/tools/         # Toolkit interface + per-group toolkits
tests/             # integration + HTTP + portal + SPA test suites
```

## Boot sequence

`cmd/mcp-test/main.go`:

1. Parse `--config`, `--address`, `--version` flags.
2. `slog` JSON logger keyed off `LOG_LEVEL`.
3. `config.Load(--config)` — parse YAML, expand env vars, apply
   defaults, validate.
4. Signal-driven context (`SIGINT`, `SIGTERM`).
5. `server.Build(ctx, cfg, logger)`:
    1. `migrate.Up(cfg.Database.URL)` — apply embedded migrations.
    2. `database.Open(ctx, cfg.Database)` — pgxpool.
    3. `auditpg.New(pool)` — Postgres audit logger.
    4. `auth.NewFileAPIKeyStore` + `apikeys.New` (Postgres) →
       `auth.CombineKeyStores`.
    5. `auth.NewOIDC(ctx, cfg.OIDC)` if `oidc.enabled`.
    6. `auth.NewChain(allowAnon, keyStore, oidcAuth)`.
    7. `mcp.NewServer(impl, &mcp.ServerOptions{ Instructions })`.
    8. Register enabled toolkits.
    9. `mcpServer.AddReceivingMiddleware(mcpmw.Audit(...))`.
    10. Build HTTP mux:
        - `/healthz`, `/readyz`, `.well-known/*`.
        - `/` → `BrowserRedirect → MCPAuthGateway → StreamableHTTPHandler`.
        - When portal enabled: `/portal/auth/*`,
          `/api/v1/portal/*`, `/api/v1/admin/*`, `/portal/*`.
6. `app.Run(ctx)` — `http.Server.ListenAndServe` in a goroutine,
   wait for ctx, drain (readiness 503 → grace period →
   `http.Server.Shutdown`).

## Auth chain

`pkg/auth.Chain.Authenticate(ctx)`:

```
token := GetToken(ctx)
switch {
case token == "":
    if allowAnonymous { return Anonymous, nil }
    return nil, ErrNotAuthenticated

case strings.HasPrefix(token, "ey"):
    // Looks like a JWT. Try OIDC first.
    if id, err := oidc.ValidateBearer(ctx, token); err == nil {
        return id, nil
    }
    // Fall through to API key.

default:
    if id, err := apikeys.Authenticate(ctx, token); err == nil {
        return id, nil
    }
    // Fall through to OIDC (covers the rare case of a non-ey JWT).
}
```

The chain is set as a context value by the audit middleware before
calling the next handler. Tool handlers read it via
`auth.GetIdentity(ctx)`.

## MCP middleware ordering

```
mcpServer.AddReceivingMiddleware(mcpmw.Audit(...))
```

The SDK applies middleware outside-in. So for a `tools/call`:

1. SDK's session dispatch.
2. `mcpmw.Audit`:
    - If `RequestExtra.Header == nil` (in-memory transport): stamp
      Anonymous, call next, return without logging.
    - Otherwise: extract token, run chain, stamp identity, sanitize
      params, call next, measure result, write audit row.
3. SDK's typed tool dispatch (decode args, call handler).

## HTTP middleware ordering

For `/`:

```
CORS
  → BrowserRedirect (302 browser GETs to /portal/)
    → MCPAuthGateway (401 without credentials, unless anonymous)
      → StreamableHTTPHandler
        → MCP middleware (above)
          → tool handler
```

For `/api/v1/portal/*` and `/api/v1/admin/*`:

```
CORS
  → PortalAuth (cookie OR X-API-Key OR Bearer; 401 otherwise)
    → handler (writeJSON / writeError; 503 if a backing store is disabled)
```

The portal SPA is mounted as a fall-through under `/portal/`; it
doesn't go through PortalAuth (the SPA itself authenticates the
user; the API calls do).

## Audit pipeline

```
Inbound HTTP
  → SDK Extra.Header populated
    → mcpmw.Audit reads token → Identity → ctx
      → SanitizeParameters(args)
        → next(ctx) → tool handler runs
          → measure result (chars, blocks)
            → audit.Logger.Log({...})
```

The Logger interface has two implementations:

- `pkg/audit.MemoryLogger` — used by tests.
- `pkg/audit/postgres.Store` — used by the binary.

Both support `Log`, `Query`, `Count`, `TimeSeries`, `Breakdown`,
`Stats`. The Postgres implementation uses `percentile_cont` for
p50/p95.

## Embedded SPA

`internal/ui/embed.go`:

```go
//go:embed all:dist
var distFS embed.FS

func FS() (fs.FS, error)         // returns dist/ subtree
func Available() bool             // true iff dist/index.html exists
```

`pkg/httpsrv/spa.go` wraps that FS in an `http.Handler` that:

- Serves files at their literal paths (`/portal/assets/index-...js`).
- For paths without an extension that look like client routes, falls
  back to `index.html` so React Router takes over.
- Returns `404` for missing assets (so a missing chunk surfaces as
  an error rather than as the fallback HTML).

The `make ui` target builds the SPA into `internal/ui/dist`. CI and
the release pipeline always run it. `make build` alone doesn't, so
binaries built that way serve a small placeholder page from
`internal/server/server.go` instead.
