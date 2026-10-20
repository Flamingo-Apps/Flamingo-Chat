// Package consumer wires a Kafka consumer group to internal/router and a
// Redis publisher, implementing sarama.ConsumerGroupHandler.
package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/delivery-worker/internal/router"
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

// Publisher publishes a payload to a Redis channel. The real
// implementation is a thin wrapper around *redis.Client.Publish; a fake
// stands in for it in tests.
type Publisher interface {
	Publish(ctx context.Context, channel string, payload []byte) error
}

// RouteFunc is one of router.RouteChatEvent, router.RouteMatchingEvent or
// router.RouteModerationEvent.
type RouteFunc func(value []byte) ([]router.Target, error)

// Handler implements sarama.ConsumerGroupHandler across every topic
// delivery-worker consumes, in one consumer group (main.go joins
// chat.events, matching.events and moderation.events with a single
// group.Consume call) - sarama hands ConsumeClaim one claim per
// topic+partition regardless of how many topics a group subscribes to,
// so routes picks the right RouteFunc by the claim's own topic name.
type Handler struct {
	routes    map[string]RouteFunc
	publisher Publisher
	logger    *slog.Logger
}

func New(routes map[string]RouteFunc, publisher Publisher, logger *slog.Logger) *Handler {
	return &Handler{routes: routes, publisher: publisher, logger: logger}
}

func (h *Handler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *Handler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim processes every message on this claim. A message that
// fails to even decode is logged and marked (skipped) - at-least-once
// delivery means a redelivery would decode exactly the same way and fail
// again, so retrying a malformed message forever would just block every
// later message on this partition behind it. A message that decodes
// fine but fails to publish (a Redis outage) is logged and left
// unmarked, so it is redelivered once Redis is reachable again -
// DEC-0047's "dependency outages degrade delivery speed, not
// correctness".
func (h *Handler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	route, ok := h.routes[claim.Topic()]
	if !ok {
		return fmt.Errorf("consumer: no route registered for topic %q", claim.Topic())
	}

	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			h.handle(session, claim, msg, route)
		case <-session.Context().Done():
			return nil
		}
	}
}

func (h *Handler) handle(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim, msg *sarama.ConsumerMessage, route RouteFunc) {
	recordLag(claim, msg)

	targets, err := route(msg.Value)
	if err != nil {
		h.logger.Error("route", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "err", err)
		session.MarkMessage(msg, "")
		return
	}

	// A MatchFound event has two targets (one per account). If the first
	// publish succeeds and the second fails, leaving the message
	// unmarked means a redelivery re-publishes to BOTH - the first
	// account's gateway sees the event twice. Accepted: Redis pub/sub is
	// not a durable store, a duplicate live notification is harmless
	// beyond a redundant UI update, and this requires one publish to
	// fail right after another succeeds, which is already a rare case.
	ctx := session.Context()
	for _, target := range targets {
		if err := h.publisher.Publish(ctx, target.Channel, target.Payload); err != nil {
			h.logger.Error("publish", "topic", msg.Topic, "channel", target.Channel, "err", err)
			return // leave the message unmarked - it will be redelivered
		}
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
