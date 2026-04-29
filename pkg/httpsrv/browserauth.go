package httpsrv

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

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

	// Nonce ledger: every PKCE cookie carries a fresh nonce; on successful
	// callback we record it as used and reject replays for 10 minutes. This
	// is defense-in-depth on top of single-use IdP codes.
	usedNoncesMu sync.Mutex
	usedNonces   map[string]time.Time
}

// NewBrowserAuth wires the flow. The validator is the OIDC authenticator
// (typically the same one used by the MCP server's auth chain). The PKCE
// signing key is derived from the long-lived cookie secret with a domain
// separator so a leak of one cookie does not directly forge the other.
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
	pkceSecret := derivePKCESecret(cfg.Portal.CookieSecret)
	pkceStore, err := NewSessionStore("mcp_test_pkce", pkceSecret, cfg.Portal.CookieSecure, 0)
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
		httpc:        &http.Client{Timeout: 10 * time.Second},
		usedNonces:   make(map[string]time.Time),
	}
	if err := b.discoverEndpoints(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

// derivePKCESecret produces a PKCE-flow signing key from the long-lived
// cookie secret. We use HMAC-SHA256 with a fixed domain-separator label so
// the two flows have independent keys derived from a single configured
// secret. This protects against cross-flow forgery if the bytes of one
// signed blob ever leaked while the secret stayed safe.
func derivePKCESecret(cookieSecret string) string {
	mac := hmac.New(sha256.New, []byte(cookieSecret))
	_, _ = mac.Write([]byte("mcp-test/pkce/v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Mount adds the three handlers to the given mux.
func (b *BrowserAuth) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /portal/auth/login", b.handleLogin)
	mux.HandleFunc("GET /portal/auth/callback", b.handleCallback)
	mux.HandleFunc("POST /portal/auth/logout", b.handleLogout)
}

func (b *BrowserAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomString(32)
	if err != nil {
		http.Error(w, "rng failure", http.StatusInternalServerError)
		return
	}
	verifier, err := randomString(64)
	if err != nil {
		http.Error(w, "rng failure", http.StatusInternalServerError)
		return
	}
	nonce, err := randomString(16)
	if err != nil {
		http.Error(w, "rng failure", http.StatusInternalServerError)
		return
	}
	challenge := pkceChallenge(verifier)

	// Stash state + verifier + nonce in a short-lived signed cookie so we can
	// recover them in the callback without server-side storage. The nonce is
	// what we record as "used" after a successful callback to prevent replay.
	pl := SessionPayload{
		Identity: &auth.Identity{Claims: map[string]any{
			"state":    state,
			"verifier": verifier,
			"nonce":    nonce,
			"return":   sanitizeReturnPath(r.URL.Query().Get("return")),
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
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", b.cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if b.cfg.Audience != "" {
		q.Set("audience", b.cfg.Audience)
	}
	sep := "?"
	if strings.Contains(b.authzURL, "?") {
		sep = "&"
	}
	http.Redirect(w, r, b.authzURL+sep+q.Encode(), http.StatusFound)
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
	nonce, _ := pl.Identity.Claims["nonce"].(string)
	returnTo, _ := pl.Identity.Claims["return"].(string)

	if r.URL.Query().Get("state") != st || st == "" {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	if !b.consumeNonce(nonce) {
		http.Error(w, "replay rejected", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	idToken, err := b.exchangeCode(r.Context(), code, verifier)
	if err != nil {
		// Surface a generic 502 to the client; the IdP body may contain
		// tokens or sensitive details that should not be echoed downstream.
		b.logger.Warn("oidc code exchange failed", "err", err)
		http.Error(w, "code exchange failed", http.StatusBadGateway)
		return
	}

	identity, err := b.validator.ValidateBearer(r.Context(), idToken)
	if err != nil {
		b.logger.Warn("oidc id_token validation failed", "err", err)
		http.Error(w, "id_token validation failed", http.StatusUnauthorized)
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

	if returnTo == "" {
		returnTo = "/portal/"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (b *BrowserAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	b.sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

// sanitizeReturnPath enforces "must be a same-origin path." Browsers parse
// `//evil.com` as a scheme-relative URL to a different origin, and `/\evil`
// as a path containing a backslash that some IdPs/browsers normalize into
// `//`. We reject both alongside the obvious "must start with /" rule.
func sanitizeReturnPath(p string) string {
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	if strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) {
		return ""
	}
	return p
}

// consumeNonce records a one-time-use nonce. Returns false if it was already
// recorded within the TTL window. Stale entries are evicted opportunistically.
func (b *BrowserAuth) consumeNonce(nonce string) bool {
	if nonce == "" {
		return false
	}
	const ttl = 10 * time.Minute
	now := time.Now()
	b.usedNoncesMu.Lock()
	defer b.usedNoncesMu.Unlock()
	// Opportunistic eviction; ledger is small.
	for k, t := range b.usedNonces {
		if now.Sub(t) > ttl {
			delete(b.usedNonces, k)
		}
	}
	if _, used := b.usedNonces[nonce]; used {
		return false
	}
	b.usedNonces[nonce] = now
	return true
}

// discoverEndpoints fetches the OIDC discovery document for authz/token URLs.
//
// We deliberately do this in BrowserAuth rather than reaching into the
// validator: the validator only needs JWKS, but PKCE login also needs the
// authorization_endpoint and token_endpoint.
func (b *BrowserAuth) discoverEndpoints(ctx context.Context) error {
	discoveryURL := strings.TrimRight(b.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	resp, err := b.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discover %s: status %d", discoveryURL, resp.StatusCode)
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
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", b.baseURL+b.redirectPath)
	form.Set("client_id", b.cfg.ClientID)
	form.Set("code_verifier", verifier)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if b.cfg.ClientSecret != "" {
		req.SetBasicAuth(b.cfg.ClientID, b.cfg.ClientSecret)
	}
	resp, err := b.httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// Truncate IdP body to a hash + length, never embed the raw body in
		// the returned error: it can carry refresh tokens or other secrets
		// depending on IdP misbehavior.
		return "", fmt.Errorf("token endpoint returned %d (body %d bytes)", resp.StatusCode, len(body))
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
