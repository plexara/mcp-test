package mcpmw

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
)

var _ = http.Header{} // ensure import stays even if other refs go away

func TestTokenFromHeader(t *testing.T) {
	cases := []struct {
		name string
		h    http.Header
		want string
	}{
		{"x-api-key", http.Header{"X-Api-Key": []string{"abc"}}, "abc"},
		{"bearer", http.Header{"Authorization": []string{"Bearer xyz"}}, "xyz"},
		{"bearer mixed case", http.Header{"Authorization": []string{"bearer xyz"}}, "xyz"},
		{"basic ignored", http.Header{"Authorization": []string{"Basic abc=="}}, ""},
		{"empty", http.Header{}, ""},
		{"x-api-key wins over bearer", http.Header{
			"X-Api-Key":     []string{"key"},
			"Authorization": []string{"Bearer tok"},
		}, "key"},
	}
	for _, tc := range cases {
		if got := tokenFromHeader(tc.h); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFirstAddr(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"10.0.0.1":           "10.0.0.1",
		"10.0.0.1, 10.0.0.2": "10.0.0.1",
		"  10.0.0.1  , x":    "10.0.0.1",
	}
	for in, want := range cases {
		if got := firstAddr(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestErrString(t *testing.T) {
	if errString(nil) != "" {
		t.Error("nil should be empty string")
	}
	if errString(errors.New("boom")) != "boom" {
		t.Error("error message not extracted")
	}
}

func TestMeasureResult(t *testing.T) {
	res := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "hello"},  // 5
			&mcp.TextContent{Text: "world!"}, // 6
		},
	}
	chars, blocks := measureResult(res)
	if chars != 11 {
		t.Errorf("chars = %d, want 11", chars)
	}
	if blocks != 2 {
		t.Errorf("blocks = %d, want 2", blocks)
	}
}

func TestExtractCallParams(t *testing.T) {
	// CallToolParams path.
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"k": "v"}},
	}
	name, args := extractCallParams(req)
	if name != "echo" {
		t.Errorf("name = %q", name)
	}
	if args["k"] != "v" {
		t.Errorf("args = %+v", args)
	}

	// CallToolParamsRaw path: JSON bytes get unmarshalled.
	req3 := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: "raw", Arguments: []byte(`{"x":42}`)},
	}
	name3, args3 := extractCallParams(req3)
	if name3 != "raw" {
		t.Errorf("raw name = %q", name3)
	}
	if v, _ := args3["x"].(float64); v != 42 {
		t.Errorf("raw args = %+v", args3)
	}

	// Unknown params type returns empty name.
	req2 := &mcp.ServerRequest[*mcp.PingParams]{Params: &mcp.PingParams{}}
	name2, args2 := extractCallParams(req2)
	if name2 != "" || args2 != nil {
		t.Errorf("unexpected params parse: name=%q args=%v", name2, args2)
	}
}

func TestSessionID_NilRequest(t *testing.T) {
	// A request that returns nil from GetSession should produce empty ID.
	req := &mcp.ServerRequest[*mcp.PingParams]{Params: &mcp.PingParams{}}
	if got := sessionID(req); got != "" {
		t.Errorf("sessionID = %q, want empty", got)
	}
}

func TestUserAgent_NoExtra(t *testing.T) {
	req := &mcp.ServerRequest[*mcp.PingParams]{Params: &mcp.PingParams{}}
	if got := userAgent(req); got != "" {
		t.Errorf("userAgent on no-extra = %q, want empty", got)
	}
}

// fakeMethodHandler is a minimal next-handler stub used by the audit middleware
// pipeline. It records whether it was called and returns whatever was preset.
type fakeMethodHandler struct {
	called bool
	res    mcp.Result
	err    error
}

func (f *fakeMethodHandler) handle(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
	f.called = true
	return f.res, f.err
}

func TestAudit_NonToolCallPassesThrough(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, nil, nil)

	next := &fakeMethodHandler{}
	wrapped := mw(next.handle)

	req := &mcp.ServerRequest[*mcp.PingParams]{Params: &mcp.PingParams{}}
	_, _ = wrapped(context.Background(), "ping", req)
	if !next.called {
		t.Error("non tools/call should pass through to next")
	}
	if len(mem.Snapshot()) != 0 {
		t.Errorf("non tools/call should not produce audit events, got %d", len(mem.Snapshot()))
	}
}

func TestAudit_InMemoryBypass(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(false, nil, nil) // anonymous OFF
	mw := Audit(chain, mem, nil, nil)

	next := &fakeMethodHandler{}
	wrapped := mw(next.handle)

	// No Extra means in-memory transport. Middleware should bypass auth and
	// audit, but stamp Anonymous on ctx so tool handlers can read identity.
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}},
	}
	_, err := wrapped(context.Background(), "tools/call", req)
	if err != nil {
		t.Fatalf("in-memory call should not error: %v", err)
	}
	if !next.called {
		t.Error("next should be called on in-memory tools/call")
	}
	if len(mem.Snapshot()) != 0 {
		t.Errorf("in-memory call should not write audit row, got %d", len(mem.Snapshot()))
	}
}

func TestAudit_AuthFailureRecordedAndReturnsError(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(false, nil, nil) // no auth methods, no anonymous
	mw := Audit(chain, mem, nil, map[string]string{"echo": "identity"})

	next := &fakeMethodHandler{}
	wrapped := mw(next.handle)

	// Non-empty Extra.Header tells the middleware we're on the HTTP path
	// (not the in-memory bypass) so it runs auth and writes an audit row.
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{}},
		Extra:  &mcp.RequestExtra{Header: http.Header{"User-Agent": []string{"test"}}},
	}
	_, err := wrapped(context.Background(), "tools/call", req)
	if err == nil {
		t.Error("expected auth error to bubble")
	}
	if next.called {
		t.Error("next must not be called on auth failure")
	}
	events := mem.Snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Success || events[0].ErrorCategory != "auth" {
		t.Errorf("auth-fail event wrong: %+v", events[0])
	}
}

// Make sure the unused import bookkeeping above doesn't fail.
var _ = config.Config{}
