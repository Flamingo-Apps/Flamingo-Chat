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
		}
	}
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
			frame, err := translate([]byte(msg.Payload), c.accountID)
			if err != nil {
				c.logger.Warn("translate", "channel", msg.Channel, "err", err)
				continue
			}
			if frame == nil {
				continue // no client-facing frame for this event - not an error
			}
			c.trackMatchRoom(frame)
			c.enqueue(frame)
		}
	}
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
func (c *conn) trackMatchRoom(frame []byte) {
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
	case "match_ended":
		c.setCurrentMatchRoom("")
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
