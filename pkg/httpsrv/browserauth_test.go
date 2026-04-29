package httpsrv

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
)

// fakeIdP serves discovery, JWKS, authorize (auto-approve, redirect), and
// token endpoints; enough to exercise the PKCE callback end-to-end.
type fakeIdP struct {
	srv       *httptest.Server
	priv      *rsa.PrivateKey
	codeStash map[string]string // code -> verifier (we don't actually use it but require its presence)
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{priv: priv, codeStash: map[string]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 idp.srv.URL,
			"jwks_uri":               idp.srv.URL + "/jwks",
			"authorization_endpoint": idp.srv.URL + "/authorize",
			"token_endpoint":         idp.srv.URL + "/token",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []any{map[string]any{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "k1", "n": n, "e": e,
			}},
		})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		// Auto-approve. Echo state, return a synthetic code.
		state := r.URL.Query().Get("state")
		redirect := r.URL.Query().Get("redirect_uri")
		idp.codeStash["test-code"] = "" // we don't enforce verifier match in this stub
		http.Redirect(w, r, redirect+"?code=test-code&state="+state, http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "missing code or code_verifier", http.StatusBadRequest)
			return
		}
		idTok := signToken(t, priv, idp.srv.URL, "mcp-test", "alice")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id_token":     idTok,
			"access_token": idTok, // doesn't matter for our tests
			"token_type":   "Bearer",
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func signToken(t *testing.T, priv *rsa.PrivateKey, iss, aud, sub string) string {
	t.Helper()
	tk := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   iss,
		"aud":   aud,
		"sub":   sub,
		"email": sub + "@example.com",
		"name":  strings.ToTitle(sub[:1]) + sub[1:],
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	tk.Header["kid"] = "k1"
	out, err := tk.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// startBrowserAuth boots a portal with the IdP wired in and returns the
// httptest.Server (the "browser-facing" portal) plus the BrowserAuth.
func startBrowserAuth(t *testing.T, idp *fakeIdP) (*httptest.Server, *BrowserAuth) {
	t.Helper()

	// Portal serves /portal/auth/* via BrowserAuth.Mount.
	mux := http.NewServeMux()

	// Two-step: we need the portal's own URL inside the BaseURL config so that
	// redirect_uri matches. Spin a placeholder server first, then build the
	// BrowserAuth, then mount its handlers.
	portal := httptest.NewServer(mux)
	t.Cleanup(portal.Close)

	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: portal.URL},
		Portal: config.PortalConfig{
			CookieName:       "mcp_test_session",
			CookieSecret:     "0123456789abcdef0123456789abcdef",
			OIDCRedirectPath: "/portal/auth/callback",
			CookieSecure:     false,
		},
		OIDC: config.OIDCConfig{
			Enabled:          true,
			Issuer:           idp.srv.URL,
			Audience:         "mcp-test",
			ClientID:         "mcp-test-portal",
			ClockSkewSeconds: 5,
			JWKSCacheTTL:     time.Hour,
		},
	}

	v, err := auth.NewOIDC(context.Background(), cfg.OIDC)
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	sessions, err := NewSessionStore(cfg.Portal.CookieName, cfg.Portal.CookieSecret, false, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	ba, err := NewBrowserAuth(context.Background(), cfg, v, sessions, logger)
	if err != nil {
		t.Fatalf("NewBrowserAuth: %v", err)
	}
	ba.Mount(mux)

	return portal, ba
}

func TestBrowserAuth_PKCEFlow(t *testing.T) {
	idp := newFakeIdP(t)
	portal, _ := startBrowserAuth(t, idp)

	// Hit /portal/auth/login; must NOT follow redirects so we can inspect the
	// Location header and the pkce cookie.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(portal.URL + "/portal/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/authorize") {
		t.Fatalf("login Location does not point to IdP authorize: %s", loc)
	}

	// Follow to the IdP, which will redirect back to /portal/auth/callback.
	idpResp, err := client.Get(loc)
	if err != nil {
		t.Fatal(err)
	}
	if idpResp.StatusCode != http.StatusFound {
		t.Fatalf("IdP authorize: status = %d, want 302", idpResp.StatusCode)
	}
	cbLoc := idpResp.Header.Get("Location")
	if !strings.HasPrefix(cbLoc, portal.URL+"/portal/auth/callback") {
		t.Fatalf("IdP callback Location wrong: %s", cbLoc)
	}

	// Hit the callback; should issue session cookie and redirect to /portal/.
	cbResp, err := client.Get(cbLoc)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(cbResp.Body)
		t.Fatalf("callback: status = %d, body=%s", cbResp.StatusCode, body)
	}
	final := cbResp.Header.Get("Location")
	if final != "/portal/" {
		t.Errorf("post-callback Location = %s, want /portal/", final)
	}

	// The session cookie should now be in the jar.
	portalURL, _ := neturl.Parse(portal.URL)
	hasSession := false
	for _, c := range jar.Cookies(portalURL) {
		if c.Name == "mcp_test_session" && c.Value != "" {
			hasSession = true
		}
	}
	if !hasSession {
		t.Errorf("session cookie not set; cookies=%v", jar.Cookies(portalURL))
	}
}
