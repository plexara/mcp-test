package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/plexara/mcp-test/pkg/auth"
)

func TestSessionStore_RoundTrip(t *testing.T) {
	s, err := NewSessionStore("test_sess", "0123456789abcdef0123456789abcdef", false, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id := &auth.Identity{Subject: "u1", Email: "u@example.com", AuthType: "oidc"}
	w := httptest.NewRecorder()
	if err := s.Issue(w, id); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookies[0])
	got := s.Read(r)
	if got == nil || got.Subject != "u1" {
		t.Errorf("read identity wrong: %+v", got)
	}
}

func TestSessionStore_TamperDetected(t *testing.T) {
	s, _ := NewSessionStore("test_sess", "0123456789abcdef0123456789abcdef", false, time.Hour)
	w := httptest.NewRecorder()
	_ = s.Issue(w, &auth.Identity{Subject: "u1"})
	cookies := w.Result().Cookies()
	tampered := *cookies[0]
	tampered.Value = tampered.Value[:len(tampered.Value)-3] + "XXX"

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&tampered)
	if got := s.Read(r); got != nil {
		t.Error("tampered cookie should not authenticate")
	}
}

func TestSessionStore_Expired(t *testing.T) {
	s, _ := NewSessionStore("test_sess", "0123456789abcdef0123456789abcdef", false, -time.Hour)
	w := httptest.NewRecorder()
	_ = s.Issue(w, &auth.Identity{Subject: "u1"})
	cookies := w.Result().Cookies()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookies[0])
	if got := s.Read(r); got != nil {
		t.Error("expired cookie should not authenticate")
	}
}

func TestSessionStore_RejectsShortSecret(t *testing.T) {
	if _, err := NewSessionStore("x", "short", false, time.Hour); err == nil {
		t.Error("expected error for short secret")
	}
}
