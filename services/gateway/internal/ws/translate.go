package ws

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
)

// envelopeKind mirrors services/delivery-worker/internal/router's
// EnvelopeKind - the two can't share a Go constant across module
// boundaries, so both implement this convention against
// docs/ARCHITECTURE.md §5.5 instead (DEC-0070, INC-0009).
type envelopeKind byte

const (
	chatEventKind       envelopeKind = 1
	matchingEventKind   envelopeKind = 2
	moderationEventKind envelopeKind = 3
)

// translate turns one message received over a room:{room_id} or
// user:{account_id} Redis channel into the WebSocket frame bytes to send
// to a client subscribed to it, or (nil, nil) when the event has no
// corresponding client-facing frame. myAccountID is the connection's own
// account id, needed to work out "partner" framing for events that name
// both sides of a match.
func translate(payload []byte, myAccountID string) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("ws: empty payload")
	}
	kind, body := envelopeKind(payload[0]), payload[1:]

	switch kind {
	case chatEventKind:
		return translateChatEvent(body, myAccountID)
	case matchingEventKind:
		return translateMatchingEvent(body, myAccountID)
	case moderationEventKind:
		return translateModerationEvent(body)
	default:
		return nil, fmt.Errorf("ws: unknown envelope kind %d", kind)
	}
}

func translateChatEvent(value []byte, myAccountID string) ([]byte, error) {
	var evt eventsv1.ChatEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return nil, fmt.Errorf("ws: unmarshal ChatEvent: %w", err)
	}

	switch p := evt.GetPayload().(type) {
	case *eventsv1.ChatEvent_MessageSent:
		// ClientMsgID is deliberately left empty here - the Kafka event
		// has no such field. The sender already reconciled their own
		// optimistic UI through the synchronous message_ack SendMessage
		// returned; this broadcast is what every room member (including
		// the sender, who also receives their own message back over this
		// same path) renders it from.
		return withType("message", messageFrame{
			MessageID:       p.MessageSent.GetMessageId(),
			RoomID:          p.MessageSent.GetRoomId(),
			SenderAccountID: p.MessageSent.GetSenderAccountId(),
			SenderPseudonym: p.MessageSent.GetSenderPseudonym(),
			Body:            p.MessageSent.GetBody(),
			SentAtUnix:      p.MessageSent.GetSentAtUnix(),
		})
	case *eventsv1.ChatEvent_MemberJoined:
		return withType("room_joined", roomJoinedFrame{RoomID: p.MemberJoined.GetRoomId()})
	case *eventsv1.ChatEvent_MemberLeft:
		return withType("room_left", roomLeftFrame{RoomID: p.MemberLeft.GetRoomId()})
	case *eventsv1.ChatEvent_RoomKept:
		// Who voted and when isn't carried by this broadcast - only that
		// the room is now kept. The voter's own You/Partner framing comes
		// from VoteKeep's synchronous response instead (see httpapi and
		// this package's conn.go); every other member just learns the
		// room survived.
		return withType("keep_status", keepStatusFrame{RoomID: p.RoomKept.GetRoomId(), Kept: true})
	case *eventsv1.ChatEvent_RoomEnded:
		// Reason here deliberately carries you_left/partner_left, not the
		// free-text value EndDirectRoom's caller originally passed
		// ("ended"/"next") - the frontend's grace-window prompt (only
		// shown to whoever did NOT end it themselves) is the one thing
		// that reads this field, mirroring MatchEnded's own
		// you_left/partner_left framing below for the same reason.
		reason := "partner_left"
		if p.RoomEnded.GetEndedByAccountId() == myAccountID {
			reason = "you_left"
		}
		return withType("match_ended", matchEndedFrame{RoomID: p.RoomEnded.GetRoomId(), Reason: reason})
	case *eventsv1.ChatEvent_KeepVote:
		// The other member hears that someone asked to keep the chat, so
		// the web app can offer "save it or carry on" (DEC-0106). The
		// voter already knows (their own VoteKeep response), and the vote
		// that completes a mutual keep is announced by RoomKept instead.
		if p.KeepVote.GetAccountId() == myAccountID || p.KeepVote.GetKept() {
			return nil, nil
		}
		return withType("keep_requested", keepRequestedFrame{RoomID: p.KeepVote.GetRoomId()})
	default:
		return nil, fmt.Errorf("ws: unhandled ChatEvent payload %T", evt.GetPayload())
	}
}

func translateMatchingEvent(value []byte, myAccountID string) ([]byte, error) {
	var evt eventsv1.MatchingEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return nil, fmt.Errorf("ws: unmarshal MatchingEvent: %w", err)
	}

	switch p := evt.GetPayload().(type) {
	case *eventsv1.MatchingEvent_MatchFound:
		mf := p.MatchFound
		partnerID, partnerPseudonym := mf.GetAccountIdB(), mf.GetPseudonymB()
		if mf.GetAccountIdA() != myAccountID {
			partnerID, partnerPseudonym = mf.GetAccountIdA(), mf.GetPseudonymA()
		}
		return withType("match_found", matchFoundFrame{
			RoomID: mf.GetRoomId(), PartnerAccountID: partnerID, PartnerPseudonym: partnerPseudonym,
		})
	case *eventsv1.MatchingEvent_MatchEnded:
		me := p.MatchEnded
		reason := "partner_left"
		if me.GetAccountId() == myAccountID {
			reason = "you_left"
		}
		return withType("match_ended", matchEndedFrame{RoomID: me.GetRoomId(), Reason: reason})
	default:
		return nil, fmt.Errorf("ws: unhandled MatchingEvent payload %T", evt.GetPayload())
	}
}

func translateModerationEvent(value []byte) ([]byte, error) {
	var evt eventsv1.ModerationEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return nil, fmt.Errorf("ws: unmarshal ModerationEvent: %w", err)
	}

	switch p := evt.GetPayload().(type) {
	case *eventsv1.ModerationEvent_AccountBanned:
		return withType("kicked", kickedFrame{Reason: p.AccountBanned.GetReason()})
	default:
		// ReportFiled, UserBlocked, AccountUnbanned never reach this
		// channel (router.RouteModerationEvent gives them no target), so
		// seeing one here would mean the two sides have drifted.
		return nil, fmt.Errorf("ws: unhandled ModerationEvent payload %T", evt.GetPayload())
	}
}
