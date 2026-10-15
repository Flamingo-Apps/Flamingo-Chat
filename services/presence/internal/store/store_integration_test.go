//go:build integration

// Run with REDIS_URL pointing at a real Redis:
//
//	go test -tags integration ./internal/store/...
package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
)

func newTestStore(t *testing.T) (*Redis, *redis.Client) {
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

func cleanupKeys(t *testing.T, client *redis.Client, ids ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, k := range []string{"presence:guest", "presence:male", "presence:female", "presence:verified_other"} {
			for _, id := range ids {
				_ = client.ZRem(context.Background(), k, id).Err()
			}
		}
	})
}

func TestRecordAndCounts(t *testing.T) {
	s, client := newTestStore(t)
	ctx := context.Background()
	guestID, maleID, femaleID, otherID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	cleanupKeys(t, client, guestID, maleID, femaleID, otherID)

	err := s.Record(ctx, []Entry{
		{AccountID: guestID, Category: presencev1.Category_CATEGORY_GUEST},
		{AccountID: maleID, Category: presencev1.Category_CATEGORY_MALE},
		{AccountID: femaleID, Category: presencev1.Category_CATEGORY_FEMALE},
		{AccountID: otherID, Category: presencev1.Category_CATEGORY_VERIFIED_OTHER},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	counts, err := s.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if counts.Guests < 1 || counts.Male < 1 || counts.Female < 1 {
		t.Fatalf("expected at least our own entries counted, got %+v", counts)
	}
	// verified_other is in Total but not Male/Female - a precise check
	// isolated from any other data already in this Redis: Total must be
	// at least Guests+Male+Female+1 (the verified_other entry).
	if counts.Total < counts.Guests+counts.Male+counts.Female+1 {
		t.Fatalf("expected verified_other to be counted in Total only, got %+v", counts)
	}
}

func TestCounts_DropsStaleEntries(t *testing.T) {
	s, client := newTestStore(t)
	ctx := context.Background()
	id := uuid.NewString()
	cleanupKeys(t, client, id)

	staleScore := float64(time.Now().Add(-dropAfter - time.Minute).Unix())
	if err := client.ZAdd(ctx, "presence:guest", redis.Z{Score: staleScore, Member: id}).Err(); err != nil {
		t.Fatalf("seed stale entry: %v", err)
	}

	if _, err := s.Counts(ctx); err != nil {
		t.Fatalf("Counts: %v", err)
	}

	score, err := client.ZScore(ctx, "presence:guest", id).Result()
	if err == nil {
		t.Fatalf("expected the stale entry to be removed, still found with score %v", score)
	} else if !errors.Is(err, redis.Nil) {
		t.Fatalf("ZScore: %v", err)
	}
}

func TestRecord_SkipsUnspecifiedCategory(t *testing.T) {
	s, client := newTestStore(t)
	ctx := context.Background()
	id := uuid.NewString()
	cleanupKeys(t, client, id)

	if err := s.Record(ctx, []Entry{{AccountID: id, Category: presencev1.Category_CATEGORY_UNSPECIFIED}}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	for _, k := range []string{"presence:guest", "presence:male", "presence:female", "presence:verified_other"} {
		if score, err := client.ZScore(ctx, k, id).Result(); err == nil {
			t.Fatalf("expected no entry in %s, found score %v", k, score)
		}
	}
}
