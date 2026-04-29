package httpsrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
	"github.com/plexara/mcp-test/pkg/tools"
	"github.com/plexara/mcp-test/pkg/tools/identity"
)

// portalTestMux returns a minimal mux with the portal API and an "always-pass"
// auth middleware that injects a synthetic identity.
func portalTestMux(t *testing.T, mem *audit.MemoryLogger) *http.ServeMux {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		OIDC:   config.OIDCConfig{Enabled: true, Issuer: "https://idp", Audience: "aud"},
		APIKeys: config.APIKeysConfig{
			File: []config.FileAPIKey{{Name: "k1", Key: "shh"}},
		},
		Portal: config.PortalConfig{Enabled: true, CookieSecret: "secret-secret"},
	}
	reg := tools.NewRegistry()
	reg.Add(identity.New([]string{"cookie"}))
	api := NewPortalAPI(cfg, reg, mem)

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(), &auth.Identity{Subject: "alice", AuthType: "oidc"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	mux := http.NewServeMux()
	api.Mount(mux, mw)
	return mux
}

func TestPortalAPI_ToolDetail_FoundAndMissing(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())

	for _, name := range []string{"whoami", "echo", "headers"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/tools/"+name, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("tool %s: status = %d", name, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/tools/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("missing tool status = %d, want 404", w.Code)
	}
}

func TestPortalAPI_Wellknown(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/wellknown", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(w.Body).Decode(&body)
	if !strings.HasSuffix(body["protected_resource_url"].(string), "/.well-known/oauth-protected-resource") {
		t.Errorf("protected_resource_url = %v", body["protected_resource_url"])
	}
	if body["authorization_server"] != "https://idp" {
		t.Errorf("authorization_server = %v", body["authorization_server"])
	}
	if body["oidc_enabled"] != true {
		t.Errorf("oidc_enabled = %v", body["oidc_enabled"])
	}
}

func TestPortalAPI_AuditTimeseriesAndBreakdown(t *testing.T) {
	mem := audit.NewMemoryLogger()
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		_ = mem.Log(context.Background(), audit.Event{
			Timestamp:  now.Add(-time.Duration(i) * time.Minute),
			ToolName:   "echo",
			Success:    i != 0,
			DurationMS: int64(10 * (i + 1)),
		})
	}
	mux := portalTestMux(t, mem)

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/timeseries?bucket=1m", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("timeseries status = %d", resp.Code)
	}
	var ts struct {
		Points []audit.TimePoint `json:"points"`
		Bucket string            `json:"bucket"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ts)
	if ts.Bucket != "1m0s" {
		t.Errorf("bucket = %q", ts.Bucket)
	}
	if len(ts.Points) == 0 {
		t.Error("no timeseries points")
	}

	resp = httptest.NewRecorder()
	mux.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/breakdown?by=tool", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("breakdown status = %d", resp.Code)
	}
	var bd struct {
		Breakdown []audit.BreakdownPoint `json:"breakdown"`
		By        string                 `json:"by"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&bd)
	if bd.By != "tool" || len(bd.Breakdown) == 0 {
		t.Errorf("breakdown = %+v", bd)
	}
}

func TestPortalAPI_AuditEvents_FilterParams(t *testing.T) {
	mem := audit.NewMemoryLogger()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = mem.Log(context.Background(), audit.Event{
			Timestamp:   now.Add(-time.Duration(i) * time.Minute),
			ToolName:    []string{"echo", "whoami"}[i%2],
			Success:     i%3 != 0,
			UserSubject: "alice",
		})
	}
	mux := portalTestMux(t, mem)

	// No filters.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events?limit=2", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Events []audit.Event `json:"events"`
		Total  int64         `json:"total"`
		Limit  int           `json:"limit"`
		Offset int           `json:"offset"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body.Limit != 2 {
		t.Errorf("limit echoed = %d, want 2", body.Limit)
	}
	if len(body.Events) != 2 {
		t.Errorf("got %d events, want 2", len(body.Events))
	}
	if body.Total != 5 {
		t.Errorf("total = %d, want 5", body.Total)
	}

	// success=false
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events?success=false", nil))
	body.Events = nil
	body.Total = 0
	_ = json.NewDecoder(w.Body).Decode(&body)
	for _, ev := range body.Events {
		if ev.Success {
			t.Errorf("success=false filter returned a success event")
		}
	}

	// from/to range parses ok (RFC3339).
	from := now.Add(-30 * time.Second).Format(time.RFC3339)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/audit/events?from="+from, nil))
	if w.Code != http.StatusOK {
		t.Errorf("from filter status = %d", w.Code)
	}
}

func TestPortalAPI_Dashboard(t *testing.T) {
	mem := audit.NewMemoryLogger()
	now := time.Now().UTC()
	_ = mem.Log(context.Background(), audit.Event{
		Timestamp: now.Add(-30 * time.Second),
		ToolName:  "x", Success: true, DurationMS: 50,
	})
	mux := portalTestMux(t, mem)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/dashboard", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Stats struct {
			Total int64 `json:"total"`
		} `json:"stats"`
		Recent []audit.Event `json:"recent"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body.Stats.Total != 1 {
		t.Errorf("stats.total = %d, want 1", body.Stats.Total)
	}
	if len(body.Recent) != 1 {
		t.Errorf("recent = %d", len(body.Recent))
	}
}
