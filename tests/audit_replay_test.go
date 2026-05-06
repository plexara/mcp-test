package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestHTTP_AuditReplay_Roundtrip locks the replay endpoint contract:
// fire a tool call, find the captured audit event, POST to replay,
// receive a new audit event with replayed_from pointing at the
// original and source=portal-replay.
func TestHTTP_AuditReplay_Roundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ts, mem := portalApp(t)

	// 1. Fire the original tool call via the portal-authed MCP path.
	httpClient := &http.Client{
		Transport: &headerInjector{rt: http.DefaultTransport, headers: http.Header{"X-API-Key": []string{portalAPIKey}}},
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: httpClient,
	}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "replay-test"}, nil)
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello-replay"},
	})
	if err != nil {
		t.Fatalf("original echo: %v", err)
	}
	if res.IsError {
		t.Fatalf("original echo IsError")
	}
	original := waitForEvent(t, mem, "echo", 2*time.Second)
	if original.Source != "mcp" {
		t.Fatalf("original.Source = %q, want mcp", original.Source)
	}

	// 2. POST to replay endpoint with the same API key.
	body := bytes.NewReader([]byte(`{}`))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		ts.URL+"/api/v1/portal/audit/events/"+original.ID+"/replay", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", portalAPIKey)
	req.Header.Set("X-Requested-With", "XMLHttpRequest") // CSRF gate
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		t.Fatalf("replay status = %d body=%s", resp.StatusCode, b.String())
	}
	var replayResp struct {
		ReplayEventID string `json:"replay_event_id"`
		ReplayedFrom  string `json:"replayed_from"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&replayResp); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayResp.ReplayedFrom != original.ID {
		t.Errorf("replay.replayed_from = %q, want %q", replayResp.ReplayedFrom, original.ID)
	}
	if replayResp.ReplayEventID == "" {
		t.Error("replay.replay_event_id is empty")
	}

	// 3. The replay must have produced a new audit event.
	deadline := time.Now().Add(2 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		for _, e := range mem.Snapshot() {
			if e.ID == replayResp.ReplayEventID && e.Source == "portal-replay" {
				found = true
				if e.Payload == nil || e.Payload.ReplayedFrom != original.ID {
					t.Errorf("replayed event missing replayed_from linkage: %+v", e.Payload)
				}
				break
			}
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Errorf("did not see new audit row id=%s source=portal-replay within 2s",
			replayResp.ReplayEventID)
	}
}

// TestHTTP_AuditReplay_RejectsRedacted refuses to replay an event whose
// captured params contain "[redacted]" sentinels: re-running a tool
// with placeholder values would mislead about what the call actually
// did.
func TestHTTP_AuditReplay_RejectsRedacted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ts, mem := portalApp(t)

	// Connect MCP via the portal-keyed path.
	httpClient := &http.Client{
		Transport: &headerInjector{rt: http.DefaultTransport, headers: http.Header{"X-API-Key": []string{portalAPIKey}}},
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: httpClient,
	}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "redact-test"}, nil)
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	// Echo with a key the test config redacts. portalApp's redact_keys
	// list includes "password".
	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "echo",
		Arguments: map[string]any{
			"message":  "hi",
			"password": "should-be-redacted",
		},
	})
	if err != nil {
		t.Fatalf("seed echo: %v", err)
	}
	original := waitForEvent(t, mem, "echo", 2*time.Second)

	body := bytes.NewReader([]byte(`{}`))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		ts.URL+"/api/v1/portal/audit/events/"+original.ID+"/replay", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", portalAPIKey)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (redacted refusal)", resp.StatusCode)
	}
	var bodyBytes bytes.Buffer
	_, _ = bodyBytes.ReadFrom(resp.Body)
	if !strings.Contains(bodyBytes.String(), "redacted") {
		t.Errorf("400 body should mention redacted: %s", bodyBytes.String())
	}
}

// TestHTTP_AuditReplay_RejectsInvalidUUID covers the boundary uuid
// validation that's also on /events/{id}.
func TestHTTP_AuditReplay_RejectsInvalidUUID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts, _ := portalApp(t)
	body := bytes.NewReader([]byte(`{}`))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		ts.URL+"/api/v1/portal/audit/events/not-a-uuid/replay", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", portalAPIKey)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
