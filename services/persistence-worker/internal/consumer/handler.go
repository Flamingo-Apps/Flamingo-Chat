// Package consumer wires a Kafka consumer group to internal/decode and
// internal/store, implementing sarama.ConsumerGroupHandler.
package consumer

import (
	"fmt"
	"log/slog"

	"github.com/IBM/sarama"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/decode"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/store"
)

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
			h.handle(session, msg, decodeEvent)
		case <-session.Context().Done():
			return nil
		}
	}
}

func (h *Handler) handle(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, decodeEvent DecodeFunc) {
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
