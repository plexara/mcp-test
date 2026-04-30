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

func TestMCPAuthGateway_RFC6750ChallengeFields(t *testing.T) {
	// RFC 6750 §3 says a bearer challenge SHOULD include error and
	// error_description when the request is rejected; clients use those
	// to surface a useful diagnostic instead of an opaque 401.
	gate := MCPAuthGateway("https://example/rm", false)
	h := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := w.Header().Get("WWW-Authenticate")
	wantSubstrings := []string{
		`Bearer realm="mcp-test"`,
		`error="invalid_request"`,
		`error_description="missing or unsupported credential`,
		`resource_metadata="https://example/rm"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("WWW-Authenticate missing %q\n got: %s", want, got)
		}
	}

	// Body also carries error_description so non-RFC-aware clients see it.
	body := w.Body.String()
	if !strings.Contains(body, `"error_description"`) {
		t.Errorf("body missing error_description: %s", body)
	}
}

func TestBuildBearerChallenge_OmitsBlankFields(t *testing.T) {
	// Both error fields blank: only realm and resource_metadata appear.
	got := buildBearerChallenge("https://x/rm", "", "")
	if strings.Contains(got, "error=") || strings.Contains(got, "error_description=") {
		t.Errorf("blank err fields should be omitted: %s", got)
	}
	if !strings.Contains(got, `resource_metadata="https://x/rm"`) {
		t.Errorf("expected resource_metadata: %s", got)
	}

	// No metadata URL either: minimal challenge.
	got = buildBearerChallenge("", "", "")
	if got != `Bearer realm="mcp-test"` {
		t.Errorf("minimal challenge = %q", got)
	}
}

func TestQuoteAuthParam_EscapesQuotesAndBackslashes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{`with "quotes"`, `with \"quotes\"`},
		{`back\slash`, `back\\slash`},
		{`both \"`, `both \\\"`},
	}
	for _, c := range cases {
		if got := quoteAuthParam(c.in); got != c.want {
			t.Errorf("quoteAuthParam(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
