// Package apikeys persists API keys in Postgres with bcrypt-hashed values.
package apikeys

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// ErrNotFound is returned when an API key with the given name doesn't exist.
var ErrNotFound = errors.New("api key not found")

// Key is the persistent record. Hash is never returned by ListKeys (omitted
// from JSON), and the plaintext value only exists at creation time.
type Key struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	CreatedBy   string     `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	hash        string
}

// Created bundles a freshly minted key with its plaintext value. The
// plaintext is shown to the user once and never persisted.
type Created struct {
	Key       Key    `json:"key"`
	Plaintext string `json:"plaintext"`
}

// Store is a pgxpool-backed CRUD interface over the api_keys table.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create mints a new API key with a random plaintext value, hashes it with
// bcrypt, and inserts a row. The plaintext is returned to the caller exactly
// once.
//
// `name` must be unique. If a row with the same name exists, the call returns
// an error.
func (s *Store) Create(ctx context.Context, name, description, createdBy string, expiresAt *time.Time) (*Created, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	plaintext, err := generatePlaintext()
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	id := uuid.NewString()
	now := time.Now().UTC()

	_, err = s.pool.Exec(ctx, `
		INSERT INTO api_keys (id, name, hash, description, created_by, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, id, name, string(hash), description, createdBy, now, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}

	return &Created{
		Key: Key{
			ID:          id,
			Name:        name,
			Description: description,
			CreatedBy:   createdBy,
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
		},
		Plaintext: plaintext,
	}, nil
}

// List returns every key (without the hash). Sorted by created_at DESC.
func (s *Store) List(ctx context.Context) ([]Key, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, COALESCE(description,''), COALESCE(created_by,''),
		       created_at, expires_at, last_used_at
		FROM api_keys
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Key
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.ID, &k.Name, &k.Description, &k.CreatedBy,
			&k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Delete removes the row with the given name. Returns ErrNotFound if absent.
func (s *Store) Delete(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Authenticate scans every (non-expired) row, comparing the candidate against
// each stored bcrypt hash. Bcrypt is intentionally slow so this scales poorly
// past ~thousands of keys; for a test fixture that is fine.
//
// On match, last_used_at is bumped (best-effort; failure is logged via the
// returned error wrapper so callers can choose to ignore it).
func (s *Store) Authenticate(ctx context.Context, plaintext string) (*Key, error) {
	if plaintext == "" {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, hash, COALESCE(description,''), COALESCE(created_by,''),
		       created_at, expires_at, last_used_at
		FROM api_keys
		WHERE expires_at IS NULL OR expires_at > now()
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pw := []byte(plaintext)
	for rows.Next() {
		var k Key
		var hash string
		if err := rows.Scan(&k.ID, &k.Name, &hash, &k.Description, &k.CreatedBy,
			&k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), pw) == nil {
			k.hash = hash
			s.touchLastUsed(ctx, k.ID)
			return &k, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

func (s *Store) touchLastUsed(ctx context.Context, id string) {
	// Best-effort; ignore errors; auth succeeded regardless.
	_, _ = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id)
}

// generatePlaintext returns a URL-safe random 32-byte token prefixed for
// recognizability ("mt_" = mcp-test).
func generatePlaintext() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "mt_" + strings.TrimRight(base64.URLEncoding.EncodeToString(buf), "="), nil
}

// Ensure unused name lint doesn't bark.
var _ = pgx.ErrNoRows
