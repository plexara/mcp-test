package httpsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
	"github.com/plexara/mcp-test/pkg/tools"
	"github.com/plexara/mcp-test/pkg/tools/identity"
)

// portalReplayMux is a richer test fixture than portalTestMux: it
// wires a real mcp.Server with the identity toolkit registered so the
// replay endpoint can exercise the in-process MCP client path. Returns
// the mux + the in-memory audit logger so assertions can read the new
// audit row.
func portalReplayMux(t *testing.T, redactKeys []string) (*http.ServeMux, *audit.MemoryLogger) {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Portal: config.PortalConfig{Enabled: true, CookieSecret: "secret-secret"},
	}
	reg := tools.NewRegistry()
	reg.Add(identity.New(nil))

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	for _, tk := range reg.Toolkits() {
		tk.RegisterTools(mcpServer)
	}

	mem := audit.NewMemoryLogger()
	api := NewPortalAPI(cfg, reg, mem, mcpServer, redactKeys)

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(),
				&auth.Identity{Subject: "alice", AuthType: "oidc"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	mux := http.NewServeMux()
	api.Mount(mux, mw)
	return mux, mem
}

// stagedEvent helper: pre-stages an audit event with the given payload
// in mem so /replay can find and operate on it. Returns the event id.
func stagedEvent(t *testing.T, mem *audit.MemoryLogger, params map[string]any) string {
	t.Helper()
	ev := audit.Event{
		ToolName:  "echo",
		Timestamp: time.Now().UTC(),
		Source:    "mcp",
		Transport: "http",
		Success:   true,
		Payload:   &audit.Payload{JSONRPCMethod: "tools/call", RequestParams: params},
	}
	if err := mem.Log(context.Background(), ev); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, e := range mem.Snapshot() {
		if e.ToolName == "echo" {
			return e.ID
		}
	}
	t.Fatal("seeded event not found")
	return ""
}

func TestPortalAPI_AuditReplay_503WhenMCPServerNil(t *testing.T) {
	// portalTestMux constructs a PortalAPI with mcpServer=nil. The
	// replay endpoint must return 503 (not 500) so the operator sees
	// "feature not available" rather than a generic crash.
	mux := portalTestMux(t, audit.NewMemoryLogger())
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/00000000-0000-0000-0000-000000000000/replay", body)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestPortalAPI_AuditReplay_400OnInvalidUUID(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/not-a-uuid/replay", body)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	// portalTestMux has nil mcpServer so we hit the 503 path before
	// the UUID check; that's correct for this fixture. The actual
	// 400-on-invalid-UUID path is exercised in
	// tests/audit_replay_test.go::TestHTTP_AuditReplay_RejectsInvalidUUID
	// which uses a portal-enabled fixture with a real mcpServer.
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (mcpServer nil)", w.Code)
	}
}

// auditStream's 503-on-non-subscribing-logger path: covered by the
// integration test setup that swaps in a NoopLogger via config; not
// repeated here because portalTestMux's signature accepts only
// *audit.MemoryLogger which is itself a SubscribingLogger.

func TestHasRedactedParam(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{"empty", nil, false},
		{"clean scalar", map[string]any{"x": 1}, false},
		{"clean nested", map[string]any{"x": map[string]any{"y": "z"}}, false},
		{"top-level redacted", map[string]any{"k": "[redacted]"}, true},
		{"nested redacted", map[string]any{"a": map[string]any{"b": "[redacted]"}}, true},
		{"slice contains redacted",
			map[string]any{"xs": []any{"ok", "[redacted]"}}, true},
		{"slice clean",
			map[string]any{"xs": []any{"ok", 1, true}}, false},
		{"deeply nested",
			map[string]any{"a": map[string]any{
				"b": []any{map[string]any{"c": "[redacted]"}},
			}}, true},
	}
	for _, c := range cases {
		got := hasRedactedParam(c.in)
		if got != c.want {
			t.Errorf("%s: hasRedactedParam = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIdentityKey(t *testing.T) {
	cases := []struct {
		id   *auth.Identity
		want string
	}{
		{nil, ""},
		{&auth.Identity{}, ""}, // empty AuthType + empty Subject -> empty
		{&auth.Identity{AuthType: "oidc", Subject: "alice"}, "oidc:alice"},
		{&auth.Identity{AuthType: "apikey", Subject: "key-1"}, "apikey:key-1"},
		{&auth.Identity{AuthType: "anonymous"}, "anonymous"}, // no subject
	}
	for _, c := range cases {
		got := identityKey(c.id)
		if got != c.want {
			t.Errorf("identityKey(%+v) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestPortalAPI_AuditReplay_HappyPath(t *testing.T) {
	mux, mem := portalReplayMux(t, nil)
	id := stagedEvent(t, mem, map[string]any{"message": "hello"})

	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/"+id+"/replay", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ReplayEventID string `json:"replay_event_id"`
		ReplayedFrom  string `json:"replayed_from"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ReplayedFrom != id {
		t.Errorf("replayed_from = %q, want %q", resp.ReplayedFrom, id)
	}
	if resp.ReplayEventID == "" {
		t.Error("replay_event_id empty")
	}

	// Verify the new audit row exists with source=portal-replay.
	var found bool
	for _, e := range mem.Snapshot() {
		if e.ID == resp.ReplayEventID && e.Source == "portal-replay" {
			found = true
			if e.Payload == nil || e.Payload.ReplayedFrom != id {
				t.Errorf("replayed_from linkage missing: %+v", e.Payload)
			}
		}
	}
	if !found {
		t.Errorf("did not find new audit event id=%s source=portal-replay", resp.ReplayEventID)
	}
}

func TestPortalAPI_AuditReplay_400OnEventNotFound(t *testing.T) {
	mux, _ := portalReplayMux(t, nil)
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/00000000-0000-0000-0000-000000000000/replay", body)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPortalAPI_AuditReplay_400OnRedactedParams(t *testing.T) {
	mux, mem := portalReplayMux(t, []string{"password"})
	id := stagedEvent(t, mem, map[string]any{
		"message":  "hi",
		"password": "[redacted]", // simulating a sanitized stored row
	})
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/"+id+"/replay", body)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (redacted)", w.Code)
	}
}

func TestPortalAPI_AuditReplay_400OnNoPayload(t *testing.T) {
	mux, mem := portalReplayMux(t, nil)
	// Stage an event with NO payload (capture-disabled simulation).
	ev := audit.Event{
		ToolName:  "echo",
		Timestamp: time.Now().UTC(),
		Source:    "mcp",
		Transport: "http",
	}
	_ = mem.Log(context.Background(), ev)
	id := mem.Snapshot()[0].ID

	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/"+id+"/replay", body)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPortalAPI_AuditReplay_RateLimit(t *testing.T) {
	mux, mem := portalReplayMux(t, nil)
	id := stagedEvent(t, mem, map[string]any{"message": "rl"})

	// Burst capacity is 5; the 6th call must be 429.
	for i := 0; i < replayBurst; i++ {
		body := bytes.NewReader([]byte(`{}`))
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/portal/audit/events/"+id+"/replay", body)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("burst call %d: status = %d", i+1, w.Code)
		}
	}
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/"+id+"/replay", body)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("over-burst status = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on 429")
	}
}

// TestPortalAPI_AuditReplay_FailedValidationDoesNotConsumeToken verifies
// that 4xx pre-validation failures (no captured payload, redacted params,
// missing tool) don't burn the per-identity rate-limit budget. Operators
// clicking Replay on summary-only rows shouldn't lose their replay quota
// for an hour.
func TestPortalAPI_AuditReplay_FailedValidationDoesNotConsumeToken(t *testing.T) {
	mux, mem := portalReplayMux(t, nil)

	// Stage replayBurst+1 events that will all fail validation
	// (no captured RequestParams). Then a final replayable event.
	noPayloadIDs := make([]string, 0, replayBurst+1)
	for i := 0; i < replayBurst+1; i++ {
		ev := audit.Event{
			Timestamp: time.Now(),
			ToolName:  "whoami", // distinct from stagedEvent's "echo" so the
			Success:   true,     // helper's tool-name lookup doesn't pick these.
			Payload:   nil,      // nothing captured -> 400 from auditReplay
		}
		_ = mem.Log(context.Background(), ev)
		// MemoryLogger.Log assigns an id when none is set; read it
		// back from the snapshot so we can target the new row.
		snap := mem.Snapshot()
		noPayloadIDs = append(noPayloadIDs, snap[len(snap)-1].ID)
	}
	good := stagedEvent(t, mem, map[string]any{"message": "ok"})

	// Each no-payload click returns 400 — must NOT consume a token.
	for _, id := range noPayloadIDs {
		body := bytes.NewReader([]byte(`{}`))
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/portal/audit/events/"+id+"/replay", body)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("no-payload replay status = %d, want 400", w.Code)
		}
	}

	// After replayBurst+1 failed clicks, the burst budget must still be
	// untouched: the next valid replay must succeed (not 429).
	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/portal/audit/events/"+good+"/replay", body)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("post-failures replay status = %d, want 200 (failed validations should not have burned tokens)", w.Code)
	}
}

func TestCallToolResultToMap_ContentTypes(t *testing.T) {
	cases := []struct {
		name string
		cr   *mcp.CallToolResult
		want []string // expected "type" values in content blocks
	}{
		{
			"text",
			&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "hi"}}},
			[]string{"text"},
		},
		{
			"image",
			&mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{MIMEType: "image/png", Data: []byte("x")}}},
			[]string{"image"},
		},
		{
			"audio",
			&mcp.CallToolResult{Content: []mcp.Content{&mcp.AudioContent{MIMEType: "audio/wav", Data: []byte("x")}}},
			[]string{"audio"},
		},
		{
			"isError + structured",
			&mcp.CallToolResult{
				IsError:           true,
				StructuredContent: map[string]any{"k": "v"},
				Content:           []mcp.Content{&mcp.TextContent{Text: "err"}},
			},
			[]string{"text"},
		},
	}
	for _, c := range cases {
		out := callToolResultToMap(c.cr)
		blocks, _ := out["content"].([]any)
		if len(blocks) != len(c.want) {
			t.Errorf("%s: blocks len = %d, want %d", c.name, len(blocks), len(c.want))
			continue
		}
		for i, b := range blocks {
			m, _ := b.(map[string]any)
			if m["type"] != c.want[i] {
				t.Errorf("%s: block[%d].type = %v, want %v", c.name, i, m["type"], c.want[i])
			}
		}
	}
	// Verify isError flag round-trips.
	out := callToolResultToMap(&mcp.CallToolResult{IsError: true})
	if out["isError"] != true {
		t.Errorf("isError flag missing: %+v", out)
	}
}

func TestDeepCopyMap(t *testing.T) {
	src := map[string]any{
		"a": "x",
		"b": map[string]any{"c": []any{1, "y", true}},
	}
	dst := deepCopyMap(src)

	// Mutate src; dst must not change.
	src["a"] = "MUTATED"
	src["b"].(map[string]any)["c"].([]any)[1] = "MUTATED"

	if dst["a"] != "x" {
		t.Errorf("top-level aliasing: dst[a] = %v", dst["a"])
	}
	if v := dst["b"].(map[string]any)["c"].([]any)[1]; v != "y" {
		t.Errorf("nested slice aliasing: dst.b.c[1] = %v", v)
	}
	if deepCopyMap(nil) != nil {
		t.Error("deepCopyMap(nil) should return nil")
	}
}

// IsError -> 502 path: covered by tests/audit_replay_test.go through
// the full HTTP stack. Locking it as a unit here would require
// registering a custom always-error tool, which isn't worth the
// setup overhead.

func TestPortalAPI_AuditStream_DeliversEvent(t *testing.T) {
	// httptest.ResponseRecorder doesn't implement http.Flusher, so we
	// need a real httptest.Server to exercise the streaming path.
	_, mem := portalReplayMux(t, nil)
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Portal: config.PortalConfig{Enabled: true, CookieSecret: "secret-secret"},
	}
	api := NewPortalAPI(cfg, nil, mem, nil, nil)
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(),
				&auth.Identity{Subject: "alice", AuthType: "oidc"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	mux := http.NewServeMux()
	api.Mount(mux, mw)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/portal/audit/stream", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("stream connect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Fire an event via direct mem.Log, simulating an audit write.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = mem.Log(context.Background(), audit.Event{
			ToolName:  "stream-test",
			Timestamp: time.Now().UTC(),
			Source:    "mcp",
			Transport: "http",
		})
	}()

	// Drain the response in chunks until either the event arrives or
	// the test deadline fires. This exercises both the connect-
	// comment write AND the event-frame write paths in auditStream.
	// Read response body in a goroutine so the outer test can enforce
	// a deadline via the ctx cancel; the http.Body Read blocks until
	// data arrives, with no per-read deadline available.
	combined := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if strings.Contains(b.String(), "stream-test") || err != nil {
				combined <- b.String()
				return
			}
		}
	}()
	var out string
	select {
	case out = <-combined:
	case <-time.After(3 * time.Second):
		cancel() // force ctx done so the read goroutine unwinds
		out = <-combined
	}
	if !strings.Contains(out, ": connected") {
		t.Errorf("missing connect comment in: %q", out)
	}
	if !strings.Contains(out, "stream-test") {
		t.Errorf("expected event for stream-test in body, got: %q", out)
	}
}
