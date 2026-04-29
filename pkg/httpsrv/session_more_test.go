package httpsrv

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionStore_ClearAndCookieName(t *testing.T) {
	s, err := NewSessionStore("test_sess", "0123456789abcdef0123456789abcdef", false, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if s.CookieName() != "test_sess" {
		t.Errorf("cookie name = %q", s.CookieName())
	}

	w := httptest.NewRecorder()
	s.Clear(w)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("clear did not set a cookie: %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "test_sess" || c.Value != "" || c.MaxAge != -1 {
		t.Errorf("clear cookie wrong: %+v", c)
	}
}

func TestSessionStore_DefaultCookieName(t *testing.T) {
	s, err := NewSessionStore("", "0123456789abcdef0123456789abcdef", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.CookieName() != "mcp_test_session" {
		t.Errorf("default cookie name = %q", s.CookieName())
	}
}
