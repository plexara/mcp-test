package auth

import "context"

// DBKeyStore is satisfied by *apikeys.Store. We declare it here as a tiny
// interface so pkg/auth doesn't import pkg/apikeys (avoiding a dependency
// cycle through internal/server, which imports both).
type DBKeyStore interface {
	Authenticate(ctx context.Context, plaintext string) (*DBKey, error)
}

// DBKey is the minimal projection of an apikeys.Key needed to build an Identity.
type DBKey struct {
	ID   string
	Name string
}

// NewDBAPIKeyStore wraps a DBKeyStore so the auth chain can use it via the
// APIKeyStore interface.
func NewDBAPIKeyStore(db DBKeyStore) APIKeyStore {
	return &dbAPIKeyStore{db: db}
}

type dbAPIKeyStore struct {
	db DBKeyStore
}

func (s *dbAPIKeyStore) Authenticate(ctx context.Context, key string) (*Identity, error) {
	k, err := s.db.Authenticate(ctx, key)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, ErrUnknownAPIKey
	}
	return &Identity{
		Subject:  "apikey:" + k.Name,
		Name:     k.Name,
		AuthType: "apikey",
		APIKeyID: k.Name,
	}, nil
}

// CombineKeyStores tries the file store first (cheap, constant-time) before
// falling back to the DB store (bcrypt scan). Either may be nil.
func CombineKeyStores(file APIKeyStore, db APIKeyStore) APIKeyStore {
	switch {
	case file == nil && db == nil:
		return nil
	case db == nil:
		return file
	case file == nil:
		return db
	}
	return &combinedKeyStore{file: file, db: db}
}

type combinedKeyStore struct {
	file APIKeyStore
	db   APIKeyStore
}

func (c *combinedKeyStore) Authenticate(ctx context.Context, key string) (*Identity, error) {
	if id, err := c.file.Authenticate(ctx, key); err == nil {
		return id, nil
	}
	return c.db.Authenticate(ctx, key)
}
