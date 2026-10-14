package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/ratelimit"
)

// authTimeout is how long a client has to send its first (auth) frame
// before the connection is closed (docs/ARCHITECTURE.md §5.2).
const authTimeout = 5 * time.Second

// touchInterval mirrors identity's own once-an-hour throttle
// (docs/ARCHITECTURE.md §4.3) - TouchAccount is cheap to call more often
// than that since Identity itself no-ops a too-recent call, but there is
// no reason to.
const touchInterval = time.Hour

// sendBufferSize bounds how many outbound frames can be queued for a
// slow client before the connection is dropped rather than letting one
// unresponsive client's buffer grow without limit.
const sendBufferSize = 64

// Deps are every dependency a connection needs, shared across every
// connection a Handler accepts.
type Deps struct {
	Identity    identityv1.IdentityServiceClient
	Chat        chatv1.ChatServiceClient
	Matching    matchingv1.MatchingServiceClient
	Sessions    Sessions
	RateLimiter RateLimiter
	Redis       *redis.Client
	Logger      *slog.Logger
}

// Sessions is the subset of internal/session.Signer this package depends
// on - the same shape internal/httpapi.Sessions declares, duplicated
// rather than shared across these two small, independent packages.
type Sessions interface {
	Issue(accountID string) (string, error)
	Verify(tokenString string) (accountID string, needsRefresh bool, err error)
}

// RateLimiter is the subset of internal/ratelimit.Limiter this package
// depends on.
type RateLimiter interface {
	Allow(ctx context.Context, action, subject string, max int, window time.Duration) (bool, error)
}

var _ RateLimiter = (*ratelimit.Limiter)(nil)

// conn is one WebSocket connection's state, from the auth frame until it
// closes.
type conn struct {
	deps Deps
	ws   *websocket.Conn

	accountID string
	pseudonym string

	pubsub *redis.PubSub
	// subscribed tracks which room:{room_id} channels this connection is
	// currently listening to, so refreshRooms can diff against Chat's
	// current answer instead of unsubscribing and resubscribing
	// everything every time.
	subscribed map[string]bool

	// currentMatchRoomID is the direct room a match_found told this
	// connection about, if any - match_end has no room_id of its own
	// (docs/ARCHITECTURE.md §5.2), so this is what a client's bare
	// match_end acts on. Guarded by mu since it's written from the
	// Redis-subscription goroutine and read from the client-frame one.
	mu                 sync.Mutex
	currentMatchRoomID string
	lastTouch          time.Time

	send   chan []byte
	logger *slog.Logger
}

// Serve upgrades r into a WebSocket connection and runs it until it
// closes. originPatterns is passed straight to coder/websocket's origin
// check (docs/ARCHITECTURE.md §4.4's "WebSocket origin check").
func Serve(deps Deps, originPatterns []string, w http.ResponseWriter, r *http.Request) {
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: originPatterns})
	if err != nil {
		return // Accept has already written the appropriate HTTP response
	}

	c := &conn{
		deps:       deps,
		ws:         wsConn,
		subscribed: map[string]bool{},
		send:       make(chan []byte, sendBufferSize),
		logger:     deps.Logger,
	}
	c.run(r.Context())
}

func (c *conn) run(ctx context.Context) {
	defer func() { _ = c.ws.CloseNow() }()

	if err := c.authenticate(ctx); err != nil {
		c.logger.Info("ws auth failed", "err", err)
		_ = c.ws.Close(websocket.StatusPolicyViolation, "authentication failed")
		return
	}
	defer func() {
		if c.pubsub != nil {
			_ = c.pubsub.Close()
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); defer cancel(); c.writeLoop(ctx) }()
	go func() { defer wg.Done(); defer cancel(); c.redisLoop(ctx) }()
	go func() { defer wg.Done(); defer cancel(); c.readLoop(ctx) }()
	wg.Wait()
}

// authenticate waits for the first client frame (which must be auth)
// within authTimeout, verifies it, fetches the account, subscribes to
// this account's Redis channels, and sends auth_ok.
func (c *conn) authenticate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()

	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return fmt.Errorf("ws: read auth frame: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != "auth" {
		return fmt.Errorf("ws: first frame must be type=auth")
	}
	var frame authFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return fmt.Errorf("ws: malformed auth frame: %w", err)
	}

	accountID, needsRefresh, err := c.deps.Sessions.Verify(frame.Token)
	if err != nil {
		return fmt.Errorf("ws: invalid session token: %w", err)
	}
	c.accountID = accountID

	resp, err := c.deps.Identity.GetAccount(ctx, &identityv1.GetAccountRequest{AccountId: accountID})
	if err != nil {
		return fmt.Errorf("ws: get account: %w", err)
	}
	c.pseudonym = resp.GetAccount().GetPseudonym()

	ok := authOKFrame{Account: toAccountJSON(resp.GetAccount())}
	if needsRefresh {
		token, err := c.deps.Sessions.Issue(accountID)
		if err != nil {
			return fmt.Errorf("ws: refresh session: %w", err)
		}
		ok.Token = token
	}
	// Subscribed (and that subscription actually confirmed by Redis, not
	// just the command written) before auth_ok goes out, so there is no
	// window where the client believes it's listening and isn't yet.
	// Subscribe's own write does not wait for the server's confirmation
	// (verified from go-redis's source: it writes the command and
	// returns), so Receive is what actually blocks for it - safe to call
	// directly here specifically because redisLoop, the only other
	// reader of this pubsub, has not started yet.
	c.pubsub = c.deps.Redis.Subscribe(ctx, "user:"+accountID)
	if _, err := c.pubsub.Receive(ctx); err != nil {
		return fmt.Errorf("ws: confirm subscription: %w", err)
	}
	c.subscribed = map[string]bool{}

	frameBytes, err := withType("auth_ok", ok)
	if err != nil {
		return fmt.Errorf("ws: encode auth_ok: %w", err)
	}
	if err := c.ws.Write(ctx, websocket.MessageText, frameBytes); err != nil {
		return fmt.Errorf("ws: write auth_ok: %w", err)
	}

	c.touch(ctx)
	c.refreshRooms(ctx)
	return nil
}

// touch calls Identity.TouchAccount, skipping the call entirely between
// connections within touchInterval - a connection that reconnects often
// (docs/ARCHITECTURE.md §5.2's reconnect-with-backoff) shouldn't turn
// into a TouchAccount call every time, even though Identity itself would
// also throttle the actual write.
func (c *conn) touch(ctx context.Context) {
	c.mu.Lock()
	stale := time.Since(c.lastTouch) >= touchInterval
	if stale {
		c.lastTouch = time.Now()
	}
	c.mu.Unlock()
	if !stale {
		return
	}
	if _, err := c.deps.Identity.TouchAccount(ctx, &identityv1.TouchAccountRequest{AccountId: c.accountID}); err != nil {
		c.logger.Warn("touch account", "account_id", c.accountID, "err", err)
	}
}

// refreshRooms re-subscribes to every room Chat currently says this
// account is in. Called once at connect and then periodically
// (readLoop's ping ticker) - a room joined over HTTP while this
// connection is already open has no dedicated WebSocket notification of
// its own (docs/ARCHITECTURE.md §5.2 defines no such client frame), so
// this polling is what picks it up (DEC-0071).
//
// Unlike the initial user:{account_id} subscription in authenticate,
// this does not wait for Redis to confirm a new room subscription before
// returning - redisLoop is already running and reading this same pubsub
// by the time this is called again, so blocking here on Receive would
// race with it for the next message on the connection. A room event
// published in the brief window before the subscription is actually
// processed could be missed; accepted since it is a rare, narrow window
// and the room's actual state is still correct via Chat's own API on the
// client's next request - nothing is lost, only a live notification
// might be.
func (c *conn) refreshRooms(ctx context.Context) {
	resp, err := c.deps.Chat.ListMyRooms(ctx, &chatv1.ListMyRoomsRequest{AccountId: c.accountID})
	if err != nil {
		c.logger.Warn("list my rooms", "account_id", c.accountID, "err", err)
		return
	}

	current := make(map[string]bool, len(resp.GetRooms()))
	for _, room := range resp.GetRooms() {
		current[room.GetId()] = true
	}

	c.mu.Lock()
	var toSubscribe, toUnsubscribe []string
	for id := range current {
		if !c.subscribed[id] {
			toSubscribe = append(toSubscribe, "room:"+id)
		}
	}
	for id := range c.subscribed {
		if !current[id] {
			toUnsubscribe = append(toUnsubscribe, "room:"+id)
		}
	}
	c.subscribed = make(map[string]bool, len(current))
	for id := range current {
		c.subscribed[id] = true
	}
	c.mu.Unlock()

	if len(toSubscribe) > 0 {
		if err := c.pubsub.Subscribe(ctx, toSubscribe...); err != nil {
			c.logger.Warn("subscribe", "channels", toSubscribe, "err", err)
		}
	}
	if len(toUnsubscribe) > 0 {
		if err := c.pubsub.Unsubscribe(ctx, toUnsubscribe...); err != nil {
			c.logger.Warn("unsubscribe", "channels", toUnsubscribe, "err", err)
		}
	}
}

// enqueue queues a frame for writeLoop, dropping the connection rather
// than blocking forever if the client isn't reading fast enough.
func (c *conn) enqueue(frame []byte) {
	select {
	case c.send <- frame:
	default:
		c.logger.Warn("send buffer full, closing connection", "account_id", c.accountID)
		_ = c.ws.Close(websocket.StatusPolicyViolation, "too slow")
	}
}

func (c *conn) sendError(code codes.Code, message, clientMsgID string) {
	b, err := withType("error", errorFrame{Code: strings.ToLower(code.String()), Message: message, ClientMsgID: clientMsgID})
	if err != nil {
		return
	}
	c.enqueue(b)
}

func (c *conn) sendErrorFromStatus(err error, clientMsgID string) {
	st, _ := status.FromError(err)
	c.sendError(st.Code(), st.Message(), clientMsgID)
}
