package mcpmw

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
)

func TestAudit_SuccessfulCall_RecordsRow(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil) // anonymous OK
	mw := Audit(chain, mem, []string{"password"}, map[string]string{"echo": "identity"})

	next := &fakeMethodHandler{
		res: &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "hello"},
				&mcp.TextContent{Text: " world"},
			},
		},
	}
	wrapped := mw(next.handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{
			"message":  "hi",
			"password": "secret-should-be-redacted",
		}},
		Extra: &mcp.RequestExtra{Header: http.Header{
			"User-Agent":      []string{"test-agent"},
			"X-Forwarded-For": []string{"1.2.3.4, 5.6.7.8"},
		}},
	}
	_, err := wrapped(context.Background(), "tools/call", req)
	if err != nil {
		t.Fatalf("call should not error: %v", err)
	}
	if !next.called {
		t.Error("next should be called")
	}
	events := mem.Snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.ToolName != "echo" {
		t.Errorf("ToolName = %q", ev.ToolName)
	}
	if ev.ToolGroup != "identity" {
		t.Errorf("ToolGroup = %q", ev.ToolGroup)
	}
	if !ev.Success {
		t.Error("expected success=true")
	}
	if ev.UserAgent != "test-agent" {
		t.Errorf("UserAgent = %q", ev.UserAgent)
	}
	if ev.RemoteAddr != "1.2.3.4" {
		t.Errorf("RemoteAddr = %q (XFF should pick first)", ev.RemoteAddr)
	}
	if ev.ResponseChars != len("hello")+len(" world") {
		t.Errorf("ResponseChars = %d", ev.ResponseChars)
	}
	if ev.ContentBlocks != 2 {
		t.Errorf("ContentBlocks = %d", ev.ContentBlocks)
	}
	// Parameters should have password redacted.
	if pw, ok := ev.Parameters["password"].(string); !ok || pw == "secret-should-be-redacted" {
		t.Errorf("password not redacted in parameters: %v", ev.Parameters["password"])
	}
}

func TestAudit_ToolReturnsIsError_RecordedAsFailure(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, nil, nil)

	next := &fakeMethodHandler{
		res: &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "nope"}}},
	}
	wrapped := mw(next.handle)
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "error"},
		Extra:  &mcp.RequestExtra{Header: http.Header{}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)
	events := mem.Snapshot()
	if len(events) != 1 || events[0].Success || events[0].ErrorCategory != "tool" {
		t.Errorf("expected failure with category=tool, got %+v", events[0])
	}
}

func TestAudit_HandlerErrorRecorded(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, nil, nil)
	next := &fakeMethodHandler{err: errors.New("boom")}
	wrapped := mw(next.handle)
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "boom"},
		Extra:  &mcp.RequestExtra{Header: http.Header{}},
	}
	_, err := wrapped(context.Background(), "tools/call", req)
	if err == nil || err.Error() != "boom" {
		t.Errorf("expected boom error, got %v", err)
	}
	events := mem.Snapshot()
	if len(events) != 1 || events[0].Success {
		t.Errorf("expected failure event, got %+v", events)
	}
}

func TestAudit_InMemoryHonorsPresetIdentity(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(false, nil, nil)
	mw := Audit(chain, mem, nil, nil)

	next := &fakeMethodHandler{}
	wrapped := mw(next.handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "whoami"},
	}
	preset := &auth.Identity{Subject: "alice", AuthType: "oidc"}
	ctx := auth.WithIdentity(context.Background(), preset)
	_, err := wrapped(ctx, "tools/call", req)
	if err != nil {
		t.Fatalf("call err = %v", err)
	}
	if !next.called {
		t.Error("next should be called")
	}
}
