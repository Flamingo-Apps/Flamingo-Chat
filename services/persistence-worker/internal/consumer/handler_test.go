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

// fakeSession implements sarama.ConsumerGroupSession, recording every
// MarkMessage call.
type fakeSession struct {
	ctx    context.Context
	marked []*sarama.ConsumerMessage
}

func newFakeSession() *fakeSession { return &fakeSession{ctx: context.Background()} }

func (f *fakeSession) Claims() map[string][]int32                      { return nil }
func (f *fakeSession) MemberID() string                                { return "fake" }
func (f *fakeSession) GenerationID() int32                             { return 0 }
func (f *fakeSession) MarkOffset(_ string, _ int32, _ int64, _ string) {}
func (f *fakeSession) Commit()                                         {}
func (f *fakeSession) ResetOffset(_ string, _ int32, _ int64, _ string) {
}
func (f *fakeSession) MarkMessage(msg *sarama.ConsumerMessage, _ string) {
	f.marked = append(f.marked, msg)
}
func (f *fakeSession) Context() context.Context { return f.ctx }

// fakeClaim implements sarama.ConsumerGroupClaim over a fixed, in-memory
// slice of messages for one topic.
type fakeClaim struct {
	topic    string
	messages chan *sarama.ConsumerMessage
}

func newFakeClaim(topic string, messages []*sarama.ConsumerMessage) *fakeClaim {
	ch := make(chan *sarama.ConsumerMessage, len(messages))
	for _, m := range messages {
		ch <- m
	}
	close(ch)
	return &fakeClaim{topic: topic, messages: ch}
}

func (f *fakeClaim) Topic() string                            { return f.topic }
func (f *fakeClaim) Partition() int32                         { return 0 }
func (f *fakeClaim) InitialOffset() int64                     { return 0 }
func (f *fakeClaim) HighWaterMarkOffset() int64               { return 0 }
func (f *fakeClaim) Messages() <-chan *sarama.ConsumerMessage { return f.messages }

// fakeStore records every call, or returns a fixed error for the next
// call named by failOn ("insert" or "log").
type fakeStore struct {
	inserted []string
	logged   []string
	failOn   string
}

func (s *fakeStore) InsertMessage(_ context.Context, messageID, _, _, _, _ string, _ time.Time) error {
	if s.failOn == "insert" {
		return errors.New("insert failed")
	}
	s.inserted = append(s.inserted, messageID)
	return nil
}

func (s *fakeStore) LogEvent(_ context.Context, eventID, _, _ string, _ []byte, _ time.Time) error {
	if s.failOn == "log" {
		return errors.New("log failed")
	}
	s.logged = append(s.logged, eventID)
	return nil
}

func msg(topic string, value []byte) *sarama.ConsumerMessage {
	return &sarama.ConsumerMessage{Topic: topic, Value: value, Offset: 1}
}

func decodeReturning(d decode.Decoded, err error) DecodeFunc {
	return func([]byte) (decode.Decoded, error) { return d, err }
}

func TestConsumeClaimLogsAndMarksAPlainEvent(t *testing.T) {
	st := &fakeStore{}
	h := New(map[string]DecodeFunc{
		"topic-a": decodeReturning(decode.Decoded{EventID: "evt-1", EventKind: "MemberJoined"}, nil),
	}, st, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-a", []*sarama.ConsumerMessage{msg("topic-a", []byte("payload"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	if len(session.marked) != 1 {
		t.Fatalf("expected the message to be marked, got %d", len(session.marked))
	}
	if len(st.logged) != 1 || st.logged[0] != "evt-1" {
		t.Fatalf("expected evt-1 to be logged, got %+v", st.logged)
	}
	if len(st.inserted) != 0 {
		t.Fatalf("expected no message insert for a non-MessageSent event")
	}
}

func TestConsumeClaimInsertsAndLogsAMessageSentEvent(t *testing.T) {
	st := &fakeStore{}
	h := New(map[string]DecodeFunc{
		"chat.events": decodeReturning(decode.Decoded{
			EventID: "evt-1", EventKind: "MessageSent",
			Message: &decode.Message{MessageID: "msg-1", RoomID: "room-1"},
		}, nil),
	}, st, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("chat.events", []*sarama.ConsumerMessage{msg("chat.events", []byte("payload"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	if len(session.marked) != 1 {
		t.Fatalf("expected the message to be marked, got %d", len(session.marked))
	}
	if len(st.inserted) != 1 || st.inserted[0] != "msg-1" {
		t.Fatalf("expected msg-1 to be inserted, got %+v", st.inserted)
	}
	if len(st.logged) != 1 || st.logged[0] != "evt-1" {
		t.Fatalf("expected evt-1 to also be logged, got %+v", st.logged)
	}
}

func TestConsumeClaimMarksAndSkipsAnUndecodableMessage(t *testing.T) {
	st := &fakeStore{}
	h := New(map[string]DecodeFunc{
		"topic-a": decodeReturning(decode.Decoded{}, errors.New("malformed")),
	}, st, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-a", []*sarama.ConsumerMessage{msg("topic-a", []byte("garbage"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	if len(session.marked) != 1 {
		t.Fatalf("expected the malformed message to be marked anyway, got %d", len(session.marked))
	}
	if len(st.logged) != 0 || len(st.inserted) != 0 {
		t.Fatalf("expected nothing written for a message that failed to decode")
	}
}

func TestConsumeClaimLeavesAFailedInsertUnmarked(t *testing.T) {
	st := &fakeStore{failOn: "insert"}
	h := New(map[string]DecodeFunc{
		"chat.events": decodeReturning(decode.Decoded{
			EventID: "evt-1", EventKind: "MessageSent",
			Message: &decode.Message{MessageID: "msg-1", RoomID: "room-1"},
		}, nil),
	}, st, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("chat.events", []*sarama.ConsumerMessage{msg("chat.events", []byte("payload"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	if len(session.marked) != 0 {
		t.Fatalf("expected the message to be left unmarked after a failed insert, got %d marks", len(session.marked))
	}
}

func TestConsumeClaimLeavesAFailedLogUnmarked(t *testing.T) {
	st := &fakeStore{failOn: "log"}
	h := New(map[string]DecodeFunc{
		"topic-a": decodeReturning(decode.Decoded{EventID: "evt-1", EventKind: "MemberJoined"}, nil),
	}, st, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-a", []*sarama.ConsumerMessage{msg("topic-a", []byte("payload"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	if len(session.marked) != 0 {
		t.Fatalf("expected the message to be left unmarked after a failed log write, got %d marks", len(session.marked))
	}
}

func TestConsumeClaimReturnsAnErrorForAnUnregisteredTopic(t *testing.T) {
	h := New(map[string]DecodeFunc{"topic-a": decodeReturning(decode.Decoded{}, nil)}, &fakeStore{}, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-b", nil)

	if err := h.ConsumeClaim(session, claim); err == nil {
		t.Fatalf("expected an error for a topic with no registered decoder")
	}
}
