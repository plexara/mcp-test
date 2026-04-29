CREATE TABLE IF NOT EXISTS api_keys (
    id            TEXT PRIMARY KEY,
    name          TEXT UNIQUE NOT NULL,
    hash          TEXT NOT NULL,
    description   TEXT,
    created_by    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS audit_events (
    id             TEXT PRIMARY KEY,
    ts             TIMESTAMPTZ NOT NULL,
    duration_ms    BIGINT NOT NULL,
    request_id     TEXT,
    session_id     TEXT,
    user_subject   TEXT,
    user_email     TEXT,
    auth_type      TEXT,
    api_key_name   TEXT,
    tool_name      TEXT NOT NULL,
    tool_group     TEXT,
    parameters     JSONB,
    success        BOOLEAN NOT NULL,
    error_message  TEXT,
    error_category TEXT,
    request_chars  INTEGER,
    response_chars INTEGER,
    content_blocks INTEGER,
    transport      TEXT NOT NULL,
    source         TEXT NOT NULL,
    remote_addr    TEXT,
    user_agent     TEXT
);

CREATE INDEX IF NOT EXISTS audit_events_ts_idx      ON audit_events (ts DESC);
CREATE INDEX IF NOT EXISTS audit_events_tool_idx    ON audit_events (tool_name, ts DESC);
CREATE INDEX IF NOT EXISTS audit_events_user_idx    ON audit_events (user_subject, ts DESC);
CREATE INDEX IF NOT EXISTS audit_events_session_idx ON audit_events (session_id, ts DESC);
CREATE INDEX IF NOT EXISTS audit_events_success_idx ON audit_events (success, ts DESC);
