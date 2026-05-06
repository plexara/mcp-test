package auth

import (
	"context"
	"net/http"
	"testing"
)

func TestContextHelpers_RoundTrip(t *testing.T) {
	ctx := context.Background()
	if v := GetToken(ctx); v != "" {
		t.Error("token should default to empty")
	}
	ctx = WithToken(ctx, "tok-1")
	if v := GetToken(ctx); v != "tok-1" {
		t.Errorf("token = %q", v)
	}

	if id := GetIdentity(ctx); id != nil {
		t.Error("identity should default to nil")
	}
	ctx = WithIdentity(ctx, &Identity{Subject: "alice", AuthType: "apikey"})
	if id := GetIdentity(ctx); id == nil || id.Subject != "alice" {
		t.Errorf("identity = %+v", id)
	}

	if h := GetHeaders(ctx); h != nil {
		t.Error("headers should default to nil")
	}
	hdrs := http.Header{"X-Test": []string{"v"}}
	ctx = WithHeaders(ctx, hdrs)
	if h := GetHeaders(ctx); h.Get("X-Test") != "v" {
		t.Errorf("headers = %+v", h)
	}

	if v := GetRequestID(ctx); v != "" {
		t.Error("request id should default to empty")
	}
	ctx = WithRequestID(ctx, "req-1")
	if v := GetRequestID(ctx); v != "req-1" {
		t.Errorf("request id = %q", v)
	}

	if v := GetRemoteAddr(ctx); v != "" {
		t.Error("remote addr should default to empty")
	}
	ctx = WithRemoteAddr(ctx, "10.0.0.1")
	if v := GetRemoteAddr(ctx); v != "10.0.0.1" {
		t.Errorf("remote addr = %q", v)
	}
}

func TestAnonymous(t *testing.T) {
	id := Anonymous()
	if id.AuthType != "anonymous" || id.Subject != "anonymous" {
		t.Errorf("anonymous identity wrong: %+v", id)
	}
}

func TestWithHeaders_RedactsSensitive(t *testing.T) {
	ctx := context.Background()
	in := http.Header{
		"Authorization":       []string{"Bearer secret-token"},
		"Cookie":              []string{"session=abc; csrf=def"},
		"Set-Cookie":          []string{"new=value"},
		"Proxy-Authorization": []string{"Basic xyz"},
		"X-Api-Key":           []string{"real-api-key"},
		"X-API-KEY":           []string{"shouted-api-key"},
		"User-Agent":          []string{"curl/8.0"},
		"X-Test":              []string{"v1", "v2"},
	}
	ctx = WithHeaders(ctx, in)
	out := GetHeaders(ctx)
	for _, name := range []string{"Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization", "X-Api-Key", "X-API-KEY"} {
		if got := out.Get(name); got != "[redacted]" {
			t.Errorf("%s should be redacted, got %q", name, got)
		}
	}
	if got := out.Get("User-Agent"); got != "curl/8.0" {
		t.Errorf("User-Agent should pass through, got %q", got)
	}
	if got := out.Values("X-Test"); len(got) != 2 || got[0] != "v1" || got[1] != "v2" {
		t.Errorf("X-Test multi-value should pass through, got %v", got)
	}
	// Mutating the input after stash must not affect what the audit row sees.
	in.Set("Authorization", "Bearer different")
	if got := out.Get("Authorization"); got != "[redacted]" {
		t.Errorf("post-stash mutation leaked: %q", got)
	}
}

func TestRedactHeaders_NilSafe(t *testing.T) {
	if got := RedactHeaders(nil); got != nil {
		t.Errorf("nil input should yield nil, got %v", got)
	}
}
