package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserRedirect_BrowserGetsRedirected(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	h := BrowserRedirect("/portal/", next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", w.Code)
	}
	if w.Header().Get("Location") != "/portal/" {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
	if called {
		t.Error("next should not be called for browser GET /")
	}
}

func TestBrowserRedirect_MCPClientPassesThrough(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	h := BrowserRedirect("/portal/", next)

	cases := []struct {
		name string
		mut  func(*http.Request)
	}{
		{"json accept", func(r *http.Request) { r.Header.Set("Accept", "application/json") }},
		{"sse accept", func(r *http.Request) { r.Header.Set("Accept", "text/event-stream") }},
		{"mcp session header", func(r *http.Request) {
			r.Header.Set("Accept", "text/html")
			r.Header.Set("Mcp-Session-Id", "abc")
		}},
		{"non-root path", func(r *http.Request) {
			r.URL.Path = "/sub"
			r.Header.Set("Accept", "text/html")
		}},
		{"POST", func(r *http.Request) { r.Method = http.MethodPost; r.Header.Set("Accept", "text/html") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.mut(req)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if !called {
				t.Errorf("expected next to be called, status=%d", w.Code)
			}
		})
	}
}

func TestBrowserRedirect_DefaultPath(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	h := BrowserRedirect("", next) // empty falls back to /portal/
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Header().Get("Location") != "/portal/" {
		t.Errorf("default path not /portal/: %q", w.Header().Get("Location"))
	}
}
