//go:build integration

// Integration test against a real Redis - the Lua script's atomicity is
// exactly the property a fake can't meaningfully prove (DEC-0048).
//
// Run with REDIS_URL pointing at a real Redis (defaults to the same
// local instance Tilt deploys):
//
//	go test -tags integration ./internal/ratelimit/...
package ratelimit

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newTestLimiter(t *testing.T) *Limiter {
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
	return New(client)
}

func TestAllowEnforcesTheLimitWithinAWindow(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()
	subject := uuid.NewString()

	for i := 1; i <= 3; i++ {
		ok, err := l.Allow(ctx, "test-action", subject, 3, time.Minute)
		if err != nil {
			t.Fatalf("Allow %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("expected request %d of 3 to be allowed", i)
		}
	}

	ok, err := l.Allow(ctx, "test-action", subject, 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow 4th: %v", err)
	}
	if ok {
		t.Fatalf("expected the 4th request to be denied with a limit of 3")
	}
}

func TestAllowIsAtomicUnderConcurrency(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()
	subject := uuid.NewString()

	const attempts = 50
	const max = 10
	var wg sync.WaitGroup
	allowed := make([]bool, attempts)
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := l.Allow(ctx, "test-action-concurrent", subject, max, time.Minute)
			if err != nil {
				t.Errorf("Allow %d: %v", i, err)
				return
			}
			allowed[i] = ok
		}(i)
	}
	wg.Wait()

	count := 0
	for _, ok := range allowed {
		if ok {
			count++
		}
	}
	if count != max {
		t.Fatalf("expected exactly %d of %d concurrent requests to be allowed, got %d", max, attempts, count)
	}
}
