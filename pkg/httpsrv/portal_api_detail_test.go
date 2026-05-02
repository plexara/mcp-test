package httpsrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plexara/mcp-test/pkg/audit"
)

func TestPortalAPI_AuditEventDetail_Found(t *testing.T) {
	mem := audit.NewMemoryLogger()
	ev := audit.Event{
		ID:        "evt-123",
		ToolName:  "echo",
		Success:   true,
		Transport: "http",
		Source:    "mcp",
	}
	_ = mem.Log(context.Background(), ev)

	mux := portalTestMux(t, mem)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events/evt-123", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got["id"] != "evt-123" {
		t.Errorf("id = %v", got["id"])
	}
	if got["tool_name"] != "echo" {
		t.Errorf("tool_name = %v", got["tool_name"])
	}
}

func TestPortalAPI_AuditEventDetail_NotFound(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPortalAPI_AuditEventDetail_PayloadAbsentForMemoryLogger(t *testing.T) {
	mem := audit.NewMemoryLogger()
	ev := audit.Event{
		ID:        "evt-456",
		ToolName:  "echo",
		Transport: "http",
		Source:    "mcp",
		// MemoryLogger doesn't persist payloads even if attached, so the
		// detail endpoint should return summary only.
		Payload: &audit.Payload{JSONRPCMethod: "tools/call"},
	}
	_ = mem.Log(context.Background(), ev)

	mux := portalTestMux(t, mem)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events/evt-456", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(w.Body).Decode(&got)
	// MemoryLogger isn't a PayloadLogger, so the detail handler clears
	// the field to nil; serialized JSON should omit it.
	if _, ok := got["payload"]; ok {
		t.Errorf("payload should be omitted for non-payload logger; body=%v", got)
	}
}
