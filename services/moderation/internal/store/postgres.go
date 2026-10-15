package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) FileReport(ctx context.Context, id, reporterAccountID, reportedAccountID, roomID, reason string) (Report, error) {
	var roomIDArg any
	if roomID != "" {
		roomIDArg = roomID
	}
	row := p.pool.QueryRow(ctx, `
		INSERT INTO reports (id, reporter_account_id, reported_account_id, room_id, reason)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, reporter_account_id, reported_account_id, coalesce(room_id::text, ''), reason, resolved, filed_at
	`, id, reporterAccountID, reportedAccountID, roomIDArg, reason)

	var r Report
	if err := row.Scan(&r.ID, &r.ReporterAccountID, &r.ReportedAccountID, &r.RoomID, &r.Reason, &r.Resolved, &r.FiledAt); err != nil {
		return Report{}, fmt.Errorf("store: file report: %w", err)
	}
	return r, nil
}

func (p *Postgres) BlockUser(ctx context.Context, accountID, blockedAccountID string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO blocks (account_id, blocked_account_id) VALUES ($1, $2)
		ON CONFLICT (account_id, blocked_account_id) DO NOTHING
	`, accountID, blockedAccountID)
	if err != nil {
		return fmt.Errorf("store: block user: %w", err)
	}
	return nil
}

func (p *Postgres) ListBlocks(ctx context.Context, accountID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT blocked_account_id FROM blocks WHERE account_id = $1`, accountID)
	if err != nil {
		return nil, fmt.Errorf("store: list blocks: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: list blocks: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list blocks: %w", err)
	}
	return ids, nil
}

func (p *Postgres) RequestGenderChange(ctx context.Context, id, accountID string, gender identityv1.Gender, note string) (GenderChangeRequest, error) {
	row := p.pool.QueryRow(ctx, `
		INSERT INTO gender_change_requests (id, account_id, requested_gender, note)
		VALUES ($1, $2, $3, $4)
		RETURNING id, account_id, requested_gender, coalesce(note, ''), resolved, approved, requested_at
	`, id, accountID, int32(gender), note)

	var g GenderChangeRequest
	var requestedGender int32
	if err := row.Scan(&g.ID, &g.AccountID, &requestedGender, &g.Note, &g.Resolved, &g.Approved, &g.RequestedAt); err != nil {
		return GenderChangeRequest{}, fmt.Errorf("store: request gender change: %w", err)
	}
	g.RequestedGender = identityv1.Gender(requestedGender)
	return g, nil
}

func (p *Postgres) ListReports(ctx context.Context, onlyUnresolved bool) ([]Report, error) {
	query := `SELECT id, reporter_account_id, reported_account_id, coalesce(room_id::text, ''), reason, resolved,
		coalesce(resolved_by::text, ''), coalesce(outcome, ''), filed_at FROM reports`
	if onlyUnresolved {
		query += ` WHERE NOT resolved`
	}
	query += ` ORDER BY filed_at`

	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: list reports: %w", err)
	}
	defer rows.Close()

	reports := []Report{}
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.ReporterAccountID, &r.ReportedAccountID, &r.RoomID, &r.Reason, &r.Resolved,
			&r.ResolvedBy, &r.Outcome, &r.FiledAt); err != nil {
			return nil, fmt.Errorf("store: list reports: scan: %w", err)
		}
		reports = append(reports, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list reports: %w", err)
	}
	return reports, nil
}

func (p *Postgres) ResolveReport(ctx context.Context, reportID, resolvedBy, outcome string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE reports SET resolved = true, resolved_by = $2, outcome = $3, resolved_at = now() WHERE id = $1
	`, reportID, resolvedBy, outcome)
	if err != nil {
		return fmt.Errorf("store: resolve report: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) BanAccount(ctx context.Context, accountID, reason, bannedBy string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO bans (account_id, reason, banned_by) VALUES ($1, $2, $3)
		ON CONFLICT (account_id) DO UPDATE SET
			reason = $2, banned_by = $3, banned_at = now(), unbanned_by = NULL, unbanned_at = NULL
	`, accountID, reason, bannedBy)
	if err != nil {
		return fmt.Errorf("store: ban account: %w", err)
	}
	return nil
}

func (p *Postgres) UnbanAccount(ctx context.Context, accountID, unbannedBy string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE bans SET unbanned_by = $2, unbanned_at = now() WHERE account_id = $1 AND unbanned_at IS NULL
	`, accountID, unbannedBy)
	if err != nil {
		return fmt.Errorf("store: unban account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either no ban row at all, or already unbanned - only the first
		// is a real error; distinguish with one more lookup rather than
		// optimistically claiming success either way.
		var exists bool
		if err := p.pool.QueryRow(ctx, `SELECT true FROM bans WHERE account_id = $1`, accountID).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("store: unban account: check existing: %w", err)
		}
	}
	return nil
}

func (p *Postgres) ListGenderChangeRequests(ctx context.Context, onlyUnresolved bool) ([]GenderChangeRequest, error) {
	query := `SELECT id, account_id, requested_gender, coalesce(note, ''), resolved, approved,
		coalesce(resolved_by::text, ''), requested_at FROM gender_change_requests`
	if onlyUnresolved {
		query += ` WHERE NOT resolved`
	}
	query += ` ORDER BY requested_at`

	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: list gender change requests: %w", err)
	}
	defer rows.Close()

	requests := []GenderChangeRequest{}
	for rows.Next() {
		var g GenderChangeRequest
		var requestedGender int32
		if err := rows.Scan(&g.ID, &g.AccountID, &requestedGender, &g.Note, &g.Resolved, &g.Approved,
			&g.ResolvedBy, &g.RequestedAt); err != nil {
			return nil, fmt.Errorf("store: list gender change requests: scan: %w", err)
		}
		g.RequestedGender = identityv1.Gender(requestedGender)
		requests = append(requests, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list gender change requests: %w", err)
	}
	return requests, nil
}

func (p *Postgres) ResolveGenderChangeRequest(ctx context.Context, requestID string, approve bool, resolvedBy string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE gender_change_requests SET resolved = true, approved = $2, resolved_by = $3, resolved_at = now()
		WHERE id = $1
	`, requestID, approve, resolvedBy)
	if err != nil {
		return fmt.Errorf("store: resolve gender change request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) LogAdminAction(ctx context.Context, id string, action AdminAction) error {
	var targetArg any
	if action.TargetAccountID != "" {
		targetArg = action.TargetAccountID
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO admin_actions (id, actor_account_id, action, target_account_id, detail)
		VALUES ($1, $2, $3, $4, $5)
	`, id, action.ActorAccountID, action.Action, targetArg, action.Detail)
	if err != nil {
		return fmt.Errorf("store: log admin action: %w", err)
	}
	return nil
}
