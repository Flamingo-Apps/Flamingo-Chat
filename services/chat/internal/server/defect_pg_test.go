//go:build defect

// Defect tests for the Chat service against a REAL Postgres. Needs a scratch
// database; skips when SCRATCH_POSTGRES_URL is unset so it can never run against
// the development database:
//
//	SCRATCH_POSTGRES_URL=postgres://.../scratch?sslmode=disable \
//	  GOTOOLCHAIN=local go test -tags defect -run TestDefect -v ./internal/server/...
package server

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/migrations"
)

func defectPG(t *testing.T) (*Server, *pgxpool.Pool, *fakeProducer) {
	t.Helper()
	url := os.Getenv("SCRATCH_POSTGRES_URL")
	if url == "" {
		t.Skip("SCRATCH_POSTGRES_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	sqlDB := stdlib.OpenDBFromPool(pool)
	if err := migrations.Run(sqlDB); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	_ = sqlDB.Close()
	prod := &fakeProducer{}
	return New(store.NewPostgres(pool), prod, newFakeDeduper(), uuid.NewString), pool, prod
}

// TestDefect_ThePersonWhoEndedTheChatCanStillKeepIt: docs say that when a
// random chat ends, "the one who stayed" gets a short window to save it and "the
// one who left gets nothing" (F-13), and the PRD says a random chat disappears
// "unless both chose to keep chatting". But EndDirectRoom only sets ended_at; it
// does not remove the ender's membership. So the ender is still a current
// member, VoteKeep's grace-window rule ("within 30s of ending, one vote is
// enough") applies to them too, and they can unilaterally save the stranger's
// chat after skipping them. The stranger is never asked.
func TestDefect_ThePersonWhoEndedTheChatCanStillKeepIt(t *testing.T) {
	srv, _, _ := defectPG(t)
	ctx := context.Background()
	alice, bob := uuid.NewString(), uuid.NewString()

	room, err := srv.CreateDirectRoom(ctx, &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{alice, bob}})
	if err != nil {
		t.Fatalf("CreateDirectRoom: %v", err)
	}
	roomID := room.GetRoom().GetId()

	// Alice presses End (the same RPC the gateway's match_end calls).
	if _, err := srv.EndDirectRoom(ctx, &chatv1.EndDirectRoomRequest{RoomId: roomID, AccountId: alice, Reason: "ended"}); err != nil {
		t.Fatalf("EndDirectRoom: %v", err)
	}

	// Alice then sends keep_chat for the room she just ended.
	resp, err := srv.VoteKeep(ctx, &chatv1.VoteKeepRequest{RoomId: roomID, AccountId: alice})
	if err == nil && resp.GetKept() {
		t.Fatalf("Bob never voted, Alice ended the chat herself, and the room is now KEPT for Alice alone")
	}
}
