// Package ratelimit implements the fixed-window Redis counters
// docs/ARCHITECTURE.md §4.4 and §5.5 describe (rl:{action}:{subject}:{window}).
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrementAndExpire is a Lua script so the increment and the TTL it's
// given on the first hit happen atomically - running them as two
// separate round trips would leave a window open where a crash between
// them leaves the key with no expiry, permanently stuck over whatever
// count it last reached.
var incrementAndExpire = redis.NewScript(`
	local count = redis.call("INCR", KEYS[1])
	if count == 1 then
		redis.call("EXPIRE", KEYS[1], ARGV[1])
	end
	return count
`)

// Limiter checks fixed-window rate limits backed by Redis.
type Limiter struct {
	client *redis.Client
}

func New(client *redis.Client) *Limiter {
	return &Limiter{client: client}
}

// Allow reports whether one more occurrence of action by subject is
// within max per window. The window is anchored to Unix time divided by
// its own length, so every caller in the same window shares one key
// without needing to agree on a start time.
func (l *Limiter) Allow(ctx context.Context, action, subject string, max int, window time.Duration) (bool, error) {
	bucket := time.Now().Unix() / int64(window.Seconds())
	key := fmt.Sprintf("rl:%s:%s:%d", action, subject, bucket)

	count, err := incrementAndExpire.Run(ctx, l.client, []string{key}, int(window.Seconds())).Int64()
	if err != nil {
		return false, fmt.Errorf("ratelimit: %w", err)
	}
	return count <= int64(max), nil
}
