//go:build integration

// Behavioral tests for the Postgres-backed audit store. Tagged
// `integration` so they only run with `go test -tags=integration`;
// they require Docker to spin up the testcontainers Postgres.
//
// These cover the parts of the store that the unit suite can't:
// the actual transactional two-row write, FK cascade behavior, the
// JSONB round-trip through real columns, and the truncation flag
// semantics observed end-to-end.
package auditpg_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/plexara/mcp-test/pkg/audit"
	auditpg "github.com/plexara/mcp-test/pkg/audit/postgres"
	"github.com/plexara/mcp-test/pkg/database/migrate"
)

func TestStore_LogPayload_RoundtripAndCascade(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(ctx, t)
	if err := migrate.Up(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store := auditpg.New(pool)

	// Write an event with a payload. Uses every captured column so
	// the JSONB round-trip is exercised end-to-end.
	ev := audit.Event{
		ID:        "evt-1",
		Timestamp: time.Now().UTC(),
		ToolName:  "echo",
		Transport: "http",
		Source:    "mcp",
		Success:   true,
		Payload: &audit.Payload{
			JSONRPCMethod: "tools/call",
			RequestParams: map[string]any{
				"message": "hello",
				"nested":  map[string]any{"k": "v"},
			},
			RequestSizeBytes: 42,
			RequestHeaders: map[string][]string{
				"User-Agent": {"test"},
				"X-Trace":    {"abc"},
			},
			RequestRemoteAddr: "10.0.0.1",
			ResponseResult: map[string]any{
				"isError": false,
				"content": []any{
					map[string]any{"type": "text", "text": "world"},
				},
			},
			ResponseSizeBytes: 73,
			Notifications: []audit.Notification{
				{
					Timestamp: time.Now().UTC(),
					Method:    "notifications/progress",
					Params:    map[string]any{"step": 1, "total": 5},
				},
			},
			NotificationsTruncated: true,
		},
	}
	if err := store.Log(ctx, ev); err != nil {
		t.Fatalf("Log: %v", err)
	}

	// Read it back.
	got, err := store.GetPayload(ctx, "evt-1")
	if err != nil {
		t.Fatalf("GetPayload: %v", err)
	}
	if got == nil {
		t.Fatal("expected payload, got nil")
	}
	if got.JSONRPCMethod != "tools/call" {
		t.Errorf("JSONRPCMethod = %q", got.JSONRPCMethod)
	}
	if got.RequestParams["message"] != "hello" {
		t.Errorf("request params lost: %+v", got.RequestParams)
	}
	if nested, _ := got.RequestParams["nested"].(map[string]any); nested == nil || nested["k"] != "v" {
		t.Errorf("nested params lost: %+v", got.RequestParams)
	}
	if ua := got.RequestHeaders["User-Agent"]; len(ua) != 1 || ua[0] != "test" {
		t.Errorf("headers lost: %+v", got.RequestHeaders)
	}
	if got.RequestRemoteAddr != "10.0.0.1" {
		t.Errorf("remote_addr lost: %q", got.RequestRemoteAddr)
	}
	if isErr, _ := got.ResponseResult["isError"].(bool); isErr {
		t.Errorf("response isError flipped to true")
	}
	if len(got.Notifications) != 1 || got.Notifications[0].Method != "notifications/progress" {
		t.Errorf("notifications lost: %+v", got.Notifications)
	}
	if !got.NotificationsTruncated {
		t.Errorf("NotificationsTruncated lost: got false, want true")
	}

	// Cascade: deleting the audit_events row should drop the payload.
	if _, err := pool.Exec(ctx, `DELETE FROM audit_events WHERE id = $1`, "evt-1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}
	got, err = store.GetPayload(ctx, "evt-1")
	if err != nil {
		t.Fatalf("GetPayload after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil payload after cascade delete, got: %+v", got)
	}
}

func TestStore_Log_NilPayloadOnlyWritesSummary(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(ctx, t)
	if err := migrate.Up(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store := auditpg.New(pool)

	ev := audit.Event{
		ID:        "evt-2",
		Timestamp: time.Now().UTC(),
		ToolName:  "summary-only",
		Transport: "http",
		Source:    "mcp",
		Success:   true,
		// No Payload.
	}
	if err := store.Log(ctx, ev); err != nil {
		t.Fatalf("Log: %v", err)
	}
	got, err := store.GetPayload(ctx, "evt-2")
	if err != nil {
		t.Fatalf("GetPayload: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil payload, got: %+v", got)
	}

	// Confirm the summary row IS present.
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE id = $1`, "evt-2").Scan(&n)
	if n != 1 {
		t.Errorf("summary row count = %d, want 1", n)
	}
}

func TestStore_JSONFilters_AndHasKeys(t *testing.T) {
	// End-to-end: write events with diverse payloads, then assert the
	// JSONB filter compiler matches the same events the MemoryLogger
	// would. Hits the real GIN(jsonb_path_ops) index path.
	ctx := context.Background()
	url := startPostgres(ctx, t)
	if err := migrate.Up(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store := auditpg.New(pool)

	now := time.Now().UTC()
	mustLog := func(id string, p *audit.Payload, success bool) {
		t.Helper()
		ev := audit.Event{
			ID:        id,
			Timestamp: now,
			ToolName:  "echo",
			Transport: "http",
			Source:    "mcp",
			Success:   success,
			Payload:   p,
		}
		if err := store.Log(ctx, ev); err != nil {
			t.Fatalf("Log %s: %v", id, err)
		}
	}
	mustLog("alice", &audit.Payload{
		JSONRPCMethod:  "tools/call",
		RequestParams:  map[string]any{"message": "hello", "user": map[string]any{"id": "alice"}},
		ResponseResult: map[string]any{"isError": false, "content": []any{map[string]any{"type": "text", "text": "hi"}}},
		RequestHeaders: map[string][]string{"User-Agent": {"curl/8.0"}},
	}, true)
	mustLog("bob", &audit.Payload{
		JSONRPCMethod:  "tools/call",
		RequestParams:  map[string]any{"message": "world", "user": map[string]any{"id": "bob"}},
		ResponseResult: map[string]any{"isError": true},
		ResponseError:  map[string]any{"category": "tool", "message": "boom"},
	}, false)
	mustLog("carol-no-payload", nil, true)

	cases := []struct {
		name   string
		filter audit.QueryFilter
		ids    []string
	}{
		{
			"param.user.id=alice",
			audit.QueryFilter{JSONFilters: []audit.JSONPathFilter{
				{Source: "param", Path: []string{"user", "id"}, Value: "alice"},
			}},
			[]string{"alice"},
		},
		{
			"response.isError=true",
			audit.QueryFilter{JSONFilters: []audit.JSONPathFilter{
				{Source: "response", Path: []string{"isError"}, Value: "true"},
			}},
			[]string{"bob"},
		},
		{
			"header.User-Agent=curl/8.0",
			audit.QueryFilter{JSONFilters: []audit.JSONPathFilter{
				{Source: "header", Path: []string{"User-Agent"}, Value: "curl/8.0"},
			}},
			[]string{"alice"},
		},
		{
			"has=response_error",
			audit.QueryFilter{HasKeys: []string{"response_error"}},
			[]string{"bob"},
		},
		{
			"has=request_headers",
			audit.QueryFilter{HasKeys: []string{"request_headers"}},
			[]string{"alice"},
		},
		{
			"AND: param.user.id=alice + has=request_headers",
			audit.QueryFilter{
				JSONFilters: []audit.JSONPathFilter{{Source: "param", Path: []string{"user", "id"}, Value: "alice"}},
				HasKeys:     []string{"request_headers"},
			},
			[]string{"alice"},
		},
		{
			"no-match path",
			audit.QueryFilter{JSONFilters: []audit.JSONPathFilter{
				{Source: "param", Path: []string{"user", "id"}, Value: "nobody"},
			}},
			nil,
		},
	}
	for _, c := range cases {
		evs, err := store.Query(ctx, c.filter)
		if err != nil {
			t.Errorf("%s: Query err: %v", c.name, err)
			continue
		}
		var ids []string
		for _, ev := range evs {
			ids = append(ids, ev.ID)
		}
		if !equalStringsAny(ids, c.ids) {
			t.Errorf("%s: ids = %v, want %v", c.name, ids, c.ids)
		}
	}
}

func TestStore_Stream_PagesThroughCursor(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(ctx, t)
	if err := migrate.Up(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store := auditpg.New(pool)

	// Write enough events to span multiple internal pages. The Stream
	// implementation pages at 1000 rows; we go just past that to prove
	// the page-loop pagination works end-to-end.
	const n = 1100
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		ev := audit.Event{
			ID:        fmt.Sprintf("evt-%04d", i),
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			ToolName:  "echo",
			Transport: "http",
			Source:    "mcp",
			Success:   true,
		}
		if err := store.Log(ctx, ev); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	seen := 0
	if err := store.Stream(ctx, audit.QueryFilter{}, func(audit.Event) error {
		seen++
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if seen != n {
		t.Errorf("Stream visited %d events, want %d", seen, n)
	}
}

func equalStringsAny(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// Postgres ORDER BY ts DESC + same ts: ordering inside a tie is
	// undefined. Compare as sets for these small fixtures.
	am := map[string]int{}
	for _, s := range a {
		am[s]++
	}
	for _, s := range b {
		am[s]--
	}
	for _, v := range am {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestStore_GetPayload_NotFound(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(ctx, t)
	if err := migrate.Up(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store := auditpg.New(pool)

	got, err := store.GetPayload(ctx, "no-such-event")
	if err != nil {
		t.Errorf("GetPayload err on missing = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("GetPayload on missing = %+v, want nil", got)
	}
}

// startPostgres spins up a fresh Postgres 16-alpine container per test.
// Mirrors tests/integration_test.go so we don't share fixtures across
// packages that have different lifetimes.
func startPostgres(ctx context.Context, t *testing.T) string {
	t.Helper()
	if os.Getenv("DOCKER_HOST") == "" && os.Getenv("HOME") == "" {
		t.Skip("no docker socket discoverable")
	}
	pgC, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("mcp_test"),
		tcpostgres.WithUsername("mcp"),
		tcpostgres.WithPassword("mcp"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	url, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("conn string: %v", err)
	}
	return url
}
