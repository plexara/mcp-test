package tests

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/internal/server"
	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
)

const portalAPIKey = "portal-test-key"

func portalApp(t *testing.T) (*httptest.Server, *audit.MemoryLogger) {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Name:    "mcp-test",
			Address: ":0",
			BaseURL: "http://localhost",
			Streamable: config.StreamableHTTP{
				SessionTimeout: 5 * time.Minute,
			},
		},
		APIKeys: config.APIKeysConfig{File: []config.FileAPIKey{{Name: "portal", Key: portalAPIKey}}},
		Auth:    config.AuthConfig{AllowAnonymous: false, RequireForMCP: true, RequireForPortal: true},
		Audit:   config.AuditConfig{Enabled: true, RedactKeys: []string{"password", "token", "secret"}},
		Portal: config.PortalConfig{
			Enabled:      true,
			CookieName:   "mcp_test_session",
			CookieSecret: "0123456789abcdef0123456789abcdef",
		},
		Tools: config.ToolsConfig{
			Identity: config.ToolGroupConfig{Enabled: true},
			Data:     config.ToolGroupConfig{Enabled: true},
		},
	}
	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(false, auth.NewFileAPIKeyStore(cfg.APIKeys.File), nil)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	app := server.BuildWithDeps(cfg, logger, chain, mem)
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	return ts, mem
}

func portalGet(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	req.Header.Set("X-API-Key", portalAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func TestPortalAPI_Unauthorized(t *testing.T) {
	ts, _ := portalApp(t)
	resp, err := http.Get(ts.URL + "/api/v1/portal/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("missing Bearer challenge: %s", resp.Header.Get("WWW-Authenticate"))
	}
}

func TestPortalAPI_Me(t *testing.T) {
	ts, _ := portalApp(t)
	resp := portalGet(t, ts, "/api/v1/portal/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var id auth.Identity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		t.Fatal(err)
	}
	if id.AuthType != "apikey" {
		t.Errorf("auth_type = %q, want apikey", id.AuthType)
	}
}

func TestPortalAPI_Tools(t *testing.T) {
	ts, _ := portalApp(t)
	resp := portalGet(t, ts, "/api/v1/portal/tools")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tools) < 5 {
		t.Errorf("tools = %d, want >= 5", len(body.Tools))
	}
	// expect whoami + fixed_response present
	names := map[string]bool{}
	for _, m := range body.Tools {
		names[m["name"].(string)] = true
	}
	for _, want := range []string{"whoami", "echo", "headers", "fixed_response", "sized_response", "lorem"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestPortalAPI_Server_RedactsSecrets(t *testing.T) {
	ts, _ := portalApp(t)
	resp := portalGet(t, ts, "/api/v1/portal/server")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if strings.Contains(s, "0123456789abcdef") {
		t.Errorf("cookie_secret leaked into /server response: %s", s)
	}
	if strings.Contains(s, portalAPIKey) {
		t.Errorf("file api key leaked into /server response")
	}
}

func TestPortalAPI_AuditEvents(t *testing.T) {
	ts, mem := portalApp(t)
	// Inject a couple of synthetic audit events by calling MCP tools first.
	httpClient := &http.Client{Transport: &headerInjector{
		rt:      http.DefaultTransport,
		headers: http.Header{"X-API-Key": []string{portalAPIKey}},
	}}
	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: httpClient}
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for i := 0; i < 3; i++ {
		_, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"message": "x"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(mem.Snapshot()); got != 3 {
		t.Fatalf("audit memory has %d events, want 3", got)
	}

	resp := portalGet(t, ts, "/api/v1/portal/audit/events?limit=10")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Events []audit.Event `json:"events"`
		Total  int64         `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 3 || len(body.Events) != 3 {
		t.Errorf("events = %d (total %d), want 3", len(body.Events), body.Total)
	}
}

func TestPortalAPI_Dashboard(t *testing.T) {
	ts, _ := portalApp(t)
	resp := portalGet(t, ts, "/api/v1/portal/dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["stats"]; !ok {
		t.Error("dashboard missing stats")
	}
}

func TestAdminAPI_TryIt(t *testing.T) {
	ts, _ := portalApp(t)
	body := strings.NewReader(`{"arguments":{"key":"hello"}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/tryit/fixed_response", body)
	req.Header.Set("X-API-Key", portalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("tryit status = %d, body=%s", resp.StatusCode, raw)
	}
	var res mcp.CallToolResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("tryit returned IsError")
	}
}
