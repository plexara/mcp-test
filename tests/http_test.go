package tests

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/internal/server"
	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
)

// httpTestApp boots the real HTTP mux against an in-memory audit logger and
// anonymous-allowed chain, returning the running httptest.Server and a handle
// on the audit log so callers can assert events.
func httpTestApp(t *testing.T) (*httptest.Server, *audit.MemoryLogger) {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Name:    "mcp-test",
			Address: ":0",
			BaseURL: "http://localhost",
			Streamable: config.StreamableHTTP{
				SessionTimeout: 5 * time.Minute,
			},
		},
		Auth:  config.AuthConfig{AllowAnonymous: true, RequireForMCP: false},
		Audit: config.AuditConfig{Enabled: true, RedactKeys: []string{"password", "token", "secret", "authorization", "cookie", "api_key"}},
		Tools: config.ToolsConfig{
			Identity:  config.ToolGroupConfig{Enabled: true},
			Data:      config.ToolGroupConfig{Enabled: true},
			Failure:   config.ToolGroupConfig{Enabled: true},
			Streaming: config.ToolGroupConfig{Enabled: true},
		},
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)

	app := server.BuildWithDeps(cfg, logger, chain, mem)

	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	return ts, mem
}

// connectClient opens an MCP session against ts.URL with the given
// extra HTTP headers and an optional progress notification handler.
func connectClient(
	t *testing.T,
	ctx context.Context,
	ts *httptest.Server,
	extraHeaders http.Header,
	onProgress func(*mcp.ProgressNotificationClientRequest),
) *mcp.ClientSession {
	t.Helper()
	httpClient := &http.Client{
		Transport: &headerInjector{
			rt:      http.DefaultTransport,
			headers: extraHeaders,
		},
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: httpClient,
	}
	opts := &mcp.ClientOptions{}
	if onProgress != nil {
		opts.ProgressNotificationHandler = func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			onProgress(req)
		}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, opts)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestHTTP_HeadersRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ts, _ := httpTestApp(t)

	headers := http.Header{}
	headers.Set("X-Test-Round-Trip", "hello-from-test")
	headers.Set("Cookie", "session=should-be-redacted")

	session := connectClient(t, ctx, ts, headers, nil)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "headers"})
	if err != nil {
		t.Fatalf("call headers: %v", err)
	}
	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content not a map: %T", res.StructuredContent)
	}
	hdrs, ok := out["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers field not a map: %T", out["headers"])
	}
	xtrt := canonicalLookup(hdrs, "X-Test-Round-Trip")
	if xtrt == "" || !strings.Contains(xtrt, "hello-from-test") {
		t.Errorf("custom header not round-tripped: got %q", xtrt)
	}
	cookie := canonicalLookup(hdrs, "Cookie")
	if !strings.Contains(cookie, "[redacted]") {
		t.Errorf("Cookie should be redacted, got %q", cookie)
	}
}

func TestHTTP_AllTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ts, mem := httpTestApp(t)
	session := connectClient(t, ctx, ts, nil, nil)

	// whoami
	r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"})
	mustOK(t, "whoami", r, err)

	// echo
	r, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"message": "hi"}})
	mustOK(t, "echo", r, err)

	// fixed_response (deterministic)
	r1, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "fixed_response", Arguments: map[string]any{"key": "k1"}})
	mustOK(t, "fixed_response", r1, err)
	r2, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "fixed_response", Arguments: map[string]any{"key": "k1"}})
	mustOK(t, "fixed_response", r2, err)
	if !sameTextContent(r1, r2) {
		t.Error("fixed_response not deterministic between calls")
	}

	// sized_response
	r, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "sized_response", Arguments: map[string]any{"size": 1234}})
	mustOK(t, "sized_response", r, err)
	out := r.StructuredContent.(map[string]any)
	if int(out["size"].(float64)) != 1234 {
		t.Errorf("sized_response size=%v, want 1234", out["size"])
	}

	// lorem (seeded)
	r1, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "lorem", Arguments: map[string]any{"words": 20, "seed": "abc"}})
	mustOK(t, "lorem", r1, err)
	r2, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "lorem", Arguments: map[string]any{"words": 20, "seed": "abc"}})
	mustOK(t, "lorem", r2, err)
	if !sameTextContent(r1, r2) {
		t.Error("lorem with same seed not deterministic")
	}

	// error (tool-level)
	r, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "error",
		Arguments: map[string]any{"message": "test-err", "as_tool": true},
	})
	if err != nil {
		t.Fatalf("tool-level error should not bubble protocol error: %v", err)
	}
	if !r.IsError {
		t.Error("expected IsError=true on tool-level error")
	}

	// slow
	start := time.Now()
	r, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "slow", Arguments: map[string]any{"milliseconds": 50}})
	mustOK(t, "slow", r, err)
	if time.Since(start) < 45*time.Millisecond {
		t.Error("slow returned too quickly")
	}

	// flaky (seeded so test is reproducible)
	r, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "flaky",
		Arguments: map[string]any{"fail_rate": 0.0, "seed": "s", "call_id": 1},
	})
	mustOK(t, "flaky", r, err)

	// long_output (multiple blocks)
	r, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "long_output", Arguments: map[string]any{"blocks": 4, "chars": 50}})
	mustOK(t, "long_output", r, err)
	if len(r.Content) != 4 {
		t.Errorf("long_output blocks = %d, want 4", len(r.Content))
	}

	// chatty
	r, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "chatty"})
	mustOK(t, "chatty", r, err)
	if len(r.Content) < 2 {
		t.Errorf("chatty blocks = %d, want >= 2", len(r.Content))
	}

	// Audit assertions: every successful call should produce an event.
	got := mem.Snapshot()
	wantToolNames := []string{
		"whoami", "echo", "fixed_response", "fixed_response",
		"sized_response", "lorem", "lorem", "error",
		"slow", "flaky", "long_output", "chatty",
	}
	if len(got) != len(wantToolNames) {
		t.Fatalf("audit events = %d, want %d", len(got), len(wantToolNames))
	}
	for i, want := range wantToolNames {
		if got[i].ToolName != want {
			t.Errorf("audit[%d] tool = %q, want %q", i, got[i].ToolName, want)
		}
		if got[i].AuthType != "anonymous" {
			t.Errorf("audit[%d] auth_type = %q, want anonymous", i, got[i].AuthType)
		}
	}
}

func TestHTTP_ProgressNotifications(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ts, _ := httpTestApp(t)

	var (
		mu  sync.Mutex
		got []string
	)
	onProgress := func(req *mcp.ProgressNotificationClientRequest) {
		mu.Lock()
		got = append(got, req.Params.Message)
		mu.Unlock()
	}
	session := connectClient(t, ctx, ts, nil, onProgress)

	steps := 4
	params := &mcp.CallToolParams{
		Name:      "progress",
		Arguments: map[string]any{"steps": steps, "step_ms": 30},
	}
	params.SetProgressToken("pt-1")
	res, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("call progress: %v", err)
	}
	if res.IsError {
		t.Fatalf("progress returned IsError")
	}

	// Allow a small window for the last notification to arrive after the
	// response if delivery is asynchronous.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= steps {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) < steps {
		t.Errorf("progress notifications received = %d, want >= %d (got: %v)", len(got), steps, got)
	}
}

// --- helpers ---

type headerInjector struct {
	rt      http.RoundTripper
	headers http.Header
}

func (h *headerInjector) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	for k, vs := range h.headers {
		for _, v := range vs {
			r2.Header.Add(k, v)
		}
	}
	return h.rt.RoundTrip(r2)
}

func mustOK(t *testing.T, name string, r *mcp.CallToolResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if r.IsError {
		t.Fatalf("%s: returned IsError, content=%v", name, r.Content)
	}
}

func sameTextContent(a, b *mcp.CallToolResult) bool {
	if len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		ta, ok := a.Content[i].(*mcp.TextContent)
		if !ok {
			return false
		}
		tb, ok := b.Content[i].(*mcp.TextContent)
		if !ok {
			return false
		}
		if ta.Text != tb.Text {
			return false
		}
	}
	return true
}

// canonicalLookup finds a header in the structured-content map. Headers are
// returned as []any from the JSON round-trip; we join them so the caller can
// substring-match.
func canonicalLookup(hdrs map[string]any, name string) string {
	canonical := http.CanonicalHeaderKey(name)
	for k, v := range hdrs {
		if http.CanonicalHeaderKey(k) != canonical {
			continue
		}
		switch vv := v.(type) {
		case []any:
			parts := make([]string, 0, len(vv))
			for _, x := range vv {
				if s, ok := x.(string); ok {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, ",")
		case string:
			return vv
		}
	}
	return ""
}
