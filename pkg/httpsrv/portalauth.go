package httpsrv

import (
	"context"
	"net/http"

	"github.com/plexara/mcp-test/pkg/auth"
)

// PortalAuth resolves the caller's identity from either:
//  1. A signed session cookie (browser flow), or
//  2. An X-API-Key header / Authorization: Bearer header (API clients).
//
// On success the Identity is attached to the request context. On failure a
// 401 is served; anonymous access is intentionally NOT honored on portal
// routes even when auth.allow_anonymous is true, because the portal exposes
// audit data and admin actions.
type PortalAuth struct {
	sessions *SessionStore
	chain    *auth.Chain
}

// NewPortalAuth returns the middleware factory.
func NewPortalAuth(sessions *SessionStore, chain *auth.Chain) *PortalAuth {
	return &PortalAuth{sessions: sessions, chain: chain}
}

// Middleware returns an http.Handler middleware that requires authentication.
func (p *PortalAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 1. Cookie path.
		if p.sessions != nil {
			if id := p.sessions.Read(r); id != nil {
				ctx = auth.WithIdentity(ctx, id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 2. API key / Bearer path. Stash the token on ctx and run the chain.
		if tok := tokenFromRequest(r); tok != "" && p.chain != nil {
			ctx = auth.WithToken(ctx, tok)
			id, err := p.chain.Authenticate(ctx)
			if err == nil && id != nil {
				ctx = auth.WithIdentity(ctx, id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-test-portal"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
}

func tokenFromRequest(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k
	}
	a := r.Header.Get("Authorization")
	if a == "" {
		return ""
	}
	if len(a) > 7 && (a[:7] == "Bearer " || a[:7] == "bearer ") {
		return a[7:]
	}
	return ""
}

// IdentityFromContext is a small re-export to keep handler callsites tidy.
func IdentityFromContext(ctx context.Context) *auth.Identity { return auth.GetIdentity(ctx) }
