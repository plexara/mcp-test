package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/build"
	"github.com/plexara/mcp-test/pkg/config"
)

func devCfg() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Name:    "mcp-test",
			Address: ":0",
			BaseURL: "http://localhost",
			Streamable: config.StreamableHTTP{
				SessionTimeout: 5 * time.Minute,
			},
		},
		Auth:  config.AuthConfig{AllowAnonymous: true},
		Audit: config.AuditConfig{Enabled: true, RedactKeys: []string{"password"}},
		Tools: config.ToolsConfig{
			Identity: config.ToolGroupConfig{Enabled: true},
			Data:     config.ToolGroupConfig{Enabled: true},
		},
	}
}

func TestBuildWithDeps_ProducesUsableHandler(t *testing.T) {
	cfg := devCfg()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := BuildWithDeps(cfg, logger, auth.NewChain(true, nil, nil), audit.NewMemoryLogger())

	if app.Handler() == nil {
		t.Fatal("Handler() is nil")
	}
	if app.MCPServer() == nil {
		t.Error("MCPServer() is nil")
	}
	if app.Registry() == nil {
		t.Error("Registry() is nil")
	}
	if app.DBKeys() != nil {
		t.Error("DBKeys() should be nil for BuildWithDeps")
	}

	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// healthz should be served regardless of auth.
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d", resp.StatusCode)
	}

	// /readyz reflects readiness.
	resp, err = http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz status = %d", resp.StatusCode)
	}

	// .well-known/oauth-protected-resource serves JSON.
	resp, err = http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("well-known content-type = %q", resp.Header.Get("Content-Type"))
	}

	// Close should be safe to call even without a DB pool.
	app.Close()
}

func TestBuildWithDeps_PortalEnabled(t *testing.T) {
	cfg := devCfg()
	cfg.Portal = config.PortalConfig{
		Enabled:      true,
		CookieName:   "mcp_test_session",
		CookieSecret: "0123456789abcdef0123456789abcdef",
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := BuildWithDeps(cfg, logger, auth.NewChain(true, nil, nil), audit.NewMemoryLogger())

	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// /portal/ should be served (placeholder, since SPA dist is empty in tests).
	resp, err := http.Get(ts.URL + "/portal/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/portal/ status = %d", resp.StatusCode)
	}
}

func TestVersion(t *testing.T) {
	got := Version()
	if !strings.Contains(got, build.Version) {
		t.Errorf("Version() = %q, missing build.Version=%q", got, build.Version)
	}
}

func TestApplication_RunDrainsOnContextCancel(t *testing.T) {
	cfg := devCfg()
	// Bind to an ephemeral port and use very short shutdown timing so the test
	// finishes quickly.
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Server.Shutdown.GracePeriod = 200 * time.Millisecond
	cfg.Server.Shutdown.PreShutdownDelay = 10 * time.Millisecond
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := BuildWithDeps(cfg, logger, auth.NewChain(true, nil, nil), audit.NewMemoryLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()

	// Give the listener a moment to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s")
	}
}

func TestApplication_RunReturnsListenError(t *testing.T) {
	// Bind twice to the same port to force a listen failure on the second.
	cfg := devCfg()
	cfg.Server.Address = "127.0.0.1:0"
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := BuildWithDeps(cfg, logger, auth.NewChain(true, nil, nil), audit.NewMemoryLogger())

	// Pre-bind to a known port and have app try to use it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	app.cfg.Server.Address = ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = app.Run(ctx)
	if err == nil {
		t.Error("expected Run to fail when address is taken")
	}
}
