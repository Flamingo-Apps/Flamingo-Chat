// Package banned checks Redis's banned set (docs/ARCHITECTURE.md §3.5,
// §5.5), which Moderation's BanAccount/UnbanAccount populate. Gateway
// checks it directly rather than calling Moderation, the same trust
// boundary presence counts and the matching queue's avoid sets already
// cross without a round trip through their owning service.
package banned

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Checker is this package's behavior, so callers can be tested against
// a fake instead of real Redis.
type Checker interface {
	IsBanned(ctx context.Context, accountID string) (bool, error)
}

// Redis is the Checker implementation backed by a real Redis instance.
type Redis struct {
	client *redis.Client
}

func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

func (r *Redis) IsBanned(ctx context.Context, accountID string) (bool, error) {
	ok, err := r.client.SIsMember(ctx, "banned", accountID).Result()
	if err != nil {
		return false, fmt.Errorf("banned: is banned: %w", err)
	}
	return ok, nil
}
