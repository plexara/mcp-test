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
