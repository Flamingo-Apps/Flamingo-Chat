package store

import (
	"context"
	"errors"
	"fmt"

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

// Postgres is the AccountStore implementation backed by the accounts table.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) Create(ctx context.Context, id, pseudonym string) (Account, error) {
	const q = `
		INSERT INTO accounts (id, pseudonym)
		VALUES ($1, $2)
		RETURNING id, pseudonym, badge_verified, gender, verified_email, created_at
	`
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
	const q = `
		SELECT id, pseudonym, badge_verified, gender, verified_email, created_at
		FROM accounts WHERE id = $1
	`
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
	const q = `
		UPDATE accounts SET pseudonym = $2 WHERE id = $1
		RETURNING id, pseudonym, badge_verified, gender, verified_email, created_at
	`
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

func (p *Postgres) SetVerified(ctx context.Context, id string, gender identityv1.Gender, verifiedEmail string) (Account, error) {
	const q = `
		UPDATE accounts
		SET badge_verified = TRUE, gender = $2, verified_email = $3
		WHERE id = $1
		RETURNING id, pseudonym, badge_verified, gender, verified_email, created_at
	`
	acc, err := scanAccount(p.pool.QueryRow(ctx, q, id, int32(gender), verifiedEmail))
	if isUniqueViolation(err) {
		return Account{}, ErrAlreadyExists
	}
	if isNotFound(err) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: set verified: %w", err)
	}
	return acc, nil
}

func scanAccount(row pgx.Row) (Account, error) {
	var (
		acc    Account
		gender int32
	)
	if err := row.Scan(&acc.ID, &acc.Pseudonym, &acc.BadgeVerified, &gender, &acc.VerifiedEmail, &acc.CreatedAt); err != nil {
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
