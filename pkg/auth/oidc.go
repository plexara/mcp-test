package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/plexara/mcp-test/pkg/config"
)

// OIDCConfig is the runtime view of config.OIDCConfig used by the validator.
//
// Defining a thin internal type lets pkg/auth stay decoupled from config in
// places where that's helpful (e.g. tests).
type OIDCConfig = config.OIDCConfig

// OIDCAuthenticator verifies bearer JWTs issued by an external OIDC provider. It
// caches JWKS public keys by kid, refreshing on a TTL.
type OIDCAuthenticator struct {
	cfg        OIDCConfig
	httpClient *http.Client
	jwksURL    string

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
}

// NewOIDC returns an authenticator. It eagerly fetches the OpenID Discovery
// document so that misconfiguration (wrong issuer, network error) fails at
// startup instead of on the first request.
func NewOIDC(ctx context.Context, cfg OIDCConfig) (*OIDCAuthenticator, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("oidc: issuer is required")
	}
	v := &OIDCAuthenticator{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		keys:       map[string]*rsa.PublicKey{},
	}
	if err := v.discover(ctx); err != nil {
		return nil, err
	}
	if err := v.refreshJWKS(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

// ValidateBearer parses, verifies, and extracts identity claims from token.
func (v *OIDCAuthenticator) ValidateBearer(ctx context.Context, token string) (*Identity, error) {
	if token == "" {
		return nil, errors.New("empty bearer token")
	}

	parser := jwt.NewParser(
		jwt.WithLeeway(time.Duration(v.cfg.ClockSkewSeconds)*time.Second),
		jwt.WithIssuer(v.cfg.Issuer),
	)

	keyfunc := func(t *jwt.Token) (any, error) {
		if v.cfg.SkipSignatureVerification {
			// jwt.Parse insists on a key for any signed token. Returning nil here
			// triggers an "invalid key type" error, so we hand back a zero RSA
			// public key; combined with our explicit unsafe-parse path below,
			// this branch should never execute.
			return nil, errors.New("signature verification skipped; do not call keyfunc")
		}
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		key, err := v.publicKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	var (
		parsed *jwt.Token
		err    error
	)
	if v.cfg.SkipSignatureVerification {
		parsed, _, err = parser.ParseUnverified(token, jwt.MapClaims{})
	} else {
		parsed, err = parser.Parse(token, keyfunc)
	}
	if err != nil {
		return nil, fmt.Errorf("parse jwt: %w", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("jwt claims not a map")
	}

	if v.cfg.Audience != "" {
		if !audienceMatches(claims["aud"], v.cfg.Audience) {
			return nil, fmt.Errorf("audience mismatch: want %q", v.cfg.Audience)
		}
	}
	if len(v.cfg.AllowedClients) > 0 {
		if !clientAllowed(claims, v.cfg.AllowedClients) {
			return nil, errors.New("client not in allowed_clients")
		}
	}

	id := &Identity{
		AuthType: "oidc",
		Claims:   map[string]any(claims),
	}
	if sub, _ := claims["sub"].(string); sub != "" {
		id.Subject = sub
	}
	if email, _ := claims["email"].(string); email != "" {
		id.Email = email
	}
	if name, _ := claims["name"].(string); name != "" {
		id.Name = name
	}
	if id.Subject == "" {
		// Some providers use unique_name or preferred_username instead.
		if v, _ := claims["preferred_username"].(string); v != "" {
			id.Subject = v
		}
	}
	return id, nil
}

func (v *OIDCAuthenticator) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	if time.Now().Before(v.expiresAt) {
		if k, ok := v.keys[kid]; ok {
			v.mu.RUnlock()
			return k, nil
		}
	}
	v.mu.RUnlock()

	if err := v.refreshJWKS(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	k, ok := v.keys[kid]
	if !ok {
		return nil, fmt.Errorf("no public key for kid %q", kid)
	}
	return k, nil
}

type discoveryDoc struct {
	JWKSURI string `json:"jwks_uri"`
}

func (v *OIDCAuthenticator) discover(ctx context.Context) error {
	url := trimRightSlash(v.cfg.Issuer) + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discover %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discover %s: status %d", url, resp.StatusCode)
	}
	var d discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return fmt.Errorf("decode discovery: %w", err)
	}
	if d.JWKSURI == "" {
		return errors.New("discovery doc missing jwks_uri")
	}
	v.jwksURL = d.JWKSURI
	return nil
}

type jwksDoc struct {
	Keys []jwkRSA `json:"keys"`
}

type jwkRSA struct {
	KTY string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (v *OIDCAuthenticator) refreshJWKS(ctx context.Context) error {
	if v.jwksURL == "" {
		if err := v.discover(ctx); err != nil {
			return err
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}
	var d jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(d.Keys))
	for _, jwk := range d.Keys {
		if jwk.KTY != "RSA" {
			continue
		}
		if jwk.Use != "" && jwk.Use != "sig" {
			continue
		}
		k, err := decodeRSA(jwk.N, jwk.E)
		if err != nil {
			continue
		}
		keys[jwk.Kid] = k
	}

	ttl := v.cfg.JWKSCacheTTL
	if ttl == 0 {
		ttl = time.Hour
	}

	v.mu.Lock()
	v.keys = keys
	v.expiresAt = time.Now().Add(ttl)
	v.mu.Unlock()
	return nil
}

func decodeRSA(nB64, eB64 string) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	e, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	if len(e) > 4 {
		return nil, fmt.Errorf("e too large: %d bytes", len(e))
	}
	var ei int
	for _, b := range e {
		ei = ei<<8 + int(b)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: ei}, nil
}

func audienceMatches(aud any, want string) bool {
	switch v := aud.(type) {
	case string:
		return v == want
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func clientAllowed(claims jwt.MapClaims, allowed []string) bool {
	for _, claim := range []string{"azp", "client_id", "appid"} {
		if v, ok := claims[claim].(string); ok && v != "" {
			for _, a := range allowed {
				if a == v {
					return true
				}
			}
		}
	}
	return false
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
