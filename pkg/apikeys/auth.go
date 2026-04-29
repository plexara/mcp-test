package apikeys

import (
	"context"

	"github.com/plexara/mcp-test/pkg/auth"
)

// AsAuthStore exposes the bcrypt-backed Store as an auth.DBKeyStore so the
// auth chain can call into it.
func (s *Store) AsAuthStore() auth.DBKeyStore { return authAdapter{s: s} }

type authAdapter struct{ s *Store }

func (a authAdapter) Authenticate(ctx context.Context, plaintext string) (*auth.DBKey, error) {
	k, err := a.s.Authenticate(ctx, plaintext)
	if err != nil {
		return nil, err
	}
	return &auth.DBKey{ID: k.ID, Name: k.Name}, nil
}
