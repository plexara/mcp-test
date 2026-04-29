package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/plexara/mcp-test/pkg/config"
)

type stubDB struct {
	want string
	key  *DBKey
	err  error
}

func (s stubDB) Authenticate(_ context.Context, plaintext string) (*DBKey, error) {
	if plaintext != s.want {
		return nil, s.err
	}
	return s.key, nil
}

func TestDBAPIKeyStore_Authenticate(t *testing.T) {
	wrap := NewDBAPIKeyStore(stubDB{want: "abc", key: &DBKey{ID: "k-1", Name: "ci"}})
	id, err := wrap.Authenticate(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if id.AuthType != "apikey" || id.APIKeyID != "ci" || id.Subject != "apikey:ci" {
		t.Errorf("identity wrong: %+v", id)
	}

	// Underlying error bubbles up.
	wrap = NewDBAPIKeyStore(stubDB{want: "abc", err: errors.New("not found")})
	if _, err := wrap.Authenticate(context.Background(), "wrong"); err == nil {
		t.Error("want error on miss")
	}

	// Underlying returning (nil, nil) is treated as unknown.
	wrap = NewDBAPIKeyStore(stubDB{want: "abc"})
	if _, err := wrap.Authenticate(context.Background(), "wrong"); !errors.Is(err, ErrUnknownAPIKey) {
		t.Errorf("nil result should map to ErrUnknownAPIKey, got %v", err)
	}
}

func TestCombineKeyStores(t *testing.T) {
	file := NewFileAPIKeyStore([]config.FileAPIKey{{Name: "f", Key: "file-secret"}})
	db := NewDBAPIKeyStore(stubDB{want: "db-secret", key: &DBKey{ID: "1", Name: "d"}})

	c := CombineKeyStores(file, db)

	// File hit first.
	id, err := c.Authenticate(context.Background(), "file-secret")
	if err != nil || id.APIKeyID != "f" {
		t.Errorf("file first: %+v err=%v", id, err)
	}

	// DB fallback.
	id, err = c.Authenticate(context.Background(), "db-secret")
	if err != nil || id.APIKeyID != "d" {
		t.Errorf("db fallback: %+v err=%v", id, err)
	}

	// Both miss.
	if _, err := c.Authenticate(context.Background(), "nope"); err == nil {
		t.Error("expected miss error")
	}

	// Nil cases.
	if got := CombineKeyStores(nil, nil); got != nil {
		t.Error("two nils should be nil")
	}
	if got := CombineKeyStores(file, nil); got != file {
		t.Error("(file, nil) should pass through file")
	}
	if got := CombineKeyStores(nil, db); got != db {
		t.Error("(nil, db) should pass through db")
	}
}
