//go:build integration

// Integration test against a real Redis - the Lua script's atomicity and
// its actual compatibility logic are exactly what a fake can't
// meaningfully prove (DEC-0048), the same reasoning gateway's ratelimit
// package integration tests already follow.
//
// Run with REDIS_URL pointing at a real Redis (defaults to the same
// local instance Tilt deploys):
//
//	go test -tags integration ./internal/queue/...
package queue

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newTestQueue(t *testing.T) *Redis {
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

func TestTryMatch_NoCandidateEnqueues(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	id := uuid.NewString()
	t.Cleanup(func() { _, _ = q.Cancel(ctx, id) })

	result, err := q.TryMatch(ctx, Candidate{AccountID: id, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "p1"})
	if err != nil {
		t.Fatalf("TryMatch: %v", err)
	}
	if result.Matched {
		t.Fatal("expected no match for the first, only, waiting account")
	}
}

func TestTryMatch_TwoAnyPreferenceAccountsMatch(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	idA, idB := uuid.NewString(), uuid.NewString()
	t.Cleanup(func() { _, _ = q.Cancel(ctx, idA); _, _ = q.Cancel(ctx, idB) })

	result, err := q.TryMatch(ctx, Candidate{AccountID: idA, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "a"})
	if err != nil {
		t.Fatalf("first TryMatch: %v", err)
	}
	if result.Matched {
		t.Fatal("first account should queue, not match itself")
	}

	result, err = q.TryMatch(ctx, Candidate{AccountID: idB, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "b"})
	if err != nil {
		t.Fatalf("second TryMatch: %v", err)
	}
	if !result.Matched || result.Partner.AccountID != idA {
		t.Fatalf("expected the second account to match the first, got %+v", result)
	}

	// The matched pair is removed - a third request shouldn't find either.
	idC := uuid.NewString()
	t.Cleanup(func() { _, _ = q.Cancel(ctx, idC) })
	result, err = q.TryMatch(ctx, Candidate{AccountID: idC, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "c"})
	if err != nil {
		t.Fatalf("third TryMatch: %v", err)
	}
	if result.Matched {
		t.Fatalf("expected no match left waiting, got %+v", result)
	}
}

func TestTryMatch_GenderPreferenceOnlyMatchesVerifiedGender(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	unverifiedMale, verifiedMale, seeker := uuid.NewString(), uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		_, _ = q.Cancel(ctx, unverifiedMale)
		_, _ = q.Cancel(ctx, verifiedMale)
		_, _ = q.Cancel(ctx, seeker)
	})

	// An unverified "male" shouldn't be matchable via a gender preference
	// at all - it never even enters mq:want:male. Its own preference is
	// set to female (not any) purely so it doesn't immediately match
	// verifiedMale the moment both are waiting - unrelated to what's
	// actually under test here.
	if _, err := q.TryMatch(ctx, Candidate{AccountID: unverifiedMale, Preference: PreferenceFemale, Gender: GenderMale, Verified: false, Pseudonym: "um"}); err != nil {
		t.Fatalf("TryMatch unverifiedMale: %v", err)
	}
	if _, err := q.TryMatch(ctx, Candidate{AccountID: verifiedMale, Preference: PreferenceAny, Gender: GenderMale, Verified: true, Pseudonym: "vm"}); err != nil {
		t.Fatalf("TryMatch verifiedMale: %v", err)
	}

	result, err := q.TryMatch(ctx, Candidate{AccountID: seeker, Preference: PreferenceMale, Gender: GenderFemale, Verified: true, Pseudonym: "seeker"})
	if err != nil {
		t.Fatalf("TryMatch seeker: %v", err)
	}
	if !result.Matched || result.Partner.AccountID != verifiedMale {
		t.Fatalf("expected the seeker to match the verified male, got %+v", result)
	}
}

func TestTryMatch_MutualPreferenceRequired(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	// candidate wants FEMALE specifically; seeker is male and verified -
	// the seeker's own preference (any) doesn't matter, the candidate's
	// preference must also accept the seeker, and it doesn't.
	candidate, seeker, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		_, _ = q.Cancel(ctx, candidate)
		_, _ = q.Cancel(ctx, seeker)
		_, _ = q.Cancel(ctx, other)
	})

	if _, err := q.TryMatch(ctx, Candidate{AccountID: candidate, Preference: PreferenceFemale, Gender: GenderFemale, Verified: true, Pseudonym: "c"}); err != nil {
		t.Fatalf("TryMatch candidate: %v", err)
	}
	// seeker's own preference is male (not any) purely so it can't later
	// be matched by `other` instead of `candidate` - unrelated to what's
	// under test here (whether `candidate`'s own preference rejects a
	// male seeker, regardless of the seeker's own stated preference).
	result, err := q.TryMatch(ctx, Candidate{AccountID: seeker, Preference: PreferenceMale, Gender: GenderMale, Verified: true, Pseudonym: "s"})
	if err != nil {
		t.Fatalf("TryMatch seeker: %v", err)
	}
	if result.Matched {
		t.Fatalf("expected no match - candidate only accepts verified females, got %+v", result)
	}

	// A compatible third account should still match the waiting candidate.
	result, err = q.TryMatch(ctx, Candidate{AccountID: other, Preference: PreferenceAny, Gender: GenderFemale, Verified: true, Pseudonym: "o"})
	if err != nil {
		t.Fatalf("TryMatch other: %v", err)
	}
	if !result.Matched || result.Partner.AccountID != candidate {
		t.Fatalf("expected other to match candidate, got %+v", result)
	}
}

func TestTryMatch_NeverRematchesPreviousPartner(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	idA, idB := uuid.NewString(), uuid.NewString()
	t.Cleanup(func() { _, _ = q.Cancel(ctx, idA); _, _ = q.Cancel(ctx, idB) })

	if _, err := q.TryMatch(ctx, Candidate{AccountID: idA, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "a", PreviousPartner: idB}); err != nil {
		t.Fatalf("TryMatch: %v", err)
	}
	result, err := q.TryMatch(ctx, Candidate{AccountID: idB, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "b"})
	if err != nil {
		t.Fatalf("TryMatch: %v", err)
	}
	if result.Matched {
		t.Fatalf("expected no match - idA has idB as a previous partner, got %+v", result)
	}
}

func TestTryMatch_RespectsAvoidSet(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	idA, idB := uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		_, _ = q.Cancel(ctx, idA)
		_, _ = q.Cancel(ctx, idB)
		_ = q.client.Del(ctx, "avoid:"+idA).Err()
	})

	if err := q.client.SAdd(ctx, "avoid:"+idA, idB).Err(); err != nil {
		t.Fatalf("seed avoid set: %v", err)
	}
	if _, err := q.TryMatch(ctx, Candidate{AccountID: idA, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "a"}); err != nil {
		t.Fatalf("TryMatch: %v", err)
	}
	result, err := q.TryMatch(ctx, Candidate{AccountID: idB, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "b"})
	if err != nil {
		t.Fatalf("TryMatch: %v", err)
	}
	if result.Matched {
		t.Fatalf("expected no match - idA avoids idB, got %+v", result)
	}
}

func TestCancel_RemovesAWaitingEntry(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	id := uuid.NewString()

	if _, err := q.TryMatch(ctx, Candidate{AccountID: id, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: "p"}); err != nil {
		t.Fatalf("TryMatch: %v", err)
	}
	cancelled, err := q.Cancel(ctx, id)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !cancelled {
		t.Fatal("expected Cancel to report the entry existed")
	}

	cancelled, err = q.Cancel(ctx, id)
	if err != nil {
		t.Fatalf("second Cancel: %v", err)
	}
	if cancelled {
		t.Fatal("expected the second Cancel to report nothing was there")
	}
}

func TestTryMatch_AtomicUnderConcurrency(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	const pairs = 20
	ids := make([]string, pairs*2)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = q.Cancel(ctx, id)
		}
	})

	results := make([]MatchResult, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			r, err := q.TryMatch(ctx, Candidate{AccountID: id, Preference: PreferenceAny, Gender: "unspecified", Pseudonym: id})
			if err != nil {
				t.Errorf("TryMatch %d: %v", i, err)
				return
			}
			results[i] = r
		}(i, id)
	}
	wg.Wait()

	matched := 0
	partners := map[string]string{}
	for i, r := range results {
		if r.Matched {
			matched++
			if other, ok := partners[r.Partner.AccountID]; ok && other != ids[i] {
				t.Fatalf("partner %s matched more than once", r.Partner.AccountID)
			}
			partners[ids[i]] = r.Partner.AccountID
		}
	}
	if matched != pairs {
		t.Fatalf("expected exactly %d matches (one side of each pair) out of %d accounts, got %d", pairs, len(ids), matched)
	}
}
