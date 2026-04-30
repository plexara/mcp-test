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
// The WWW-Authenticate challenge follows RFC 6750 §3:
//
//	Bearer realm="mcp-test", error="invalid_request",
//	  error_description="missing or unsupported credential",
//	  resource_metadata="<url>"
//
// MCP clients that follow the spec surface error / error_description to
// the user, so the rejection has a useful diagnostic instead of an
// opaque 401.
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
			w.Header().Set("WWW-Authenticate", buildBearerChallenge(resourceMetadataURL,
				"invalid_request",
				"missing or unsupported credential; supply X-API-Key or Authorization: Bearer <token>",
			))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","error_description":"missing or unsupported credential"}`))
		})
	}
}

// buildBearerChallenge composes an RFC 6750 §3 Bearer challenge. error
// and errorDescription are optional; pass empty strings to omit them.
// Quote-escaping follows the auth-param ABNF (RFC 7235 §2.1): backslash
// and double-quote are escaped; other characters pass through. The
// validators we use construct error_description strings without those
// metacharacters, so escaping is belt-and-suspenders rather than a
// real defense.
func buildBearerChallenge(resourceMetadataURL, errCode, errDescription string) string {
	parts := []string{`Bearer realm="mcp-test"`}
	if errCode != "" {
		parts = append(parts, `error="`+quoteAuthParam(errCode)+`"`)
	}
	if errDescription != "" {
		parts = append(parts, `error_description="`+quoteAuthParam(errDescription)+`"`)
	}
	if resourceMetadataURL != "" {
		parts = append(parts, `resource_metadata="`+quoteAuthParam(resourceMetadataURL)+`"`)
	}
	return strings.Join(parts, ", ")
}

func quoteAuthParam(s string) string {
	if !strings.ContainsAny(s, `\"`) {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return r.Replace(s)
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
