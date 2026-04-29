package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/plexara/mcp-test/pkg/config"
)

func TestFileAPIKeyStore_Authenticate(t *testing.T) {
	s := NewFileAPIKeyStore([]config.FileAPIKey{
		{Name: "alpha", Key: "secret-1", Description: "first"},
		{Name: "beta", Key: "secret-2"},
		{Name: "skipped", Key: ""}, // empty key must not authenticate
	})

	id, err := s.Authenticate(context.Background(), "secret-1")
	if err != nil {
		t.Fatalf("expected secret-1 to authenticate: %v", err)
	}
	if id.AuthType != "apikey" || id.APIKeyID != "alpha" {
		t.Errorf("identity wrong: %+v", id)
	}

	if _, err := s.Authenticate(context.Background(), ""); !errors.Is(err, ErrUnknownAPIKey) {
		t.Errorf("empty key must error, got %v", err)
	}
	if _, err := s.Authenticate(context.Background(), "nope"); !errors.Is(err, ErrUnknownAPIKey) {
		t.Errorf("unknown key must error, got %v", err)
	}
}

type stubOIDC struct {
	id  *Identity
	err error
}

func (s stubOIDC) ValidateBearer(_ context.Context, _ string) (*Identity, error) {
	return s.id, s.err
}

func TestChain_AnonymousFallback(t *testing.T) {
	c := NewChain(true, nil, nil)
	id, err := c.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id.AuthType != "anonymous" {
		t.Errorf("want anonymous, got %s", id.AuthType)
	}
}

func TestChain_APIKey(t *testing.T) {
	store := NewFileAPIKeyStore([]config.FileAPIKey{{Name: "k1", Key: "abc"}})
	c := NewChain(false, store, nil)

	ctx := WithToken(context.Background(), "abc")
	id, err := c.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id.AuthType != "apikey" {
		t.Errorf("want apikey, got %s", id.AuthType)
	}

	ctx = WithToken(context.Background(), "wrong")
	if _, err := c.Authenticate(ctx); !errors.Is(err, ErrNotAuthenticated) {
		t.Errorf("want not authenticated, got %v", err)
	}
}

func TestChain_OIDCPath(t *testing.T) {
	stub := stubOIDC{id: &Identity{Subject: "u1", AuthType: "oidc"}}
	c := NewChain(false, nil, stub)

	ctx := WithToken(context.Background(), "ey.fake.token")
	id, err := c.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id.AuthType != "oidc" {
		t.Errorf("want oidc, got %s", id.AuthType)
	}
}
