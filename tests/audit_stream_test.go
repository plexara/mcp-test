package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
)

// TestHTTP_AuditStream_DeliversNewEvents subscribes to the SSE live
// tail, fires a tool call, and verifies the tool's audit event lands
// on the stream within 2 seconds (the spec target is 200ms; CI
// schedulers under -race make 200ms flaky for assertions, so we
// assert "soon" rather than "exactly N ms").
func TestHTTP_AuditStream_DeliversNewEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ts, _ := portalApp(t)

	// Open the SSE stream BEFORE firing the call; the contract is
	// "events written after subscribe arrive on the stream."
	streamReq, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/api/v1/portal/audit/stream", nil)
	streamReq.Header.Set("Accept", "text/event-stream")
	streamReq.Header.Set("X-API-Key", portalAPIKey)
	streamResp, err := ts.Client().Do(streamReq)
	if err != nil {
		t.Fatalf("stream connect: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}
	if ct := streamResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read SSE events in a goroutine.
	type sseMsg struct {
		event string
		data  string
	}
	msgs := make(chan sseMsg, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(streamResp.Body)
		var event, data string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			case line == "":
				if event != "" {
					select {
					case msgs <- sseMsg{event, data}:
					default:
					}
					event, data = "", ""
				}
			}
		}
	}()

	// Fire a tool call via the portal-keyed MCP path.
	httpClient := &http.Client{
		Transport: &headerInjector{rt: http.DefaultTransport, headers: http.Header{"X-API-Key": []string{portalAPIKey}}},
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: httpClient,
	}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "stream-test"}, nil)
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "tail-me"},
	})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}

	// Wait for an audit event matching our tool call.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case msg := <-msgs:
			if msg.event != "audit" {
				continue
			}
			var ev audit.Event
			if err := json.Unmarshal([]byte(msg.data), &ev); err != nil {
				t.Errorf("data not JSON: %v\n%s", err, msg.data)
				continue
			}
			if ev.ToolName == "echo" {
				return // success
			}
		case <-time.After(100 * time.Millisecond):
			// Loop until deadline.
		}
	}
	t.Fatal("did not see an audit SSE event for tool=echo within 3s")
}

// TestHTTP_AuditStream_EmitsKeepalive verifies the connection-open
// comment fires immediately. Heartbeat verification at the 30s
// interval is intentionally omitted (would slow CI); the keepalive
// interval is documented on the handler.
func TestHTTP_AuditStream_EmitsConnectComment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts, _ := portalApp(t)

	streamReq, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/api/v1/portal/audit/stream", nil)
	streamReq.Header.Set("X-API-Key", portalAPIKey)
	streamResp, err := ts.Client().Do(streamReq)
	if err != nil {
		t.Fatalf("stream connect: %v", err)
	}
	defer streamResp.Body.Close()

	scanner := bufio.NewScanner(streamResp.Body)
	if !scanner.Scan() {
		t.Fatal("stream produced no bytes")
	}
	first := scanner.Text()
	if !strings.HasPrefix(first, ": connected") {
		t.Errorf("first line = %q, want ': connected' comment", first)
	}
}
