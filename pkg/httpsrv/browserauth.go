package httpsrv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/config"
)

// BrowserAuth implements the OIDC PKCE login flow for the portal SPA.
//
// Endpoints:
//   - GET  /portal/auth/login   ; generates state + code_verifier, sets a
//     short-lived signed cookie, redirects to the IdP authorization endpoint.
//   - GET  /portal/auth/callback; validates state cookie, exchanges code for
//     tokens, validates the ID token via the OIDC validator, and issues the
//     long-lived session cookie carrying the resolved Identity.
//   - POST /portal/auth/logout  ; clears the session cookie.
type BrowserAuth struct {
	cfg          config.OIDCConfig
	baseURL      string
	redirectPath string
	validator    *auth.OIDCAuthenticator
	sessions     *SessionStore
	pkceCookie   *SessionStore // separate signing for the short-lived state cookie
	logger       *slog.Logger

	authzURL string
	tokenURL string
	httpc    *http.Client
}

// NewBrowserAuth wires the flow. The validator is the OIDC authenticator
// (typically the same one used by the MCP server's auth chain). pkceSecret
// must be at least 16 bytes; sessions is the long-lived cookie store.
func NewBrowserAuth(
	ctx context.Context,
	cfg *config.Config,
	validator *auth.OIDCAuthenticator,
	sessions *SessionStore,
	logger *slog.Logger,
) (*BrowserAuth, error) {
	if !cfg.OIDC.Enabled {
		return nil, fmt.Errorf("oidc is not enabled")
	}
	if validator == nil {
		return nil, fmt.Errorf("validator is required")
	}
	pkceStore, err := NewSessionStore("mcp_test_pkce", "pkce|"+cfg.Portal.CookieSecret, cfg.Portal.CookieSecure, 0)
	if err != nil {
		return nil, err
	}

	b := &BrowserAuth{
		cfg:          cfg.OIDC,
		baseURL:      strings.TrimRight(cfg.Server.BaseURL, "/"),
		redirectPath: cfg.Portal.OIDCRedirectPath,
		validator:    validator,
		sessions:     sessions,
		pkceCookie:   pkceStore,
		logger:       logger,
		httpc:        &http.Client{},
	}
	if err := b.discoverEndpoints(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

// Mount adds the three handlers to the given mux.
func (b *BrowserAuth) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /portal/auth/login", b.handleLogin)
	mux.HandleFunc("GET /portal/auth/callback", b.handleCallback)
	mux.HandleFunc("POST /portal/auth/logout", b.handleLogout)
}

func (b *BrowserAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, _ := randomString(32)
	verifier, _ := randomString(64)
	challenge := pkceChallenge(verifier)

	// Stash state + verifier in a short-lived signed cookie so we can recover
	// them in the callback without server-side storage.
	pl := SessionPayload{
		Identity: &auth.Identity{Claims: map[string]any{
			"state":    state,
			"verifier": verifier,
			"return":   r.URL.Query().Get("return"),
		}},
	}
	encoded, err := b.pkceCookie.encode(pl)
	if err != nil {
		http.Error(w, "encode pkce: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// #nosec G124 -- Secure is set from config (dev/HTTP-only deployments need false).
	http.SetCookie(w, &http.Cookie{
		Name:     b.pkceCookie.cookieName,
		Value:    encoded,
		Path:     "/portal/auth/",
		HttpOnly: true,
		Secure:   b.pkceCookie.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	redirectURI := b.baseURL + b.redirectPath
	u := b.authzURL
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	u = u + sep +
		"response_type=code" +
		"&client_id=" + httpQueryEscape(b.cfg.ClientID) +
		"&redirect_uri=" + httpQueryEscape(redirectURI) +
		"&scope=" + httpQueryEscape("openid email profile") +
		"&state=" + httpQueryEscape(state) +
		"&code_challenge=" + httpQueryEscape(challenge) +
		"&code_challenge_method=S256"
	if b.cfg.Audience != "" {
		u += "&audience=" + httpQueryEscape(b.cfg.Audience)
	}
	http.Redirect(w, r, u, http.StatusFound)
}

func (b *BrowserAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(b.pkceCookie.cookieName)
	if err != nil {
		http.Error(w, "missing pkce cookie", http.StatusBadRequest)
		return
	}
	pl, err := b.pkceCookie.decode(c.Value)
	if err != nil {
		http.Error(w, "bad pkce cookie", http.StatusBadRequest)
		return
	}
	st, _ := pl.Identity.Claims["state"].(string)
	verifier, _ := pl.Identity.Claims["verifier"].(string)
	returnTo, _ := pl.Identity.Claims["return"].(string)

	if r.URL.Query().Get("state") != st || st == "" {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	idToken, err := b.exchangeCode(r.Context(), code, verifier)
	if err != nil {
		http.Error(w, "code exchange: "+err.Error(), http.StatusBadGateway)
		return
	}

	identity, err := b.validator.ValidateBearer(r.Context(), idToken)
	if err != nil {
		http.Error(w, "validate id_token: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Clear the short-lived PKCE cookie.
	// #nosec G124 -- Secure follows config; SameSite default is fine for clears.
	http.SetCookie(w, &http.Cookie{
		Name:     b.pkceCookie.cookieName,
		Value:    "",
		Path:     "/portal/auth/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   b.pkceCookie.secure,
		SameSite: http.SameSiteLaxMode,
	})

	if err := b.sessions.Issue(w, identity); err != nil {
		http.Error(w, "issue session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if returnTo == "" || !strings.HasPrefix(returnTo, "/") {
		returnTo = "/portal/"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (b *BrowserAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	b.sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

// discoverEndpoints fetches the OIDC discovery document for authz/token URLs.
//
// We deliberately do this in BrowserAuth rather than reaching into the
// validator: the validator only needs JWKS, but PKCE login also needs the
// authorization_endpoint and token_endpoint.
func (b *BrowserAuth) discoverEndpoints(ctx context.Context) error {
	url := strings.TrimRight(b.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := b.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discover %s: status %d", url, resp.StatusCode)
	}
	var doc struct {
		AuthzEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("decode discovery: %w", err)
	}
	if doc.AuthzEndpoint == "" || doc.TokenEndpoint == "" {
		return fmt.Errorf("discovery missing authorization_endpoint or token_endpoint")
	}
	b.authzURL = doc.AuthzEndpoint
	b.tokenURL = doc.TokenEndpoint
	return nil
}

func (b *BrowserAuth) exchangeCode(ctx context.Context, code, verifier string) (string, error) {
	form := strings.NewReader(strings.Join([]string{
		"grant_type=authorization_code",
		"code=" + httpQueryEscape(code),
		"redirect_uri=" + httpQueryEscape(b.baseURL+b.redirectPath),
		"client_id=" + httpQueryEscape(b.cfg.ClientID),
		"code_verifier=" + httpQueryEscape(verifier),
	}, "&"))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.tokenURL, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if b.cfg.ClientSecret != "" {
		req.SetBasicAuth(b.cfg.ClientID, b.cfg.ClientSecret)
	}
	resp, err := b.httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tok.IDToken == "" {
		return "", fmt.Errorf("token response missing id_token")
	}
	return tok.IDToken, nil
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// httpQueryEscape avoids importing net/url just for one helper.
func httpQueryEscape(s string) string {
	// Match url.QueryEscape semantics for the characters we expect to encounter.
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteRune(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
