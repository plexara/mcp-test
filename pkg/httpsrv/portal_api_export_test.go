package httpsrv

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plexara/mcp-test/pkg/audit"
)

// TestPortalAPI_AuditExport_StreamsNDJSON exercises the new
// /audit/export?format=jsonl endpoint against the in-memory logger.
// Each line should be a valid JSON event; payload (when set on the
// stored event) must be cleared in the export view.
func TestPortalAPI_AuditExport_StreamsNDJSON(t *testing.T) {
	mem := audit.NewMemoryLogger()
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		_ = mem.Log(context.Background(), audit.Event{
			ID:        string(rune('a' + i)),
			ToolName:  "echo",
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Success:   true,
			Transport: "http",
			Source:    "mcp",
			Payload:   &audit.Payload{JSONRPCMethod: "tools/call"},
		})
	}

	mux := portalTestMux(t, mem)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/export?format=jsonl", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("Content-Type = %q, want application/x-ndjson", ct)
	}
	scanner := bufio.NewScanner(w.Body)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d not valid JSON: %v\n%s", count, err, line)
		}
		if _, hasPayload := ev["payload"]; hasPayload {
			t.Errorf("line %d carried payload; export should be summary-only: %s", count, line)
		}
		count++
	}
	if count != 3 {
		t.Errorf("got %d lines, want 3", count)
	}
}

func TestPortalAPI_AuditExport_RejectsUnknownFormat(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/export?format=csv", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unsupported format") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestPortalAPI_AuditExport_DefaultsToJSONLWhenFormatOmitted(t *testing.T) {
	// No `format=` param at all should be allowed and treated as jsonl
	// (the only format we support today). Documented in http-api.md.
	mux := portalTestMux(t, audit.NewMemoryLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/export", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestParseQueryFilter_JSONFilters(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/events?param.user.id=alice&response.isError=true&header.User-Agent=curl/8.0&has=response_error&has=notifications",
		nil)
	f := parseQueryFilter(r)

	if len(f.JSONFilters) != 3 {
		t.Fatalf("JSONFilters len = %d, want 3: %+v", len(f.JSONFilters), f.JSONFilters)
	}
	// Verify each filter resolved correctly. Order isn't guaranteed
	// (URL.Query is a map), so check by source.
	got := map[string]audit.JSONPathFilter{}
	for _, jf := range f.JSONFilters {
		got[jf.Source] = jf
	}
	if p := got["param"]; len(p.Path) != 2 || p.Path[0] != "user" || p.Path[1] != "id" || p.Value != "alice" {
		t.Errorf("param filter wrong: %+v", p)
	}
	if r := got["response"]; len(r.Path) != 1 || r.Path[0] != "isError" || r.Value != "true" {
		t.Errorf("response filter wrong: %+v", r)
	}
	if h := got["header"]; len(h.Path) != 1 || h.Path[0] != "User-Agent" || h.Value != "curl/8.0" {
		t.Errorf("header filter wrong: %+v", h)
	}
	if len(f.HasKeys) != 2 {
		t.Errorf("HasKeys = %v, want 2", f.HasKeys)
	}
}

func TestParseQueryFilter_RejectsUnknownSourceAndKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/events?evil.k=v&has=secret_table",
		nil)
	f := parseQueryFilter(r)
	if len(f.JSONFilters) != 0 {
		t.Errorf("unknown source should be rejected, got: %+v", f.JSONFilters)
	}
	if len(f.HasKeys) != 0 {
		t.Errorf("unknown has key should be rejected, got: %v", f.HasKeys)
	}
}
