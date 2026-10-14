//go:build integration

// Integration tests against a real Postgres - confirms both idempotent
// insert paths (ON CONFLICT DO NOTHING) actually behave that way against
// real unique constraints, not just an in-memory fake's bookkeeping
// (DEC-0048).
//
// Run with POSTGRES_URL pointing at a real Postgres:
//
//	go test -tags integration ./internal/store/...
//
// This test creates its own minimal rooms/messages tables rather than
// importing services/chat's actual migrations - persistence-worker's
// Go module has no business depending on another service's module just
// for a test fixture. In the real deployment, Chat's own migration is
// what creates these tables; this stands in for that here, with just
// enough columns for InsertMessage's foreign key and insert to work.
package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/migrations"
)

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
		t.Fatalf("persistence-worker migrations: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("closing migration db handle: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS rooms (id UUID PRIMARY KEY, type SMALLINT NOT NULL);
		CREATE TABLE IF NOT EXISTS messages (
			message_id UUID PRIMARY KEY,
			room_id UUID NOT NULL REFERENCES rooms (id),
			sender_account_id UUID NOT NULL,
			sender_pseudonym TEXT NOT NULL,
			body TEXT NOT NULL,
			sent_at TIMESTAMPTZ NOT NULL
		);
		TRUNCATE messages, rooms;
		TRUNCATE analytics.events;
	`); err != nil {
		t.Fatalf("set up tables: %v", err)
	}
	return NewPostgres(pool)
}

func TestPostgresInsertMessageIsIdempotent(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, senderID := uuid.NewString(), uuid.NewString()

	// messages.room_id has a foreign key to rooms - a real room has to
	// exist first, exactly as it would in production once Chat has
	// created one.
	if _, err := st.pool.Exec(ctx, "INSERT INTO rooms (id, type) VALUES ($1, 1)", roomID); err != nil {
		t.Fatalf("insert room: %v", err)
	}

	messageID := uuid.NewString()
	sentAt := time.Now().Truncate(time.Microsecond)
	for i := range 2 {
		if err := st.InsertMessage(ctx, messageID, roomID, senderID, "Fox", "hi", sentAt); err != nil {
			t.Fatalf("InsertMessage attempt %d: %v", i, err)
		}
	}

	var count int
	if err := st.pool.QueryRow(ctx, "SELECT COUNT(*) FROM messages WHERE message_id = $1", messageID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row after 2 identical inserts, got %d", count)
	}
}

func TestPostgresLogEventIsIdempotent(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	eventID := uuid.NewString()
	occurredAt := time.Now().Truncate(time.Microsecond)

	for i := range 2 {
		if err := st.LogEvent(ctx, eventID, "chat.events", "MemberJoined", []byte("payload"), occurredAt); err != nil {
			t.Fatalf("LogEvent attempt %d: %v", i, err)
		}
	}

	var count int
	if err := st.pool.QueryRow(ctx, "SELECT COUNT(*) FROM analytics.events WHERE event_id = $1", eventID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row after 2 identical log writes, got %d", count)
	}
}
