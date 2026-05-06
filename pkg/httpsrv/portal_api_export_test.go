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

func TestParseQueryFilter_DropsEmptyPathSegments(t *testing.T) {
	// "?param.a..b=v" parses to path ["a","","b"]: an empty segment
	// can't match any real payload, so the whole filter is dropped.
	r := httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/events?param.a..b=v&param..leading=v&param.trailing.=v",
		nil)
	f := parseQueryFilter(r)
	if len(f.JSONFilters) != 0 {
		t.Errorf("empty-segment paths should be dropped, got: %+v", f.JSONFilters)
	}
}

func TestParseQueryFilter_CanonicalizesHeaderName(t *testing.T) {
	// Operators routinely write headers in lower-case in URLs. The
	// stored JSONB carries the canonical Go form (User-Agent), so the
	// parser must canonicalize before matching.
	r := httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/events?header.user-agent=curl/8.0&header.x-api-key=k",
		nil)
	f := parseQueryFilter(r)
	if len(f.JSONFilters) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(f.JSONFilters), f.JSONFilters)
	}
	got := map[string]string{}
	for _, jf := range f.JSONFilters {
		got[jf.Path[0]] = jf.Value
	}
	if got["User-Agent"] != "curl/8.0" {
		t.Errorf("User-Agent canonicalization failed: %+v", got)
	}
	if got["X-Api-Key"] != "k" {
		t.Errorf("X-Api-Key canonicalization failed: %+v", got)
	}
}

func TestPortalAPI_AuditEvents_AppliesJSONFilters(t *testing.T) {
	// /audit/events runs the same parseQueryFilter -> Logger.Query
	// path; locks the wiring so a refactor that drops f.JSONFilters
	// or f.HasKeys before forwarding to the logger surfaces here.
	mem := audit.NewMemoryLogger()
	now := time.Now().UTC()
	_ = mem.Log(context.Background(), audit.Event{
		ID: "alice", ToolName: "echo", Timestamp: now, Success: true,
		Transport: "http", Source: "mcp",
		Payload: &audit.Payload{
			RequestParams:  map[string]any{"user": map[string]any{"id": "alice"}},
			RequestHeaders: map[string][]string{"User-Agent": {"curl/8.0"}},
		},
	})
	_ = mem.Log(context.Background(), audit.Event{
		ID: "bob", ToolName: "echo", Timestamp: now.Add(time.Second), Success: false,
		Transport: "http", Source: "mcp",
		Payload: &audit.Payload{
			RequestParams: map[string]any{"user": map[string]any{"id": "bob"}},
			ResponseError: map[string]any{"category": "tool"},
		},
	})

	cases := []struct {
		name string
		url  string
		ids  []string
	}{
		{"param path filter", "?param.user.id=alice", []string{"alice"}},
		{"has= filter", "?has=response_error", []string{"bob"}},
		{"header case-insensitive", "?header.user-agent=curl/8.0", []string{"alice"}},
	}
	mux := portalTestMux(t, mem)
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/portal/audit/events"+c.url, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d body=%s", c.name, w.Code, w.Body.String())
			continue
		}
		var resp struct {
			Events []audit.Event `json:"events"`
		}
		_ = json.NewDecoder(w.Body).Decode(&resp)
		got := make([]string, 0, len(resp.Events))
		for _, ev := range resp.Events {
			got = append(got, ev.ID)
		}
		if !equalStringSlice(got, c.ids) {
			t.Errorf("%s: ids = %v, want %v", c.name, got, c.ids)
		}
	}
}

func TestPortalAPI_AuditExport_DoesNotEscapeHTMLChars(t *testing.T) {
	// SetEscapeHTML(false) on the encoder: tool output containing
	// <, >, & must hit the wire unescaped so an operator eyeballing
	// the JSONL file sees the original bytes. Standard json.Encoder
	// would emit "<" etc., which is technically valid JSON but
	// surprising in a human-readable export.
	mem := audit.NewMemoryLogger()
	_ = mem.Log(context.Background(), audit.Event{
		ID:           "html-event",
		ToolName:     "echo",
		Timestamp:    time.Now().UTC(),
		Success:      true,
		Transport:    "http",
		Source:       "mcp",
		ErrorMessage: "<script>alert(\"x\")</script> & friends",
	})

	mux := portalTestMux(t, mem)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/export?format=jsonl", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	// With Go's default encoder, '<' is emitted as the 6-byte unicode
	// escape "<", '>' as ">", '&' as "&".
	// SetEscapeHTML(false) lets the raw bytes through. Assert the raw
	// form, fail on any of the escape forms.
	escapes := []string{
		"\\u003c", // <
		"\\u003e", // >
		"\\u0026", // &
	}
	for _, esc := range escapes {
		if strings.Contains(body, esc) {
			t.Errorf("SetEscapeHTML(false) regression: body still has %q escape:\n%s", esc, body)
		}
	}
	if !strings.Contains(body, "<script>") {
		t.Errorf("expected raw '<script>' in body, got:\n%s", body)
	}
}

func TestPortalAPI_AuditExport_HonorsLimitCap(t *testing.T) {
	// Lock the cap path: with N events stored and ?limit=K (K<N),
	// the export emits exactly K lines. A regression that flips the
	// >= comparison or skips the early return would show here.
	mem := audit.NewMemoryLogger()
	now := time.Now().UTC()
	const stored = 10
	for i := 0; i < stored; i++ {
		_ = mem.Log(context.Background(), audit.Event{
			ID:        string(rune('a' + i)),
			ToolName:  "echo",
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Success:   true,
			Transport: "http",
			Source:    "mcp",
		})
	}
	mux := portalTestMux(t, mem)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/export?format=jsonl&limit=4", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var lines int
	scanner := bufio.NewScanner(w.Body)
	for scanner.Scan() {
		if scanner.Text() != "" {
			lines++
		}
	}
	if lines != 4 {
		t.Errorf("limit=4 with %d events stored: got %d lines, want 4", stored, lines)
	}
}

// equalStringSlice is a small set-or-order-insensitive helper local
// to this file; tests don't share helpers across files in this package.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
