//go:build defect

// Defect tests for Matching against a REAL Redis (the Lua script is the thing
// under test). They use DB 14 only, flush it, and refuse to run on DB 0.
//
//	SCRATCH_REDIS_ADDR=localhost:6379 \
//	  GOTOOLCHAIN=local go test -tags defect -run TestDefect -v ./internal/server/...
package server

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/pairing"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/queue"
)

const defectRedisDB = 14

func defectRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("SCRATCH_REDIS_ADDR")
	if addr == "" {
		t.Skip("SCRATCH_REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr, DB: defectRedisDB})
	ctx := context.Background()
	if err := c.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	c.FlushDB(ctx) // DB 14 only: the dev stack uses DB 0
	t.Cleanup(func() { c.FlushDB(context.Background()); _ = c.Close() })
	return c
}

func anyRequest(id, name string) *matchingv1.RequestMatchRequest {
	return &matchingv1.RequestMatchRequest{
		AccountId: id, Pseudonym: name, Preference: matchingv1.Preference_PREFERENCE_ANY,
		Gender: identityv1.Gender_GENDER_UNSPECIFIED,
	}
}

// TestDefect_FailedRoomCreationStrandsBothUsers: when Chat.CreateDirectRoom
// fails, tryMatchAndRoom "best-effort re-enqueues both". But requeueBestEffort
// calls TryMatch, which is not "enqueue": it is "find a match, else enqueue".
// Re-enqueueing the seeker puts them in the queue; re-enqueueing the partner
// then MATCHES the seeker and removes them both again, and the result is
// thrown away (`_, _ = s.queue.TryMatch`). Net effect: no room, nobody in the
// queue, and the waiting partner is never told anything.
func TestDefect_FailedRoomCreationStrandsBothUsers(t *testing.T) {
	rc := defectRedis(t)
	q := queue.NewRedis(rc)
	chat := &fakeChat{nextRoomID: "room-1", createErr: errors.New("chat is restarting")}
	srv := New(q, pairing.NewRedis(rc), chat, &fakeProducer{}, sequentialIDs("evt"))
	ctx := context.Background()
	alice, bob := uuid.NewString(), uuid.NewString()

	if _, err := srv.RequestMatch(ctx, anyRequest(alice, "Alice")); err != nil {
		t.Fatalf("alice RequestMatch: %v", err) // nobody waiting: she queues
	}
	if _, err := srv.RequestMatch(ctx, anyRequest(bob, "Bob")); err == nil {
		t.Fatal("expected bob's RequestMatch to fail because Chat is down")
	}

	// No room exists, so both people should still be waiting.
	aliceWaiting, _ := q.Cancel(ctx, alice)
	bobWaiting, _ := q.Cancel(ctx, bob)
	t.Logf("still queued after the failure: alice=%v bob=%v", aliceWaiting, bobWaiting)
	if !aliceWaiting || !bobWaiting {
		t.Fatalf("after Chat failed, alice waiting=%v, bob waiting=%v: whoever is missing is in no queue and no room, and was never told", aliceWaiting, bobWaiting)
	}
}

// TestDefect_FailedPublishAfterRoomCreationLeavesTheMatchedUserInTheDark: the
// room exists and both users are out of the queue, but MatchFound never reaches
// Kafka. Nobody is told, nobody is re-queued, and the seeker gets an error for a
// match that actually happened. (There is no outbox; the code's own comment
// calls this a known simplification.)
func TestDefect_FailedPublishAfterRoomCreationLeavesTheMatchedUserInTheDark(t *testing.T) {
	rc := defectRedis(t)
	q := queue.NewRedis(rc)
	prod := &fakeProducer{err: errors.New("kafka: broker not available")}
	chat := &fakeChat{nextRoomID: "room-1"}
	srv := New(q, pairing.NewRedis(rc), chat, prod, sequentialIDs("evt"))
	ctx := context.Background()
	alice, bob := uuid.NewString(), uuid.NewString()

	_, _ = srv.RequestMatch(ctx, anyRequest(alice, "Alice"))
	_, err := srv.RequestMatch(ctx, anyRequest(bob, "Bob"))
	if err == nil {
		t.Fatal("expected an error")
	}
	aliceWaiting, _ := q.Cancel(ctx, alice)
	bobWaiting, _ := q.Cancel(ctx, bob)
	t.Logf("rooms created=%d, match_found published=%d, alice still queued=%v, bob still queued=%v",
		len(chat.createCalls), len(prod.published), aliceWaiting, bobWaiting)
	if len(chat.createCalls) == 1 && !aliceWaiting && !bobWaiting && len(prod.published) == 0 {
		t.Fatal("a room was created for alice and bob, neither is queued, and no match_found was ever published: both wait forever for a chat nobody announced")
	}
}

// TestDefect_OldIncompatibleEntriesBlockTwoCompatibleUsers: the matching script
// only looks at the OLDEST 100 waiting entries ("enough at pilot scale",
// docs/ARCHITECTURE.md 3.2). If the first 100 are people none of whom the
// newcomer can be paired with, a newcomer never sees anyone behind them, even
// another newcomer who would gladly match. Here: 100 verified men waiting for
// a woman (a realistic imbalance), then two ordinary guests who both said "anyone".
// The entries are written the way TryMatch writes them, five minutes ago.
func TestDefect_OldIncompatibleEntriesBlockTwoCompatibleUsers(t *testing.T) {
	rc := defectRedis(t)
	q := queue.NewRedis(rc)
	ctx := context.Background()

	n := 100 // CONTROL: REVIEW_OLD_ENTRIES=50 must let the two guests match
	if v := os.Getenv("REVIEW_OLD_ENTRIES"); v != "" {
		n, _ = strconv.Atoi(v)
	}
	old := time.Now().Add(-5 * time.Minute).Unix()
	for i := 0; i < n; i++ {
		id := "man-" + strconv.Itoa(1000+i)
		rc.ZAdd(ctx, "mq:any", redis.Z{Score: float64(old + int64(i)), Member: id})
		rc.ZAdd(ctx, "mq:want:male", redis.Z{Score: float64(old + int64(i)), Member: id})
		rc.HSet(ctx, "mq:entry:"+id, "preference", "female", "gender", "male", "verified", "1", "pseudonym", id, "previous_partner", "")
		rc.Expire(ctx, "mq:entry:"+id, 10*time.Minute)
	}

	g1, g2 := uuid.NewString(), uuid.NewString()
	r1, err := q.TryMatch(ctx, queue.Candidate{AccountID: g1, Preference: queue.PreferenceAny, Gender: "unspecified", Pseudonym: "G1"})
	if err != nil || r1.Matched {
		t.Fatalf("g1: matched=%v err=%v (nobody compatible is waiting, so queueing is correct)", r1.Matched, err)
	}
	r2, err := q.TryMatch(ctx, queue.Candidate{AccountID: g2, Preference: queue.PreferenceAny, Gender: "unspecified", Pseudonym: "G2"})
	if err != nil {
		t.Fatalf("g2: %v", err)
	}
	if !r2.Matched {
		t.Fatalf("g1 is waiting and both want 'anyone', but g2 was not matched with them: the script never looked past the 100 older incompatible entries")
	}
}

// TestDefect_SecondNextOnTheSameRoomQueuesNobody: when both people press Next at
// about the same time (or one double-taps), the first call deletes the pairing
// cache entry; the second finds nothing, ends the already-ended room, and
// answers Queued=false with NO error. The gateway sends match_queued only when
// Queued is true, so that person is told nothing and is in no queue.
func TestDefect_SecondNextOnTheSameRoomQueuesNobody(t *testing.T) {
	rc := defectRedis(t)
	q := queue.NewRedis(rc)
	chat := &fakeChat{nextRoomID: "room-1"}
	srv := New(q, pairing.NewRedis(rc), chat, &fakeProducer{}, sequentialIDs("evt"))
	ctx := context.Background()
	alice, bob := uuid.NewString(), uuid.NewString()

	_, _ = srv.RequestMatch(ctx, anyRequest(alice, "Alice"))
	if _, err := srv.RequestMatch(ctx, anyRequest(bob, "Bob")); err != nil {
		t.Fatalf("bob RequestMatch: %v", err)
	}

	first, err := srv.Next(ctx, &matchingv1.NextRequest{AccountId: alice, CurrentRoomId: "room-1"})
	if err != nil || !first.GetQueued() {
		t.Fatalf("alice Next: queued=%v err=%v", first.GetQueued(), err)
	}
	second, err := srv.Next(ctx, &matchingv1.NextRequest{AccountId: bob, CurrentRoomId: "room-1"})
	if err != nil {
		t.Fatalf("bob Next: %v", err)
	}
	bobWaiting, _ := q.Cancel(ctx, bob)
	if !second.GetQueued() && !bobWaiting {
		t.Fatalf("bob pressed Next and got Queued=%v with no error, and is not in the queue (waiting=%v)", second.GetQueued(), bobWaiting)
	}
}
