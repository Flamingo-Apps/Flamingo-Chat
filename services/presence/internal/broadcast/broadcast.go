// Package broadcast publishes live counts to Redis's presence channel
// every 5 seconds (docs/ARCHITECTURE.md §3.4), so every gateway instance
// (and through it, every connected browser) sees the same numbers
// without polling GetLiveCounts itself.
//
// The published payload is plain JSON - {total, guests, male, female},
// no envelope-kind tag byte - since, unlike room:{room_id} and
// user:{account_id}, this channel only ever carries one kind of message
// (DEC-0070/INC-0009's ambiguity problem doesn't apply here). Gateway's
// WebSocket layer forwards it to clients as the presence frame
// docs/ARCHITECTURE.md §5.2 already documents.
package broadcast

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/presence/internal/store"
)

// Channel is the Redis channel live counts are published to.
const Channel = "presence"

// Interval is docs/ARCHITECTURE.md §3.4's "every 5 seconds".
const Interval = 5 * time.Second

type payload struct {
	Total  int64 `json:"total"`
	Guests int64 `json:"guests"`
	Male   int64 `json:"male"`
	Female int64 `json:"female"`
}

// Run publishes the current counts every Interval until ctx is canceled.
// A single failed publish is logged and retried on the next tick rather
// than stopping the loop - these are live, non-durable counts
// (docs/ARCHITECTURE.md §3.4), so the next tick's fresher number is a
// complete replacement for a missed one, not something to recover with
// retries of its own.
func Run(ctx context.Context, s store.Store, client *redis.Client, logger *slog.Logger) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := publishOnce(ctx, s, client); err != nil {
				logger.Warn("presence broadcast", "err", err)
			}
		}
	}
}

func publishOnce(ctx context.Context, s store.Store, client *redis.Client) error {
	counts, err := s.Counts(ctx)
	if err != nil {
		return fmt.Errorf("broadcast: counts: %w", err)
	}
	b, err := json.Marshal(payload{Total: counts.Total, Guests: counts.Guests, Male: counts.Male, Female: counts.Female})
	if err != nil {
		return fmt.Errorf("broadcast: marshal: %w", err)
	}
	if err := client.Publish(ctx, Channel, b).Err(); err != nil {
		return fmt.Errorf("broadcast: publish: %w", err)
	}
	return nil
}
