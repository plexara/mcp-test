package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIdP serves /.well-known/openid-configuration + /jwks and signs tokens
// with a fresh RSA keypair, so a test can fully exercise the OIDC validator.
type fakeIdP struct {
	srv  *httptest.Server
	priv *rsa.PrivateKey
	kid  string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{priv: priv, kid: "test-kid-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   idp.srv.URL,
			"jwks_uri": idp.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes())
		eBytes := []byte{0x01, 0x00, 0x01} // 65537
		e := base64.RawURLEncoding.EncodeToString(eBytes)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []any{map[string]any{
				"kty": "RSA", "use": "sig", "alg": "RS256",
				"kid": idp.kid, "n": n, "e": e,
			}},
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

// sign returns a signed JWT with the given claims.
func (f *fakeIdP) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tk := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tk.Header["kid"] = f.kid
	signed, err := tk.SignedString(f.priv)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func newValidator(t *testing.T, idp *fakeIdP, audience string) *OIDCAuthenticator {
	t.Helper()
	v, err := NewOIDC(context.Background(), OIDCConfig{
		Enabled:          true,
		Issuer:           idp.srv.URL,
		Audience:         audience,
		ClockSkewSeconds: 5,
		JWKSCacheTTL:     time.Hour,
	})
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	return v
}

func TestOIDC_ValidToken(t *testing.T) {
	idp := newFakeIdP(t)
	v := newValidator(t, idp, "mcp-test")
	tok := idp.sign(t, jwt.MapClaims{
		"iss":   idp.srv.URL,
		"aud":   "mcp-test",
		"sub":   "user-42",
		"email": "alice@example.com",
		"name":  "Alice",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	id, err := v.ValidateBearer(context.Background(), tok)
	if err != nil {
		t.Fatalf("ValidateBearer: %v", err)
	}
	if id.Subject != "user-42" || id.Email != "alice@example.com" || id.Name != "Alice" {
		t.Errorf("identity wrong: %+v", id)
	}
	if id.AuthType != "oidc" {
		t.Errorf("auth_type = %q, want oidc", id.AuthType)
	}
}

func TestOIDC_ExpiredToken(t *testing.T) {
	idp := newFakeIdP(t)
	v := newValidator(t, idp, "mcp-test")
	tok := idp.sign(t, jwt.MapClaims{
		"iss": idp.srv.URL,
		"aud": "mcp-test",
		"sub": "user",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
	})
	_, err := v.ValidateBearer(context.Background(), tok)
	if err == nil {
		t.Fatal("expected expiry error")
	}
}

func TestOIDC_WrongIssuer(t *testing.T) {
	idp := newFakeIdP(t)
	v := newValidator(t, idp, "mcp-test")
	tok := idp.sign(t, jwt.MapClaims{
		"iss": "https://evil.example.com",
		"aud": "mcp-test",
		"sub": "user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	_, err := v.ValidateBearer(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Errorf("expected issuer mismatch error, got %v", err)
	}
}

func TestOIDC_WrongAudience(t *testing.T) {
	idp := newFakeIdP(t)
	v := newValidator(t, idp, "mcp-test")
	tok := idp.sign(t, jwt.MapClaims{
		"iss": idp.srv.URL,
		"aud": "different",
		"sub": "user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	_, err := v.ValidateBearer(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "audience") {
		t.Errorf("expected audience mismatch, got %v", err)
	}
}

func TestOIDC_WrongKid(t *testing.T) {
	idp := newFakeIdP(t)
	v := newValidator(t, idp, "mcp-test")

	tk := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": idp.srv.URL,
		"aud": "mcp-test",
		"sub": "user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tk.Header["kid"] = "unknown-kid"
	signed, err := tk.SignedString(idp.priv)
	if err != nil {
		t.Fatal(err)
	}

	_, err = v.ValidateBearer(context.Background(), signed)
	if err == nil {
		t.Fatal("expected unknown-kid error")
	}
}

func TestOIDC_AllowedClients(t *testing.T) {
	idp := newFakeIdP(t)
	v, err := NewOIDC(context.Background(), OIDCConfig{
		Enabled:          true,
		Issuer:           idp.srv.URL,
		Audience:         "mcp-test",
		AllowedClients:   []string{"client-a", "client-b"},
		ClockSkewSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	allowed := idp.sign(t, jwt.MapClaims{
		"iss": idp.srv.URL,
		"aud": "mcp-test",
		"sub": "user",
		"azp": "client-a",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.ValidateBearer(context.Background(), allowed); err != nil {
		t.Errorf("allowed client rejected: %v", err)
	}

	denied := idp.sign(t, jwt.MapClaims{
		"iss": idp.srv.URL,
		"aud": "mcp-test",
		"sub": "user",
		"azp": "client-c",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.ValidateBearer(context.Background(), denied); err == nil {
		t.Error("expected denied client to fail")
	}
}

func TestOIDC_DiscoveryFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := NewOIDC(context.Background(), OIDCConfig{Enabled: true, Issuer: srv.URL})
	if err == nil || !errors.Is(err, err) /* nontrivial */ {
		// non-nil err is enough; just verify we got it
	}
	if err == nil {
		t.Error("expected discovery failure")
	}
}
