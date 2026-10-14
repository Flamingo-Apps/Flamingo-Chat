package decode

import (
	"testing"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
)

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	return b
}

func TestDecodeChatEventMessageSentIncludesAMessage(t *testing.T) {
	event := &eventsv1.ChatEvent{
		EventId: "evt-1",
		Payload: &eventsv1.ChatEvent_MessageSent{
			MessageSent: &eventsv1.MessageSent{
				MessageId: "msg-1", RoomId: "room-1", SenderAccountId: "a",
				SenderPseudonym: "Fox", Body: "hi", SentAtUnix: 100,
			},
		},
	}
	d, err := DecodeChatEvent(mustMarshal(t, event))
	if err != nil {
		t.Fatalf("DecodeChatEvent: %v", err)
	}
	if d.EventID != "evt-1" || d.EventKind != "MessageSent" {
		t.Fatalf("unexpected decoded envelope: %+v", d)
	}
	if d.Message == nil || d.Message.MessageID != "msg-1" || d.Message.Body != "hi" {
		t.Fatalf("expected a populated Message, got %+v", d.Message)
	}
}

func TestDecodeChatEventOtherKindsHaveNoMessage(t *testing.T) {
	cases := []*eventsv1.ChatEvent{
		{EventId: "e", Payload: &eventsv1.ChatEvent_MemberJoined{MemberJoined: &eventsv1.MemberJoined{RoomId: "r"}}},
		{EventId: "e", Payload: &eventsv1.ChatEvent_MemberLeft{MemberLeft: &eventsv1.MemberLeft{RoomId: "r"}}},
		{EventId: "e", Payload: &eventsv1.ChatEvent_KeepVote{KeepVote: &eventsv1.KeepVote{RoomId: "r"}}},
		{EventId: "e", Payload: &eventsv1.ChatEvent_RoomKept{RoomKept: &eventsv1.RoomKept{RoomId: "r"}}},
		{EventId: "e", Payload: &eventsv1.ChatEvent_RoomEnded{RoomEnded: &eventsv1.RoomEnded{RoomId: "r"}}},
	}
	wantKinds := []string{"MemberJoined", "MemberLeft", "KeepVote", "RoomKept", "RoomEnded"}
	for i, event := range cases {
		d, err := DecodeChatEvent(mustMarshal(t, event))
		if err != nil {
			t.Fatalf("DecodeChatEvent: %v", err)
		}
		if d.EventKind != wantKinds[i] {
			t.Fatalf("expected kind %s, got %s", wantKinds[i], d.EventKind)
		}
		if d.Message != nil {
			t.Fatalf("expected no Message for %s, got %+v", wantKinds[i], d.Message)
		}
	}
}

func TestDecodeChatEventRejectsUndecodable(t *testing.T) {
	if _, err := DecodeChatEvent([]byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatalf("expected an error for undecodable bytes")
	}
}

func TestDecodeMatchingEventKinds(t *testing.T) {
	cases := []*eventsv1.MatchingEvent{
		{EventId: "e", Payload: &eventsv1.MatchingEvent_MatchFound{MatchFound: &eventsv1.MatchFound{RoomId: "r"}}},
		{EventId: "e", Payload: &eventsv1.MatchingEvent_MatchEnded{MatchEnded: &eventsv1.MatchEnded{RoomId: "r"}}},
	}
	wantKinds := []string{"MatchFound", "MatchEnded"}
	for i, event := range cases {
		d, err := DecodeMatchingEvent(mustMarshal(t, event))
		if err != nil {
			t.Fatalf("DecodeMatchingEvent: %v", err)
		}
		if d.EventKind != wantKinds[i] {
			t.Fatalf("expected kind %s, got %s", wantKinds[i], d.EventKind)
		}
	}
}

func TestDecodeModerationEventKinds(t *testing.T) {
	cases := []*eventsv1.ModerationEvent{
		{EventId: "e", Payload: &eventsv1.ModerationEvent_ReportFiled{ReportFiled: &eventsv1.ReportFiled{ReportId: "r"}}},
		{EventId: "e", Payload: &eventsv1.ModerationEvent_UserBlocked{UserBlocked: &eventsv1.UserBlocked{AccountId: "a"}}},
		{EventId: "e", Payload: &eventsv1.ModerationEvent_AccountBanned{AccountBanned: &eventsv1.AccountBanned{AccountId: "a"}}},
		{EventId: "e", Payload: &eventsv1.ModerationEvent_AccountUnbanned{AccountUnbanned: &eventsv1.AccountUnbanned{AccountId: "a"}}},
	}
	wantKinds := []string{"ReportFiled", "UserBlocked", "AccountBanned", "AccountUnbanned"}
	for i, event := range cases {
		d, err := DecodeModerationEvent(mustMarshal(t, event))
		if err != nil {
			t.Fatalf("DecodeModerationEvent: %v", err)
		}
		if d.EventKind != wantKinds[i] {
			t.Fatalf("expected kind %s, got %s", wantKinds[i], d.EventKind)
		}
	}
}
