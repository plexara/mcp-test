package httpsrv

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitizeReturnPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"/portal/", "/portal/"},
		{"/portal/audit", "/portal/audit"},
		{"https://evil.com/", ""},   // absolute URL rejected
		{"//evil.com/", ""},         // protocol-relative rejected
		{`/\evil.com`, ""},          // backslash trick rejected
		{"javascript:alert(1)", ""}, // not a path
		{"portal/audit", ""},        // missing leading slash
	}
	for _, c := range cases {
		got := sanitizeReturnPath(c.in)
		if got != c.want {
			t.Errorf("sanitizeReturnPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDerivePKCESecret(t *testing.T) {
	a := derivePKCESecret("hunter2-cookie-secret-min-16")
	b := derivePKCESecret("hunter2-cookie-secret-min-16")
	if a != b {
		t.Errorf("derivePKCESecret should be deterministic")
	}
	c := derivePKCESecret("different-secret-min-16")
	if a == c {
		t.Errorf("derivePKCESecret should differ for different inputs")
	}
	if strings.Contains(a, "hunter2") {
		t.Errorf("PKCE secret should not echo the input cookie secret")
	}
	if len(a) < 16 {
		t.Errorf("PKCE secret too short: %d", len(a))
	}
}

func TestRandomString(t *testing.T) {
	for _, n := range []int{1, 16, 32, 64} {
		s, err := randomString(n)
		if err != nil {
			t.Fatalf("randomString(%d) err = %v", n, err)
		}
		if len(s) < n {
			t.Errorf("randomString(%d) returned %d chars", n, len(s))
		}
	}
	a, _ := randomString(32)
	b, _ := randomString(32)
	if a == b {
		t.Error("two random strings collided")
	}
}

func TestPKCEChallenge_RFC7636Vector(t *testing.T) {
	// RFC 7636 Appendix B test vector.
	v := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceChallenge(v); got != want {
		t.Errorf("pkceChallenge mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestBrowserAuth_ConsumeNonce(t *testing.T) {
	b := &BrowserAuth{usedNonces: make(map[string]time.Time)}
	if !b.consumeNonce("abc") {
		t.Error("first use of nonce should succeed")
	}
	if b.consumeNonce("abc") {
		t.Error("second use of same nonce should fail")
	}
	if b.consumeNonce("") {
		t.Error("empty nonce should fail")
	}
	if !b.consumeNonce("xyz") {
		t.Error("different nonce should succeed")
	}
}

func TestBrowserAuth_ConsumeNonceEvictsStale(t *testing.T) {
	b := &BrowserAuth{usedNonces: make(map[string]time.Time)}
	// Pre-seed an entry that's already past TTL (10 minutes).
	b.usedNonces["old"] = time.Now().Add(-15 * time.Minute)
	if !b.consumeNonce("trigger") {
		t.Fatal("seed nonce failed")
	}
	if _, present := b.usedNonces["old"]; present {
		t.Error("stale entry should have been evicted opportunistically")
	}
}

func TestBrowserAuth_HandleLogout(t *testing.T) {
	store, err := NewSessionStore("test_session", "16+chars-secret-padded-out", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	b := &BrowserAuth{sessions: store}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/portal/auth/logout", strings.NewReader(""))
	b.handleLogout(w, req)
	if w.Code != 204 {
		t.Errorf("status = %d, want 204", w.Code)
	}
}
