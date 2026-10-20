// Package consumer wires a Kafka consumer group to internal/decode and
// internal/store, implementing sarama.ConsumerGroupHandler.
package consumer

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/decode"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/store"
)

// consumerLag estimates how many messages are still unconsumed on a
// partition: the claim's own high water mark (sarama's own words: "the
// offset that will be used for the next message that will be produced")
// minus the offset of the message just received (DEC-0094). Partition
// count is small and fixed per topic (docs/ARCHITECTURE.md's Kafka
// topics), so the label cardinality here is bounded.
var consumerLag = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kafka_consumer_lag",
	Help: "Approximate unconsumed messages on this partition: the partition's high water mark minus the last message this consumer group has seen.",
}, []string{"topic", "partition"})

// DecodeFunc is one of decode.DecodeChatEvent, decode.DecodeMatchingEvent
// or decode.DecodeModerationEvent.
type DecodeFunc func(value []byte) (decode.Decoded, error)

// Handler implements sarama.ConsumerGroupHandler across every topic
// persistence-worker consumes, in one consumer group (main.go joins all
// three with a single group.Consume call) - the same shape
// delivery-worker's Handler uses, for the same reason: sarama hands
// ConsumeClaim one claim per topic+partition regardless of how many
// topics a group subscribes to.
type Handler struct {
	decoders map[string]DecodeFunc
	store    store.Store
	logger   *slog.Logger
}

func New(decoders map[string]DecodeFunc, st store.Store, logger *slog.Logger) *Handler {
	return &Handler{decoders: decoders, store: st, logger: logger}
}

func (h *Handler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *Handler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim processes every message on this claim. A message that
// fails to decode is logged and marked (skipped) - it will fail to
// decode exactly the same way on every redelivery, so retrying it
// forever would just block every later message on this partition behind
// it. A message that decodes fine but fails to write is logged and left
// unmarked, so it is redelivered - this is also what makes persistence-worker
// safe to start before Chat has ever run its own migrations: the first
// few MessageSent events simply retry against a missing table until
// Chat's migration creates it (internal/store's own comment).
func (h *Handler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	decodeEvent, ok := h.decoders[claim.Topic()]
	if !ok {
		return fmt.Errorf("consumer: no decoder registered for topic %q", claim.Topic())
	}

	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			h.handle(session, claim, msg, decodeEvent)
		case <-session.Context().Done():
			return nil
		}
	}
}

func (h *Handler) handle(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim, msg *sarama.ConsumerMessage, decodeEvent DecodeFunc) {
	recordLag(claim, msg)

	d, err := decodeEvent(msg.Value)
	if err != nil {
		h.logger.Error("decode", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "err", err)
		session.MarkMessage(msg, "")
		return
	}

	ctx := session.Context()
	if d.Message != nil {
		m := d.Message
		if err := h.store.InsertMessage(ctx, m.MessageID, m.RoomID, m.SenderAccountID, m.SenderPseudonym, m.Body, m.SentAt); err != nil {
			h.logger.Error("insert message", "topic", msg.Topic, "err", err)
			return // leave unmarked - retried on redelivery
		}
	}
	if err := h.store.LogEvent(ctx, d.EventID, msg.Topic, d.EventKind, msg.Value, d.OccurredAt); err != nil {
		h.logger.Error("log event", "topic", msg.Topic, "err", err)
		return
	}
	session.MarkMessage(msg, "")
}

// recordLag updates consumerLag with how far behind this partition's
// high water mark the message just received is. HighWaterMarkOffset is
// the offset that will be used for the next message produced, so
// subtracting the current message's offset (plus one, since that offset
// is now consumed) gives the count still waiting.
func recordLag(claim sarama.ConsumerGroupClaim, msg *sarama.ConsumerMessage) {
	lag := claim.HighWaterMarkOffset() - msg.Offset - 1
	if lag < 0 {
		lag = 0
	}
	consumerLag.WithLabelValues(msg.Topic, strconv.Itoa(int(msg.Partition))).Set(float64(lag))
}
