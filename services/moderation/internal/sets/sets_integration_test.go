//go:build integration

// Run with REDIS_URL pointing at a real Redis:
//
//	go test -tags integration ./internal/sets/...
package sets

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newTestSets(t *testing.T) (*Redis, *redis.Client) {
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
	return NewRedis(client), client
}

func TestBanAndUnban(t *testing.T) {
	s, client := newTestSets(t)
	ctx := context.Background()
	id := uuid.NewString()
	t.Cleanup(func() { _ = client.SRem(ctx, "banned", id).Err() })

	if err := s.Ban(ctx, id); err != nil {
		t.Fatalf("Ban: %v", err)
	}
	if ok, err := client.SIsMember(ctx, "banned", id).Result(); err != nil || !ok {
		t.Fatalf("expected %s to be in banned, ok=%v err=%v", id, ok, err)
	}

	if err := s.Unban(ctx, id); err != nil {
		t.Fatalf("Unban: %v", err)
	}
	if ok, err := client.SIsMember(ctx, "banned", id).Result(); err != nil || ok {
		t.Fatalf("expected %s to be removed from banned, ok=%v err=%v", id, ok, err)
	}
}

func TestAddMutualAvoid(t *testing.T) {
	s, client := newTestSets(t)
	ctx := context.Background()
	a, b := uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		_ = client.SRem(ctx, "avoid:"+a, b).Err()
		_ = client.SRem(ctx, "avoid:"+b, a).Err()
	})

	if err := s.AddMutualAvoid(ctx, a, b); err != nil {
		t.Fatalf("AddMutualAvoid: %v", err)
	}
	if ok, err := client.SIsMember(ctx, "avoid:"+a, b).Result(); err != nil || !ok {
		t.Fatalf("expected b in avoid:a, ok=%v err=%v", ok, err)
	}
	if ok, err := client.SIsMember(ctx, "avoid:"+b, a).Result(); err != nil || !ok {
		t.Fatalf("expected a in avoid:b, ok=%v err=%v", ok, err)
	}
}
