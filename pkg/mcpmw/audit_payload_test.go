package mcpmw

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
)

func TestAudit_NoOptions_NoPayloadCaptured(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, nil, nil) // no opts → no capture
	wrapped := mw((&fakeMethodHandler{
		res: &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}},
	}).handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"k": "v"}},
		Extra:  &mcp.RequestExtra{Header: http.Header{"User-Agent": []string{"test"}}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)
	events := mem.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Payload != nil {
		t.Errorf("expected nil payload without WithPayloadCapture")
	}
}

func TestAudit_WithPayloadCapture_FillsRequestAndResponse(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, []string{"password"}, nil,
		WithPayloadCapture(0), // 0 → default 65536
	)
	wrapped := mw((&fakeMethodHandler{
		res: &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "hello"},
				&mcp.TextContent{Text: " world"},
			},
		},
	}).handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{
			"message":  "hi",
			"password": "should-be-redacted",
		}},
		Extra: &mcp.RequestExtra{Header: http.Header{
			"User-Agent": []string{"test"},
		}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)
	events := mem.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	p := events[0].Payload
	if p == nil {
		t.Fatal("expected non-nil payload")
	}
	if p.JSONRPCMethod != "tools/call" {
		t.Errorf("JSONRPCMethod = %q", p.JSONRPCMethod)
	}
	// Sanitization carried over.
	if pw, _ := p.RequestParams["password"].(string); pw != "[redacted]" {
		t.Errorf("password not redacted in payload: %v", p.RequestParams["password"])
	}
	if msg, _ := p.RequestParams["message"].(string); msg != "hi" {
		t.Errorf("message not preserved: %v", p.RequestParams["message"])
	}
	// Response shape.
	result := p.ResponseResult
	if result == nil {
		t.Fatal("expected non-nil response_result")
	}
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError should be false")
	}
	blocks, _ := result["content"].([]any)
	if len(blocks) != 2 {
		t.Errorf("expected 2 content blocks, got %d", len(blocks))
	}
	if p.RequestSizeBytes <= 0 || p.ResponseSizeBytes <= 0 {
		t.Errorf("size fields not populated: req=%d resp=%d", p.RequestSizeBytes, p.ResponseSizeBytes)
	}
	if p.RequestTruncated || p.ResponseTruncated {
		t.Errorf("unexpected truncation: %+v", p)
	}
}

func TestAudit_PayloadCapture_TruncatesOversizeResponse(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	// Tight cap that the response will exceed.
	mw := Audit(chain, mem, nil, nil, WithPayloadCapture(8))
	huge := strings.Repeat("x", 10_000)
	wrapped := mw((&fakeMethodHandler{
		res: &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: huge}}},
	}).handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "long_output"},
		Extra:  &mcp.RequestExtra{Header: http.Header{}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)
	p := mem.Snapshot()[0].Payload
	if p == nil {
		t.Fatal("expected payload")
	}
	if !p.ResponseTruncated {
		t.Error("expected ResponseTruncated=true for oversize response")
	}
	if p.ResponseResult != nil {
		t.Errorf("oversize response should be dropped, got: %+v", p.ResponseResult)
	}
	if p.ResponseSizeBytes <= 8 {
		t.Errorf("size still recorded; got %d", p.ResponseSizeBytes)
	}
}

func TestAudit_PayloadCapture_HeadersOptIn(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)

	// Without WithHeaderCapture: headers not captured.
	mw := Audit(chain, mem, nil, nil, WithPayloadCapture(0))
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "headers"},
		Extra:  &mcp.RequestExtra{Header: http.Header{"X-Test": []string{"abc"}}},
	}
	wrapped := mw((&fakeMethodHandler{}).handle)
	_, _ = wrapped(context.Background(), "tools/call", req)
	if h := mem.Snapshot()[0].Payload.RequestHeaders; h != nil {
		t.Errorf("headers captured without opt-in: %+v", h)
	}

	// With WithHeaderCapture: headers stored.
	mem2 := audit.NewMemoryLogger()
	mw2 := Audit(chain, mem2, nil, nil, WithPayloadCapture(0), WithHeaderCapture())
	wrapped2 := mw2((&fakeMethodHandler{}).handle)
	_, _ = wrapped2(context.Background(), "tools/call", req)
	h := mem2.Snapshot()[0].Payload.RequestHeaders
	if h == nil {
		t.Fatal("expected headers captured with WithHeaderCapture")
	}
	if got := h["X-Test"]; len(got) != 1 || got[0] != "abc" {
		t.Errorf("X-Test = %v", got)
	}
}

func TestAudit_AuthFailure_StillCapturesPayload(t *testing.T) {
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(false, nil, nil) // no anonymous, no stores → auth fails
	mw := Audit(chain, mem, nil, nil, WithPayloadCapture(0))
	wrapped := mw((&fakeMethodHandler{}).handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"a": 1}},
		Extra:  &mcp.RequestExtra{Header: http.Header{"User-Agent": []string{"x"}}},
	}
	_, err := wrapped(context.Background(), "tools/call", req)
	if !errors.Is(err, auth.ErrNotAuthenticated) {
		t.Fatalf("err = %v", err)
	}
	events := mem.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Payload == nil {
		t.Fatal("expected payload even on auth failure")
	}
	if events[0].Payload.ResponseError == nil {
		t.Errorf("expected ResponseError on auth-failure payload")
	}
}
