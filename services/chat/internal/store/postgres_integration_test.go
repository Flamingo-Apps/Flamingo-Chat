//go:build integration

// Integration tests against a real Postgres - the fake in
// internal/server's unit tests can't reproduce real database behaviour
// (INC-0003), and VoteKeep's row-locking concurrency guarantee in
// particular is only really proven by the database actually enforcing it
// (DEC-0048).
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/migrations"
)

// newTestPostgres connects to a real Postgres, runs migrations, and
// truncates every table each test touches so each test starts from a
// clean slate without needing a database of its own.
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

	if _, err := pool.Exec(ctx, "TRUNCATE keep_votes, messages, room_members, rooms"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return NewPostgres(pool)
}

func TestPostgresCreateGroupRoomAddsOwnerAsMember(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, ownerID := uuid.NewString(), uuid.NewString()

	room, err := st.CreateGroupRoom(ctx, roomID, ownerID, "Study group", "CODE1234")
	if err != nil {
		t.Fatalf("CreateGroupRoom: %v", err)
	}
	if room.Name == nil || *room.Name != "Study group" {
		t.Fatalf("unexpected room: %+v", room)
	}

	isMember, err := st.IsCurrentMember(ctx, roomID, ownerID)
	if err != nil {
		t.Fatalf("IsCurrentMember: %v", err)
	}
	if !isMember {
		t.Fatalf("expected the owner to be a member")
	}
}

func TestPostgresCreateGroupRoomRejectsDuplicateInviteCode(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()

	if _, err := st.CreateGroupRoom(ctx, uuid.NewString(), uuid.NewString(), "Room A", "DUPCODE1"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := st.CreateGroupRoom(ctx, uuid.NewString(), uuid.NewString(), "Room B", "DUPCODE1"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists for a duplicate invite code, got %v", err)
	}
}

func TestPostgresJoinAndRejoinViaAddMember(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, ownerID, memberID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := st.CreateGroupRoom(ctx, roomID, ownerID, "Room", "REJOIN01"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := st.AddMember(ctx, roomID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := st.RemoveMember(ctx, roomID, memberID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	isMember, _ := st.IsCurrentMember(ctx, roomID, memberID)
	if isMember {
		t.Fatalf("expected the member to have left")
	}
	everMember, err := st.HasEverBeenMember(ctx, roomID, memberID)
	if err != nil {
		t.Fatalf("HasEverBeenMember: %v", err)
	}
	if !everMember {
		t.Fatalf("expected the member to still count as a former member")
	}

	// Rejoining clears left_at rather than erroring on the primary key.
	if err := st.AddMember(ctx, roomID, memberID); err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	isMember, _ = st.IsCurrentMember(ctx, roomID, memberID)
	if !isMember {
		t.Fatalf("expected the member to be a current member again after rejoining")
	}
}

func TestPostgresVoteKeepRequiresEveryCurrentMember(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := st.CreateDirectRoom(ctx, roomID, []string{a, b}); err != nil {
		t.Fatalf("create: %v", err)
	}

	kept, err := st.VoteKeep(ctx, roomID, a)
	if err != nil {
		t.Fatalf("first vote: %v", err)
	}
	if kept {
		t.Fatalf("expected not kept after only one of two votes")
	}

	kept, err = st.VoteKeep(ctx, roomID, b)
	if err != nil {
		t.Fatalf("second vote: %v", err)
	}
	if !kept {
		t.Fatalf("expected kept once both members have voted")
	}

	room, err := st.GetRoom(ctx, roomID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if !room.Kept {
		t.Fatalf("expected the room's own Kept flag to be set too")
	}
}

// TestPostgresVoteKeepSoloWithinGraceWindowAfterRoomEnded proves
// DEC-0091's fix against a real Postgres, not just the in-memory fake -
// the FOR UPDATE row lock and the ended_at comparison are both real
// database behavior worth confirming directly (DEC-0048).
func TestPostgresVoteKeepSoloWithinGraceWindowAfterRoomEnded(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := st.CreateDirectRoom(ctx, roomID, []string{a, b}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.EndDirectRoom(ctx, roomID); err != nil {
		t.Fatalf("EndDirectRoom: %v", err)
	}

	kept, err := st.VoteKeep(ctx, roomID, a)
	if err != nil {
		t.Fatalf("solo vote: %v", err)
	}
	if !kept {
		t.Fatalf("expected a solo vote to keep the room within the grace window after the other side left")
	}
}

// TestPostgresVoteKeepSoloAfterGraceWindowExpiredDoesNotKeep proves the
// window is a real bound in the database, not just in application code.
func TestPostgresVoteKeepSoloAfterGraceWindowExpiredDoesNotKeep(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := st.CreateDirectRoom(ctx, roomID, []string{a, b}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.EndDirectRoom(ctx, roomID); err != nil {
		t.Fatalf("EndDirectRoom: %v", err)
	}
	// Backdate ended_at directly, well outside the grace window - the
	// fastest way to prove the window's upper bound without an actual
	// sleep in the test.
	if _, err := st.pool.Exec(ctx, `UPDATE rooms SET ended_at = now() - interval '1 hour' WHERE id = $1`, roomID); err != nil {
		t.Fatalf("backdate ended_at: %v", err)
	}

	kept, err := st.VoteKeep(ctx, roomID, a)
	if err != nil {
		t.Fatalf("solo vote: %v", err)
	}
	if kept {
		t.Fatalf("expected a solo vote well outside the grace window to NOT keep the room")
	}
}

func TestPostgresVoteKeepReturnsNotFoundForMissingRoom(t *testing.T) {
	st := newTestPostgres(t)
	if _, err := st.VoteKeep(context.Background(), uuid.NewString(), uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresEndDirectRoomLeavesKeptRoomsAlone(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := st.CreateDirectRoom(ctx, roomID, []string{a, b}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.VoteKeep(ctx, roomID, a); err != nil {
		t.Fatalf("vote a: %v", err)
	}
	if _, err := st.VoteKeep(ctx, roomID, b); err != nil {
		t.Fatalf("vote b: %v", err)
	}

	if err := st.EndDirectRoom(ctx, roomID); err != nil {
		t.Fatalf("EndDirectRoom: %v", err)
	}
	room, err := st.GetRoom(ctx, roomID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if room.EndedAt != nil {
		t.Fatalf("expected a kept room to stay open even after EndDirectRoom, got EndedAt=%v", room.EndedAt)
	}
}

func TestPostgresListRoomsForAccountReturnsGroupAndKeptDirectOnly(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()

	me, other, other2 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	groupID := uuid.NewString()
	keptDirectID := uuid.NewString()
	unkeptDirectID := uuid.NewString()
	if _, err := st.CreateGroupRoom(ctx, groupID, me, "G", "LISTROOM"); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := st.CreateDirectRoom(ctx, keptDirectID, []string{me, other}); err != nil {
		t.Fatalf("create kept direct: %v", err)
	}
	if _, err := st.VoteKeep(ctx, keptDirectID, me); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if _, err := st.VoteKeep(ctx, keptDirectID, other); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if _, err := st.CreateDirectRoom(ctx, unkeptDirectID, []string{me, other2}); err != nil {
		t.Fatalf("create unkept direct: %v", err)
	}

	rooms, err := st.ListRoomsForAccount(ctx, me)
	if err != nil {
		t.Fatalf("ListRoomsForAccount: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("expected 2 rooms (group + kept direct), got %d: %+v", len(rooms), rooms)
	}
}

func TestPostgresListMessagesPaginatesNewestFirst(t *testing.T) {
	st := newTestPostgres(t)
	ctx := context.Background()
	roomID, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := st.CreateDirectRoom(ctx, roomID, []string{a, b}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Messages are only ever written by the persistence worker in
	// production (internal/store's package comment) - inserted directly
	// here since that's the only writer this test has.
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		_, err := st.pool.Exec(ctx, `
			INSERT INTO messages (message_id, room_id, sender_account_id, sender_pseudonym, body, sent_at)
			VALUES ($1, $2, $3, 'Fox', $4, now() + ($5 * interval '1 second'))
		`, id, roomID, a, "message "+id, i)
		if err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
	}

	page, err := st.ListMessages(ctx, roomID, "", 2)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(page) != 2 || page[0].MessageID != ids[2] || page[1].MessageID != ids[1] {
		t.Fatalf("expected the newest 2 messages in reverse order, got %+v", page)
	}

	nextPage, err := st.ListMessages(ctx, roomID, page[len(page)-1].MessageID, 2)
	if err != nil {
		t.Fatalf("ListMessages (next page): %v", err)
	}
	if len(nextPage) != 1 || nextPage[0].MessageID != ids[0] {
		t.Fatalf("expected the oldest message as the final page, got %+v", nextPage)
	}
}
