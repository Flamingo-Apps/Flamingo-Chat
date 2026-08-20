# Interfaces for testability (the repository pattern)

## The idea

`SYSTEM_DESIGN.md` already calls for accessing storage through a repository interface rather than scattering raw SQL through business logic, so the persistence layer can be swapped later. Identity Service applies the exact same idea to two different boundaries, not just storage:

- `services/identity/internal/store/store.go` defines `AccountStore` - an interface, no SQL in sight.
- `services/identity/internal/verify/verify.go` defines `BadgeVerifier` - an interface, no Google/OIDC in sight.

Business logic (`services/identity/internal/server/server.go`) only ever talks to these two interfaces. It has no idea whether it's talking to real Postgres and real Google, or something else entirely.

## Why this is worth the extra file

The payoff shows up directly in `server_test.go`: `fakeStore` and `fakeVerifier` are small structs backed by plain Go maps, implementing the same two interfaces. Because `server.go` only depends on the interface, the test suite can exercise every rejection path (wrong email domain, unverified email, duplicate pseudonym, missing gender, account not found) in milliseconds, with no Docker container, no network call, no real Google OAuth credentials.

Without this split, testing `VerifyBadge`'s logic would mean either standing up a real Postgres + a real Google-signed token for every test run, or not testing it at all and hoping it's right.

## The shape, concretely

```go
// store.go - the boundary
type AccountStore interface {
    Create(ctx context.Context, id, pseudonym string) (Account, error)
    Get(ctx context.Context, id string) (Account, error)
    // ...
}

// postgres.go - the real implementation, used by main.go
type Postgres struct { pool *pgxpool.Pool }
func (p *Postgres) Create(...) (Account, error) { /* real SQL */ }

// server_test.go - a fake implementation, used only by tests
type fakeStore struct { byID map[string]store.Account }
func (f *fakeStore) Create(...) (store.Account, error) { /* map operations */ }
```

`server.New(accountStore, verifier, ...)` takes the interface type as a parameter, so `main.go` passes in the real `Postgres`/`OIDCVerifier`, and `server_test.go` passes in `fakeStore`/`fakeVerifier` - same constructor, same code path being tested, different concrete implementation underneath.

This only works because Go interfaces are satisfied implicitly (no `implements` keyword, no explicit declaration) - any type with the right methods satisfies the interface automatically, so a test-only fake costs nothing beyond writing the struct itself.
