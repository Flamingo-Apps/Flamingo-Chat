# Schema migrations (golang-migrate)

## The problem

A service's database schema changes over time (new table, new column). Hand-running SQL against production whenever that happens doesn't scale past one person, and doesn't leave a record of what changed, when, or in what order. A migration tool tracks a numbered sequence of schema changes and applies whichever ones a given database hasn't seen yet.

## The pieces

**Migration files** - `services/identity/migrations/0001_create_accounts.up.sql` (apply) and its `.down.sql` sibling (undo). The `0001` prefix is the sequence number; a future schema change would be `0002_...`.

**Embedding them into the binary** - `migrations/migrations.go`:

```go
//go:embed *.sql
var files embed.FS
```

`//go:embed` is a compiler directive that bakes the matched files into the compiled binary as a virtual filesystem (`embed.FS`), readable at runtime without the original files existing on disk next to it. This matters for deployment: a container image just needs the compiled binary, not a separate migrations folder shipped and mounted alongside it.

**Running them against Postgres** - the fiddly part. `golang-migrate` predates `pgx` v5's native interface and still expects a `database/sql`-style `*sql.DB`, not a `*pgxpool.Pool`. Rather than opening a second, separate connection pool just for migrations, `pgx/v5/stdlib.OpenDBFromPool` wraps the *existing* pool as a `*sql.DB`:

```go
sqlDB := stdlib.OpenDBFromPool(pool)   // same pool, just a compatible wrapper
defer sqlDB.Close()                     // closes the wrapper only, not the pool itself

driver, _ := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
src, _ := iofs.New(files, ".")          // the embedded .sql files as a migration source
m, _ := migrate.NewWithInstance("iofs", src, "pgx", driver)
m.Up()                                  // apply every migration not yet applied
```

## Why this runs automatically on every startup

`golang-migrate` takes a Postgres advisory lock for the duration of `Up()`, so even if multiple replicas of Identity start at once (once it's scaled past one instance), only one actually runs the migration while the others wait, then find there's nothing left to do. That's what makes "just run migrations on startup" safe rather than a footgun waiting for the day there are 2+ replicas.

## Confirming it worked

`golang-migrate` tracks a `schema_migrations` table (created automatically) recording the highest applied version and whether it's mid-migration ("dirty"):

```sql
SELECT * FROM schema_migrations;
-- version | dirty
-- 1       | f
```
