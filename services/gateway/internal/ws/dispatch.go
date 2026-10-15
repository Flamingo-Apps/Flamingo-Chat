package ws

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/grpc/codes"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
)

// pingInterval is how often this package re-syncs room subscriptions
// (conn.go's refreshRooms) while a connection is open - reusing the
// client's own documented 25-second ping cadence (docs/ARCHITECTURE.md
// §5.2) as a convenient, already-expected heartbeat rather than running
// a second independent timer.
const pingInterval = 25 * time.Second

// writeLoop is the only goroutine that calls c.ws.Write, so writes from
// readLoop (direct responses) and redisLoop (broadcasts) never race on
// the connection.
func (c *conn) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-c.send:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Write(writeCtx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				return
			}
			// writeLoop is the only goroutine that calls c.ws.Write, so
			// this is the only place that can be sure a kicked frame
			// actually went out over the wire before closing - closing
			// from redisLoop right after enqueueing it would race with
			// writeLoop still having it queued.
			if isFrameType(frame, "kicked") {
				_ = c.ws.Close(websocket.StatusPolicyViolation, "banned")
				return
			}
		}
	}
}

func isFrameType(frame []byte, typ string) bool {
	var env envelope
	if err := json.Unmarshal(frame, &env); err != nil {
		return false
	}
	return env.Type == typ
}

// redisLoop forwards every message this connection's pubsub receives,
// translated into a WebSocket frame, to writeLoop via enqueue.
func (c *conn) redisLoop(ctx context.Context) {
	ch := c.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg.Channel == presenceChannel {
				c.forwardPresence([]byte(msg.Payload))
				continue
			}
			frame, err := translate([]byte(msg.Payload), c.accountID)
			if err != nil {
				c.logger.Warn("translate", "channel", msg.Channel, "err", err)
				continue
			}
			if frame == nil {
				continue // no client-facing frame for this event - not an error
			}
			c.trackMatchRoom(ctx, frame)
			c.enqueue(frame)
		}
	}
}

// forwardPresence re-wraps the presence channel's plain JSON payload
// (presence's own broadcast.Run - no envelope-kind tag byte, see
// presenceFrame's doc comment) as a client frame and sends it.
func (c *conn) forwardPresence(payload []byte) {
	var counts presenceFrame
	if err := json.Unmarshal(payload, &counts); err != nil {
		c.logger.Warn("unmarshal presence payload", "err", err)
		return
	}
	frame, err := withType("presence", counts)
	if err != nil {
		c.logger.Warn("encode presence frame", "err", err)
		return
	}
	c.enqueue(frame)
}

// readLoop reads and dispatches every client frame after auth.
func (c *conn) readLoop(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refreshRooms(ctx)
			}
		}
	}()

	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		c.dispatch(ctx, data)
	}
}

func (c *conn) dispatch(ctx context.Context, data []byte) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		c.sendError(codes.InvalidArgument, "malformed frame", "")
		return
	}

	switch env.Type {
	case "ping":
		b, _ := withType("pong", pongFrame{})
		c.enqueue(b)
	case "send_message":
		c.handleSendMessage(ctx, data)
	case "keep_chat":
		c.handleKeepChat(ctx, data)
	case "match_request":
		c.handleMatchRequest(ctx, data)
	case "match_cancel":
		c.handleMatchCancel(ctx)
	case "match_next":
		c.handleMatchNext(ctx)
	case "match_end":
		c.handleMatchEnd(ctx)
	default:
		c.sendError(codes.InvalidArgument, "unknown frame type: "+env.Type, "")
	}
}

func (c *conn) handleSendMessage(ctx context.Context, data []byte) {
	var frame sendMessageFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		c.sendError(codes.InvalidArgument, "malformed send_message frame", "")
		return
	}

	// Checked on every send, not just at connect (docs/ARCHITECTURE.md
	// §3.5) - the Redis-pubsub kicked path (translate.go,
	// AccountBanned) already closes a connection banned mid-session, but
	// this catches a message already in flight before that arrives, or a
	// connection that somehow missed it.
	if bannedAccount, err := c.deps.Banned.IsBanned(ctx, c.accountID); err != nil {
		c.sendError(codes.Internal, "banned check failed", frame.ClientMsgID)
		return
	} else if bannedAccount {
		// Refuses the send but doesn't force-close the connection itself -
		// only writeLoop does that (after a kicked frame it just wrote,
		// so it's sure the client saw it first); the actual disconnect
		// for an account banned mid-session comes from the kicked event
		// BanAccount produces, which every connection is already
		// subscribed to receive.
		c.sendError(codes.PermissionDenied, "account is banned", frame.ClientMsgID)
		return
	}

	allowed, err := c.deps.RateLimiter.Allow(ctx, "send_message", c.accountID, 20, 10*time.Second)
	if err != nil {
		c.sendError(codes.Internal, "rate limit check failed", frame.ClientMsgID)
		return
	}
	if !allowed {
		c.sendError(codes.ResourceExhausted, "sending messages too quickly", frame.ClientMsgID)
		return
	}

	resp, err := c.deps.Chat.SendMessage(ctx, &chatv1.SendMessageRequest{
		RoomId: frame.RoomID, SenderAccountId: c.accountID, SenderPseudonym: c.pseudonym,
		Body: frame.Body, ClientMsgId: frame.ClientMsgID,
	})
	if err != nil {
		c.sendErrorFromStatus(err, frame.ClientMsgID)
		return
	}
	b, err := withType("message_ack", messageAckFrame{ClientMsgID: frame.ClientMsgID, MessageID: resp.GetMessageId(), SentAtUnix: resp.GetSentAtUnix()})
	if err != nil {
		return
	}
	c.enqueue(b)
}

func (c *conn) handleKeepChat(ctx context.Context, data []byte) {
	var frame keepChatFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		c.sendError(codes.InvalidArgument, "malformed keep_chat frame", "")
		return
	}
	resp, err := c.deps.Chat.VoteKeep(ctx, &chatv1.VoteKeepRequest{RoomId: frame.RoomID, AccountId: c.accountID})
	if err != nil {
		c.sendErrorFromStatus(err, "")
		return
	}
	// This connection's own vote is reported immediately with you=true;
	// the partner's own vote state isn't known here (VoteKeepResponse
	// only reports whether the room is NOW kept, not each member's
	// individual vote) - see keepStatusFrame's doc comment. If kept is
	// true, both sides have voted, so partner=true follows; if not,
	// partner's status is genuinely unknown from this response alone,
	// so it is left false rather than guessed at.
	b, err := withType("keep_status", keepStatusFrame{RoomID: frame.RoomID, You: true, Partner: resp.GetKept(), Kept: resp.GetKept()})
	if err != nil {
		return
	}
	c.enqueue(b)
}

func (c *conn) handleMatchRequest(ctx context.Context, data []byte) {
	var frame matchRequestFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		c.sendError(codes.InvalidArgument, "malformed match_request frame", "")
		return
	}

	allowed, err := c.deps.RateLimiter.Allow(ctx, "match_request", c.accountID, 30, time.Minute)
	if err != nil {
		c.sendError(codes.Internal, "rate limit check failed", "")
		return
	}
	if !allowed {
		c.sendError(codes.ResourceExhausted, "too many match requests", "")
		return
	}

	accResp, err := c.deps.Identity.GetAccount(ctx, &identityv1.GetAccountRequest{AccountId: c.accountID})
	if err != nil {
		c.sendErrorFromStatus(err, "")
		return
	}
	acc := accResp.GetAccount()

	if _, err := c.deps.Matching.RequestMatch(ctx, &matchingv1.RequestMatchRequest{
		AccountId: c.accountID, Preference: preferenceFromString(frame.Preference),
		Verified: acc.GetGoogleLinked(), Gender: acc.GetGender(), Pseudonym: acc.GetPseudonym(),
	}); err != nil {
		c.sendErrorFromStatus(err, "")
		return
	}
	b, _ := withType("match_queued", matchQueuedFrame{})
	c.enqueue(b)
}

func (c *conn) handleMatchCancel(ctx context.Context) {
	if _, err := c.deps.Matching.CancelMatch(ctx, &matchingv1.CancelMatchRequest{AccountId: c.accountID}); err != nil {
		c.sendErrorFromStatus(err, "")
	}
}

// handleMatchNext mirrors handleMatchRequest's match_queued
// acknowledgement (DEC-0080) - without it, a client that called Next had
// no way to tell "ended and re-queued" apart from "ended and now stuck",
// since match_ended (the room ending) and match_queued (having rejoined
// the queue) are two separate, independent facts.
func (c *conn) handleMatchNext(ctx context.Context) {
	roomID := c.getCurrentMatchRoom()
	resp, err := c.deps.Matching.Next(ctx, &matchingv1.NextRequest{AccountId: c.accountID, CurrentRoomId: roomID})
	if err != nil {
		c.sendErrorFromStatus(err, "")
		return
	}
	if resp.GetQueued() {
		b, _ := withType("match_queued", matchQueuedFrame{})
		c.enqueue(b)
	}
}

// handleMatchEnd ends the connection's current direct room directly
// through Chat, since docs/ARCHITECTURE.md §5.2 gives match_end no
// room_id of its own to act on - see currentMatchRoomID's doc comment.
func (c *conn) handleMatchEnd(ctx context.Context) {
	roomID := c.getCurrentMatchRoom()
	if roomID == "" {
		c.sendError(codes.FailedPrecondition, "no active match to end", "")
		return
	}
	if _, err := c.deps.Chat.EndDirectRoom(ctx, &chatv1.EndDirectRoomRequest{RoomId: roomID, AccountId: c.accountID, Reason: "ended"}); err != nil {
		c.sendErrorFromStatus(err, "")
	}
}

// trackMatchRoom keeps currentMatchRoomID in sync with the match_found/
// match_ended frames this connection's own redisLoop just translated -
// the only source gateway has for "what is my current direct room",
// since match_end's client frame carries no room_id of its own.
func (c *conn) trackMatchRoom(ctx context.Context, frame []byte) {
	var env struct {
		Type   string `json:"type"`
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(frame, &env); err != nil {
		return
	}
	switch env.Type {
	case "match_found":
		c.setCurrentMatchRoom(env.RoomID)
		c.subscribeToNewRoom(ctx, env.RoomID)
	case "match_ended":
		c.setCurrentMatchRoom("")
	}
}

// subscribeToNewRoom subscribes this connection's pubsub to room:{roomID}
// right when match_found names it, instead of waiting for refreshRooms's
// next periodic tick (up to 25 seconds later, DEC-0073) - unlike a room
// joined over HTTP, which has no live signal to hook into, match_found
// is itself a live event this connection just received, so there's no
// reason to wait for one. Like refreshRooms's own subscriptions (and for
// the same reason: this runs on redisLoop, the same goroutine that's
// actively ranging over c.pubsub.Channel(), so calling Receive here to
// wait for Redis's confirmation would race with that read), this does
// not wait for confirmation - a message published in the brief gap
// between this call and Redis actually registering it could still be
// missed, but that window is now milliseconds, not up to 25 seconds.
func (c *conn) subscribeToNewRoom(ctx context.Context, roomID string) {
	c.mu.Lock()
	already := c.subscribed[roomID]
	c.subscribed[roomID] = true
	c.mu.Unlock()
	if already {
		return
	}
	if err := c.pubsub.Subscribe(ctx, "room:"+roomID); err != nil {
		c.logger.Warn("subscribe to new match room", "room_id", roomID, "err", err)
	}
}

func (c *conn) getCurrentMatchRoom() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentMatchRoomID
}

func (c *conn) setCurrentMatchRoom(roomID string) {
	c.mu.Lock()
	c.currentMatchRoomID = roomID
	c.mu.Unlock()
}

func preferenceFromString(s string) matchingv1.Preference {
	switch s {
	case "any":
		return matchingv1.Preference_PREFERENCE_ANY
	case "male":
		return matchingv1.Preference_PREFERENCE_MALE
	case "female":
		return matchingv1.Preference_PREFERENCE_FEMALE
	default:
		return matchingv1.Preference_PREFERENCE_UNSPECIFIED
	}
}
