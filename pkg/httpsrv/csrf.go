package httpsrv

import (
	"net/http"
)

// requireCSRFHeader rejects state-changing requests (POST/PUT/PATCH/DELETE)
// that don't carry the X-Requested-With header. Defense-in-depth on top of
// SameSite=Lax cookies: a browser <form> submission cannot set custom
// headers, and a cross-origin fetch can only set X-Requested-With through
// a CORS preflight (which our CORS handler does not approve), so this
// header acts as a portable CSRF token without requiring per-request
// minting.
func requireCSRFHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Header.Get("X-Requested-With") == "" {
				http.Error(w, "missing X-Requested-With header", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
