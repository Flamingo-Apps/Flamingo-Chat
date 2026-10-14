package dedup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// window is how long a claimed triple is remembered. It only has to
// outlast a realistic client retry, not the room's lifetime - an
// unbounded key per message ever sent would never get cleaned up.
const window = 5 * time.Minute

// Redis is the Deduper implementation backed by Redis's SETNX, which is
// what makes "first writer wins" atomic without a round trip to check
// first.
type Redis struct {
	client *redis.Client
}

func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

func (r *Redis) Claim(ctx context.Context, roomID, senderAccountID, clientMsgID string, result Result) (Result, bool, error) {
	key := "chat:dedup:" + roomID + ":" + senderAccountID + ":" + clientMsgID
	value := encode(result)

	ok, err := r.client.SetNX(ctx, key, value, window).Result()
	if err != nil {
		return Result{}, false, fmt.Errorf("dedup: claim: %w", err)
	}
	if ok {
		return result, true, nil
	}

	existing, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		// The key expired between our failed SETNX and this GET - vanishingly
		// unlikely given the window is minutes, but if it happens, the
		// caller is safer treating it as a fresh claim than erroring.
		return result, true, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("dedup: claim: read existing: %w", err)
	}
	prev, err := decode(existing)
	if err != nil {
		return Result{}, false, fmt.Errorf("dedup: claim: %w", err)
	}
	return prev, false, nil
}

func encode(r Result) string {
	return r.MessageID + "|" + strconv.FormatInt(r.SentAtUnix, 10)
}

func decode(s string) (Result, error) {
	id, sentAt, ok := strings.Cut(s, "|")
	if !ok {
		return Result{}, fmt.Errorf("malformed dedup value %q", s)
	}
	n, err := strconv.ParseInt(sentAt, 10, 64)
	if err != nil {
		return Result{}, fmt.Errorf("malformed dedup value %q: %w", s, err)
	}
	return Result{MessageID: id, SentAtUnix: n}, nil
}
