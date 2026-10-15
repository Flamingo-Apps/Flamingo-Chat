//go:build e2e

package e2e

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

// TestMain resets this suite's own guest-signup rate-limit counters
// before running - this suite creates about 6 guest accounts across its
// scenarios, one more than docs/ARCHITECTURE.md §4.4's 5-per-hour-per-IP
// limit, all from the one source IP gateway sees through the port-
// forward. That limit exists to slow down a real attacker hammering
// signup from one address, which is exactly what running this suite
// repeatedly looks like from gateway's point of view - resetting it here
// is this test harness clearing its own fixture state, the same thing
// the service-level integration tests do by truncating their own
// Postgres tables before running, not a change to the limit itself or
// anything a real client could do to it.
func TestMain(m *testing.M) {
	url := "redis://localhost:6379/0"
	if v := os.Getenv("REDIS_URL"); v != "" {
		url = v
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		log.Fatalf("redis.ParseURL: %v", err)
	}
	client := redis.NewClient(opts)
	ctx := context.Background()
	keys, err := client.Keys(ctx, "rl:guest_signup:*").Result()
	if err != nil {
		log.Fatalf("list guest_signup rate limit keys: %v", err)
	}
	if len(keys) > 0 {
		if err := client.Del(ctx, keys...).Err(); err != nil {
			log.Fatalf("reset guest_signup rate limit keys: %v", err)
		}
	}
	if err := client.Close(); err != nil {
		log.Printf("closing redis client: %v", err)
	}

	os.Exit(m.Run())
}
