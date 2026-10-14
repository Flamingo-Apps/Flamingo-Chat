//go:build integration

// End-to-end integration test against real Kafka and Postgres: produces
// a MessageSent event to chat.events and confirms persistence-worker's
// actual consumer group wiring (not just the unit-tested decode/store
// logic in isolation) writes both the message row and the analytics log
// entry (DEC-0048).
//
// Run with KAFKA_BROKERS and POSTGRES_URL pointing at real instances
// (defaulting to the same local instances Tilt deploys):
//
//	go test -tags integration .
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/consumer"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/decode"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/store"
)

func testBrokers() []string {
	v := os.Getenv("KAFKA_BROKERS")
	if v == "" {
		v = "localhost:9092"
	}
	return strings.Split(v, ",")
}

func testPostgresURL() string {
	v := os.Getenv("POSTGRES_URL")
	if v == "" {
		v = "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable"
	}
	return v
}

// readyHandler wraps a sarama.ConsumerGroupHandler and closes Ready once
// Setup has run at least once, so a test can wait for the consumer group
// to have actually joined before producing - see delivery-worker's
// identical helper for the full reasoning.
type readyHandler struct {
	inner sarama.ConsumerGroupHandler
	once  sync.Once
	Ready chan struct{}
}

func newReadyHandler(inner sarama.ConsumerGroupHandler) *readyHandler {
	return &readyHandler{inner: inner, Ready: make(chan struct{})}
}

func (h *readyHandler) Setup(s sarama.ConsumerGroupSession) error {
	err := h.inner.Setup(s)
	h.once.Do(func() { close(h.Ready) })
	return err
}

func (h *readyHandler) Cleanup(s sarama.ConsumerGroupSession) error {
	return h.inner.Cleanup(s)
}

func (h *readyHandler) ConsumeClaim(s sarama.ConsumerGroupSession, c sarama.ConsumerGroupClaim) error {
	return h.inner.ConsumeClaim(s, c)
}

func TestPersistenceWorkerWritesAMessageSentEvent(t *testing.T) {
	brokers := testBrokers()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, testPostgresURL())
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	roomID, senderID, messageID := uuid.NewString(), uuid.NewString(), uuid.NewString()
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
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO rooms (id, type) VALUES ($1, 1)", roomID); err != nil {
		t.Fatalf("insert room: %v", err)
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V4_3_1_0
	group, err := sarama.NewConsumerGroup(brokers, "persistence-worker-integration-test-"+uuid.NewString(), cfg)
	if err != nil {
		t.Fatalf("sarama.NewConsumerGroup: %v", err)
	}
	t.Cleanup(func() { _ = group.Close() })

	handler := newReadyHandler(consumer.New(map[string]consumer.DecodeFunc{
		"chat.events": decode.DecodeChatEvent,
	}, store.NewPostgres(pool), slog.Default()))

	consumeCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go func() {
		for consumeCtx.Err() == nil {
			if err := group.Consume(consumeCtx, []string{"chat.events"}, handler); err != nil && consumeCtx.Err() == nil {
				t.Logf("group.Consume: %v", err)
			}
		}
	}()

	select {
	case <-handler.Ready:
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for the consumer group to join")
	}

	producer, err := sarama.NewSyncProducer(brokers, func() *sarama.Config {
		c := sarama.NewConfig()
		c.Version = sarama.V4_3_1_0
		c.Producer.RequiredAcks = sarama.WaitForAll
		c.Producer.Return.Successes = true
		return c
	}())
	if err != nil {
		t.Fatalf("sarama.NewSyncProducer: %v", err)
	}
	t.Cleanup(func() { _ = producer.Close() })

	eventID := uuid.NewString()
	event := &eventsv1.ChatEvent{
		EventId: eventID,
		Payload: &eventsv1.ChatEvent_MessageSent{
			MessageSent: &eventsv1.MessageSent{
				MessageId: messageID, RoomId: roomID, SenderAccountId: senderID,
				SenderPseudonym: "Fox", Body: "hello from persistence-worker's integration test",
			},
		},
	}
	payload, err := proto.Marshal(event)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	if _, _, err := producer.SendMessage(&sarama.ProducerMessage{
		Topic: "chat.events", Key: sarama.StringEncoder(roomID), Value: sarama.ByteEncoder(payload),
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		var body string
		err := pool.QueryRow(ctx, "SELECT body FROM messages WHERE message_id = $1", messageID).Scan(&body)
		if err == nil {
			if body != "hello from persistence-worker's integration test" {
				t.Fatalf("unexpected body: %q", body)
			}
			var eventKind string
			if err := pool.QueryRow(ctx, "SELECT event_kind FROM analytics.events WHERE event_id = $1", eventID).Scan(&eventKind); err != nil {
				t.Fatalf("expected an analytics log entry for this event: %v", err)
			}
			if eventKind != "MessageSent" {
				t.Fatalf("unexpected event_kind: %q", eventKind)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the message to be written: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
