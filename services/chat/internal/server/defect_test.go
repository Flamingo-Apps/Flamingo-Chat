//go:build defect

// Defect tests for the Chat service. Each one asserts what the architecture
// document promises and FAILS while the defect exists; they are not part of the
// normal suite (see CONTRIBUTING.md, "Tests for known defects"):
//
//	GOTOOLCHAIN=local go test -tags defect -run TestDefect -v ./internal/server/...
//
// They use the package's existing in-memory fakes (fakes_test.go), so they
// need no infrastructure.
package server

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/store"
)

func defectGroupRoom(fs *fakeStore, roomID, member string) {
	fs.rooms[roomID] = store.Room{ID: roomID, Type: chatv1.RoomType_ROOM_TYPE_GROUP}
	fs.addMemberLocked(roomID, member)
}

// TestDefect_RetryAfterFailedPublishIsAckedButNeverSent: SendMessage claims the
// (room, sender, client_msg_id) dedup slot BEFORE it produces to Kafka and never
// releases it when the produce fails. docs/ARCHITECTURE.md section 7 says the
// client "shows 'not sent' and retries with the same client_msg_id". The retry
// finds the slot taken, is treated as a duplicate of a message that was never
// produced, and is answered with a success. The user sees a delivered message
// that nobody will ever receive and that is not in history.
//
// The fake deduper has the same semantics as dedup.Redis (SETNX, no release);
// see services/chat/internal/dedup/redis.go.
func TestDefect_RetryAfterFailedPublishIsAckedButNeverSent(t *testing.T) {
	fs := newFakeStore()
	defectGroupRoom(fs, "room-1", "alice")
	prod := &fakeProducer{err: errors.New("kafka: broker not available")}
	srv := New(fs, prod, newFakeDeduper(), uuid.NewString)

	req := &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "alice", SenderPseudonym: "Alice", Body: "are you there?", ClientMsgId: "c-1"}

	if _, err := srv.SendMessage(context.Background(), req); err == nil {
		t.Fatal("expected the first send to fail while Kafka is down")
	}

	prod.err = nil // Kafka comes back; the client retries with the same client_msg_id.
	resp, err := srv.SendMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if len(prod.published) == 0 {
		t.Fatalf("the retry was acknowledged (message_id %q) but nothing was ever produced to Kafka: the message is silently lost", resp.GetMessageId())
	}
}
