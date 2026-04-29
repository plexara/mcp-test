//go:build integration

// Package tests / integration covers the full HTTP + Postgres stack end-to-end.
// Requires Docker on the host; run with `go test -tags integration ./tests/...`.
package tests

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/plexara/mcp-test/internal/server"
	"github.com/plexara/mcp-test/pkg/audit"
	auditpg "github.com/plexara/mcp-test/pkg/audit/postgres"
	"github.com/plexara/mcp-test/pkg/config"
)

const testAPIKey = "integration-test-key"

func TestIntegration_WhoamiOverHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pgURL := startPostgres(ctx, t)

	cfg := &config.Config{
		Server:   config.ServerConfig{Name: "mcp-test", Address: ":0", BaseURL: "http://localhost:0"},
		Auth:     config.AuthConfig{AllowAnonymous: false, RequireForMCP: true},
		APIKeys:  config.APIKeysConfig{File: []config.FileAPIKey{{Name: "it", Key: testAPIKey}}},
		Database: config.DatabaseConfig{URL: pgURL},
		Audit:    config.AuditConfig{Enabled: true, RedactKeys: []string{"password", "token", "secret"}},
		Tools:    config.ToolsConfig{Identity: config.ToolGroupConfig{Enabled: true}},
	}
	cfg.Server.Streamable.SessionTimeout = 5 * time.Minute
	cfg.Server.ReadHeaderTimeout = 5 * time.Second
	cfg.Server.Shutdown.GracePeriod = 5 * time.Second

	logger := slogDiscard(t)
	app, err := server.Build(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("build app: %v", err)
	}
	t.Cleanup(app.Close)

	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)

	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: ts.Client(),
		// Custom headers; the audit pipeline reads X-API-Key from the inbound headers.
	}
	transport.HTTPClient = ts.Client()
	// Inject API key on every request via http.RoundTripper wrapper.
	transport.HTTPClient.Transport = withHeader(ts.Client().Transport, "X-API-Key", testAPIKey)

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("whoami structured content not a map: %T", res.StructuredContent)
	}
	if sc["auth_type"] != "apikey" {
		t.Errorf("auth_type = %v, want apikey", sc["auth_type"])
	}

	// Audit assertions: pull the events back from Postgres.
	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		t.Fatalf("pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store := auditpg.New(pool)
	events, err := store.Query(ctx, audit.QueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].ToolName != "whoami" {
		t.Errorf("audit tool name = %q, want whoami", events[0].ToolName)
	}
	if events[0].AuthType != "apikey" {
		t.Errorf("audit auth_type = %q, want apikey", events[0].AuthType)
	}
}

func startPostgres(ctx context.Context, t *testing.T) string {
	t.Helper()
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
