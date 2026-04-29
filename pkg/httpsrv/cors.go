package httpsrv

import "net/http"

// CORS adds permissive CORS headers suitable for an OSS test server.
// MCP clients require Mcp-Session-Id and Mcp-Protocol-Version to round-trip.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers",
			"Authorization, Content-Type, X-API-Key, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
		h.Set("Access-Control-Expose-Headers", "Mcp-Session-Id, Mcp-Protocol-Version")
		h.Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
