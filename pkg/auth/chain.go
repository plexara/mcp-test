package auth

import (
	"context"
	"errors"
	"strings"
)

// ErrNotAuthenticated is returned when no auth method matched and anonymous is disabled.
var ErrNotAuthenticated = errors.New("not authenticated")

// OIDCValidator verifies an OIDC bearer token and returns the matching Identity.
type OIDCValidator interface {
	ValidateBearer(ctx context.Context, token string) (*Identity, error)
}

// Chain attempts API key auth first, then OIDC bearer, then anonymous (when allowed).
type Chain struct {
	allowAnonymous bool
	apiKeys        APIKeyStore
	oidc           OIDCValidator
}

// NewChain returns a chain. Either of apiKeys or oidc may be nil.
func NewChain(allowAnonymous bool, apiKeys APIKeyStore, oidc OIDCValidator) *Chain {
	return &Chain{allowAnonymous: allowAnonymous, apiKeys: apiKeys, oidc: oidc}
}

// Authenticate inspects the token stashed on ctx and returns the identity.
//
// Discrimination heuristic: a token starting with "ey" (typical JWT header) is
// tried as an OIDC bearer first; everything else is tried as an API key first.
// If both stores are configured, the second is attempted on miss.
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
		if id, err := c.oidc.ValidateBearer(ctx, tok); err == nil {
			return id, nil
		}
	}
	if c.apiKeys != nil {
		if id, err := c.apiKeys.Authenticate(ctx, tok); err == nil {
			return id, nil
		}
	}
	if !tryOIDCFirst && c.oidc != nil {
		if id, err := c.oidc.ValidateBearer(ctx, tok); err == nil {
			return id, nil
		}
	}
	return nil, ErrNotAuthenticated
}
