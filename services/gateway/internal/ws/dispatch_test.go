package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
)

// newTestConn builds a conn with fakes, bypassing Serve/authenticate -
// these tests exercise dispatch.go's handlers directly, reading
// whatever they enqueue off c.send instead of a real WebSocket.
func newTestConn(identity *fakeIdentity, chat *fakeChat, matching *fakeMatching) *conn {
	return &conn{
		deps: Deps{
			Identity: identity, Chat: chat, Matching: matching,
			Sessions: &fakeSessions{}, RateLimiter: &fakeRateLimiter{}, Banned: &fakeBanned{}, Logger: slog.Default(),
		},
		accountID:  "acc-1",
		pseudonym:  "Fox",
		send:       make(chan []byte, sendBufferSize),
		subscribed: map[string]bool{},
		logger:     slog.Default(),
	}
}

func (c *conn) nextFrame(t *testing.T) map[string]any {
	t.Helper()
	select {
	case b := <-c.send:
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("unmarshal %q: %v", b, err)
		}
		return m
	default:
		t.Fatalf("expected a frame to be enqueued, got none")
		return nil
	}
}

func TestDispatchPing(t *testing.T) {
	c := newTestConn(&fakeIdentity{}, &fakeChat{}, &fakeMatching{})
	c.dispatch(context.Background(), []byte(`{"type":"ping"}`))
	if frame := c.nextFrame(t); frame["type"] != "pong" {
		t.Fatalf("expected pong, got %+v", frame)
	}
}

func TestDispatchUnknownType(t *testing.T) {
	c := newTestConn(&fakeIdentity{}, &fakeChat{}, &fakeMatching{})
	c.dispatch(context.Background(), []byte(`{"type":"not_a_real_type"}`))
	frame := c.nextFrame(t)
	if frame["type"] != "error" {
		t.Fatalf("expected an error frame for an unknown type, got %+v", frame)
	}
}

func TestHandleSendMessageSuccess(t *testing.T) {
	chat := &fakeChat{sendMessage: func(_ context.Context, req *chatv1.SendMessageRequest) (*chatv1.SendMessageResponse, error) {
		if req.GetSenderAccountId() != "acc-1" || req.GetSenderPseudonym() != "Fox" || req.GetBody() != "hi" {
			t.Fatalf("unexpected request: %+v", req)
		}
		return &chatv1.SendMessageResponse{MessageId: "msg-1", SentAtUnix: 100}, nil
	}}
	c := newTestConn(&fakeIdentity{}, chat, &fakeMatching{})
	c.dispatch(context.Background(), []byte(`{"type":"send_message","room_id":"r1","client_msg_id":"c1","body":"hi"}`))

	frame := c.nextFrame(t)
	if frame["type"] != "message_ack" || frame["message_id"] != "msg-1" || frame["client_msg_id"] != "c1" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestHandleSendMessagePropagatesAGRPCError(t *testing.T) {
	chat := &fakeChat{sendMessage: func(context.Context, *chatv1.SendMessageRequest) (*chatv1.SendMessageResponse, error) {
		return nil, errNotConfigured // any gRPC status error
	}}
	c := newTestConn(&fakeIdentity{}, chat, &fakeMatching{})
	c.dispatch(context.Background(), []byte(`{"type":"send_message","room_id":"r1","client_msg_id":"c1","body":"hi"}`))

	frame := c.nextFrame(t)
	if frame["type"] != "error" || frame["client_msg_id"] != "c1" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestHandleSendMessageRejectsBannedAccount(t *testing.T) {
	chat := &fakeChat{sendMessage: func(context.Context, *chatv1.SendMessageRequest) (*chatv1.SendMessageResponse, error) {
		t.Fatal("Chat.SendMessage should not be called for a banned account")
		return nil, nil
	}}
	c := newTestConn(&fakeIdentity{}, chat, &fakeMatching{})
	c.deps.Banned = &fakeBanned{banned: true}
	c.dispatch(context.Background(), []byte(`{"type":"send_message","room_id":"r1","client_msg_id":"c1","body":"hi"}`))

	frame := c.nextFrame(t)
	if frame["type"] != "error" || frame["code"] != "permissiondenied" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestHandleKeepChatReportsYouTrueAlways(t *testing.T) {
	chat := &fakeChat{voteKeep: func(context.Context, *chatv1.VoteKeepRequest) (*chatv1.VoteKeepResponse, error) {
		return &chatv1.VoteKeepResponse{Kept: false}, nil
	}}
	c := newTestConn(&fakeIdentity{}, chat, &fakeMatching{})
	c.dispatch(context.Background(), []byte(`{"type":"keep_chat","room_id":"r1"}`))

	frame := c.nextFrame(t)
	if frame["type"] != "keep_status" || frame["you"] != true || frame["kept"] != false {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestHandleMatchRequestUsesTheAccountsOwnAttributes(t *testing.T) {
	identity := &fakeIdentity{getAccount: func(_ context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
		return &identityv1.GetAccountResponse{Account: &identityv1.Account{
			Id: req.GetAccountId(), Pseudonym: "Fox", Gender: identityv1.Gender_GENDER_MALE, GoogleLinked: true,
		}}, nil
	}}
	matching := &fakeMatching{requestMatch: func(_ context.Context, req *matchingv1.RequestMatchRequest) (*matchingv1.RequestMatchResponse, error) {
		if req.GetAccountId() != "acc-1" || req.GetPseudonym() != "Fox" || !req.GetVerified() || req.GetGender() != identityv1.Gender_GENDER_MALE {
			t.Fatalf("unexpected request: %+v", req)
		}
		if req.GetPreference() != matchingv1.Preference_PREFERENCE_FEMALE {
			t.Fatalf("expected the requested preference to be passed through, got %v", req.GetPreference())
		}
		return &matchingv1.RequestMatchResponse{Queued: true}, nil
	}}
	c := newTestConn(identity, &fakeChat{}, matching)
	c.dispatch(context.Background(), []byte(`{"type":"match_request","preference":"female"}`))

	frame := c.nextFrame(t)
	if frame["type"] != "match_queued" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestHandleMatchCancelAndNextAndEnd(t *testing.T) {
	var gotCancel, gotNext bool
	matching := &fakeMatching{
		cancelMatch: func(context.Context, *matchingv1.CancelMatchRequest) (*matchingv1.CancelMatchResponse, error) {
			gotCancel = true
			return &matchingv1.CancelMatchResponse{Cancelled: true}, nil
		},
		next: func(_ context.Context, req *matchingv1.NextRequest) (*matchingv1.NextResponse, error) {
			gotNext = true
			if req.GetCurrentRoomId() != "room-current" {
				t.Fatalf("expected the tracked current match room, got %q", req.GetCurrentRoomId())
			}
			return &matchingv1.NextResponse{Queued: true}, nil
		},
	}
	c := newTestConn(&fakeIdentity{}, &fakeChat{}, matching)

	c.dispatch(context.Background(), []byte(`{"type":"match_cancel"}`))
	if !gotCancel {
		t.Fatalf("expected CancelMatch to be called")
	}

	c.setCurrentMatchRoom("room-current")
	c.dispatch(context.Background(), []byte(`{"type":"match_next"}`))
	if !gotNext {
		t.Fatalf("expected Next to be called")
	}
	frame := c.nextFrame(t)
	if frame["type"] != "match_queued" {
		t.Fatalf("expected a match_queued frame after a successful Next, got %+v", frame)
	}
}

// TestHandleMatchNextSendsNoFrameWhenNotReQueued covers DEC-0080 and
// DEC-0078's graceful degradation together: when Matching has nothing
// cached to re-enqueue the caller with, Next still ends the room but
// reports Queued: false - the client already learns the room ended from
// the room's own match_ended broadcast, so nothing more is sent here.
func TestHandleMatchNextSendsNoFrameWhenNotReQueued(t *testing.T) {
	matching := &fakeMatching{
		next: func(context.Context, *matchingv1.NextRequest) (*matchingv1.NextResponse, error) {
			return &matchingv1.NextResponse{Queued: false}, nil
		},
	}
	c := newTestConn(&fakeIdentity{}, &fakeChat{}, matching)
	c.setCurrentMatchRoom("room-current")

	c.dispatch(context.Background(), []byte(`{"type":"match_next"}`))
	select {
	case b := <-c.send:
		t.Fatalf("expected no frame to be sent, got %q", b)
	default:
	}
}

func TestHandleMatchEndWithNoCurrentMatchReturnsAnError(t *testing.T) {
	c := newTestConn(&fakeIdentity{}, &fakeChat{}, &fakeMatching{})
	c.dispatch(context.Background(), []byte(`{"type":"match_end"}`))
	frame := c.nextFrame(t)
	if frame["type"] != "error" {
		t.Fatalf("expected an error frame when there is no current match, got %+v", frame)
	}
}

func TestHandleMatchEndUsesTheTrackedRoom(t *testing.T) {
	var gotRoomID string
	chat := &fakeChat{endDirectRoom: func(_ context.Context, req *chatv1.EndDirectRoomRequest) (*chatv1.EndDirectRoomResponse, error) {
		gotRoomID = req.GetRoomId()
		return &chatv1.EndDirectRoomResponse{}, nil
	}}
	c := newTestConn(&fakeIdentity{}, chat, &fakeMatching{})
	c.setCurrentMatchRoom("room-current")

	c.dispatch(context.Background(), []byte(`{"type":"match_end"}`))
	if gotRoomID != "room-current" {
		t.Fatalf("expected EndDirectRoom to be called with the tracked room, got %q", gotRoomID)
	}
}

func TestTrackMatchRoomUpdatesOnFoundAndClearsOnEnded(t *testing.T) {
	c := newTestConn(&fakeIdentity{}, &fakeChat{}, &fakeMatching{})

	found, _ := withType("match_found", matchFoundFrame{RoomID: "room-1"})
	c.trackMatchRoom(found)
	if got := c.getCurrentMatchRoom(); got != "room-1" {
		t.Fatalf("expected room-1, got %q", got)
	}

	ended, _ := withType("match_ended", matchEndedFrame{RoomID: "room-1"})
	c.trackMatchRoom(ended)
	if got := c.getCurrentMatchRoom(); got != "" {
		t.Fatalf("expected the tracked room to be cleared, got %q", got)
	}
}
