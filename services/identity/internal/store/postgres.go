package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

// Postgres error codes - see
// https://www.postgresql.org/docs/current/errcodes-appendix.html.
const (
	postgresUniqueViolation           = "23505"
	postgresInvalidTextRepresentation = "22P02" // e.g. a non-UUID string where a uuid column is expected
)

// accountColumns is the column list every query below selects, in the
// order scanAccount expects. Kept in one place so a column added to one
// doesn't quietly fall out of sync with the other.
const accountColumns = "id, pseudonym, google_sub, gender, gender_set_at, verified_email, created_at, last_seen_at"

// Postgres is the AccountStore implementation backed by the accounts table.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) Create(ctx context.Context, id, pseudonym string) (Account, error) {
	q := `
		INSERT INTO accounts (id, pseudonym)
		VALUES ($1, $2)
		RETURNING ` + accountColumns
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id, pseudonym))
	if isUniqueViolation(err) {
		return Account{}, ErrAlreadyExists
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: create: %w", err)
	}
	return acc, nil
}

func (p *Postgres) Get(ctx context.Context, id string) (Account, error) {
	q := `SELECT ` + accountColumns + ` FROM accounts WHERE id = $1`
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id))
	if isNotFound(err) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: get: %w", err)
	}
	return acc, nil
}

func (p *Postgres) UpdatePseudonym(ctx context.Context, id, newPseudonym string) (Account, error) {
	q := `
		UPDATE accounts SET pseudonym = $2 WHERE id = $1
		RETURNING ` + accountColumns
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id, newPseudonym))
	if isUniqueViolation(err) {
		return Account{}, ErrAlreadyExists
	}
	if isNotFound(err) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: update pseudonym: %w", err)
	}
	return acc, nil
}

func (p *Postgres) GetByGoogleSub(ctx context.Context, sub string) (Account, error) {
	q := `SELECT ` + accountColumns + ` FROM accounts WHERE google_sub = $1`
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, sub))
	if isNotFound(err) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: get by google sub: %w", err)
	}
	return acc, nil
}

func (p *Postgres) LinkGoogle(ctx context.Context, id, sub, verifiedEmail string) (Account, error) {
	q := `
		UPDATE accounts SET google_sub = $2, verified_email = $3
		WHERE id = $1
		RETURNING ` + accountColumns
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id, sub, verifiedEmail))
	if isUniqueViolation(err) {
		return Account{}, ErrAlreadyExists
	}
	if isNotFound(err) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: link google: %w", err)
	}
	return acc, nil
}

func (p *Postgres) CreateVerified(ctx context.Context, id, pseudonym, sub, verifiedEmail string) (Account, error) {
	q := `
		INSERT INTO accounts (id, pseudonym, google_sub, verified_email)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + accountColumns
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id, pseudonym, sub, verifiedEmail))
	if isUniqueViolation(err) {
		return Account{}, ErrAlreadyExists
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: create verified: %w", err)
	}
	return acc, nil
}

func (p *Postgres) SetGender(ctx context.Context, id string, gender identityv1.Gender) (Account, error) {
	q := `
		UPDATE accounts SET gender = $2, gender_set_at = now()
		WHERE id = $1 AND gender_set_at IS NULL
		RETURNING ` + accountColumns
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id, int32(gender)))
	if isNotFound(err) {
		// The WHERE clause matched nothing either because the account
		// doesn't exist, or because gender was already set - tell them
		// apart with a cheap follow-up read rather than a second code
		// path for what is otherwise the same query.
		if _, getErr := p.Get(ctx, id); errors.Is(getErr, ErrNotFound) {
			return Account{}, ErrNotFound
		}
		return Account{}, ErrAlreadySet
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: set gender: %w", err)
	}
	return acc, nil
}

func (p *Postgres) AdminSetGender(ctx context.Context, id string, gender identityv1.Gender) (Account, error) {
	// COALESCE keeps the original gender_set_at if one already exists -
	// an admin correcting the value isn't the same event as the user
	// first choosing it - and sets it now in the unlikely case gender is
	// being set for the first time this way.
	q := `
		UPDATE accounts SET gender = $2, gender_set_at = COALESCE(gender_set_at, now())
		WHERE id = $1
		RETURNING ` + accountColumns
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id, int32(gender)))
	if isNotFound(err) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: admin set gender: %w", err)
	}
	return acc, nil
}

func (p *Postgres) Touch(ctx context.Context, id string) error {
	const q = `
		UPDATE accounts SET last_seen_at = now()
		WHERE id = $1 AND last_seen_at < now() - interval '1 hour'
	`
	if _, err := p.pool.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("store: touch: %w", err)
	}
	return nil
}

func (p *Postgres) DeleteStaleGuests(ctx context.Context, olderThan time.Duration) (int64, error) {
	const q = `
		DELETE FROM accounts
		WHERE google_sub IS NULL AND last_seen_at < now() - ($1 * interval '1 second')
	`
	tag, err := p.pool.Exec(ctx, q, olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("store: delete stale guests: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanAccount(row pgx.Row) (Account, error) {
	var (
		acc    Account
		gender int32
	)
	if err := row.Scan(&acc.ID, &acc.Pseudonym, &acc.GoogleSub, &gender, &acc.GenderSetAt, &acc.VerifiedEmail, &acc.CreatedAt, &acc.LastSeenAt); err != nil {
		return Account{}, err
	}
	acc.Gender = identityv1.Gender(gender)
	return acc, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation
}

// isNotFound covers both "the query found no row" (pgx.ErrNoRows) and "the
// id wasn't even a well-formed UUID" (Postgres rejects it before the query
// can run at all) - a malformed account ID is indistinguishable from a
// nonexistent one to the caller, so both map to ErrNotFound rather than
// leaking a database syntax error as an Internal error.
func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresInvalidTextRepresentation
}
