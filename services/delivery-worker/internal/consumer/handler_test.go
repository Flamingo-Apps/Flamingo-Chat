package consumer

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/IBM/sarama"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/delivery-worker/internal/router"
)

// fakeSession implements sarama.ConsumerGroupSession, recording every
// MarkMessage call - that's the only thing this package's tests need to
// observe to tell "processed" from "left for redelivery" apart.
type fakeSession struct {
	ctx    context.Context
	marked []*sarama.ConsumerMessage
}

func newFakeSession() *fakeSession { return &fakeSession{ctx: context.Background()} }

func (f *fakeSession) Claims() map[string][]int32                       { return nil }
func (f *fakeSession) MemberID() string                                 { return "fake" }
func (f *fakeSession) GenerationID() int32                              { return 0 }
func (f *fakeSession) MarkOffset(_ string, _ int32, _ int64, _ string)  {}
func (f *fakeSession) Commit()                                          {}
func (f *fakeSession) ResetOffset(_ string, _ int32, _ int64, _ string) {}
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

// fakePublisher records every publish, or returns a fixed error for a
// specific channel to exercise the "left unmarked for redelivery" path.
type fakePublisher struct {
	published map[string][]byte
	failOn    string
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{published: map[string][]byte{}}
}

func (p *fakePublisher) Publish(_ context.Context, channel string, payload []byte) error {
	if channel == p.failOn {
		return errors.New("publish failed")
	}
	p.published[channel] = payload
	return nil
}

func msg(topic string, value []byte) *sarama.ConsumerMessage {
	return &sarama.ConsumerMessage{Topic: topic, Value: value, Offset: 1}
}

func okRoute(value []byte) ([]router.Target, error) {
	return []router.Target{{Channel: "room:room-1", Payload: value}}, nil
}

func failingRoute([]byte) ([]router.Target, error) {
	return nil, errors.New("malformed")
}

func TestConsumeClaimMarksAMessageItPublishedSuccessfully(t *testing.T) {
	pub := newFakePublisher()
	h := New(map[string]RouteFunc{"topic-a": okRoute}, pub, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-a", []*sarama.ConsumerMessage{msg("topic-a", []byte("payload"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	if len(session.marked) != 1 {
		t.Fatalf("expected the message to be marked, got %d marks", len(session.marked))
	}
	if string(pub.published["room:room-1"]) != "payload" {
		t.Fatalf("expected the payload to be published to room:room-1, got %+v", pub.published)
	}
}

func TestConsumeClaimMarksAndSkipsAnUndecodableMessage(t *testing.T) {
	pub := newFakePublisher()
	h := New(map[string]RouteFunc{"topic-a": failingRoute}, pub, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-a", []*sarama.ConsumerMessage{msg("topic-a", []byte("garbage"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	// A malformed message is marked (skipped), not retried forever - see
	// the handler's own comment.
	if len(session.marked) != 1 {
		t.Fatalf("expected the malformed message to be marked anyway, got %d marks", len(session.marked))
	}
	if len(pub.published) != 0 {
		t.Fatalf("expected nothing published for a message that failed to route")
	}
}

func TestConsumeClaimLeavesAFailedPublishUnmarked(t *testing.T) {
	pub := newFakePublisher()
	pub.failOn = "room:room-1"
	h := New(map[string]RouteFunc{"topic-a": okRoute}, pub, slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-a", []*sarama.ConsumerMessage{msg("topic-a", []byte("payload"))})

	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}
	// Left unmarked so Kafka redelivers it once Redis is reachable again
	// (DEC-0047).
	if len(session.marked) != 0 {
		t.Fatalf("expected the message to be left unmarked after a failed publish, got %d marks", len(session.marked))
	}
}

func TestConsumeClaimReturnsAnErrorForAnUnregisteredTopic(t *testing.T) {
	h := New(map[string]RouteFunc{"topic-a": okRoute}, newFakePublisher(), slog.Default())
	session := newFakeSession()
	claim := newFakeClaim("topic-b", nil)

	if err := h.ConsumeClaim(session, claim); err == nil {
		t.Fatalf("expected an error for a topic with no registered route")
	}
}
