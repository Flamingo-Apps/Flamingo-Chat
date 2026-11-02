//go:build defect

// Defect test, not part of the normal suite: run with
//
//	GOTOOLCHAIN=local go test -tags defect -run TestDefect ./internal/consumer/...
package consumer

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/IBM/sarama"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/delivery-worker/internal/router"
)

type defectFlakyPublisher struct {
	failChannel string
	failed      bool
	delivered   map[string]bool
}

func (p *defectFlakyPublisher) Publish(_ context.Context, channel string, _ []byte) error {
	if channel == p.failChannel && !p.failed {
		p.failed = true
		return errors.New("redis: connection refused")
	}
	p.delivered[channel] = true
	return nil
}

// TestDefect_OneFailedRedisPublishDropsTheEventForever: the same defect as the
// persistence worker. The delivery handler "leaves the message unmarked so it is
// redelivered" (handler.go), but the next successful message is marked, which
// moves the committed offset past the failed one. A Redis blip therefore drops
// whichever events were in flight, and the doc comment's promise that "dependency
// outages degrade delivery speed, not correctness" (DEC-0047) does not hold.
// If the dropped event is a match_found, both users wait forever in a room
// nobody told them about; if it is a "kicked", the banned user stays connected.
func TestDefect_OneFailedRedisPublishDropsTheEventForever(t *testing.T) {
	pub := &defectFlakyPublisher{failChannel: "room:r2", delivered: map[string]bool{}}
	route := func(value []byte) ([]router.Target, error) {
		return []router.Target{{Channel: "room:" + string(value), Payload: value}}, nil
	}
	h := New(map[string]RouteFunc{"chat.events": route}, pub, slog.Default())

	session := newFakeSession()
	claim := newFakeClaim("chat.events", []*sarama.ConsumerMessage{
		{Topic: "chat.events", Offset: 10, Value: []byte("r1")},
		{Topic: "chat.events", Offset: 11, Value: []byte("r2")},
		{Topic: "chat.events", Offset: 12, Value: []byte("r3")},
	})
	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}

	var committed int64
	for _, m := range session.marked {
		if m.Offset+1 > committed {
			committed = m.Offset + 1
		}
	}
	t.Logf("delivered=%v committed offset=%d (r2 sits at offset 11)", pub.delivered, committed)

	if !pub.delivered["room:r2"] && committed > 11 {
		t.Fatalf("event r2 was never published to Redis and the committed offset (%d) is past it: it will never be redelivered", committed)
	}
}
