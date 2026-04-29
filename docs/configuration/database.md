# Database & migrations

mcp-test uses PostgreSQL for two things: the audit-event log and the
bcrypt-hashed API-key store.

## Connection

Configure a standard PostgreSQL DSN.

```yaml
database:
  url: "postgres://mcp:mcp@localhost:5432/mcp_test?sslmode=disable"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 1h
```

The binary uses the [pgx/v5](https://github.com/jackc/pgx) driver
through pgxpool. There's no support for other engines (MySQL, SQLite)
and we don't intend to add it; the audit aggregations rely on
`percentile_cont` and `FILTER` clauses that aren't portable.

## Migrations

Migrations are embedded into the binary and applied automatically at
startup before any other initialization. There's no separate
`migrate` step.

The migration source uses [golang-migrate](https://github.com/golang-migrate/migrate)
with the pgx/v5 driver. mcp-test rewrites `postgres://` DSNs to
`pgx5://` internally because that's the scheme the driver registers
under.

### Schema

#### `api_keys`

Bcrypt-hashed API keys with optional expiry.

```sql
CREATE TABLE api_keys (
    id            TEXT PRIMARY KEY,
    name          TEXT UNIQUE NOT NULL,
    hash          TEXT NOT NULL,         -- bcrypt
    description   TEXT,
    created_by    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ
);
```

#### `audit_events`

One row per `tools/call`. Plus auth failures and the portal's Try-It
proxy invocations (tagged `source=portal-tryit`).

```sql
CREATE TABLE audit_events (
    id             TEXT PRIMARY KEY,
    ts             TIMESTAMPTZ NOT NULL,
    duration_ms    BIGINT NOT NULL,
    request_id     TEXT,
    session_id     TEXT,
    user_subject   TEXT,
    user_email     TEXT,
    auth_type      TEXT,        -- oidc | apikey | anonymous
    api_key_name   TEXT,
    tool_name      TEXT NOT NULL,
    tool_group     TEXT,
    parameters     JSONB,       -- sanitized (redact_keys applied)
    success        BOOLEAN NOT NULL,
    error_message  TEXT,
    error_category TEXT,
    request_chars  INTEGER,
    response_chars INTEGER,
    content_blocks INTEGER,
    transport      TEXT NOT NULL,   -- "http"
    source         TEXT NOT NULL,   -- "mcp" | "portal-tryit"
    remote_addr    TEXT,
    user_agent     TEXT
);

CREATE INDEX audit_events_ts_idx      ON audit_events (ts DESC);
CREATE INDEX audit_events_tool_idx    ON audit_events (tool_name, ts DESC);
CREATE INDEX audit_events_user_idx    ON audit_events (user_subject, ts DESC);
CREATE INDEX audit_events_session_idx ON audit_events (session_id, ts DESC);
CREATE INDEX audit_events_success_idx ON audit_events (success, ts DESC);
```

## Operating with the schema

### Manual queries

The portal exposes the audit log via REST, but you can hit Postgres
directly for analyses the portal doesn't cover:

```sql
-- Tool call rate per minute, last hour
SELECT date_trunc('minute', ts) AS bucket, count(*)
FROM audit_events
WHERE ts >= now() - interval '1 hour'
GROUP BY bucket
ORDER BY bucket;

-- Slowest 20 calls in the last day
SELECT ts, tool_name, user_subject, duration_ms, success
FROM audit_events
WHERE ts >= now() - interval '1 day'
ORDER BY duration_ms DESC
LIMIT 20;
```

### Retention

`audit.retention_days` is documented (default `30`) but mcp-test
does not auto-prune. Schedule a cron job to delete old rows:

```sql
DELETE FROM audit_events WHERE ts < now() - interval '30 days';
```

### Backup

Standard `pg_dump` works. The audit log can grow quickly at high
call rates; expect ~500 bytes per row (variable based on the
sanitized parameter payload) plus index overhead. For deployments
with sustained traffic, partition by month or move to a dedicated
audit-only database.

## Local dev

The bundled `docker-compose.dev.yml` ships a Postgres 16 container
with a volume:

```bash
make dev-up      # starts postgres + keycloak
make dev-down    # stops them, keeps volumes
docker compose -f docker-compose.dev.yml down -v   # also clears volumes
```

The dev config defaults the DSN to
`postgres://mcp:mcp@localhost:5432/mcp_test?sslmode=disable`. If you
already run a local Postgres on 5432, override `MCPTEST_DATABASE_URL`
or stop the bundled one.
