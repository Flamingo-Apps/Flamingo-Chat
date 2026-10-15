// Package events publishes MatchingEvent envelopes (proto/events/v1) to
// the matching.events Kafka topic, keyed by room_id so one room's events
// stay in order (docs/ARCHITECTURE.md §5.4, DEC-0043) - the same shape
// as services/chat/internal/events, mirrored here rather than shared
// because the two produce different envelope types.
package events

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
)

// Topic is the Kafka topic matching events are produced to.
const Topic = "matching.events"

// Producer publishes a MatchingEvent envelope.
type Producer interface {
	Publish(ctx context.Context, roomID string, event *eventsv1.MatchingEvent) error
	Close() error
}

// SaramaProducer is the Producer implementation backed by a real Kafka
// broker, using a synchronous producer so Publish only returns once the
// broker has acknowledged the write.
type SaramaProducer struct {
	producer sarama.SyncProducer
}

// NewSaramaProducer connects to the given brokers (e.g. []string{"kafka:9092"}).
func NewSaramaProducer(brokers []string) (*SaramaProducer, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 5
	cfg.Producer.Return.Successes = true
	// The exact version the local cluster runs (deploy/k8s/dev/kafka.yaml),
	// same as services/chat/internal/events - confirmed present in
	// sarama's own SupportedVersions list (DEC-0061).
	cfg.Version = sarama.V4_3_1_0

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("events: new producer: %w", err)
	}
	return &SaramaProducer{producer: producer}, nil
}

// Publish marshals and sends event. The context is accepted for
// consistency with the rest of this codebase's call signatures, but
// sarama's synchronous producer has no way to honor cancellation
// mid-send - a canceled context does not abort an in-flight produce.
func (p *SaramaProducer) Publish(_ context.Context, roomID string, event *eventsv1.MatchingEvent) error {
	payload, err := proto.Marshal(event)
	if err != nil {
		return fmt.Errorf("events: marshal: %w", err)
	}
	_, _, err = p.producer.SendMessage(&sarama.ProducerMessage{
		Topic: Topic,
		Key:   sarama.StringEncoder(roomID),
		Value: sarama.ByteEncoder(payload),
	})
	if err != nil {
		return fmt.Errorf("events: publish: %w", err)
	}
	return nil
}

func (p *SaramaProducer) Close() error {
	return p.producer.Close()
}
