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

func TestAudit_PayloadCapture_PreservesNonTextContent(t *testing.T) {
	// An EmbeddedResource block (i.e. not text/image/audio) must fall
	// through callToolResultToMap's JSON-marshal path so the actual
	// resource data lands in the payload row, not a "non-textual block"
	// placeholder. Regression coverage for M-2 in the PR review.
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, nil, nil, WithPayloadCapture(0))

	wrapped := mw((&fakeMethodHandler{
		res: &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
				URI:      "file:///tmp/example.txt",
				MIMEType: "text/plain",
				Text:     "hello from the resource",
			}},
		}},
	}).handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo"},
		Extra:  &mcp.RequestExtra{Header: http.Header{}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)

	p := mem.Snapshot()[0].Payload
	if p == nil {
		t.Fatal("expected payload")
	}
	blocks, _ := p.ResponseResult["content"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	first, _ := blocks[0].(map[string]any)
	if first["type"] != "resource" {
		t.Errorf("type = %v, want \"resource\" (from MarshalJSON)", first["type"])
	}
	res, _ := first["resource"].(map[string]any)
	if res == nil {
		t.Fatalf("expected resource sub-object, got: %v", first)
	}
	if res["uri"] != "file:///tmp/example.txt" {
		t.Errorf("resource.uri = %v", res["uri"])
	}
	if res["text"] != "hello from the resource" {
		t.Errorf("resource.text = %v", res["text"])
	}
}

func TestAudit_PayloadCapturesNotifications(t *testing.T) {
	// End-to-end: Audit (receiving) seeds a recorder onto ctx; the
	// fake handler simulates a tool firing notifications by calling
	// the Notifications sending middleware against the same ctx.
	// The captured slice must land in Payload.Notifications.
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, nil, nil, WithPayloadCapture(0), WithMaxNotifications(10))
	sender := Notifications()

	wrapped := mw(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		// Simulate the tool firing two progress notifications via the
		// sending pipeline. In the real server the tool calls
		// req.Session.NotifyProgress which dispatches through the
		// sending middleware chain; here we invoke the sending
		// middleware directly with the same ctx.
		passthrough := sender(func(context.Context, string, mcp.Request) (mcp.Result, error) {
			return nil, nil
		})
		_, _ = passthrough(ctx, "notifications/progress",
			&mcp.ServerRequest[*mcp.ProgressNotificationParams]{
				Params: &mcp.ProgressNotificationParams{Progress: 1, Total: 3, Message: "step 1"},
			})
		_, _ = passthrough(ctx, "notifications/progress",
			&mcp.ServerRequest[*mcp.ProgressNotificationParams]{
				Params: &mcp.ProgressNotificationParams{Progress: 2, Total: 3, Message: "step 2"},
			})
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil
	})

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "progress"},
		Extra:  &mcp.RequestExtra{Header: http.Header{}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)

	p := mem.Snapshot()[0].Payload
	if p == nil {
		t.Fatal("expected payload")
	}
	if len(p.Notifications) != 2 {
		t.Fatalf("captured %d notifications, want 2", len(p.Notifications))
	}
	if p.Notifications[0].Method != "notifications/progress" {
		t.Errorf("Method = %q", p.Notifications[0].Method)
	}
	if p.Notifications[0].Params["message"] != "step 1" {
		t.Errorf("Params[message] = %v", p.Notifications[0].Params["message"])
	}
}

func TestAudit_AuthFailureCarriesCategory(t *testing.T) {
	// M-3 in the review: response_error must carry a structured
	// category in addition to the message.
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(false, nil, nil) // anonymous off + no stores → fails
	mw := Audit(chain, mem, nil, nil, WithPayloadCapture(0))
	wrapped := mw((&fakeMethodHandler{}).handle)

	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo"},
		Extra:  &mcp.RequestExtra{Header: http.Header{"User-Agent": []string{"x"}}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)
	p := mem.Snapshot()[0].Payload
	if p == nil {
		t.Fatal("expected payload")
	}
	if p.ResponseError == nil {
		t.Fatal("expected response_error")
	}
	if cat, _ := p.ResponseError["category"].(string); cat != "auth" {
		t.Errorf("response_error.category = %q, want auth", cat)
	}
}

func TestAudit_PayloadCaptures_DispatchedMethod(t *testing.T) {
	// M-1 in the review: jsonrpc_method must reflect what the receiving
	// middleware actually saw, not a hard-coded "tools/call" string.
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true, nil, nil)
	mw := Audit(chain, mem, nil, nil, WithPayloadCapture(0))
	wrapped := mw((&fakeMethodHandler{}).handle)
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "echo"},
		Extra:  &mcp.RequestExtra{Header: http.Header{}},
	}
	_, _ = wrapped(context.Background(), "tools/call", req)
	p := mem.Snapshot()[0].Payload
	if p.JSONRPCMethod != "tools/call" {
		t.Errorf("JSONRPCMethod = %q, want tools/call", p.JSONRPCMethod)
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
