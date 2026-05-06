package httpsrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plexara/mcp-test/pkg/audit"
)

// Audit event IDs are UUIDs assigned by the storage layer
// (Postgres on Log, MemoryLogger on Log) when the caller leaves
// Event.ID empty. The detail endpoint validates the path param as a
// UUID to block the gosec G706 log-injection flow. These tests set
// the ID explicitly so they exercise the same path operators hit.
const (
	testEventIDFound  = "11111111-1111-1111-1111-111111111111"
	testEventIDOther  = "22222222-2222-2222-2222-222222222222"
	testEventIDAbsent = "33333333-3333-3333-3333-333333333333"
)

func TestPortalAPI_AuditEventDetail_Found(t *testing.T) {
	mem := audit.NewMemoryLogger()
	ev := audit.Event{
		ID:        testEventIDFound,
		ToolName:  "echo",
		Success:   true,
		Transport: "http",
		Source:    "mcp",
	}
	_ = mem.Log(context.Background(), ev)

	mux := portalTestMux(t, mem)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events/"+testEventIDFound, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got["id"] != testEventIDFound {
		t.Errorf("id = %v", got["id"])
	}
	if got["tool_name"] != "echo" {
		t.Errorf("tool_name = %v", got["tool_name"])
	}
}

func TestPortalAPI_AuditEventDetail_NotFound(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events/"+testEventIDAbsent, nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPortalAPI_AuditEventDetail_RejectsNonUUID(t *testing.T) {
	// New: the detail endpoint validates the path param as a UUID
	// before any DB work or logging. Anything else is a 400.
	mux := portalTestMux(t, audit.NewMemoryLogger())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events/not-a-uuid", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPortalAPI_AuditEventDetail_PayloadIncludedForMemoryLogger(t *testing.T) {
	// MemoryLogger now implements PayloadLogger (added when the
	// replay endpoint needed in-memory payload retrieval), so the
	// detail endpoint returns the payload alongside the summary.
	mem := audit.NewMemoryLogger()
	ev := audit.Event{
		ID:        testEventIDOther,
		ToolName:  "echo",
		Transport: "http",
		Source:    "mcp",
		Payload:   &audit.Payload{JSONRPCMethod: "tools/call"},
	}
	_ = mem.Log(context.Background(), ev)

	mux := portalTestMux(t, mem)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events/"+testEventIDOther, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(w.Body).Decode(&got)
	payload, ok := got["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing or wrong type; body=%v", got)
	}
	if payload["jsonrpc_method"] != "tools/call" {
		t.Errorf("payload.jsonrpc_method = %v, want tools/call", payload["jsonrpc_method"])
	}
}
