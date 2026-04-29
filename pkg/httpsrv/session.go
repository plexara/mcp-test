package httpsrv

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/plexara/mcp-test/pkg/auth"
)

// SessionPayload is what we encode in the cookie. Identity is the resolved
// browser-session identity; Expires is enforced server-side.
type SessionPayload struct {
	Identity *auth.Identity `json:"identity"`
	Expires  time.Time      `json:"expires"`
}

// SessionStore signs and verifies cookie payloads with HMAC-SHA256.
type SessionStore struct {
	cookieName string
	secret     []byte
	secure     bool
	maxAge     time.Duration
}

// NewSessionStore returns a store. secret should be at least 32 bytes.
func NewSessionStore(cookieName, secret string, secure bool, maxAge time.Duration) (*SessionStore, error) {
	if len(secret) < 16 {
		return nil, errors.New("session secret too short (need >= 16 bytes)")
	}
	if cookieName == "" {
		cookieName = "mcp_test_session"
	}
	if maxAge == 0 {
		maxAge = 12 * time.Hour
	}
	return &SessionStore{cookieName: cookieName, secret: []byte(secret), secure: secure, maxAge: maxAge}, nil
}

// Issue writes the cookie carrying the given Identity.
func (s *SessionStore) Issue(w http.ResponseWriter, id *auth.Identity) error {
	pl := SessionPayload{Identity: id, Expires: time.Now().Add(s.maxAge)}
	enc, err := s.encode(pl)
	if err != nil {
		return err
	}
	// #nosec G124 -- Secure is set from config (dev/HTTP-only deployments need false).
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    enc,
		Path:     "/",
		Expires:  pl.Expires,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Clear removes the cookie.
func (s *SessionStore) Clear(w http.ResponseWriter) {
	// #nosec G124 -- Secure is set from config; SameSite default is fine for clears.
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Read returns the Identity from the request's session cookie, or nil if no
// valid cookie is present (missing, signature mismatch, expired).
func (s *SessionStore) Read(r *http.Request) *auth.Identity {
	c, err := r.Cookie(s.cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	pl, err := s.decode(c.Value)
	if err != nil {
		return nil
	}
	if time.Now().After(pl.Expires) {
		return nil
	}
	return pl.Identity
}

// CookieName returns the cookie name (used by tests).
func (s *SessionStore) CookieName() string { return s.cookieName }

func (s *SessionStore) encode(pl SessionPayload) (string, error) {
	body, err := json.Marshal(pl)
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(b64))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return b64 + "." + sig, nil
}

func (s *SessionStore) decode(v string) (SessionPayload, error) {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return SessionPayload{}, errors.New("malformed cookie")
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return SessionPayload{}, fmt.Errorf("decode sig: %w", err)
	}
	wantBytes, err := base64.RawURLEncoding.DecodeString(want)
	if err != nil {
		return SessionPayload{}, err
	}
	if !hmac.Equal(got, wantBytes) {
		return SessionPayload{}, errors.New("bad signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return SessionPayload{}, err
	}
	var pl SessionPayload
	if err := json.Unmarshal(body, &pl); err != nil {
		return SessionPayload{}, err
	}
	return pl, nil
}
