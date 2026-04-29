package httpsrv

import (
	"net/http"
	"strings"
)

// BrowserRedirect bounces apparent browser GETs at "/" to the portal SPA so
// that operators visiting the bare host get a UI instead of a 405. MCP clients
// negotiate via Accept: application/json and/or text/event-stream; they don't
// look like browsers and pass through to next.
//
// The check is intentionally narrow:
//   - method must be GET
//   - URL.Path must be exactly "/"
//   - Accept must include text/html
//   - no MCP-specific headers (Mcp-Session-Id, Mcp-Protocol-Version)
func BrowserRedirect(portalPath string, next http.Handler) http.Handler {
	if portalPath == "" {
		portalPath = "/portal/"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isBrowserRoot(r) {
			http.Redirect(w, r, portalPath, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isBrowserRoot(r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/" {
		return false
	}
	if r.Header.Get("Mcp-Session-Id") != "" || r.Header.Get("Mcp-Protocol-Version") != "" {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
