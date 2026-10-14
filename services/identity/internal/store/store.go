// Package store defines Identity's persistence boundary. Business logic
// (internal/server) depends only on the AccountStore interface, never on
// Postgres directly - this is the same "swappable storage" pattern
// docs/ARCHITECTURE.md applies to every service's store, extended here to
// accounts, and it's what makes the in-memory fake used in server_test.go
// possible without a real database.
package store

import (
	"context"
	"errors"
	"time"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

// ErrNotFound is returned when an account lookup finds nothing.
var ErrNotFound = errors.New("account not found")

// ErrAlreadyExists is returned on a pseudonym, Google identity or email
// collision.
var ErrAlreadyExists = errors.New("account already exists")

// ErrAlreadySet is returned by SetGender when the account's gender has
// already been chosen (DEC-0028).
var ErrAlreadySet = errors.New("already set")

// Account is Identity's persistence-layer view of an account. Deliberately
// separate from the generated identityv1.Account protobuf type - this is
// the shape the database understands, not the shape the wire understands,
// even though they overlap heavily right now.
type Account struct {
	ID        string
	Pseudonym string
	// GoogleSub is Google's permanent identifier for the account this
	// Flamingo account is linked to - nil for a guest account that has
	// never signed in with Google (DEC-0027, DEC-0041).
	GoogleSub     *string
	Gender        identityv1.Gender
	GenderSetAt   *time.Time
	VerifiedEmail *string
	CreatedAt     time.Time
	LastSeenAt    time.Time
}

// AccountStore is the persistence boundary for accounts.
type AccountStore interface {
	// Create inserts a new, unverified (guest) account. Returns
	// ErrAlreadyExists if the pseudonym is taken.
	Create(ctx context.Context, id, pseudonym string) (Account, error)

	// Get fetches an account by ID. Returns ErrNotFound if it doesn't exist.
	Get(ctx context.Context, id string) (Account, error)

	// UpdatePseudonym changes an existing account's pseudonym. Returns
	// ErrNotFound if the account doesn't exist, ErrAlreadyExists if the new
	// pseudonym is taken.
	UpdatePseudonym(ctx context.Context, id, newPseudonym string) (Account, error)

	// GetByGoogleSub finds the account already linked to this Google
	// identity, if any. Returns ErrNotFound if none is linked yet.
	GetByGoogleSub(ctx context.Context, sub string) (Account, error)

	// LinkGoogle upgrades an existing (normally guest) account in place,
	// linking it to a Google identity - SignInWithGoogle's "upgrade a
	// guest account" case (DEC-0027, DEC-0041). Returns ErrNotFound if the
	// account doesn't exist, ErrAlreadyExists if this Google identity or
	// email is already linked to a different account.
	LinkGoogle(ctx context.Context, id, sub, verifiedEmail string) (Account, error)

	// CreateVerified creates a brand new account already linked to a
	// Google identity - SignInWithGoogle's case where there is no guest
	// account to upgrade. Returns ErrAlreadyExists on a pseudonym, Google
	// identity or email collision.
	CreateVerified(ctx context.Context, id, pseudonym, sub, verifiedEmail string) (Account, error)

	// SetGender sets the gender once (DEC-0028). Returns ErrAlreadySet if
	// it was already chosen, ErrNotFound if the account doesn't exist.
	SetGender(ctx context.Context, id string, gender identityv1.Gender) (Account, error)

	// AdminSetGender overwrites the gender unconditionally, after a
	// reviewed change request (DEC-0028, DEC-0029). It does not itself
	// record who approved it or why - that log lives in Moderation's own
	// schema (docs/ARCHITECTURE.md §6), not here. Returns ErrNotFound if
	// the account doesn't exist.
	AdminSetGender(ctx context.Context, id string, gender identityv1.Gender) (Account, error)

	// Touch updates last_seen_at, but only when it is more than an hour
	// stale, so a caller that calls this often doesn't turn every call
	// into a write (docs/ARCHITECTURE.md §4.3). It never reports whether
	// the account existed - it is a best-effort liveness signal for guest
	// cleanup, not something callers branch on.
	Touch(ctx context.Context, id string) error

	// DeleteStaleGuests deletes guest accounts (no linked Google identity)
	// whose last_seen_at is older than olderThan, and reports how many
	// were removed (DEC-0026).
	DeleteStaleGuests(ctx context.Context, olderThan time.Duration) (int64, error)
}
