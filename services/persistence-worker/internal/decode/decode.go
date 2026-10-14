// Package decode is the pure (no I/O) half of persistence-worker's
// consumer: it unmarshals one topic's envelope and pulls out what
// internal/store needs to write, so that logic can be tested without
// Kafka or Postgres.
package decode

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
)

// Message is the structured form of a chat.events MessageSent payload -
// the one event kind that also needs a row in Chat's own messages table,
// not just an analytics log entry.
type Message struct {
	MessageID       string
	RoomID          string
	SenderAccountID string
	SenderPseudonym string
	Body            string
	SentAt          time.Time
}

// Decoded is what every event kind, across all three topics, reduces to.
type Decoded struct {
	EventID    string
	EventKind  string
	OccurredAt time.Time
	// Message is non-nil only for a chat.events MessageSent.
	Message *Message
}

// DecodeChatEvent handles a message from the chat.events topic.
func DecodeChatEvent(value []byte) (Decoded, error) {
	var evt eventsv1.ChatEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return Decoded{}, fmt.Errorf("decode: unmarshal ChatEvent: %w", err)
	}
	d := Decoded{EventID: evt.GetEventId(), OccurredAt: time.Unix(evt.GetOccurredAtUnix(), 0)}

	switch p := evt.GetPayload().(type) {
	case *eventsv1.ChatEvent_MessageSent:
		d.EventKind = "MessageSent"
		d.Message = &Message{
			MessageID:       p.MessageSent.GetMessageId(),
			RoomID:          p.MessageSent.GetRoomId(),
			SenderAccountID: p.MessageSent.GetSenderAccountId(),
			SenderPseudonym: p.MessageSent.GetSenderPseudonym(),
			Body:            p.MessageSent.GetBody(),
			SentAt:          time.Unix(p.MessageSent.GetSentAtUnix(), 0),
		}
	case *eventsv1.ChatEvent_MemberJoined:
		d.EventKind = "MemberJoined"
	case *eventsv1.ChatEvent_MemberLeft:
		d.EventKind = "MemberLeft"
	case *eventsv1.ChatEvent_KeepVote:
		d.EventKind = "KeepVote"
	case *eventsv1.ChatEvent_RoomKept:
		d.EventKind = "RoomKept"
	case *eventsv1.ChatEvent_RoomEnded:
		d.EventKind = "RoomEnded"
	default:
		return Decoded{}, fmt.Errorf("decode: unhandled ChatEvent payload %T", evt.GetPayload())
	}
	return d, nil
}

// DecodeMatchingEvent handles a message from the matching.events topic.
func DecodeMatchingEvent(value []byte) (Decoded, error) {
	var evt eventsv1.MatchingEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return Decoded{}, fmt.Errorf("decode: unmarshal MatchingEvent: %w", err)
	}
	d := Decoded{EventID: evt.GetEventId(), OccurredAt: time.Unix(evt.GetOccurredAtUnix(), 0)}

	switch evt.GetPayload().(type) {
	case *eventsv1.MatchingEvent_MatchFound:
		d.EventKind = "MatchFound"
	case *eventsv1.MatchingEvent_MatchEnded:
		d.EventKind = "MatchEnded"
	default:
		return Decoded{}, fmt.Errorf("decode: unhandled MatchingEvent payload %T", evt.GetPayload())
	}
	return d, nil
}

// DecodeModerationEvent handles a message from the moderation.events
// topic. Unlike delivery-worker's router, every moderation event kind is
// logged here - the analytics trail cares about all of them, even the
// ones with no live-delivery target.
func DecodeModerationEvent(value []byte) (Decoded, error) {
	var evt eventsv1.ModerationEvent
	if err := proto.Unmarshal(value, &evt); err != nil {
		return Decoded{}, fmt.Errorf("decode: unmarshal ModerationEvent: %w", err)
	}
	d := Decoded{EventID: evt.GetEventId(), OccurredAt: time.Unix(evt.GetOccurredAtUnix(), 0)}

	switch evt.GetPayload().(type) {
	case *eventsv1.ModerationEvent_ReportFiled:
		d.EventKind = "ReportFiled"
	case *eventsv1.ModerationEvent_UserBlocked:
		d.EventKind = "UserBlocked"
	case *eventsv1.ModerationEvent_AccountBanned:
		d.EventKind = "AccountBanned"
	case *eventsv1.ModerationEvent_AccountUnbanned:
		d.EventKind = "AccountUnbanned"
	default:
		return Decoded{}, fmt.Errorf("decode: unhandled ModerationEvent payload %T", evt.GetPayload())
	}
	return d, nil
}
