//go:build defect

// Defect test, not part of the normal suite: run with
//
//	GOTOOLCHAIN=local go test -tags defect -run TestDefect ./internal/consumer/...
//
// It asserts what docs/ARCHITECTURE.md section 7 promises ("Postgres
// unavailable: the persistence worker retries with backoff without
// committing") and FAILS because the code does not do that.
package consumer

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/decode"
)

// defectFlakyStore fails InsertMessage exactly once for failID, the way a
// connection reset or a brief Postgres restart would, and works otherwise.
type defectFlakyStore struct {
	failID    string
	failed    bool
	persisted map[string]bool
}

func (s *defectFlakyStore) InsertMessage(_ context.Context, messageID, _, _, _, _ string, _ time.Time) error {
	if messageID == s.failID && !s.failed {
		s.failed = true
		return errors.New("connection reset by peer")
	}
	s.persisted[messageID] = true
	return nil
}

func (s *defectFlakyStore) LogEvent(context.Context, string, string, string, []byte, time.Time) error {
	return nil
}

// TestDefect_OneTransientInsertFailureLosesTheMessageForever feeds three
// messages (offsets 10, 11, 12) to the real ConsumeClaim. The insert of the
// middle one fails once. The handler's comment says a failed message is
// "left unmarked - retried on redelivery". But ConsumeClaim just moves on to
// message 12 and marks it, and sarama's MarkOffset only ever moves forward
// (offset_manager.go: `if offset > pom.offset`), so the committed offset
// becomes 13 and offset 11 is never read again.
func TestDefect_OneTransientInsertFailureLosesTheMessageForever(t *testing.T) {
	st := &defectFlakyStore{failID: "m2", persisted: map[string]bool{}}
	decoder := func(value []byte) (decode.Decoded, error) {
		id := string(value)
		return decode.Decoded{
			EventID: "evt-" + id, EventKind: "MessageSent", OccurredAt: time.Now(),
			Message: &decode.Message{MessageID: id, RoomID: "room", SenderAccountID: "a", SenderPseudonym: "A", Body: "hi " + id, SentAt: time.Now()},
		}, nil
	}
	h := New(map[string]DecodeFunc{"chat.events": decoder}, st, slog.Default())

	session := newFakeSession()
	claim := newFakeClaim("chat.events", []*sarama.ConsumerMessage{
		{Topic: "chat.events", Offset: 10, Value: []byte("m1")},
		{Topic: "chat.events", Offset: 11, Value: []byte("m2")},
		{Topic: "chat.events", Offset: 12, Value: []byte("m3")},
	})
	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}

	// What Kafka would hold as the committed offset: the highest mark wins.
	var committed int64
	for _, m := range session.marked {
		if m.Offset+1 > committed {
			committed = m.Offset + 1
		}
	}
	t.Logf("persisted=%v committed offset=%d (m2 sits at offset 11)", st.persisted, committed)

	if !st.persisted["m2"] && committed > 11 {
		t.Fatalf("message m2 was acknowledged to its sender and delivered live, but it is NOT in Postgres and the committed offset (%d) is past it, so a restart will not retry it: it is gone from history for good", committed)
	}
}
