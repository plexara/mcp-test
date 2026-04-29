// Package httpsrv builds the HTTP layer that fronts the MCP server and portal.
package httpsrv

import (
	"net/http"
	"strings"
)

// MCPAuthGateway returns middleware that enforces the presence of either an
// X-API-Key header or an Authorization: Bearer token. On miss, it serves a 401
// with a WWW-Authenticate header that points MCP clients at the
// /.well-known/oauth-protected-resource document.
//
// Validation of the token (signature, audience, key lookup) is intentionally
// deferred to the MCP-side audit middleware so that failed-auth attempts also
// produce audit rows.
//
// If allowAnonymous is true, the gateway is a no-op.
func MCPAuthGateway(resourceMetadataURL string, allowAnonymous bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if allowAnonymous {
				next.ServeHTTP(w, r)
				return
			}
			if hasCredential(r) {
				next.ServeHTTP(w, r)
				return
			}
			challenge := `Bearer realm="mcp-test"`
			if resourceMetadataURL != "" {
				challenge += `, resource_metadata="` + resourceMetadataURL + `"`
			}
			w.Header().Set("WWW-Authenticate", challenge)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		})
	}
}

func hasCredential(r *http.Request) bool {
	if r.Header.Get("X-API-Key") != "" {
		return true
	}
	a := r.Header.Get("Authorization")
	if a == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(a), "bearer ")
}
