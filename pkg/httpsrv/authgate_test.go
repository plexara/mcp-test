package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPAuthGateway_AnonymousModePassesThrough(t *testing.T) {
	called := false
	gate := MCPAuthGateway("https://example/.well-known/oauth-protected-resource", true)
	h := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Error("anonymous mode should pass through")
	}
}

func TestMCPAuthGateway_RejectsWithoutCredential(t *testing.T) {
	gate := MCPAuthGateway("https://example/rm", false)
	h := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next should not be called")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, `resource_metadata="https://example/rm"`) {
		t.Errorf("WWW-Authenticate missing resource_metadata: %s", got)
	}
}

func TestMCPAuthGateway_AcceptsCredentials(t *testing.T) {
	cases := []struct {
		name string
		set  func(*http.Request)
	}{
		{"x-api-key", func(r *http.Request) { r.Header.Set("X-API-Key", "abc") }},
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer xyz") }},
		{"bearer mixed", func(r *http.Request) { r.Header.Set("Authorization", "bearer xyz") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			gate := MCPAuthGateway("rm", false)
			h := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.set(req)
			h.ServeHTTP(httptest.NewRecorder(), req)
			if !called {
				t.Error("expected pass-through")
			}
		})
	}
}

func TestMCPAuthGateway_BasicAuthRejected(t *testing.T) {
	gate := MCPAuthGateway("rm", false)
	h := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("Basic auth should not be accepted")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic abc==")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
}
