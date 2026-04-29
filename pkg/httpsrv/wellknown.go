package httpsrv

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/plexara/mcp-test/pkg/config"
)

// ProtectedResourceMetadata responds to RFC 9728 metadata queries.
//
// The server is the protected resource; the configured OIDC issuer is the
// authorization server. The resource value is a URL identifier (matched
// against the JWT aud claim), not the literal endpoint path; we use the
// base URL so it stays stable regardless of where the MCP handler is mounted.
func ProtectedResourceMetadata(cfg *config.Config) http.HandlerFunc {
	resource := strings.TrimRight(cfg.Server.BaseURL, "/") + "/"
	authServers := []string{}
	if cfg.OIDC.Enabled && cfg.OIDC.Issuer != "" {
		authServers = append(authServers, cfg.OIDC.Issuer)
	}
	body := map[string]any{
		"resource":                 resource,
		"authorization_servers":    authServers,
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{},
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}

// AuthorizationServerStub responds to /.well-known/oauth-authorization-server
// with a minimal pointer to the upstream OIDC issuer's metadata. The real
// metadata lives at <issuer>/.well-known/openid-configuration.
func AuthorizationServerStub(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"issuer":                   cfg.OIDC.Issuer,
			"openid_configuration_url": strings.TrimRight(cfg.OIDC.Issuer, "/") + "/.well-known/openid-configuration",
			"note":                     "mcp-test delegates auth to the issuer above; fetch the openid_configuration_url for the real authorization server metadata",
		}
		_ = json.NewEncoder(w).Encode(body)
	}
}

// ProtectedResourceMetadataURL returns the absolute URL of the metadata doc
// for use in WWW-Authenticate challenges.
func ProtectedResourceMetadataURL(cfg *config.Config) string {
	return strings.TrimRight(cfg.Server.BaseURL, "/") + "/.well-known/oauth-protected-resource"
}
