// Package migrations embeds the chat service's SQL schema migrations into
// the compiled binary, so a deployed container carries its own schema
// history and doesn't need migration files shipped alongside it
// separately.
//
// Every service currently shares one Postgres database (DEC-0006), so
// golang-migrate's own tracking table needs a service-specific name -
// its default, "schema_migrations", is one table shared by whoever
// connects, and a second service's migrations collide with the first's
// version number the moment both exist in the same database (INC-0008).
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

// Run applies any pending migrations. golang-migrate takes a Postgres
// advisory lock for the duration, so this stays safe to call from every
// replica's startup rather than needing a separate migration step.
func Run(sqlDB *sql.DB) error {
	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{MigrationsTable: "chat_schema_migrations"})
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
	// Best-effort cleanup: Up() already ran by the time this runs, so a
	// failure to close the source/database handles here doesn't change
	// whether the migration itself succeeded.
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}
