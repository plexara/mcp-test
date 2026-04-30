package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// ErrNotAuthenticated is returned when no auth method matched and anonymous is disabled.
var ErrNotAuthenticated = errors.New("not authenticated")

// OIDCValidator verifies an OIDC bearer token and returns the matching Identity.
type OIDCValidator interface {
	ValidateBearer(ctx context.Context, token string) (*Identity, error)
}

// Chain attempts API key auth first, then OIDC bearer, then anonymous (when allowed).
//
// Each authenticator's rejection is logged at WARN before the chain falls
// through, so operators can tell whether a 401 was caused by an expired
// JWT, a JWKS miss, an unknown API key name, or simply the absence of any
// credential. The token itself is never logged; only the underlying error.
type Chain struct {
	allowAnonymous bool
	apiKeys        APIKeyStore
	oidc           OIDCValidator
	logger         *slog.Logger
}

// NewChain returns a chain. Either of apiKeys or oidc may be nil. The
// returned chain logs auth failures via slog.Default(); call WithLogger
// to attach a tagged logger.
func NewChain(allowAnonymous bool, apiKeys APIKeyStore, oidc OIDCValidator) *Chain {
	return &Chain{
		allowAnonymous: allowAnonymous,
		apiKeys:        apiKeys,
		oidc:           oidc,
		logger:         slog.Default(),
	}
}

// WithLogger returns the receiver after replacing the chain's logger.
// Pass nil to fall back to slog.Default().
func (c *Chain) WithLogger(l *slog.Logger) *Chain {
	if l == nil {
		l = slog.Default()
	}
	c.logger = l
	return c
}

// Authenticate inspects the token stashed on ctx and returns the identity.
//
// Discrimination heuristic: a token starting with "ey" (typical JWT header) is
// tried as an OIDC bearer first; everything else is tried as an API key first.
// If both stores are configured, the second is attempted on miss.
//
// Auth failures from each configured store are logged at WARN before the
// chain falls through. ErrNotAuthenticated is returned only when every
// configured store rejected the token (or no token was present and
// anonymous is off); the per-authenticator errors stay in the log so
// operators can diagnose which validation step actually failed.
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
		c.logAuthFailure("oidc", err)
	}
	if c.apiKeys != nil {
		id, err := c.apiKeys.Authenticate(ctx, tok)
		if err == nil {
			return id, nil
		}
		c.logAuthFailure("apikey", err)
	}
	if !tryOIDCFirst && c.oidc != nil {
		id, err := c.oidc.ValidateBearer(ctx, tok)
		if err == nil {
			return id, nil
		}
		c.logAuthFailure("oidc", err)
	}
	return nil, ErrNotAuthenticated
}

// logAuthFailure emits a single WARN line tagged with the authenticator
// that rejected the token. The token itself is never included; only the
// underlying error is, since validators construct their own messages
// without echoing the credential bytes.
func (c *Chain) logAuthFailure(method string, err error) {
	if c.logger == nil {
		return
	}
	c.logger.Warn("auth: token rejected",
		"method", method,
		"error", err,
	)
}
