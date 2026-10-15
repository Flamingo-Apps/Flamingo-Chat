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
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/banned"
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
		Sessions: &fakeSessions{}, RateLimiter: &fakeRateLimiter{}, Banned: &fakeBanned{},
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

	// A real presence count published to the global presence channel
	// (the shape presence's own broadcast.Run publishes - plain JSON, no
	// envelope-kind tag byte, docs/ARCHITECTURE.md §3.4) should arrive
	// forwarded as a presence frame - proves authenticate's second
	// Subscribe+Receive for this channel actually works, not just the
	// per-account one. Checked before the AccountBanned event below,
	// since that one legitimately closes the connection (DEC-0083) and
	// nothing can be read on it afterward.
	if err := redisClient.Publish(ctx, presenceChannel, `{"total":5,"guests":2,"male":2,"female":1}`).Err(); err != nil {
		t.Fatalf("Publish presence: %v", err)
	}
	frame = readFrame(t, ctx, client)
	if frame["type"] != "presence" || frame["total"] != float64(5) || frame["guests"] != float64(2) ||
		frame["male"] != float64(2) || frame["female"] != float64(1) {
		t.Fatalf("expected a presence frame with the published counts, got %+v", frame)
	}

	// A real event published to this account's own Redis channel should
	// arrive translated - proves the subscription, the tag-byte
	// convention (DEC-0070) and the translation layer all work together
	// through the real pubsub, not just in isolation. This is the last
	// thing this test does with the connection: a kicked frame now
	// closes it (DEC-0083), proven in detail by
	// TestKickedFrameClosesTheConnection below.
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
}

// TestServeRejectsBannedAccountAtConnect proves authenticate's banned
// check against a real Redis "banned" set, not just a fake.
func TestServeRejectsBannedAccountAtConnect(t *testing.T) {
	redisClient := newTestRedisClient(t)
	ctx := context.Background()
	authID := "integration-test-banned-acc"
	if err := redisClient.SAdd(ctx, "banned", authID).Err(); err != nil {
		t.Fatalf("seed banned set: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.SRem(context.Background(), "banned", authID).Err() })

	identity := &fakeIdentity{
		getAccount: func(_ context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
			return &identityv1.GetAccountResponse{Account: &identityv1.Account{Id: req.GetAccountId(), Pseudonym: "Fox"}}, nil
		},
	}
	deps := Deps{
		Identity: identity, Chat: &fakeChat{}, Matching: &fakeMatching{},
		Sessions: &fakeSessions{}, RateLimiter: &fakeRateLimiter{}, Banned: banned.NewRedis(redisClient),
		Redis: redisClient, Logger: slog.Default(),
		PresenceRegistry: presence.NewRegistry(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(deps, nil, w, r)
	}))
	defer srv.Close()

	dialCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	if err := client.Write(dialCtx, websocket.MessageText, []byte(`{"type":"auth","token":"session-for-`+authID+`"}`)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if _, _, err := client.Read(dialCtx); err == nil {
		t.Fatal("expected the connection to be closed for a banned account, got a frame instead")
	}
}

// TestKickedFrameClosesTheConnection proves a real AccountBanned event
// arriving mid-session (docs/ARCHITECTURE.md §3.5's "gateways... close
// that user's connections") actually ends the connection, not just
// delivers a notification the client could ignore.
func TestKickedFrameClosesTheConnection(t *testing.T) {
	redisClient := newTestRedisClient(t)
	authID := "integration-test-kick-acc"

	identity := &fakeIdentity{
		getAccount: func(_ context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
			return &identityv1.GetAccountResponse{Account: &identityv1.Account{Id: req.GetAccountId(), Pseudonym: "Fox"}}, nil
		},
	}
	deps := Deps{
		Identity: identity, Chat: &fakeChat{}, Matching: &fakeMatching{},
		Sessions: &fakeSessions{}, RateLimiter: &fakeRateLimiter{}, Banned: &fakeBanned{},
		Redis: redisClient, Logger: slog.Default(),
		PresenceRegistry: presence.NewRegistry(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(deps, nil, w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	if err := client.Write(ctx, websocket.MessageText, []byte(`{"type":"auth","token":"session-for-`+authID+`"}`)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if frame := readFrame(t, ctx, client); frame["type"] != "auth_ok" {
		t.Fatalf("expected auth_ok, got %+v", frame)
	}

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

	if frame := readFrame(t, ctx, client); frame["type"] != "kicked" {
		t.Fatalf("expected a kicked frame, got %+v", frame)
	}
	if _, _, err := client.Read(ctx); err == nil {
		t.Fatal("expected the connection to be closed after the kicked frame, got another frame instead")
	}
}

// TestMatchFoundSubscribesToTheNewRoomImmediately proves
// subscribeToNewRoom's fix: without it, a message published to the new
// room right after match_found would only arrive once refreshRooms's
// periodic tick happens to catch up - up to 25 seconds later (DEC-0073)
// - and Redis pub/sub has no replay, so a connection not yet subscribed
// at publish time wouldn't just see it late, it would never see it at
// all. This test's own timeout (well under 25s) would catch a
// regression back to that behavior.
func TestMatchFoundSubscribesToTheNewRoomImmediately(t *testing.T) {
	redisClient := newTestRedisClient(t)
	authID := "integration-test-matchfound-acc"
	roomID := "integration-test-matchfound-room"

	identity := &fakeIdentity{
		getAccount: func(_ context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
			return &identityv1.GetAccountResponse{Account: &identityv1.Account{Id: req.GetAccountId(), Pseudonym: "Fox"}}, nil
		},
	}
	deps := Deps{
		Identity: identity, Chat: &fakeChat{}, Matching: &fakeMatching{},
		Sessions: &fakeSessions{}, RateLimiter: &fakeRateLimiter{}, Banned: &fakeBanned{},
		Redis: redisClient, Logger: slog.Default(),
		PresenceRegistry: presence.NewRegistry(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(deps, nil, w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	if err := client.Write(ctx, websocket.MessageText, []byte(`{"type":"auth","token":"session-for-`+authID+`"}`)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if frame := readFrame(t, ctx, client); frame["type"] != "auth_ok" {
		t.Fatalf("expected auth_ok, got %+v", frame)
	}

	matchFound := &eventsv1.MatchingEvent{Payload: &eventsv1.MatchingEvent_MatchFound{MatchFound: &eventsv1.MatchFound{
		RoomId: roomID, AccountIdA: authID, AccountIdB: "integration-test-matchfound-partner",
		PseudonymA: "Fox", PseudonymB: "Hound",
	}}}
	matchFoundPayload, err := proto.Marshal(matchFound)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	if err := redisClient.Publish(ctx, "user:"+authID, append([]byte{byte(matchingEventKind)}, matchFoundPayload...)).Err(); err != nil {
		t.Fatalf("Publish MatchFound: %v", err)
	}
	if frame := readFrame(t, ctx, client); frame["type"] != "match_found" || frame["room_id"] != roomID {
		t.Fatalf("expected match_found for %s, got %+v", roomID, frame)
	}

	// No sleep, no retry loop - if subscribeToNewRoom's fix regressed,
	// this message would be published before the subscription actually
	// exists and would simply never arrive, failing this read outright
	// rather than just arriving late.
	messageSent := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_MessageSent{MessageSent: &eventsv1.MessageSent{
		MessageId: "msg-1", RoomId: roomID, SenderAccountId: "integration-test-matchfound-partner",
		SenderPseudonym: "Hound", Body: "hello immediately after match",
	}}}
	messageSentPayload, err := proto.Marshal(messageSent)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	if err := redisClient.Publish(ctx, "room:"+roomID, append([]byte{byte(chatEventKind)}, messageSentPayload...)).Err(); err != nil {
		t.Fatalf("Publish MessageSent: %v", err)
	}
	if frame := readFrame(t, ctx, client); frame["type"] != "message" || frame["body"] != "hello immediately after match" {
		t.Fatalf("expected the message sent right after match_found to arrive immediately, got %+v", frame)
	}
}
