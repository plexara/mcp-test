package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
)

func TestPortalAuth_NoCredentials(t *testing.T) {
	chain := auth.NewChain(false, nil, nil)
	pa := NewPortalAuth(nil, chain)
	h := pa.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next should not be called when unauthenticated")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate header")
	}
}

func TestPortalAuth_APIKey(t *testing.T) {
	store := auth.NewFileAPIKeyStore([]config.FileAPIKey{{Name: "k", Key: "abc"}})
	chain := auth.NewChain(false, store, nil)
	pa := NewPortalAuth(nil, chain)

	gotID := ""
	h := pa.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if id := auth.GetIdentity(r.Context()); id != nil {
			gotID = id.APIKeyID
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/portal/me", nil)
	req.Header.Set("X-API-Key", "abc")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if gotID != "k" {
		t.Errorf("identity api key = %q, want k", gotID)
	}
}

func TestPortalAuth_BearerHeader(t *testing.T) {
	store := auth.NewFileAPIKeyStore([]config.FileAPIKey{{Name: "k", Key: "raw"}})
	chain := auth.NewChain(false, store, nil)
	pa := NewPortalAuth(nil, chain)
	h := pa.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer raw")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}

func TestPortalAuth_CookiePath(t *testing.T) {
	sessions, err := NewSessionStore("test_session", "0123456789abcdef0123456789abcdef", false, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	chain := auth.NewChain(false, nil, nil)
	pa := NewPortalAuth(sessions, chain)

	// Issue a session cookie and present it.
	w := httptest.NewRecorder()
	if err := sessions.Issue(w, &auth.Identity{Subject: "alice", AuthType: "oidc"}); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()

	gotSubject := ""
	h := pa.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if id := auth.GetIdentity(r.Context()); id != nil {
			gotSubject = id.Subject
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(cookies[0])
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req)
	if w2.Code != http.StatusOK {
		t.Errorf("status = %d", w2.Code)
	}
	if gotSubject != "alice" {
		t.Errorf("subject = %q, want alice", gotSubject)
	}
}
