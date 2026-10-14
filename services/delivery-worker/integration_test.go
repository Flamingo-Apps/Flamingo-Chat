//go:build integration

// End-to-end integration test against real Kafka and Redis: produces an
// event to chat.events and confirms delivery-worker's actual consumer
// group wiring (not just the unit-tested routing logic in isolation)
// gets it onto the right Redis channel (DEC-0048).
//
// Run with KAFKA_BROKERS and REDIS_URL pointing at real instances
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
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/delivery-worker/internal/consumer"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/delivery-worker/internal/router"
)

func testBrokers() []string {
	v := os.Getenv("KAFKA_BROKERS")
	if v == "" {
		v = "localhost:9092"
	}
	return strings.Split(v, ",")
}

func testRedisURL() string {
	v := os.Getenv("REDIS_URL")
	if v == "" {
		v = "redis://localhost:6379/0"
	}
	return v
}

// readyHandler wraps a sarama.ConsumerGroupHandler and closes Ready once
// Setup has run at least once, so a test can wait for the consumer group
// to have actually joined before producing - otherwise a message
// produced before the group finishes its first rebalance would never be
// seen (this group starts with no committed offset, so it would be
// delivered from "newest", not "from the beginning").
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

func TestDeliveryWorkerRoutesAChatEventOntoItsRoomChannel(t *testing.T) {
	brokers := testBrokers()
	roomID := uuid.NewString()

	redisOpts, err := redis.ParseURL(testRedisURL())
	if err != nil {
		t.Fatalf("redis.ParseURL: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	t.Cleanup(func() { _ = redisClient.Close() })

	sub := redisClient.Subscribe(context.Background(), "room:"+roomID)
	t.Cleanup(func() { _ = sub.Close() })
	if _, err := sub.Receive(context.Background()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V4_3_1_0
	// A fresh, unique group id with no committed offset: without this,
	// Config.Consumer.Offsets.Initial's default of OffsetNewest only
	// applies the first time a group is ever seen - reusing a group id
	// across test runs would pick up wherever that group left off.
	group, err := sarama.NewConsumerGroup(brokers, "delivery-worker-integration-test-"+uuid.NewString(), cfg)
	if err != nil {
		t.Fatalf("sarama.NewConsumerGroup: %v", err)
	}
	t.Cleanup(func() { _ = group.Close() })

	handler := newReadyHandler(consumer.New(map[string]consumer.RouteFunc{
		"chat.events": router.RouteChatEvent,
	}, &redisPublisher{client: redisClient}, slog.Default()))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for ctx.Err() == nil {
			if err := group.Consume(ctx, []string{"chat.events"}, handler); err != nil && ctx.Err() == nil {
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

	want := &eventsv1.ChatEvent{
		EventId: "integration-test-" + roomID,
		Payload: &eventsv1.ChatEvent_MessageSent{
			MessageSent: &eventsv1.MessageSent{RoomId: roomID, Body: "hello from delivery-worker's integration test"},
		},
	}
	payload, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	if _, _, err := producer.SendMessage(&sarama.ProducerMessage{
		Topic: "chat.events",
		Key:   sarama.StringEncoder(roomID),
		Value: sarama.ByteEncoder(payload),
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	select {
	case redisMsg := <-sub.Channel():
		got := &eventsv1.ChatEvent{}
		if err := proto.Unmarshal([]byte(redisMsg.Payload), got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !proto.Equal(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for the event to arrive on room:%s", roomID)
	}
}
