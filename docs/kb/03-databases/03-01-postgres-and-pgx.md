# Postgres access with pgx

## Why pgx, not database/sql + lib/pq

Go's standard library has a generic `database/sql` package, and historically `lib/pq` was the common Postgres driver underneath it. `lib/pq` is now archived/unmaintained. `pgx` (`github.com/jackc/pgx/v5`) is the current standard: faster, actively maintained, and has its own richer native interface (`pgx.Row`, `pgx.Rows`) in addition to being usable through `database/sql` if something else needs that compatibility.

Identity uses `pgx` natively via `pgxpool`, its connection-pool package:

```go
import "github.com/jackc/pgx/v5/pgxpool"

pool, err := pgxpool.New(ctx, postgresURL)   // one pool for the service's lifetime
defer pool.Close()

row := pool.QueryRow(ctx, "SELECT ... WHERE id = $1", id)
```

A pool, not a single connection: gRPC handles many concurrent requests, and a pool hands out/reclaims connections per-query rather than serializing everything through one.

## Version pinned, not @latest

`pgx/v5` is pinned to `v5.8.0` in `services/identity/go.mod`, not `@latest`. Same reason as the grpc pin from Phase 0 (see [../01-go-fundamentals/01-03-modules-and-toolchains.md](../01-go-fundamentals/01-03-modules-and-toolchains.md)): `pgx`'s latest release requires Go 1.25, and this network can't reliably fetch that toolchain. `v5.8.0` is the newest release that still only needs Go 1.24.

## Translating Postgres errors into domain errors

`internal/store/postgres.go` never lets a raw `*pgconn.PgError` leak out of the store - it checks the Postgres error code and maps it to one of the store's own sentinel errors (`ErrNotFound`, `ErrAlreadyExists`):

```go
const postgresUniqueViolation = "23505"
const postgresInvalidTextRepresentation = "22P02" // malformed UUID literal

func isUniqueViolation(err error) bool {
    var pgErr *pgconn.PgError
    return errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation
}
```

`errors.As` unwraps a possibly-wrapped error chain looking for a specific concrete type (`*pgconn.PgError`) - the standard Go idiom for "is this error actually this specific kind, however it got wrapped." Postgres error codes are a fixed, documented list (see the [errcodes appendix](https://www.postgresql.org/docs/current/errcodes-appendix.html)) - `23505` is always "unique violation," `22P02` is always "invalid text representation" (e.g. handing a non-UUID string to a `uuid` column), regardless of which query triggered it.

This mapping is what let a real bug get caught by manual testing rather than silently misbehaving in production: see [../02-protobuf-and-grpc/02-03-grpcurl.md](../02-protobuf-and-grpc/02-03-grpcurl.md) for the actual incident.
