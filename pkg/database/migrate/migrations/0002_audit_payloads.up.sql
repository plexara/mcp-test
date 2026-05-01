-- Adds the sibling audit_payloads table that carries the full request and
-- response detail for an audit_events row. Two-table model keeps the
-- summary row (used for indexed time/user/tool queries) free of multi-KB
-- JSONB blobs; the detail join only runs when an operator drills into a
-- single event in the portal.
--
-- Cascade delete keeps retention cleanup atomic: deleting an audit_events
-- row drops its payload row in the same statement, no second policy.

CREATE TABLE IF NOT EXISTS audit_payloads (
    event_id              TEXT PRIMARY KEY REFERENCES audit_events(id) ON DELETE CASCADE,

    -- JSON-RPC envelope as received. Only what the audit middleware can
    -- observe today: the dispatched method and the params object.
    jsonrpc_method        TEXT,
    request_params        JSONB,
    request_size_bytes    INTEGER NOT NULL DEFAULT 0,
    request_truncated     BOOLEAN NOT NULL DEFAULT false,

    -- HTTP layer (best-effort; only what the audit middleware can read
    -- from ctx).
    request_headers       JSONB,
    request_remote_addr   TEXT,

    -- JSON-RPC response.
    response_result       JSONB,
    response_error        JSONB,
    response_size_bytes   INTEGER NOT NULL DEFAULT 0,
    response_truncated    BOOLEAN NOT NULL DEFAULT false,

    -- Notifications fired during the call window: array of {ts, method, params}.
    notifications         JSONB,

    -- Replay linkage; ON DELETE SET NULL so deleting the original doesn't
    -- cascade into the replay's payload row.
    replayed_from         TEXT REFERENCES audit_events(id) ON DELETE SET NULL,

    -- captured_at supports forensic queries ("payloads written between
    -- t1 and t2") that the call's own audit_events.ts can't answer when
    -- the audit pipeline is delayed (async drain backlog).
    captured_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_payloads_replayed_from_idx
    ON audit_payloads (replayed_from)
    WHERE replayed_from IS NOT NULL;

-- jsonb_path_ops indexes: smaller and faster than the default GIN for the
-- @> containment operator we use in the JSONB path filter compiler.
CREATE INDEX IF NOT EXISTS audit_payloads_request_gin
    ON audit_payloads USING gin (request_params jsonb_path_ops);
CREATE INDEX IF NOT EXISTS audit_payloads_response_gin
    ON audit_payloads USING gin (response_result jsonb_path_ops);
