package ws

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
)

func tagged(kind envelopeKind, value []byte) []byte {
	return append([]byte{byte(kind)}, value...)
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	return b
}

func decodeFrame(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal %q: %v", b, err)
	}
	return m
}

func TestTranslateMessageSent(t *testing.T) {
	evt := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_MessageSent{
		MessageSent: &eventsv1.MessageSent{MessageId: "m1", RoomId: "r1", SenderAccountId: "a1", SenderPseudonym: "Fox", Body: "hi", SentAtUnix: 100},
	}}
	out, err := translate(tagged(chatEventKind, mustMarshal(t, evt)), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame := decodeFrame(t, out)
	if frame["type"] != "message" || frame["message_id"] != "m1" || frame["body"] != "hi" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if _, hasClientMsgID := frame["client_msg_id"]; hasClientMsgID {
		t.Fatalf("expected no client_msg_id on the broadcast frame, got %+v", frame)
	}
}

func TestTranslateMemberJoinedAndLeft(t *testing.T) {
	joined := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_MemberJoined{MemberJoined: &eventsv1.MemberJoined{RoomId: "r1"}}}
	out, err := translate(tagged(chatEventKind, mustMarshal(t, joined)), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if frame := decodeFrame(t, out); frame["type"] != "room_joined" || frame["room_id"] != "r1" {
		t.Fatalf("unexpected frame: %+v", frame)
	}

	left := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_MemberLeft{MemberLeft: &eventsv1.MemberLeft{RoomId: "r1"}}}
	out, err = translate(tagged(chatEventKind, mustMarshal(t, left)), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if frame := decodeFrame(t, out); frame["type"] != "room_left" || frame["room_id"] != "r1" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

// Someone asking to keep the chat is news for the OTHER person only, and
// only while the room is not yet saved (a saved room is announced by
// RoomKept instead), DEC-0106.
func TestTranslateKeepVoteTellsTheOtherPersonThatSomeoneAskedToKeep(t *testing.T) {
	evt := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_KeepVote{KeepVote: &eventsv1.KeepVote{RoomId: "r1", AccountId: "a1"}}}
	out, err := translate(tagged(chatEventKind, mustMarshal(t, evt)), "a2")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame := decodeFrame(t, out)
	if frame["type"] != "keep_requested" || frame["room_id"] != "r1" {
		t.Fatalf("expected a keep_requested frame for the other member, got %+v", frame)
	}
}

func TestTranslateKeepVoteStaysSilentForTheVoterAndForTheVoteThatSavedTheRoom(t *testing.T) {
	for name, tc := range map[string]struct {
		me    string
		saved bool
	}{
		"the voter's own vote":           {me: "a1", saved: false},
		"the vote that saved the room":   {me: "a2", saved: true},
		"the voter when it saved a room": {me: "a1", saved: true},
	} {
		evt := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_KeepVote{KeepVote: &eventsv1.KeepVote{RoomId: "r1", AccountId: "a1", Kept: tc.saved}}}
		out, err := translate(tagged(chatEventKind, mustMarshal(t, evt)), tc.me)
		if err != nil {
			t.Fatalf("%s: translate: %v", name, err)
		}
		if out != nil {
			t.Fatalf("%s: expected no frame, got %s", name, out)
		}
	}
}

func TestTranslateRoomKept(t *testing.T) {
	evt := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_RoomKept{RoomKept: &eventsv1.RoomKept{RoomId: "r1"}}}
	out, err := translate(tagged(chatEventKind, mustMarshal(t, evt)), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame := decodeFrame(t, out)
	if frame["type"] != "keep_status" || frame["room_id"] != "r1" || frame["kept"] != true {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

// TestTranslateRoomEnded proves translateChatEvent's RoomEnded case
// reports you_left/partner_left based on who ended it (EndedByAccountId),
// the same distinction MatchFound already gives a client for
// partner/not-partner (DEC-0091) - not EndDirectRoom's original free-text
// Reason, which no client-facing code reads.
func TestTranslateRoomEnded(t *testing.T) {
	evt := &eventsv1.ChatEvent{Payload: &eventsv1.ChatEvent_RoomEnded{
		RoomEnded: &eventsv1.RoomEnded{RoomId: "r1", Reason: "next", EndedByAccountId: "a2"},
	}}
	out, err := translate(tagged(chatEventKind, mustMarshal(t, evt)), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame := decodeFrame(t, out)
	if frame["type"] != "match_ended" || frame["room_id"] != "r1" || frame["reason"] != "partner_left" {
		t.Fatalf("expected partner_left for a different account's EndedByAccountId, got %+v", frame)
	}

	out, err = translate(tagged(chatEventKind, mustMarshal(t, evt)), "a2")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame = decodeFrame(t, out)
	if frame["reason"] != "you_left" {
		t.Fatalf("expected you_left when EndedByAccountId matches myAccountID, got %+v", frame)
	}
}

func TestTranslateMatchFoundPicksThePartnerByAccountID(t *testing.T) {
	evt := &eventsv1.MatchingEvent{Payload: &eventsv1.MatchingEvent_MatchFound{MatchFound: &eventsv1.MatchFound{
		RoomId: "r1", AccountIdA: "a1", AccountIdB: "a2", PseudonymA: "Fox", PseudonymB: "Owl",
	}}}
	value := mustMarshal(t, evt)

	out, err := translate(tagged(matchingEventKind, value), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame := decodeFrame(t, out)
	if frame["partner_account_id"] != "a2" || frame["partner_pseudonym"] != "Owl" {
		t.Fatalf("expected a2/Owl as the partner for a1, got %+v", frame)
	}

	out, err = translate(tagged(matchingEventKind, value), "a2")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame = decodeFrame(t, out)
	if frame["partner_account_id"] != "a1" || frame["partner_pseudonym"] != "Fox" {
		t.Fatalf("expected a1/Fox as the partner for a2, got %+v", frame)
	}
}

func TestTranslateMatchEndedReasonDependsOnWhoEndedIt(t *testing.T) {
	evt := &eventsv1.MatchingEvent{Payload: &eventsv1.MatchingEvent_MatchEnded{MatchEnded: &eventsv1.MatchEnded{RoomId: "r1", AccountId: "a1"}}}
	value := mustMarshal(t, evt)

	out, err := translate(tagged(matchingEventKind, value), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if frame := decodeFrame(t, out); frame["reason"] != "you_left" {
		t.Fatalf("expected you_left for the account that ended it, got %+v", frame)
	}

	out, err = translate(tagged(matchingEventKind, value), "a2")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if frame := decodeFrame(t, out); frame["reason"] != "partner_left" {
		t.Fatalf("expected partner_left for the other account, got %+v", frame)
	}
}

func TestTranslateAccountBanned(t *testing.T) {
	evt := &eventsv1.ModerationEvent{Payload: &eventsv1.ModerationEvent_AccountBanned{AccountBanned: &eventsv1.AccountBanned{AccountId: "a1", Reason: "spam"}}}
	out, err := translate(tagged(moderationEventKind, mustMarshal(t, evt)), "a1")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	frame := decodeFrame(t, out)
	if frame["type"] != "kicked" || frame["reason"] != "spam" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestTranslateRejectsAnEmptyPayload(t *testing.T) {
	if _, err := translate(nil, "a1"); err == nil {
		t.Fatalf("expected an error for an empty payload")
	}
}

func TestTranslateRejectsAnUnknownKind(t *testing.T) {
	if _, err := translate([]byte{99, 1, 2, 3}, "a1"); err == nil {
		t.Fatalf("expected an error for an unknown envelope kind")
	}
}
