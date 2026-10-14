//go:build integration

// Integration test against a real Kafka broker - confirms a produced
// ChatEvent is actually readable back off the topic, which a fake
// producer can't prove (DEC-0048).
//
// Run with KAFKA_BROKERS pointing at a real broker (defaults to the same
// local instance Tilt deploys):
//
//	go test -tags integration ./internal/events/...
package events

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
)

func testBrokers() []string {
	v := os.Getenv("KAFKA_BROKERS")
	if v == "" {
		v = "localhost:9092"
	}
	return strings.Split(v, ",")
}

func TestSaramaProducerPublishIsReadableBack(t *testing.T) {
	brokers := testBrokers()

	consumerCfg := sarama.NewConfig()
	consumerCfg.Version = sarama.V4_3_1_0
	client, err := sarama.NewClient(brokers, consumerCfg)
	if err != nil {
		t.Fatalf("sarama.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		t.Fatalf("sarama.NewConsumerFromClient: %v", err)
	}
	t.Cleanup(func() { _ = consumer.Close() })

	partitions, err := consumer.Partitions(Topic)
	if err != nil {
		t.Fatalf("Partitions: %v", err)
	}

	// Start every partition consumer at the newest offset before
	// producing, so this test only ever sees the event it is about to
	// produce - not whatever other tests or services have left on the
	// topic before it.
	messages := make(chan *sarama.ConsumerMessage, 32)
	for _, p := range partitions {
		pc, err := consumer.ConsumePartition(Topic, p, sarama.OffsetNewest)
		if err != nil {
			t.Fatalf("ConsumePartition(%d): %v", p, err)
		}
		t.Cleanup(func() { _ = pc.Close() })
		go func() {
			for msg := range pc.Messages() {
				messages <- msg
			}
		}()
	}

	producer, err := NewSaramaProducer(brokers)
	if err != nil {
		t.Fatalf("NewSaramaProducer: %v", err)
	}
	t.Cleanup(func() { _ = producer.Close() })

	sentAt := time.Now().Unix()
	want := &eventsv1.ChatEvent{
		EventId:        "integration-test-event",
		OccurredAtUnix: sentAt,
		Payload: &eventsv1.ChatEvent_MessageSent{
			MessageSent: &eventsv1.MessageSent{
				MessageId:       "integration-test-message",
				RoomId:          "integration-test-room",
				SenderAccountId: "a",
				SenderPseudonym: "Fox",
				Body:            "hello from the integration test",
				SentAtUnix:      sentAt,
			},
		},
	}
	if err := producer.Publish(context.Background(), "integration-test-room", want); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-messages:
			got := &eventsv1.ChatEvent{}
			if err := proto.Unmarshal(msg.Value, got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got.GetEventId() != want.GetEventId() {
				continue // some other event produced around the same time
			}
			if string(msg.Key) != "integration-test-room" {
				t.Fatalf("expected the message to be keyed by room id, got key %q", msg.Key)
			}
			if !proto.Equal(got, want) {
				t.Fatalf("round-tripped event does not match: got %+v, want %+v", got, want)
			}
			return
		case <-deadline:
			t.Fatalf("timed out waiting for the produced event to come back off %s", Topic)
		}
	}
}
