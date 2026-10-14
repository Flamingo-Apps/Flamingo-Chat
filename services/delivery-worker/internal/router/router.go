// Package router decides which Redis channel(s) (docs/ARCHITECTURE.md
// §5.5) a Kafka event should be published to, and is deliberately pure:
// no Kafka, no Redis, just bytes in and channel names out, so it can be
// tested without either.
package router

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
)

// Target is one (channel, payload) pair to PUBLISH. payload is always the
// original, unmodified event bytes - delivery-worker re-publishes
// verbatim rather than inventing a second wire format, so a future
// gateway decodes it with the same proto/events/v1 package that produced
// it (DEC-0043's "no schema registry" reasoning extends naturally here).
type Target struct {
	Channel string
	Payload []byte
}

// RouteChatEvent handles a message from the chat.events topic. Every
// payload type carries a room_id, and every one of them is a "room
// event" in docs/ARCHITECTURE.md §5.5's terms, so they all go to the
// same room:{room_id} channel.
func RouteChatEvent(value []byte) ([]Target, error) {
	var evt eventsv1.ChatEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return nil, fmt.Errorf("router: unmarshal ChatEvent: %w", err)
	}

	var roomID string
	switch p := evt.GetPayload().(type) {
	case *eventsv1.ChatEvent_MessageSent:
		roomID = p.MessageSent.GetRoomId()
	case *eventsv1.ChatEvent_MemberJoined:
		roomID = p.MemberJoined.GetRoomId()
	case *eventsv1.ChatEvent_MemberLeft:
		roomID = p.MemberLeft.GetRoomId()
	case *eventsv1.ChatEvent_KeepVote:
		roomID = p.KeepVote.GetRoomId()
	case *eventsv1.ChatEvent_RoomKept:
		roomID = p.RoomKept.GetRoomId()
	case *eventsv1.ChatEvent_RoomEnded:
		roomID = p.RoomEnded.GetRoomId()
	default:
		return nil, fmt.Errorf("router: unhandled ChatEvent payload %T", evt.GetPayload())
	}
	return []Target{{Channel: "room:" + roomID, Payload: value}}, nil
}

// RouteMatchingEvent handles a message from the matching.events topic.
// MatchFound bootstraps both accounts' gateways onto the new room - they
// aren't subscribed to room:{room_id} yet, since the room didn't exist
// until this event - so it goes to each account's own user:{account_id}
// channel (DEC-0067). MatchEnded, by contrast, happens after both
// gateways are already subscribed to the room from that bootstrap, so it
// is routed like any other room event.
func RouteMatchingEvent(value []byte) ([]Target, error) {
	var evt eventsv1.MatchingEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return nil, fmt.Errorf("router: unmarshal MatchingEvent: %w", err)
	}

	switch p := evt.GetPayload().(type) {
	case *eventsv1.MatchingEvent_MatchFound:
		return []Target{
			{Channel: "user:" + p.MatchFound.GetAccountIdA(), Payload: value},
			{Channel: "user:" + p.MatchFound.GetAccountIdB(), Payload: value},
		}, nil
	case *eventsv1.MatchingEvent_MatchEnded:
		return []Target{{Channel: "room:" + p.MatchEnded.GetRoomId(), Payload: value}}, nil
	default:
		return nil, fmt.Errorf("router: unhandled MatchingEvent payload %T", evt.GetPayload())
	}
}

// RouteModerationEvent handles a message from the moderation.events
// topic. Only AccountBanned has a live-delivery target (the "kicked"
// signal docs/ARCHITECTURE.md §5.5 lists under user:{account_id});
// ReportFiled, UserBlocked and AccountUnbanned have no corresponding
// WebSocket message in §5.2 and are correctly ignored here - they reach
// the admin queue or client-side enforcement some other way, not through
// live delivery (DEC-0067).
func RouteModerationEvent(value []byte) ([]Target, error) {
	var evt eventsv1.ModerationEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return nil, fmt.Errorf("router: unmarshal ModerationEvent: %w", err)
	}

	switch p := evt.GetPayload().(type) {
	case *eventsv1.ModerationEvent_AccountBanned:
		return []Target{{Channel: "user:" + p.AccountBanned.GetAccountId(), Payload: value}}, nil
	case *eventsv1.ModerationEvent_ReportFiled, *eventsv1.ModerationEvent_UserBlocked, *eventsv1.ModerationEvent_AccountUnbanned:
		return nil, nil
	default:
		return nil, fmt.Errorf("router: unhandled ModerationEvent payload %T", evt.GetPayload())
	}
}
