//go:build defect

// Takes about 26 seconds: pingInterval (the room re-sync ticker) is a constant.
//
//	SCRATCH_REDIS_ADDR=localhost:6379 \
//	  GOTOOLCHAIN=local go test -tags defect -run TestDefect_AStaleRoomList -v ./internal/ws/...
package ws

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/presence"
)

// defectRedis connects to a scratch Redis (database 15 only, never 0) and skips
// when SCRATCH_REDIS_ADDR is unset, so it can never touch the development one.
func defectRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("SCRATCH_REDIS_ADDR")
	if addr == "" {
		t.Skip("SCRATCH_REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestDefect_AStaleRoomListOverwritesANewerOneAndSilencesTheRoom: refreshRooms
// is called from two goroutines (the 25-second ticker, and the sync_rooms frame
// the web app sends right after creating or joining a room, INC-0024). Each call
// asks Chat for the list, then diffs it against c.subscribed. Nothing orders the
// two calls, so a ticker call whose ListMyRooms answer is OLDER than a later
// sync_rooms call's answer can finish last and unsubscribe the room that
// sync_rooms had just subscribed. Messages in that room then reach this
// connection's user nowhere, for up to another 25 seconds; they are not replayed.
//
// The test makes the interleaving deterministic by holding the ticker's
// ListMyRooms reply until after sync_rooms has completed.
func TestDefect_AStaleRoomListOverwritesANewerOneAndSilencesTheRoom(t *testing.T) {
	rc := defectRedis(t)
	const roomID = "review-room-x"

	var calls atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	doRelease := func() { releaseOnce.Do(func() { close(release) }) }
	chat := &fakeChat{
		listMyRooms: func(ctx context.Context, _ *chatv1.ListMyRoomsRequest) (*chatv1.ListMyRoomsResponse, error) {
			switch calls.Add(1) {
			case 1: // authenticate()
				return &chatv1.ListMyRoomsResponse{}, nil
			case 2: // the 25s ticker: asked BEFORE the room existed, answers late
				select {
				case <-release:
				case <-ctx.Done():
				}
				return &chatv1.ListMyRoomsResponse{}, nil
			default: // sync_rooms: asked after the person joined the room
				return &chatv1.ListMyRoomsResponse{Rooms: []*chatv1.Room{{Id: roomID, Type: chatv1.RoomType_ROOM_TYPE_GROUP}}}, nil
			}
		},
	}
	identity := &fakeIdentity{
		getAccount: func(_ context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
			return &identityv1.GetAccountResponse{Account: &identityv1.Account{Id: req.GetAccountId(), Pseudonym: "Fox"}}, nil
		},
	}
	deps := Deps{
		Identity: identity, Chat: chat, Matching: &fakeMatching{},
		Sessions: &fakeSessions{}, RateLimiter: &fakeRateLimiter{}, Banned: &fakeBanned{},
		Redis: rc, Logger: slog.Default(), PresenceRegistry: presence.NewRegistry(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Serve(deps, nil, w, r) }))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.CloseNow() }()
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"type":"auth","token":"session-for-review-race"}`)); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if _, data, err := client.Read(ctx); err != nil || !strings.Contains(string(data), "auth_ok") {
		t.Fatalf("auth_ok: %v %s", err, data)
	}

	// Wait for the ticker's refresh to start and block inside ListMyRooms.
	for deadline := time.Now().Add(40 * time.Second); calls.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatal("the ticker never fired")
	}

	// CONTROL (REVIEW_RACE_CONTROL=1): let the old answer land BEFORE sync_rooms, so
	// there is no overlap. The message must then be delivered; if it is not, this
	// harness is wrong, not the gateway.
	if os.Getenv("REVIEW_RACE_CONTROL") != "" {
		doRelease()
		time.Sleep(300 * time.Millisecond)
	}

	// The person joins a room over HTTP and the web app sends sync_rooms.
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"type":"sync_rooms"}`)); err != nil {
		t.Fatalf("sync_rooms: %v", err)
	}
	time.Sleep(500 * time.Millisecond) // let the subscribe reach Redis
	doRelease()                        // now the OLD answer arrives and is applied last
	time.Sleep(500 * time.Millisecond) // let the unsubscribe reach Redis

	evt := &eventsv1.ChatEvent{EventId: "e1", Payload: &eventsv1.ChatEvent_MessageSent{MessageSent: &eventsv1.MessageSent{
		MessageId: "m1", RoomId: roomID, SenderAccountId: "someone", SenderPseudonym: "S", Body: "anyone there?", SentAtUnix: 1,
	}}}
	payload, _ := proto.Marshal(evt)
	if err := rc.Publish(ctx, "room:"+roomID, append([]byte{byte(chatEventKind)}, payload...)).Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The "presence" channel is global in Redis (pub/sub ignores DB numbers), so
	// the dev stack's presence service may interleave its own frames: skip them
	// and look for the message until the deadline.
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	var seen []string
	for {
		_, data, err := client.Read(readCtx)
		if err != nil {
			t.Fatalf("a message published to the room the user had just joined never reached them within 3s (frames seen: %v): the stale ticker result unsubscribed it", seen)
		}
		if strings.Contains(string(data), `"type":"message"`) && strings.Contains(string(data), "anyone there?") {
			return // delivered: the bug did not reproduce
		}
		seen = append(seen, string(data))
	}
}
