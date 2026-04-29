package httpsrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/config"
)

func TestPortalAPI_Me(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/me", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["subject"] != "alice" {
		t.Errorf("subject = %v", body["subject"])
	}
}

func TestPortalAPI_Server_RedactsSecrets(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/server", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "[redacted]") {
		t.Errorf("expected secrets redacted; body = %s", body)
	}
	if strings.Contains(body, `"key":"shh"`) {
		t.Errorf("file API key plaintext leaked")
	}
}

func TestPortalAPI_Tools(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/tools", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Tools []map[string]any `json:"tools"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	if len(body.Tools) != 3 {
		t.Errorf("got %d tools, want 3 (whoami, echo, headers)", len(body.Tools))
	}
}

func TestPortalAPI_Instructions(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "http://localhost", Instructions: "test fixtures only"},
		OIDC:    config.OIDCConfig{Enabled: false},
		APIKeys: config.APIKeysConfig{},
		Portal:  config.PortalConfig{Enabled: true, CookieSecret: "secret-secret-padded"},
	}
	api := NewPortalAPI(cfg, nil, audit.NewMemoryLogger())
	mux := http.NewServeMux()
	api.Mount(mux, func(h http.Handler) http.Handler { return h })

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portal/instructions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["instructions"] != "test fixtures only" {
		t.Errorf("instructions = %q", body["instructions"])
	}
}

func TestPortalAPI_AuditTimeseries_RejectsHugeWindow(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	from := time.Now().Add(-365 * 24 * time.Hour).Format(time.RFC3339)
	to := time.Now().Format(time.RFC3339)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/timeseries?from="+from+"&to="+to+"&bucket=1m", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for excess window", w.Code)
	}
}

func TestPortalAPI_AuditTimeseries_BucketTooSmall(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	w := httptest.NewRecorder()
	// 100ms bucket is rounded up to 1s
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/timeseries?bucket=100ms", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body struct {
		Bucket string `json:"bucket"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body.Bucket != "1s" {
		t.Errorf("bucket = %q, want 1s (rounded up)", body.Bucket)
	}
}

func TestPortalAPI_AuditEvents_BadParamsAreTolerated(t *testing.T) {
	mux := portalTestMux(t, audit.NewMemoryLogger())
	// invalid limit/offset/from -- handler ignores parse errors.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/portal/audit/events?limit=foo&offset=bar&from=not-a-date&success=maybe", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with bad params", w.Code)
	}
}

func TestRedactIfSet(t *testing.T) {
	if redactIfSet("") != "" {
		t.Error("empty input should remain empty")
	}
	if redactIfSet("anything") != "[redacted]" {
		t.Error("non-empty input should redact")
	}
}

func TestSanitizedConfig_DeepCopiesFileKeys(t *testing.T) {
	original := &config.Config{
		APIKeys: config.APIKeysConfig{
			File: []config.FileAPIKey{{Name: "k1", Key: "shh"}},
		},
		Database: config.DatabaseConfig{URL: "postgres://user:pass@host/db"},
	}
	originalKey := original.APIKeys.File[0].Key

	_ = sanitizedConfig(original)

	if original.APIKeys.File[0].Key != originalKey {
		t.Errorf("sanitizedConfig mutated original config: key now %q", original.APIKeys.File[0].Key)
	}
	if !strings.Contains(original.Database.URL, "user:pass") {
		t.Errorf("sanitizedConfig mutated original DB URL: %q", original.Database.URL)
	}
}
