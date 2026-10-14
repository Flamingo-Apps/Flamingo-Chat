//go:build integration

// Integration tests against a real Postgres - the fakes in
// internal/server's unit tests can't reproduce real database behaviour
// (INC-0003), and several of the store's new guarantees (SetGender's
// once-only enforcement, Touch's write throttle, the unique constraints
// on google_sub and verified_email) are only really proven by the
// database actually enforcing them.
//
// Run with POSTGRES_URL pointing at a real Postgres (defaults to the same
// local instance Tilt deploys - see the README's "Run it locally"):
//
//	go test -tags integration ./internal/store/...
package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/migrations"
)

// newTestPostgres connects to a real Postgres, runs migrations, and
// truncates the accounts table so each test starts from a clean slate
// without needing a database of its own.
func newTestPostgres(t *testing.T) *Postgres {
	t.Helper()
	ctx := context.Background()

	url := "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable"
	if v := os.Getenv("POSTGRES_URL"); v != "" {
		url = v
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	sqlDB := stdlib.OpenDBFromPool(pool)
	if err := migrations.Run(sqlDB); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("closing migration db handle: %v", err)
	}

	if _, err := pool.Exec(ctx, "TRUNCATE accounts"); err != nil {
		t.Fatalf("truncate accounts: %v", err)
	}
	return NewPostgres(pool)
}

func TestPostgresCreateAndGet(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	id := uuid.NewString()

	acc, err := st.Create(ctx, id, "integration_fox")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if acc.GoogleSub != nil || acc.GenderSetAt != nil {
		t.Fatalf("a new account should be unlinked and ungendered: %+v", acc)
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Pseudonym != "integration_fox" {
		t.Fatalf("unexpected account: %+v", got)
	}
}

func TestPostgresMalformedIDIsNotFound(t *testing.T) {
	// The exact regression INC-0003 found, for the store's new query
	// shapes too: a malformed (non-UUID) id must map to ErrNotFound, not
	// leak a raw Postgres error.
	st := newTestPostgres(t)
	if _, err := st.Get(context.Background(), "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a malformed id, got %v", err)
	}
}

func TestPostgresLinkGoogleAndFindBySub(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	id := uuid.NewString()
	if _, err := st.Create(ctx, id, "integration_owl"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	acc, err := st.LinkGoogle(ctx, id, "sub-xyz", "person@kiit.ac.in")
	if err != nil {
		t.Fatalf("LinkGoogle: %v", err)
	}
	if acc.GoogleSub == nil || *acc.GoogleSub != "sub-xyz" {
		t.Fatalf("unexpected account: %+v", acc)
	}

	found, err := st.GetByGoogleSub(ctx, "sub-xyz")
	if err != nil {
		t.Fatalf("GetByGoogleSub: %v", err)
	}
	if found.ID != id {
		t.Fatalf("expected %s, got %s", id, found.ID)
	}
}

func TestPostgresLinkGoogleRejectsDuplicateSub(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	id1, id2 := uuid.NewString(), uuid.NewString()
	if _, err := st.Create(ctx, id1, "integration_lynx_one"); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := st.Create(ctx, id2, "integration_lynx_two"); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	if _, err := st.LinkGoogle(ctx, id1, "dup-sub", "a@kiit.ac.in"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if _, err := st.LinkGoogle(ctx, id2, "dup-sub", "b@kiit.ac.in"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists for a duplicate google_sub, got %v", err)
	}
}

func TestPostgresSetGenderOnceEnforcedByTheDatabase(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	id := uuid.NewString()
	if _, err := st.Create(ctx, id, "integration_badger"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := st.SetGender(ctx, id, identityv1.Gender_GENDER_MALE); err != nil {
		t.Fatalf("first SetGender: %v", err)
	}
	if _, err := st.SetGender(ctx, id, identityv1.Gender_GENDER_FEMALE); !errors.Is(err, ErrAlreadySet) {
		t.Fatalf("expected ErrAlreadySet on a second call, got %v", err)
	}
}

func TestPostgresTouchThrottlesWrites(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	id := uuid.NewString()
	if _, err := st.Create(ctx, id, "integration_heron"); err != nil {
		t.Fatalf("create: %v", err)
	}

	before, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if err := st.Touch(ctx, id); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	after, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// A fresh account's last_seen_at defaults to its creation time, well
	// under an hour ago, so this Touch should be throttled into a no-op.
	if !after.LastSeenAt.Equal(before.LastSeenAt) {
		t.Fatalf("expected Touch to be throttled on a freshly created account, but last_seen_at moved from %v to %v", before.LastSeenAt, after.LastSeenAt)
	}
}

func TestPostgresDeleteStaleGuestsLeavesVerifiedAccountsAlone(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()

	staleGuest := uuid.NewString()
	freshGuest := uuid.NewString()
	staleVerified := uuid.NewString()
	if _, err := st.Create(ctx, staleGuest, "integration_stale_guest"); err != nil {
		t.Fatalf("create stale guest: %v", err)
	}
	if _, err := st.Create(ctx, freshGuest, "integration_fresh_guest"); err != nil {
		t.Fatalf("create fresh guest: %v", err)
	}
	if _, err := st.CreateVerified(ctx, staleVerified, "integration_stale_verified", "sub-stale", "stale@kiit.ac.in"); err != nil {
		t.Fatalf("create verified: %v", err)
	}

	// Backdate last_seen_at directly - Touch only ever moves it forward,
	// so this is the only way to put an account far enough in the past.
	if _, err := st.pool.Exec(ctx,
		"UPDATE accounts SET last_seen_at = now() - interval '61 days' WHERE id IN ($1, $2)",
		staleGuest, staleVerified,
	); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}

	n, err := st.DeleteStaleGuests(ctx, 60*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteStaleGuests: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 deletion, got %d", n)
	}

	if _, err := st.Get(ctx, staleGuest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the stale guest to be deleted, got %v", err)
	}
	if _, err := st.Get(ctx, freshGuest); err != nil {
		t.Fatalf("the fresh guest should remain: %v", err)
	}
	if _, err := st.Get(ctx, staleVerified); err != nil {
		t.Fatalf("the stale but verified account should remain: %v", err)
	}
}
