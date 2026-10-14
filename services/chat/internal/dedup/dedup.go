// Package dedup lets SendMessage recognize a retried call - the gateway
// retrying after a dropped response, not Kafka's own at-least-once
// redelivery - as the same send rather than a new message.
//
// Chat's SendMessage does not write to Postgres itself (see
// internal/store's package comment); it only produces a MessageSent event
// to Kafka. That means there is nothing in the database to check a retry
// against, so this package exists to hold that state somewhere else
// instead (DEC-0061).
package dedup

import "context"

// Result is what a successful SendMessage call reports back, and what a
// retry of the same call needs to reproduce exactly.
type Result struct {
	MessageID  string
	SentAtUnix int64
}

// Deduper claims a (room, sender, client message id) triple exactly once.
type Deduper interface {
	// Claim assigns result to this triple if nothing has claimed it yet
	// within the dedup window, returning (result, true, nil). If the
	// triple was already claimed - a retry - it returns the previously
	// stored result and false instead, so the caller can reply with the
	// original message_id and sent_at rather than producing a second
	// event.
	Claim(ctx context.Context, roomID, senderAccountID, clientMsgID string, result Result) (Result, bool, error)
}
