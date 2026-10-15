//go:build integration

// Run with REDIS_URL pointing at a real Redis:
//
//	go test -tags integration ./internal/pairing/...
package pairing

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newTestCache(t *testing.T) *Redis {
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

func TestSaveAndGetRoundTrips(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	roomID := uuid.NewString()
	t.Cleanup(func() { _ = c.Delete(ctx, roomID) })

	a := Side{AccountID: "acc-a", Preference: "any", Gender: "male", Verified: true, Pseudonym: "aye"}
	b := Side{AccountID: "acc-b", Preference: "female", Gender: "female", Verified: true, Pseudonym: "bee"}
	if err := c.Save(ctx, roomID, a, b); err != nil {
		t.Fatalf("Save: %v", err)
	}

	gotA, gotB, found, err := c.Get(ctx, roomID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected to find the saved pairing")
	}
	if gotA != a || gotB != b {
		t.Fatalf("got a=%+v b=%+v, want a=%+v b=%+v", gotA, gotB, a, b)
	}
}

func TestGet_NotFoundForUnknownRoom(t *testing.T) {
	c := newTestCache(t)
	_, _, found, err := c.Get(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected not found for a room that was never saved")
	}
}

func TestDelete_RemovesTheEntry(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	roomID := uuid.NewString()

	if err := c.Save(ctx, roomID, Side{AccountID: "a"}, Side{AccountID: "b"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := c.Delete(ctx, roomID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, _, found, err := c.Get(ctx, roomID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected the entry to be gone after Delete")
	}
}
