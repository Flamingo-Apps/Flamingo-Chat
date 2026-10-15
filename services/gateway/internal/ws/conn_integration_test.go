//go:build integration

// Integration test against a real Redis, driving the full connection
// lifecycle through a real coder/websocket client dialing a real
// httptest.Server - the plumbing (auth handshake, the write-serializing
// goroutine, the Redis subscription actually receiving a published
// message) is exactly what a unit test of dispatch logic in isolation
// can't prove (DEC-0048). The gRPC clients are the same hand-written
// fakes internal/httpapi uses, which is a legitimate substitute per that
// package's own tests - standing up real Identity/Chat/Matching servers
// for this would test coder/websocket and go-redis, not this package.
//
// Run with REDIS_URL pointing at a real Redis (defaults to the same
// local instance Tilt deploys):
//
//	go test -tags integration ./internal/ws/...
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func newTestRedisClient(t *testing.T) *redis.Client {
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
	return client
}

func readFrame(t *testing.T, ctx context.Context, c *websocket.Conn) map[string]any {
	t.Helper()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}
	return m
}

func TestServeFullConnectionLifecycle(t *testing.T) {
	redisClient := newTestRedisClient(t)

	identity := &fakeIdentity{
		getAccount: func(_ context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
			return &identityv1.GetAccountResponse{Account: &identityv1.Account{Id: req.GetAccountId(), Pseudonym: "Fox"}}, nil
		},
	}
	chat := &fakeChat{
		sendMessage: func(_ context.Context, req *chatv1.SendMessageRequest) (*chatv1.SendMessageResponse, error) {
			return &chatv1.SendMessageResponse{MessageId: "msg-1", SentAtUnix: 100}, nil
		},
	}
	deps := Deps{
		Identity: identity, Chat: chat, Matching: &fakeMatching{},
		Sessions: &fakeSessions{}, RateLimiter: &fakeRateLimiter{},
		Redis: redisClient, Logger: slog.Default(),
		PresenceRegistry: presence.NewRegistry(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(deps, nil, w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	// auth
	authID := "integration-test-acc-1"
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"type":"auth","token":"session-for-`+authID+`"}`)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	frame := readFrame(t, ctx, client)
	if frame["type"] != "auth_ok" {
		t.Fatalf("expected auth_ok, got %+v", frame)
	}
	account, _ := frame["account"].(map[string]any)
	if account["id"] != authID {
		t.Fatalf("expected account id %s, got %+v", authID, account)
	}

	// ping/pong
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	if frame := readFrame(t, ctx, client); frame["type"] != "pong" {
		t.Fatalf("expected pong, got %+v", frame)
	}

	// send_message -> message_ack
	sendMsg := `{"type":"send_message","room_id":"room-1","client_msg_id":"c1","body":"hello"}`
	if err := client.Write(ctx, websocket.MessageText, []byte(sendMsg)); err != nil {
		t.Fatalf("write send_message: %v", err)
	}
	frame = readFrame(t, ctx, client)
	if frame["type"] != "message_ack" || frame["message_id"] != "msg-1" || frame["client_msg_id"] != "c1" {
		t.Fatalf("unexpected frame: %+v", frame)
	}

	// A real event published to this account's own Redis channel should
	// arrive translated - proves the subscription, the tag-byte
	// convention (DEC-0070) and the translation layer all work together
	// through the real pubsub, not just in isolation.
	evt := &eventsv1.ModerationEvent{Payload: &eventsv1.ModerationEvent_AccountBanned{
		AccountBanned: &eventsv1.AccountBanned{AccountId: authID, Reason: "integration test"},
	}}
	payload, err := proto.Marshal(evt)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	tagged := append([]byte{byte(moderationEventKind)}, payload...)
	if err := redisClient.Publish(ctx, "user:"+authID, tagged).Err(); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	frame = readFrame(t, ctx, client)
	if frame["type"] != "kicked" || frame["reason"] != "integration test" {
		t.Fatalf("expected a kicked frame from the published event, got %+v", frame)
	}

	// A real presence count published to the global presence channel
	// (the shape presence's own broadcast.Run publishes - plain JSON, no
	// envelope-kind tag byte, docs/ARCHITECTURE.md §3.4) should arrive
	// forwarded as a presence frame - proves authenticate's second
	// Subscribe+Receive for this channel actually works, not just the
	// per-account one.
	if err := redisClient.Publish(ctx, presenceChannel, `{"total":5,"guests":2,"male":2,"female":1}`).Err(); err != nil {
		t.Fatalf("Publish presence: %v", err)
	}
	frame = readFrame(t, ctx, client)
	if frame["type"] != "presence" || frame["total"] != float64(5) || frame["guests"] != float64(2) ||
		frame["male"] != float64(2) || frame["female"] != float64(1) {
		t.Fatalf("expected a presence frame with the published counts, got %+v", frame)
	}

	_ = client.Close(websocket.StatusNormalClosure, "")
}
