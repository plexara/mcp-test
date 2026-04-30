package auth

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrNotAuthenticated is returned when no auth method matched and anonymous is disabled.
var ErrNotAuthenticated = errors.New("not authenticated")

// OIDCValidator verifies an OIDC bearer token and returns the matching Identity.
type OIDCValidator interface {
	ValidateBearer(ctx context.Context, token string) (*Identity, error)
}

// Chain attempts API key auth first, then OIDC bearer, then anonymous (when allowed).
//
// Each authenticator's rejection is recorded in the slog so operators can
// tell whether a 401 was caused by an expired JWT, a JWKS miss, an
// unknown API key name, or simply the absence of any credential. The
// first failure per remote_addr within a 60-second window is emitted at
// WARN; subsequent failures in the same window are emitted at DEBUG to
// keep a noisy scanner from drowning the log. Token bytes embedded in
// validator error messages are redacted (JWT-shape stripped) before the
// log line is written.
type Chain struct {
	allowAnonymous bool
	apiKeys        APIKeyStore
	oidc           OIDCValidator
	logger         *slog.Logger
	rateLimit      *failureRateLimiter
}

// NewChain returns a chain. Either of apiKeys or oidc may be nil. The
// returned chain logs auth failures via slog.Default(); call SetLogger to
// attach a tagged logger.
func NewChain(allowAnonymous bool, apiKeys APIKeyStore, oidc OIDCValidator) *Chain {
	return &Chain{
		allowAnonymous: allowAnonymous,
		apiKeys:        apiKeys,
		oidc:           oidc,
		logger:         slog.Default(),
		rateLimit:      newFailureRateLimiter(time.Minute),
	}
}

// SetLogger replaces the chain's logger and returns the receiver for
// chaining. Pass nil to fall back to slog.Default(). This mutates the
// receiver; do not call it concurrently with Authenticate.
func (c *Chain) SetLogger(l *slog.Logger) *Chain {
	if l == nil {
		l = slog.Default()
	}
	c.logger = l
	return c
}

// WithLogger is a deprecated alias for SetLogger kept for binary
// compatibility with the original PR. Prefer SetLogger.
//
// Deprecated: use SetLogger.
func (c *Chain) WithLogger(l *slog.Logger) *Chain { return c.SetLogger(l) }

// Authenticate inspects the token stashed on ctx and returns the identity.
//
// Discrimination heuristic: a token starting with "ey" (typical JWT header) is
// tried as an OIDC bearer first; everything else is tried as an API key first.
// If both stores are configured, the second is attempted on miss.
//
// Auth failures from each configured store are logged with method,
// request_id, remote_addr, and a redacted error before the chain falls
// through. ErrNotAuthenticated is returned only when every configured
// store rejected the token (or no token was present and anonymous is
// off); the per-authenticator errors stay in the log so operators can
// diagnose which validation step actually failed.
func (c *Chain) Authenticate(ctx context.Context) (*Identity, error) {
	tok := GetToken(ctx)
	if tok == "" {
		if c.allowAnonymous {
			return Anonymous(), nil
		}
		return nil, ErrNotAuthenticated
	}

	tryOIDCFirst := strings.HasPrefix(tok, "ey")

	if tryOIDCFirst && c.oidc != nil {
		id, err := c.oidc.ValidateBearer(ctx, tok)
		if err == nil {
			return id, nil
		}
		c.logAuthFailure(ctx, "oidc", err)
	}
	if c.apiKeys != nil {
		id, err := c.apiKeys.Authenticate(ctx, tok)
		if err == nil {
			return id, nil
		}
		c.logAuthFailure(ctx, "apikey", err)
	}
	if !tryOIDCFirst && c.oidc != nil {
		id, err := c.oidc.ValidateBearer(ctx, tok)
		if err == nil {
			return id, nil
		}
		c.logAuthFailure(ctx, "oidc", err)
	}
	return nil, ErrNotAuthenticated
}

// logAuthFailure emits one log line per rejection. The first failure per
// remote_addr per window is WARN; subsequent failures in the window are
// DEBUG. Correlation fields (request_id, remote_addr) come from ctx so
// the line can be joined against the matching audit_events row.
func (c *Chain) logAuthFailure(ctx context.Context, method string, err error) {
	level := slog.LevelDebug
	remote := GetRemoteAddr(ctx)
	if c.rateLimit.shouldWarn(remote) {
		level = slog.LevelWarn
	}
	c.logger.LogAttrs(ctx, level, "auth: token rejected",
		slog.String("method", method),
		slog.String("error", redactTokenLikes(err.Error())),
		slog.String("request_id", GetRequestID(ctx)),
		slog.String("remote_addr", remote),
	)
}

// jwtPattern matches three base64-url segments separated by dots, the
// classic JWT shape. The minimum-length thresholds avoid stripping
// short identifiers that happen to contain dots; real JWT segments are
// well above 8 characters.
var jwtPattern = regexp.MustCompile(`[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`)

// redactTokenLikes strips JWT-shaped substrings from a string so a
// validator that quoted the rejected token in its error message can't
// leak token bytes through the auth-failure log. The chain doesn't try
// to scrub every conceivable secret shape; this is defense-in-depth on
// top of the validator-error-doesn't-include-tokens contract.
func redactTokenLikes(s string) string {
	return jwtPattern.ReplaceAllString(s, "[redacted-jwt]")
}

// failureRateLimiter keeps one entry per remote_addr noting when the
// last WARN-level auth-failure log was emitted for that source. The
// first failure in a window earns WARN; subsequent failures in the
// same window are demoted to DEBUG. Stale entries are evicted lazily
// during shouldWarn calls so the map stays bounded under steady load.
type failureRateLimiter struct {
	window  time.Duration
	mu      sync.Mutex
	lastLog map[string]time.Time
}

func newFailureRateLimiter(window time.Duration) *failureRateLimiter {
	if window <= 0 {
		window = time.Minute
	}
	return &failureRateLimiter{
		window:  window,
		lastLog: make(map[string]time.Time),
	}
}

// shouldWarn returns true the first time a key is seen within a window,
// false on subsequent calls until the window elapses. Empty keys (no
// remote_addr available) never rate-limit; every such call returns
// true so requests without correlation context still surface.
func (r *failureRateLimiter) shouldWarn(key string) bool {
	if r == nil {
		return true
	}
	if key == "" {
		return true
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	// Opportunistic eviction; the ledger is small under normal load and
	// this keeps it bounded under sustained pressure.
	for k, t := range r.lastLog {
		if now.Sub(t) > r.window {
			delete(r.lastLog, k)
		}
	}
	if last, ok := r.lastLog[key]; ok && now.Sub(last) <= r.window {
		return false
	}
	r.lastLog[key] = now
	return true
}
