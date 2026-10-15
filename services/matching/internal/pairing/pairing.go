// Package pairing remembers the two accounts a direct room was created
// for, keyed by room id, so "Next" can re-enqueue the account that asked
// for it with the same attributes it originally matched with.
//
// docs/ARCHITECTURE.md §5.2 gives match_next's client frame no fields at
// all ("none (end the current random chat and queue again)"), so by the
// time Next arrives, Matching has nothing but an account id and a room
// id to work with - not the preference, gender, verified status or
// pseudonym TryMatch needs to look for a new match. This cache is what
// bridges that gap: whatever a direct room's two accounts matched with
// is saved when the room is created, and read back (and removed) when
// either of them asks to move on.
package pairing

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/queue"
)

// ttl is a safety net, not the normal cleanup path: Next explicitly
// deletes its room's entry once it's done with it, but the "End" button
// (docs/ARCHITECTURE.md §5.2's match_end) ends a direct room through
// Chat directly, bypassing Matching entirely (DEC-0074) - so a room that
// is only ever ended that way would otherwise leak its cache entry
// forever. A day is far longer than any real direct room is expected to
// stay open.
const ttl = 24 * time.Hour

// Side is one account's cached matching attributes for a given room.
type Side struct {
	AccountID  string
	Preference string
	Gender     string
	Verified   bool
	Pseudonym  string
}

// Cache is pairing's behavior, so server can be tested against a fake
// instead of real Redis.
type Cache interface {
	Save(ctx context.Context, roomID string, a, b Side) error
	// Get reports (zero value, false, nil) when roomID has no cached
	// pairing - an expected, non-error outcome (TTL expiry, or a room
	// this cache never knew about), not something callers should treat
	// as a failure.
	Get(ctx context.Context, roomID string) (a, b Side, found bool, err error)
	Delete(ctx context.Context, roomID string) error
}

// saveScript sets every field and the TTL in one round trip, so a crash
// between an HSET and a separate EXPIRE call can never leave a
// non-expiring entry behind.
var saveScript = redis.NewScript(`
redis.call("HSET", KEYS[1],
	"a_id", ARGV[1], "a_pref", ARGV[2], "a_gender", ARGV[3], "a_verified", ARGV[4], "a_pseudonym", ARGV[5],
	"b_id", ARGV[6], "b_pref", ARGV[7], "b_gender", ARGV[8], "b_verified", ARGV[9], "b_pseudonym", ARGV[10])
redis.call("EXPIRE", KEYS[1], ARGV[11])
return "ok"
`)

// Redis is the Cache implementation backed by a real Redis instance.
type Redis struct {
	client *redis.Client
}

func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

func key(roomID string) string {
	return "match:members:" + roomID
}

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (r *Redis) Save(ctx context.Context, roomID string, a, b Side) error {
	err := saveScript.Run(ctx, r.client, []string{key(roomID)},
		a.AccountID, a.Preference, a.Gender, boolString(a.Verified), a.Pseudonym,
		b.AccountID, b.Preference, b.Gender, boolString(b.Verified), b.Pseudonym,
		int64(ttl.Seconds()),
	).Err()
	if err != nil {
		return fmt.Errorf("pairing: save: %w", err)
	}
	return nil
}

func (r *Redis) Get(ctx context.Context, roomID string) (Side, Side, bool, error) {
	values, err := r.client.HGetAll(ctx, key(roomID)).Result()
	if err != nil {
		return Side{}, Side{}, false, fmt.Errorf("pairing: get: %w", err)
	}
	if len(values) == 0 {
		return Side{}, Side{}, false, nil
	}
	a := Side{
		AccountID: values["a_id"], Preference: values["a_pref"], Gender: values["a_gender"],
		Verified: values["a_verified"] == "1", Pseudonym: values["a_pseudonym"],
	}
	b := Side{
		AccountID: values["b_id"], Preference: values["b_pref"], Gender: values["b_gender"],
		Verified: values["b_verified"] == "1", Pseudonym: values["b_pseudonym"],
	}
	return a, b, true, nil
}

func (r *Redis) Delete(ctx context.Context, roomID string) error {
	if err := r.client.Del(ctx, key(roomID)).Err(); err != nil {
		return fmt.Errorf("pairing: delete: %w", err)
	}
	return nil
}

// FromCandidate adapts a queue.Candidate (TryMatch's vocabulary) to a
// Side (this package's vocabulary) - the two packages are kept separate
// since pairing has no need for queue.Candidate's PreviousPartner field,
// but server wires them together constantly enough that this avoids
// repeating the same five-field copy at every call site.
func FromCandidate(c queue.Candidate) Side {
	return Side{AccountID: c.AccountID, Preference: c.Preference, Gender: c.Gender, Verified: c.Verified, Pseudonym: c.Pseudonym}
}
