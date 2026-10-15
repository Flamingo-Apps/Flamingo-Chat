// Package migrations embeds the moderation service's SQL schema
// migrations into the compiled binary - see
// services/chat/migrations/migrations.go's doc comment for why
// MigrationsTable is service-specific (INC-0008).
package migrations

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed *.sql
var files embed.FS

// Run applies any pending migrations.
func Run(sqlDB *sql.DB) error {
	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{MigrationsTable: "moderation_schema_migrations"})
	if err != nil {
		return fmt.Errorf("migrations: pgx driver: %w", err)
	}

	src, err := iofs.New(files, ".")
	if err != nil {
		return fmt.Errorf("migrations: source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx", driver)
	if err != nil {
		return fmt.Errorf("migrations: instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}
