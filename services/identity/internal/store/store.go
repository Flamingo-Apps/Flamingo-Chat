// Package store defines Identity's persistence boundary. Business logic
// (internal/server) depends only on the AccountStore interface, never on
// Postgres directly - this is the same "swappable storage" pattern
// SYSTEM_DESIGN.md applies to Chat's message store, extended here to
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

// ErrAlreadyExists is returned on a pseudonym or verified-email collision.
var ErrAlreadyExists = errors.New("account already exists")

// Account is Identity's persistence-layer view of an account. Deliberately
// separate from the generated identityv1.Account protobuf type - this is
// the shape the database understands, not the shape the wire understands,
// even though they overlap heavily right now.
type Account struct {
	ID            string
	Pseudonym     string
	BadgeVerified bool
	Gender        identityv1.Gender
	VerifiedEmail *string
	CreatedAt     time.Time
}

// AccountStore is the persistence boundary for accounts.
type AccountStore interface {
	// Create inserts a new, unverified account. Returns ErrAlreadyExists if
	// the pseudonym is taken.
	Create(ctx context.Context, id, pseudonym string) (Account, error)

	// Get fetches an account by ID. Returns ErrNotFound if it doesn't exist.
	Get(ctx context.Context, id string) (Account, error)

	// UpdatePseudonym changes an existing account's pseudonym. Returns
	// ErrNotFound if the account doesn't exist, ErrAlreadyExists if the new
	// pseudonym is taken.
	UpdatePseudonym(ctx context.Context, id, newPseudonym string) (Account, error)

	// SetVerified marks an account badge-verified with the given gender and
	// verified email. Returns ErrNotFound if the account doesn't exist,
	// ErrAlreadyExists if the email is already verified on another account.
	SetVerified(ctx context.Context, id string, gender identityv1.Gender, verifiedEmail string) (Account, error)
}
