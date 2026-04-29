package auth

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/plexara/mcp-test/pkg/config"
)

// ErrUnknownAPIKey indicates the presented key didn't match any configured key.
var ErrUnknownAPIKey = errors.New("unknown api key")

// APIKeyStore validates an API key and returns the matching Identity.
type APIKeyStore interface {
	Authenticate(ctx context.Context, key string) (*Identity, error)
}

// FileAPIKeyStore validates against a static set of plaintext keys.
//
// Comparison is constant-time. Keys with empty strings are skipped (so an
// unset env var doesn't accidentally enable an empty-string credential).
type FileAPIKeyStore struct {
	keys map[string]fileEntry
}

type fileEntry struct {
	name        string
	description string
}

// NewFileAPIKeyStore builds the store from config entries.
func NewFileAPIKeyStore(entries []config.FileAPIKey) *FileAPIKeyStore {
	s := &FileAPIKeyStore{keys: make(map[string]fileEntry)}
	for _, e := range entries {
		if e.Key == "" {
			continue
		}
		s.keys[e.Key] = fileEntry{name: e.Name, description: e.Description}
	}
	return s
}

// Authenticate matches key against every configured entry in constant time.
//
// We iterate every entry on every call rather than doing a map lookup to avoid
// timing leaks when the caller probes for valid prefixes.
func (s *FileAPIKeyStore) Authenticate(_ context.Context, key string) (*Identity, error) {
	if key == "" || len(s.keys) == 0 {
		return nil, ErrUnknownAPIKey
	}
	keyB := []byte(key)
	var matched *fileEntry
	for k, entry := range s.keys {
		if subtle.ConstantTimeCompare([]byte(k), keyB) == 1 {
			e := entry
			matched = &e
			// don't break; keep comparing to keep timing flat
		}
	}
	if matched == nil {
		return nil, ErrUnknownAPIKey
	}
	return &Identity{
		Subject:  "apikey:" + matched.name,
		Name:     matched.name,
		AuthType: "apikey",
		APIKeyID: matched.name,
	}, nil
}
