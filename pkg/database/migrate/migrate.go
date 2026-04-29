// Package migrate runs golang-migrate against an embedded migrations FS.
package migrate

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Up applies all pending migrations using the given Postgres URL.
func Up(databaseURL string) error {
	return runMigration(databaseURL, func(m *migrate.Migrate) error { return m.Up() })
}

// Down rolls back all migrations. Used in tests only.
func Down(databaseURL string) error {
	return runMigration(databaseURL, func(m *migrate.Migrate) error { return m.Down() })
}

func runMigration(databaseURL string, op func(*migrate.Migrate) error) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, toMigrateURL(databaseURL))
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := op(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// toMigrateURL rewrites a Postgres DSN to the scheme golang-migrate's pgx/v5
// driver registers under. The pgx/v5 driver registers as "pgx5", so a stock
// "postgres://..." DSN won't be matched.
func toMigrateURL(dsn string) string {
	switch {
	case strings.HasPrefix(dsn, "postgres://"):
		return "pgx5://" + strings.TrimPrefix(dsn, "postgres://")
	case strings.HasPrefix(dsn, "postgresql://"):
		return "pgx5://" + strings.TrimPrefix(dsn, "postgresql://")
	}
	return dsn
}

// Ensure the pgx/v5 driver is registered with golang-migrate.
var _ = pgx.Postgres{}
