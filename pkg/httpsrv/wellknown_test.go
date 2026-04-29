package httpsrv

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plexara/mcp-test/pkg/config"
)

func TestProtectedResourceMetadata(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "https://mcp.example.com/"},
		OIDC:   config.OIDCConfig{Enabled: true, Issuer: "https://idp.example.com/realm"},
	}
	w := httptest.NewRecorder()
	ProtectedResourceMetadata(cfg)(w, httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil))

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["resource"] != "https://mcp.example.com/" {
		t.Errorf("resource = %v", body["resource"])
	}
	srvs, ok := body["authorization_servers"].([]any)
	if !ok || len(srvs) != 1 || srvs[0] != "https://idp.example.com/realm" {
		t.Errorf("authorization_servers = %v", body["authorization_servers"])
	}
}

func TestProtectedResourceMetadata_OIDCDisabled(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "https://mcp.example.com"},
		OIDC:   config.OIDCConfig{Enabled: false},
	}
	w := httptest.NewRecorder()
	ProtectedResourceMetadata(cfg)(w, httptest.NewRequest("GET", "/", nil))
	var body map[string]any
	_ = json.NewDecoder(w.Body).Decode(&body)
	srvs := body["authorization_servers"].([]any)
	if len(srvs) != 0 {
		t.Errorf("authorization_servers should be empty when OIDC is disabled, got %v", srvs)
	}
}

func TestAuthorizationServerStub(t *testing.T) {
	cfg := &config.Config{OIDC: config.OIDCConfig{Issuer: "https://idp.example.com/realm/"}}
	w := httptest.NewRecorder()
	AuthorizationServerStub(cfg)(w, httptest.NewRequest("GET", "/", nil))
	var body map[string]any
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["issuer"] != "https://idp.example.com/realm/" {
		t.Errorf("issuer = %v", body["issuer"])
	}
	if !strings.HasSuffix(body["openid_configuration_url"].(string), "/.well-known/openid-configuration") {
		t.Errorf("openid_configuration_url wrong: %v", body["openid_configuration_url"])
	}
}

func TestProtectedResourceMetadataURL(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{BaseURL: "https://mcp.example.com/"}}
	got := ProtectedResourceMetadataURL(cfg)
	want := "https://mcp.example.com/.well-known/oauth-protected-resource"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
