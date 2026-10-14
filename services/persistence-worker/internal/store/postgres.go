package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the Store implementation. The messages table isn't created
// by any migration here - it's Chat's own table (services/chat/migrations),
// since persistence-worker is effectively Chat's background half
// (docs/ARCHITECTURE.md §2). If a MessageSent event arrives before Chat's
// migration has ever run against this database, the insert below fails
// with an undefined-table error; ConsumeClaim leaves that message
// unmarked rather than erroring out, so it is simply retried until the
// table exists - no explicit startup ordering between the two services
// is needed.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) InsertMessage(ctx context.Context, messageID, roomID, senderAccountID, senderPseudonym, body string, sentAt time.Time) error {
	q := `
		INSERT INTO messages (message_id, room_id, sender_account_id, sender_pseudonym, body, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (message_id) DO NOTHING
	`
	if _, err := p.pool.Exec(ctx, q, messageID, roomID, senderAccountID, senderPseudonym, body, sentAt); err != nil {
		return fmt.Errorf("store: insert message: %w", err)
	}
	return nil
}

func (p *Postgres) LogEvent(ctx context.Context, eventID, topic, eventKind string, payload []byte, occurredAt time.Time) error {
	q := `
		INSERT INTO analytics.events (event_id, topic, event_kind, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_id) DO NOTHING
	`
	if _, err := p.pool.Exec(ctx, q, eventID, topic, eventKind, payload, occurredAt); err != nil {
		return fmt.Errorf("store: log event: %w", err)
	}
	return nil
}
