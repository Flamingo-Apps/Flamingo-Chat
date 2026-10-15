// Package sets manages the two Redis sets docs/ARCHITECTURE.md §5.5
// describes that other services read directly, rather than calling
// Moderation: banned (gateways check it) and avoid:{account_id}
// (Matching checks it). Postgres (internal/store) is still the durable
// record; these are a live cache derived from it, not the other way
// around.
package sets

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Sets is this package's behavior, so server can be tested against a
// fake instead of real Redis.
type Sets interface {
	Ban(ctx context.Context, accountID string) error
	Unban(ctx context.Context, accountID string) error
	// AddMutualAvoid adds each of a and b to the other's avoid set
	// (docs/ARCHITECTURE.md §3.5: "adds each account to the other's
	// avoid:{account_id} set") so Matching never suggests either to the
	// other regardless of who blocked whom.
	AddMutualAvoid(ctx context.Context, a, b string) error
}

// Redis is the Sets implementation backed by a real Redis instance.
type Redis struct {
	client *redis.Client
}

func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

func (r *Redis) Ban(ctx context.Context, accountID string) error {
	if err := r.client.SAdd(ctx, "banned", accountID).Err(); err != nil {
		return fmt.Errorf("sets: ban: %w", err)
	}
	return nil
}

func (r *Redis) Unban(ctx context.Context, accountID string) error {
	if err := r.client.SRem(ctx, "banned", accountID).Err(); err != nil {
		return fmt.Errorf("sets: unban: %w", err)
	}
	return nil
}

func (r *Redis) AddMutualAvoid(ctx context.Context, a, b string) error {
	pipe := r.client.Pipeline()
	pipe.SAdd(ctx, "avoid:"+a, b)
	pipe.SAdd(ctx, "avoid:"+b, a)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("sets: add mutual avoid: %w", err)
	}
	return nil
}
