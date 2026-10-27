package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/store"
)

// idGen returns an IDGenerator that hands out the given ids in order,
// then panics on exhaustion - a test bug, not a thing to handle
// gracefully. Mirrors identity's server_test.go.
func idGen(ids ...string) IDGenerator {
	i := 0
	return func() string {
		id := ids[i]
		i++
		return id
	}
}

func newTestServer(ids ...string) (*Server, *fakeStore, *fakeProducer) {
	st := newFakeStore()
	prod := &fakeProducer{}
	s := New(st, prod, newFakeDeduper(), idGen(ids...))
	return s, st, prod
}

func TestCreateGroupRoom(t *testing.T) {
	s, st, _ := newTestServer("room-1")

	resp, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{
		OwnerAccountId: "owner-1",
		Name:           "Study group",
	})
	if err != nil {
		t.Fatalf("CreateGroupRoom: %v", err)
	}
	room := resp.GetRoom()
	if room.GetId() != "room-1" || room.GetName() != "Study group" || room.GetOwnerAccountId() != "owner-1" {
		t.Fatalf("unexpected room: %+v", room)
	}
	if room.GetInviteCode() == "" {
		t.Fatalf("expected a generated invite code")
	}
	isMember, _ := st.IsCurrentMember(context.Background(), "room-1", "owner-1")
	if !isMember {
		t.Fatalf("expected the owner to be a member of the room it created")
	}

	if _, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: "owner-1", Name: ""}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty name, got %v", err)
	}
	if _, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: "", Name: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty owner, got %v", err)
	}
}

func TestCreateDirectRoom(t *testing.T) {
	s, st, prod := newTestServer("room-1")

	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for one member, got %v", err)
	}

	resp, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("CreateDirectRoom: %v", err)
	}
	if resp.GetRoom().GetId() != "room-1" || resp.GetRoom().GetType() != chatv1.RoomType_ROOM_TYPE_DIRECT {
		t.Fatalf("unexpected room: %+v", resp.GetRoom())
	}
	for _, accountID := range []string{"a", "b"} {
		isMember, _ := st.IsCurrentMember(context.Background(), "room-1", accountID)
		if !isMember {
			t.Fatalf("expected %s to be a member", accountID)
		}
	}
	if len(prod.published) != 0 {
		t.Fatalf("expected CreateDirectRoom to publish no events (Matching's MatchFound covers it), got %d", len(prod.published))
	}
}

func TestJoinRoomByCode(t *testing.T) {
	s, st, prod := newTestServer("room-1", "event-1")
	if _, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: "owner", Name: "g"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	code := *st.rooms["room-1"].InviteCode

	if _, err := s.JoinRoomByCode(context.Background(), &chatv1.JoinRoomByCodeRequest{AccountId: "joiner", InviteCode: "does-not-exist"}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}

	resp, err := s.JoinRoomByCode(context.Background(), &chatv1.JoinRoomByCodeRequest{AccountId: "joiner", InviteCode: code})
	if err != nil {
		t.Fatalf("JoinRoomByCode: %v", err)
	}
	if resp.GetRoom().GetId() != "room-1" {
		t.Fatalf("unexpected room: %+v", resp.GetRoom())
	}
	isMember, _ := st.IsCurrentMember(context.Background(), "room-1", "joiner")
	if !isMember {
		t.Fatalf("expected joiner to be a member")
	}
	if len(prod.published) != 1 {
		t.Fatalf("expected one MemberJoined event, got %d", len(prod.published))
	}
	if _, ok := prod.published[0].GetPayload().(*eventsv1.ChatEvent_MemberJoined); !ok {
		t.Fatalf("expected a MemberJoined event, got %T", prod.published[0].GetPayload())
	}
}

func TestLeaveRoom(t *testing.T) {
	s, st, prod := newTestServer("room-1", "event-1", "event-2")
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.LeaveRoom(context.Background(), &chatv1.LeaveRoomRequest{RoomId: "room-1", AccountId: "stranger"}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound for a non-member, got %v", err)
	}

	if _, err := s.LeaveRoom(context.Background(), &chatv1.LeaveRoomRequest{RoomId: "room-1", AccountId: "a"}); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	isMember, _ := st.IsCurrentMember(context.Background(), "room-1", "a")
	if isMember {
		t.Fatalf("expected a to no longer be a member")
	}
	// A direct room closes when someone leaves it (DEC-0106), so two events:
	// the room ending, then the member leaving.
	if len(prod.published) != 2 {
		t.Fatalf("expected RoomEnded and MemberLeft events, got %d", len(prod.published))
	}
	if st.rooms["room-1"].EndedAt == nil {
		t.Fatalf("expected leaving a direct room to close it")
	}
}

func TestRotateInviteCode(t *testing.T) {
	s, st, _ := newTestServer("room-1")
	if _, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: "owner", Name: "g"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	original := *st.rooms["room-1"].InviteCode

	if _, err := s.RotateInviteCode(context.Background(), &chatv1.RotateInviteCodeRequest{RoomId: "room-1", RequestedByAccountId: "not-owner"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a non-owner, got %v", err)
	}

	resp, err := s.RotateInviteCode(context.Background(), &chatv1.RotateInviteCodeRequest{RoomId: "room-1", RequestedByAccountId: "owner"})
	if err != nil {
		t.Fatalf("RotateInviteCode: %v", err)
	}
	if resp.GetInviteCode() == "" || resp.GetInviteCode() == original {
		t.Fatalf("expected a new, different invite code, got %q (was %q)", resp.GetInviteCode(), original)
	}
}

func TestListMyRooms(t *testing.T) {
	s, _, _ := newTestServer("group-1", "direct-1", "direct-2", "event-1", "event-2", "event-3")
	if _, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: "me", Name: "g"}); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"me", "other"}}); err != nil {
		t.Fatalf("create direct: %v", err)
	}
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"me", "other2"}}); err != nil {
		t.Fatalf("create direct 2: %v", err)
	}
	if _, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "direct-1", AccountId: "me"}); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if _, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "direct-1", AccountId: "other"}); err != nil {
		t.Fatalf("vote: %v", err)
	}

	resp, err := s.ListMyRooms(context.Background(), &chatv1.ListMyRoomsRequest{AccountId: "me"})
	if err != nil {
		t.Fatalf("ListMyRooms: %v", err)
	}
	// The group room, the kept direct room, and the still-open random chat
	// (direct-2): the last is listed so that a connection that reconnects can
	// listen to it again (DEC-0106, INC-0024).
	if len(resp.GetRooms()) != 3 {
		t.Fatalf("expected 3 rooms (group + kept direct + active chat), got %d: %+v", len(resp.GetRooms()), resp.GetRooms())
	}
}

func TestSendMessage(t *testing.T) {
	// SendMessage consumes a genID() call for a candidate message id on
	// every accepted call (even a retry, which then discards it - see
	// SendMessage's comment), plus one more for each event it actually
	// publishes. The id list below accounts for exactly that: room-1,
	// then {candidate, event} for the first send, {wasted candidate} for
	// the retry, then {candidate, event} for a second, distinct message.
	s, _, prod := newTestServer("room-1", "c1-msg", "c1-evt", "c1-retry-wasted", "c2-msg", "c2-evt")
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "a", Body: "", ClientMsgId: "c1"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty body, got %v", err)
	}
	if _, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "a", Body: "hi", ClientMsgId: ""}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty client_msg_id, got %v", err)
	}
	if _, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "stranger", Body: "hi", ClientMsgId: "c1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a non-member, got %v", err)
	}

	resp1, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "a", Body: "hi", ClientMsgId: "c1"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp1.GetMessageId() != "c1-msg" {
		t.Fatalf("expected c1-msg, got %s", resp1.GetMessageId())
	}
	if len(prod.published) != 1 {
		t.Fatalf("expected one MessageSent event, got %d", len(prod.published))
	}

	// A retry with the same client_msg_id must not produce a second
	// event or mint a second message id (DEC-0047, DEC-0061).
	resp2, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "a", Body: "hi", ClientMsgId: "c1"})
	if err != nil {
		t.Fatalf("SendMessage retry: %v", err)
	}
	if resp2.GetMessageId() != resp1.GetMessageId() || resp2.GetSentAtUnix() != resp1.GetSentAtUnix() {
		t.Fatalf("expected the retry to return the original result, got %+v vs %+v", resp2, resp1)
	}
	if len(prod.published) != 1 {
		t.Fatalf("expected the retry to publish no additional event, still have %d", len(prod.published))
	}

	// A different client_msg_id from the same sender is a new message.
	if _, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "a", Body: "second", ClientMsgId: "c2"}); err != nil {
		t.Fatalf("SendMessage second: %v", err)
	}
	if len(prod.published) != 2 {
		t.Fatalf("expected a second event for a different client_msg_id, got %d", len(prod.published))
	}
}

func TestSendMessageFailsLoudlyOnProduceError(t *testing.T) {
	st := newFakeStore()
	prod := &fakeProducer{err: status.Error(codes.Unavailable, "kafka unreachable")}
	s := New(st, prod, newFakeDeduper(), idGen("room-1", "msg-1", "event-1"))
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "a", Body: "hi", ClientMsgId: "c1"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal when the producer fails (DEC-0047), got %v", err)
	}
}

func TestVoteKeep(t *testing.T) {
	s, _, prod := newTestServer("room-1", "event-1", "event-2", "event-3")
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: "stranger"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a non-member, got %v", err)
	}

	resp, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: "a"})
	if err != nil {
		t.Fatalf("VoteKeep: %v", err)
	}
	if resp.GetKept() {
		t.Fatalf("expected not kept after only one of two votes")
	}
	if len(prod.published) != 1 {
		t.Fatalf("expected one KeepVote event, got %d", len(prod.published))
	}

	resp, err = s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: "b"})
	if err != nil {
		t.Fatalf("VoteKeep second: %v", err)
	}
	if !resp.GetKept() {
		t.Fatalf("expected kept once both members have voted")
	}
	if len(prod.published) != 3 {
		t.Fatalf("expected a second KeepVote plus a RoomKept event, got %d", len(prod.published))
	}
}

// TestVoteKeepSoloWithinGraceWindowAfterRoomEnded proves DEC-0091's fix:
// once one side has left (ending the room before both voted), the side
// who stayed can no longer ever complete a mutual vote - the member who
// left is gone and will never vote. Without the grace-window path, "keep"
// would be permanently impossible from this point on.
func TestVoteKeepSoloWithinGraceWindowAfterRoomEnded(t *testing.T) {
	s, _, _ := newTestServer("room-1", "event-1", "event-2", "event-3")
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.EndDirectRoom(context.Background(), &chatv1.EndDirectRoomRequest{RoomId: "room-1", AccountId: "b", Reason: "ended"}); err != nil {
		t.Fatalf("EndDirectRoom: %v", err)
	}

	resp, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: "a"})
	if err != nil {
		t.Fatalf("VoteKeep: %v", err)
	}
	if !resp.GetKept() {
		t.Fatalf("expected a solo vote to keep the room within the grace window after the other side left")
	}
}

// TestVoteKeepSoloAfterGraceWindowExpiredDoesNotKeep proves the grace
// window is an actual bound, not unlimited - a vote arriving long after
// the room ended should not resurrect it.
func TestVoteKeepSoloAfterGraceWindowExpiredDoesNotKeep(t *testing.T) {
	s, st, _ := newTestServer("room-1", "event-1", "event-2", "event-3")
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.EndDirectRoom(context.Background(), &chatv1.EndDirectRoomRequest{RoomId: "room-1", AccountId: "b", Reason: "ended"}); err != nil {
		t.Fatalf("EndDirectRoom: %v", err)
	}
	room := st.rooms["room-1"]
	longAgo := room.EndedAt.Add(-2 * store.VoteKeepGraceWindow)
	room.EndedAt = &longAgo
	st.rooms["room-1"] = room

	resp, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: "a"})
	if err != nil {
		t.Fatalf("VoteKeep: %v", err)
	}
	if resp.GetKept() {
		t.Fatalf("expected a solo vote well outside the grace window to NOT keep the room")
	}
}

func TestEndDirectRoom(t *testing.T) {
	s, st, prod := newTestServer("room-1", "event-1")
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.EndDirectRoom(context.Background(), &chatv1.EndDirectRoomRequest{RoomId: "room-1", AccountId: "stranger"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a non-member, got %v", err)
	}

	if _, err := s.EndDirectRoom(context.Background(), &chatv1.EndDirectRoomRequest{RoomId: "room-1", AccountId: "a", Reason: "next"}); err != nil {
		t.Fatalf("EndDirectRoom: %v", err)
	}
	if st.rooms["room-1"].EndedAt == nil {
		t.Fatalf("expected the room to be marked ended")
	}
	if len(prod.published) != 1 {
		t.Fatalf("expected one RoomEnded event, got %d", len(prod.published))
	}
}

func TestRemoveMember(t *testing.T) {
	s, st, prod := newTestServer("room-1", "event-1")
	if _, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: "owner", Name: "g"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.AddMember(context.Background(), "room-1", "member"); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if _, err := s.RemoveMember(context.Background(), &chatv1.RemoveMemberRequest{RoomId: "room-1", AccountId: "member", RequestedByAccountId: "not-owner"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a non-owner, got %v", err)
	}

	if _, err := s.RemoveMember(context.Background(), &chatv1.RemoveMemberRequest{RoomId: "room-1", AccountId: "member", RequestedByAccountId: "owner"}); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	isMember, _ := st.IsCurrentMember(context.Background(), "room-1", "member")
	if isMember {
		t.Fatalf("expected member to be removed")
	}
	if len(prod.published) != 1 {
		t.Fatalf("expected one MemberLeft event, got %d", len(prod.published))
	}
}

func TestListMessagesRequiresMembership(t *testing.T) {
	s, _, _ := newTestServer("room-1")
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.ListMessages(context.Background(), &chatv1.ListMessagesRequest{RoomId: "room-1", AccountId: "stranger"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a non-member, got %v", err)
	}
	if _, err := s.ListMessages(context.Background(), &chatv1.ListMessagesRequest{RoomId: "room-1", AccountId: "a"}); err != nil {
		t.Fatalf("expected a current member to be allowed to list messages, got %v", err)
	}
}

// --- DEC-0106: what a saved chat is, who sees it, and when it goes quiet ---

func newDirect(t *testing.T, ids ...string) (*Server, *fakeStore, *fakeProducer) {
	t.Helper()
	s, st, prod := newTestServer(ids...)
	if _, err := s.CreateDirectRoom(context.Background(), &chatv1.CreateDirectRoomRequest{MemberAccountIds: []string{"a", "b"}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	return s, st, prod
}

func roomIDs(rooms []*chatv1.Room) []string {
	var ids []string
	for _, r := range rooms {
		ids = append(ids, r.GetId())
	}
	return ids
}

func TestListMyRoomsIncludesAnActiveRandomChatForBothMembers(t *testing.T) {
	s, _, _ := newDirect(t, "room-1")
	for _, who := range []string{"a", "b"} {
		resp, err := s.ListMyRooms(context.Background(), &chatv1.ListMyRoomsRequest{AccountId: who})
		if err != nil {
			t.Fatalf("ListMyRooms(%s): %v", who, err)
		}
		if got := roomIDs(resp.GetRooms()); len(got) != 1 || got[0] != "room-1" {
			t.Fatalf("%s should see the open chat, got %v", who, got)
		}
		if members := resp.GetRooms()[0].GetMemberAccountIds(); len(members) != 2 || members[0] != "a" || members[1] != "b" {
			t.Fatalf("expected both members listed so the other person can be named, got %v", members)
		}
	}
}

func TestListMyRoomsDropsAChatThatEndedWithoutBeingSaved(t *testing.T) {
	s, _, _ := newDirect(t, "room-1", "event-1")
	if _, err := s.EndDirectRoom(context.Background(), &chatv1.EndDirectRoomRequest{RoomId: "room-1", AccountId: "a"}); err != nil {
		t.Fatalf("end: %v", err)
	}
	resp, _ := s.ListMyRooms(context.Background(), &chatv1.ListMyRoomsRequest{AccountId: "b"})
	if len(resp.GetRooms()) != 0 {
		t.Fatalf("an ended, unsaved chat should not be listed, got %v", roomIDs(resp.GetRooms()))
	}
}

// The person who stayed and saved the chat has it; the person who skipped
// does not get it in their Inbox just because they were once a member.
func TestASoloSavedChatIsInTheKeepersInboxOnly(t *testing.T) {
	s, _, _ := newDirect(t, "room-1", "event-1", "event-2", "event-3")
	if _, err := s.EndDirectRoom(context.Background(), &chatv1.EndDirectRoomRequest{RoomId: "room-1", AccountId: "a"}); err != nil {
		t.Fatalf("end: %v", err)
	}
	if resp, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: "b"}); err != nil || !resp.GetKept() {
		t.Fatalf("expected the solo keep within the grace window to save it: %v kept=%v", err, resp.GetKept())
	}
	mine, _ := s.ListMyRooms(context.Background(), &chatv1.ListMyRoomsRequest{AccountId: "b"})
	if got := roomIDs(mine.GetRooms()); len(got) != 1 || got[0] != "room-1" {
		t.Fatalf("the keeper should have it, got %v", got)
	}
	theirs, _ := s.ListMyRooms(context.Background(), &chatv1.ListMyRoomsRequest{AccountId: "a"})
	if len(theirs.GetRooms()) != 0 {
		t.Fatalf("the person who skipped must not see it, got %v", roomIDs(theirs.GetRooms()))
	}
}

func TestSendMessageIsRefusedInAClosedRoomButStillWorksInAnOpenOne(t *testing.T) {
	s, _, _ := newDirect(t, "room-1", "event-1", "event-2", "event-3")
	send := func(who, id string) error {
		_, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: who, SenderPseudonym: who, Body: "hi", ClientMsgId: id})
		return err
	}
	if err := send("a", "m1"); err != nil {
		t.Fatalf("an open room should accept messages: %v", err)
	}
	if _, err := s.EndDirectRoom(context.Background(), &chatv1.EndDirectRoomRequest{RoomId: "room-1", AccountId: "a"}); err != nil {
		t.Fatalf("end: %v", err)
	}
	if err := send("b", "m2"); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition for a message into an ended room, got %v", err)
	}
	if err := send("stranger", "m3"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a stranger is still PermissionDenied, got %v", err)
	}
}

func TestCloseDirectRoomStopsMessagingEvenInASavedChat(t *testing.T) {
	s, st, prod := newDirect(t, "room-1", "event-1", "event-2", "event-3", "event-4")
	for _, who := range []string{"a", "b"} {
		if _, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: who}); err != nil {
			t.Fatalf("vote: %v", err)
		}
	}
	if !st.rooms["room-1"].Kept {
		t.Fatalf("setup: expected a saved chat")
	}
	before := len(prod.published)

	if _, err := s.CloseDirectRoom(context.Background(), &chatv1.CloseDirectRoomRequest{RoomId: "room-1", AccountId: "stranger"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a non-member, got %v", err)
	}
	if _, err := s.CloseDirectRoom(context.Background(), &chatv1.CloseDirectRoomRequest{RoomId: "room-1", AccountId: "a"}); err != nil {
		t.Fatalf("CloseDirectRoom: %v", err)
	}
	if st.rooms["room-1"].EndedAt == nil || !st.rooms["room-1"].Kept {
		t.Fatalf("expected the room closed but still saved")
	}
	ended := prod.published[before].GetRoomEnded()
	if ended == nil || ended.GetEndedByAccountId() != "a" {
		t.Fatalf("expected a RoomEnded event naming who closed it, got %v", prod.published[before])
	}
	if _, err := s.SendMessage(context.Background(), &chatv1.SendMessageRequest{RoomId: "room-1", SenderAccountId: "b", Body: "hi", ClientMsgId: "x"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected the closed room to refuse messages, got %v", err)
	}
	// Closing again changes nothing and is not an error.
	if _, err := s.CloseDirectRoom(context.Background(), &chatv1.CloseDirectRoomRequest{RoomId: "room-1", AccountId: "b"}); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
}

func TestCloseDirectRoomRefusesAGroupRoom(t *testing.T) {
	s, _, _ := newTestServer("group-1")
	if _, err := s.CreateGroupRoom(context.Background(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: "o", Name: "g"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.CloseDirectRoom(context.Background(), &chatv1.CloseDirectRoomRequest{RoomId: "group-1", AccountId: "o"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for a group room, got %v", err)
	}
}

func TestKeepVoteEventSaysWhetherThatVoteSavedTheRoom(t *testing.T) {
	s, _, prod := newDirect(t, "room-1", "event-1", "event-2", "event-3")
	for _, who := range []string{"a", "b"} {
		if _, err := s.VoteKeep(context.Background(), &chatv1.VoteKeepRequest{RoomId: "room-1", AccountId: who}); err != nil {
			t.Fatalf("vote: %v", err)
		}
	}
	first, second := prod.published[0].GetKeepVote(), prod.published[1].GetKeepVote()
	if first == nil || first.GetKept() {
		t.Fatalf("the first vote has not saved the room: %v", first)
	}
	if second == nil || !second.GetKept() {
		t.Fatalf("the second vote saved the room and its event must say so: %v", second)
	}
}
