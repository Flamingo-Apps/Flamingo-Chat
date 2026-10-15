// Package store implements the Redis-backed presence tracking
// docs/ARCHITECTURE.md §3.4 and §5.5 describe: one sorted set per
// category (presence:{category}), scored by last-heartbeat time, with
// entries older than 45 seconds dropped.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
)

// dropAfter is docs/ARCHITECTURE.md §3.4's "drops entries older than 45
// seconds" - three missed 15-second heartbeats, not just one, so a
// single slow or dropped heartbeat doesn't flicker an account in and out
// of the live count.
const dropAfter = 45 * time.Second

// Entry is one account's current presence category.
type Entry struct {
	AccountID string
	Category  presencev1.Category
}

// Counts is docs/ARCHITECTURE.md §3.4's live counts. CATEGORY_VERIFIED_OTHER
// accounts are counted in Total only, never in Male or Female, matching
// the proto's own comment on that category - there is no separate
// "verified" field to also add them to, since GetLiveCountsResponse only
// declares total/guests/male/female.
type Counts struct {
	Total  int64
	Guests int64
	Male   int64
	Female int64
}

// Store is presence's behavior, so server and broadcast can be tested
// against a fake instead of real Redis.
type Store interface {
	Record(ctx context.Context, entries []Entry) error
	Counts(ctx context.Context) (Counts, error)
}

// Redis is the Store implementation backed by a real Redis instance.
type Redis struct {
	client *redis.Client
}

func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

func key(c presencev1.Category) string {
	switch c {
	case presencev1.Category_CATEGORY_GUEST:
		return "presence:guest"
	case presencev1.Category_CATEGORY_MALE:
		return "presence:male"
	case presencev1.Category_CATEGORY_FEMALE:
		return "presence:female"
	case presencev1.Category_CATEGORY_VERIFIED_OTHER:
		return "presence:verified_other"
	default:
		return ""
	}
}

// Record upserts entries' last-heartbeat time. An entry with
// CATEGORY_UNSPECIFIED (or an empty account id) is skipped rather than
// failing the whole batch - gateway's registry never produces one, but a
// single malformed entry is not a reason to drop every other account's
// heartbeat in the same batch (docs/ARCHITECTURE.md §3.4's batching is
// meant to scale with gateway instances, not become a single point of
// failure for all of them at once).
func (r *Redis) Record(ctx context.Context, entries []Entry) error {
	now := float64(time.Now().Unix())
	pipe := r.client.Pipeline()
	for _, e := range entries {
		k := key(e.Category)
		if k == "" || e.AccountID == "" {
			continue
		}
		pipe.ZAdd(ctx, k, redis.Z{Score: now, Member: e.AccountID})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store: record: %w", err)
	}
	return nil
}

// Counts drops stale entries from every category's sorted set, then
// reports how many remain - the same lazy-cleanup-on-read pattern
// services/matching/internal/queue uses, so a category nobody is
// currently polling is still bounded by dropAfter once anyone next asks
// (here, the 5-second broadcaster always does, so cleanup runs
// continuously regardless of whether GetLiveCounts itself is ever
// called).
func (r *Redis) Counts(ctx context.Context) (Counts, error) {
	cutoff := time.Now().Add(-dropAfter).Unix()
	cats := []presencev1.Category{
		presencev1.Category_CATEGORY_GUEST,
		presencev1.Category_CATEGORY_MALE,
		presencev1.Category_CATEGORY_FEMALE,
		presencev1.Category_CATEGORY_VERIFIED_OTHER,
	}

	pipe := r.client.Pipeline()
	cards := make([]*redis.IntCmd, len(cats))
	for i, c := range cats {
		k := key(c)
		pipe.ZRemRangeByScore(ctx, k, "-inf", fmt.Sprintf("%d", cutoff))
		cards[i] = pipe.ZCard(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return Counts{}, fmt.Errorf("store: counts: %w", err)
	}

	guests, male, female, verifiedOther := cards[0].Val(), cards[1].Val(), cards[2].Val(), cards[3].Val()
	return Counts{
		Total:  guests + male + female + verifiedOther,
		Guests: guests,
		Male:   male,
		Female: female,
	}, nil
}
