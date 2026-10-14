package router

import (
	"bytes"
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

// wantTagged is what every Target.Payload must look like: the
// EnvelopeKind tag byte, then the original bytes unchanged.
func wantTagged(kind EnvelopeKind, value []byte) []byte {
	return append([]byte{byte(kind)}, value...)
}

func TestRouteChatEventGoesToItsRoom(t *testing.T) {
	cases := []struct {
		name  string
		event *eventsv1.ChatEvent
	}{
		{"MessageSent", &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_MessageSent{MessageSent: &eventsv1.MessageSent{RoomId: "room-1"}}}},
		{"MemberJoined", &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_MemberJoined{MemberJoined: &eventsv1.MemberJoined{RoomId: "room-1"}}}},
		{"MemberLeft", &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_MemberLeft{MemberLeft: &eventsv1.MemberLeft{RoomId: "room-1"}}}},
		{"KeepVote", &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_KeepVote{KeepVote: &eventsv1.KeepVote{RoomId: "room-1"}}}},
		{"RoomKept", &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_RoomKept{RoomKept: &eventsv1.RoomKept{RoomId: "room-1"}}}},
		{"RoomEnded", &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_RoomEnded{RoomEnded: &eventsv1.RoomEnded{RoomId: "room-1"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value := mustMarshal(t, c.event)
			targets, err := RouteChatEvent(value)
			if err != nil {
				t.Fatalf("RouteChatEvent: %v", err)
			}
			if len(targets) != 1 || targets[0].Channel != "room:room-1" {
				t.Fatalf("unexpected targets: %+v", targets)
			}
			if !bytes.Equal(targets[0].Payload, wantTagged(ChatEventKind, value)) {
				t.Fatalf("expected the payload to be the ChatEventKind tag byte plus the original bytes unchanged")
			}
		})
	}
}

func TestRouteChatEventRejectsUndecodable(t *testing.T) {
	if _, err := RouteChatEvent([]byte("not a protobuf message")); err == nil {
		t.Fatalf("expected an error for undecodable bytes")
	}
}

func TestRouteMatchingEventMatchFoundGoesToBothAccounts(t *testing.T) {
	event := &eventsv1.MatchingEvent{
		Payload: &eventsv1.MatchingEvent_MatchFound{
			MatchFound: &eventsv1.MatchFound{RoomId: "room-1", AccountIdA: "a", AccountIdB: "b"},
		},
	}
	value := mustMarshal(t, event)
	targets, err := RouteMatchingEvent(value)
	if err != nil {
		t.Fatalf("RouteMatchingEvent: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d: %+v", len(targets), targets)
	}
	channels := map[string]bool{targets[0].Channel: true, targets[1].Channel: true}
	if !channels["user:a"] || !channels["user:b"] {
		t.Fatalf("expected user:a and user:b, got %+v", targets)
	}
	for _, target := range targets {
		if !bytes.Equal(target.Payload, wantTagged(MatchingEventKind, value)) {
			t.Fatalf("expected the payload to be the MatchingEventKind tag byte plus the original bytes unchanged")
		}
	}
}

func TestRouteMatchingEventMatchEndedGoesToTheRoom(t *testing.T) {
	event := &eventsv1.MatchingEvent{
		Payload: &eventsv1.MatchingEvent_MatchEnded{
			MatchEnded: &eventsv1.MatchEnded{RoomId: "room-1", AccountId: "a"},
		},
	}
	value := mustMarshal(t, event)
	targets, err := RouteMatchingEvent(value)
	if err != nil {
		t.Fatalf("RouteMatchingEvent: %v", err)
	}
	if len(targets) != 1 || targets[0].Channel != "room:room-1" {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	if !bytes.Equal(targets[0].Payload, wantTagged(MatchingEventKind, value)) {
		t.Fatalf("expected the payload to be the MatchingEventKind tag byte plus the original bytes unchanged")
	}
}

func TestRouteModerationEventAccountBannedGoesToTheAccount(t *testing.T) {
	event := &eventsv1.ModerationEvent{
		Payload: &eventsv1.ModerationEvent_AccountBanned{
			AccountBanned: &eventsv1.AccountBanned{AccountId: "a", Reason: "spam"},
		},
	}
	value := mustMarshal(t, event)
	targets, err := RouteModerationEvent(value)
	if err != nil {
		t.Fatalf("RouteModerationEvent: %v", err)
	}
	if len(targets) != 1 || targets[0].Channel != "user:a" {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	if !bytes.Equal(targets[0].Payload, wantTagged(ModerationEventKind, value)) {
		t.Fatalf("expected the payload to be the ModerationEventKind tag byte plus the original bytes unchanged")
	}
}

func TestRouteModerationEventOtherKindsHaveNoLiveTarget(t *testing.T) {
	cases := []*eventsv1.ModerationEvent{
		{Payload: &eventsv1.ModerationEvent_ReportFiled{ReportFiled: &eventsv1.ReportFiled{ReportId: "r1"}}},
		{Payload: &eventsv1.ModerationEvent_UserBlocked{UserBlocked: &eventsv1.UserBlocked{AccountId: "a"}}},
		{Payload: &eventsv1.ModerationEvent_AccountUnbanned{AccountUnbanned: &eventsv1.AccountUnbanned{AccountId: "a"}}},
	}
	for _, event := range cases {
		targets, err := RouteModerationEvent(mustMarshal(t, event))
		if err != nil {
			t.Fatalf("RouteModerationEvent: %v", err)
		}
		if len(targets) != 0 {
			t.Fatalf("expected no targets for %T, got %+v", event.GetPayload(), targets)
		}
	}
}
