// Package store defines persistence-worker's write boundary: one
// structured insert into Chat's own messages table (DEC-0047,
// docs/ARCHITECTURE.md §3.1) and one generic log insert into this
// service's own analytics.events table (DEC-0069), both idempotent so
// Kafka's at-least-once redelivery never produces a duplicate row.
package store

import (
	"context"
	"time"
)

// Store is the persistence boundary internal/consumer writes through.
type Store interface {
	// InsertMessage writes a chat message. Idempotent on messageID: a
	// redelivered MessageSent event is a no-op, not a duplicate.
	InsertMessage(ctx context.Context, messageID, roomID, senderAccountID, senderPseudonym, body string, sentAt time.Time) error

	// LogEvent records one event in the analytics log. Idempotent on
	// eventID for the same reason.
	LogEvent(ctx context.Context, eventID, topic, eventKind string, payload []byte, occurredAt time.Time) error
}
