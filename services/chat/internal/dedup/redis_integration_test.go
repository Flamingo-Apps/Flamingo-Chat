//go:build integration

// Integration test against a real Redis - SETNX's atomicity under
// concurrent callers is exactly the property a fake can't meaningfully
// prove (DEC-0048).
//
// Run with REDIS_URL pointing at a real Redis (defaults to the same local
// instance Tilt deploys):
//
//	go test -tags integration ./internal/dedup/...
package dedup

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *Redis {
	t.Helper()
	url := "redis://localhost:6379/0"
	if v := os.Getenv("REDIS_URL"); v != "" {
		url = v
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis.ParseURL: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	return NewRedis(client)
}

func TestRedisClaimFirstCallerWins(t *testing.T) {
	d := newTestRedis(t)
	ctx := context.Background()

	first := Result{MessageID: "msg-a", SentAtUnix: 100}
	second := Result{MessageID: "msg-b", SentAtUnix: 200}

	result, claimed, err := d.Claim(ctx, "room-1", "sender-1", "client-msg-1", first)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !claimed || result != first {
		t.Fatalf("expected the first claim to win with %+v, got claimed=%v result=%+v", first, claimed, result)
	}

	result, claimed, err = d.Claim(ctx, "room-1", "sender-1", "client-msg-1", second)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if claimed || result != first {
		t.Fatalf("expected the second claim to lose and return %+v, got claimed=%v result=%+v", first, claimed, result)
	}
}

func TestRedisClaimIsAtomicUnderConcurrency(t *testing.T) {
	d := newTestRedis(t)
	ctx := context.Background()

	const attempts = 20
	var wg sync.WaitGroup
	wins := make([]bool, attempts)
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, claimed, err := d.Claim(ctx, "room-2", "sender-1", "client-msg-race", Result{MessageID: "msg", SentAtUnix: int64(i)})
			if err != nil {
				t.Errorf("claim %d: %v", i, err)
				return
			}
			wins[i] = claimed
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, w := range wins {
		if w {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one of %d concurrent claims to win, got %d", attempts, winners)
	}
}
