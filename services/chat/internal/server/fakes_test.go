package server

import (
	"context"
	"sort"
	"time"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/dedup"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/store"
)

const (
	roomTypeGroup  = chatv1.RoomType_ROOM_TYPE_GROUP
	roomTypeDirect = chatv1.RoomType_ROOM_TYPE_DIRECT
)

// fakeStore is an in-memory RoomStore. This is what lets server logic be
// tested without a real database - see store.RoomStore's doc comment. It
// does not replicate VoteKeep's row-locking concurrency guarantee - that's
// a database-level property, exercised by the integration tests against
// real Postgres, not something a unit test of the gRPC layer needs to
// assert on.
type fakeStore struct {
	rooms        map[string]store.Room
	byInviteCode map[string]string          // invite code -> room id
	members      map[string]map[string]bool // room id -> account id -> currently a member
	everMembers  map[string]map[string]bool // room id -> account id -> ever been a member
	keepVotes    map[string]map[string]bool // room id -> account id -> voted
	messages     map[string][]store.Message // room id -> messages, oldest first
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rooms:        map[string]store.Room{},
		byInviteCode: map[string]string{},
		members:      map[string]map[string]bool{},
		everMembers:  map[string]map[string]bool{},
		keepVotes:    map[string]map[string]bool{},
		messages:     map[string][]store.Message{},
	}
}

func (f *fakeStore) addMemberLocked(roomID, accountID string) {
	if f.members[roomID] == nil {
		f.members[roomID] = map[string]bool{}
	}
	if f.everMembers[roomID] == nil {
		f.everMembers[roomID] = map[string]bool{}
	}
	f.members[roomID][accountID] = true
	f.everMembers[roomID][accountID] = true
}

func (f *fakeStore) CreateGroupRoom(_ context.Context, id, ownerAccountID, name, inviteCode string) (store.Room, error) {
	if _, taken := f.byInviteCode[inviteCode]; taken {
		return store.Room{}, store.ErrAlreadyExists
	}
	room := store.Room{
		ID:             id,
		Type:           roomTypeGroup,
		Name:           &name,
		OwnerAccountID: &ownerAccountID,
		InviteCode:     &inviteCode,
		CreatedAt:      time.Unix(0, 0),
	}
	f.rooms[id] = room
	f.byInviteCode[inviteCode] = id
	f.addMemberLocked(id, ownerAccountID)
	return room, nil
}

func (f *fakeStore) CreateDirectRoom(_ context.Context, id string, memberAccountIDs []string) (store.Room, error) {
	room := store.Room{ID: id, Type: roomTypeDirect, CreatedAt: time.Now()}
	f.rooms[id] = room
	for _, accountID := range memberAccountIDs {
		f.addMemberLocked(id, accountID)
	}
	return room, nil
}

func (f *fakeStore) GetRoom(_ context.Context, id string) (store.Room, error) {
	room, ok := f.rooms[id]
	if !ok {
		return store.Room{}, store.ErrNotFound
	}
	return room, nil
}

func (f *fakeStore) GetRoomByInviteCode(_ context.Context, inviteCode string) (store.Room, error) {
	id, ok := f.byInviteCode[inviteCode]
	if !ok {
		return store.Room{}, store.ErrNotFound
	}
	return f.rooms[id], nil
}

func (f *fakeStore) RotateInviteCode(_ context.Context, roomID, newInviteCode string) (store.Room, error) {
	room, ok := f.rooms[roomID]
	if !ok {
		return store.Room{}, store.ErrNotFound
	}
	if _, taken := f.byInviteCode[newInviteCode]; taken {
		return store.Room{}, store.ErrAlreadyExists
	}
	if room.InviteCode != nil {
		delete(f.byInviteCode, *room.InviteCode)
	}
	room.InviteCode = &newInviteCode
	f.rooms[roomID] = room
	f.byInviteCode[newInviteCode] = roomID
	return room, nil
}

func (f *fakeStore) AddMember(_ context.Context, roomID, accountID string) error {
	f.addMemberLocked(roomID, accountID)
	return nil
}

func (f *fakeStore) RemoveMember(_ context.Context, roomID, accountID string) error {
	if f.members[roomID] != nil {
		delete(f.members[roomID], accountID)
	}
	return nil
}

func (f *fakeStore) IsCurrentMember(_ context.Context, roomID, accountID string) (bool, error) {
	return f.members[roomID][accountID], nil
}

func (f *fakeStore) HasEverBeenMember(_ context.Context, roomID, accountID string) (bool, error) {
	return f.everMembers[roomID][accountID], nil
}

// MembershipState mirrors the real store's single query: whether the
// account is a current member, and whether the room is still open.
func (f *fakeStore) MembershipState(_ context.Context, roomID, accountID string) (bool, bool, error) {
	if !f.members[roomID][accountID] {
		return false, false, nil
	}
	return true, f.rooms[roomID].EndedAt == nil, nil
}

// ListRoomsForAccount mirrors the real query's rules (DEC-0106): every group
// room; a direct room the person voted to keep; and a direct room that is
// still an open, unkept, recent random chat, so a reconnecting connection
// can listen to it again.
func (f *fakeStore) ListRoomsForAccount(_ context.Context, accountID string) ([]store.Room, error) {
	var rooms []store.Room
	for roomID, current := range f.members {
		if !current[accountID] {
			continue
		}
		room := f.rooms[roomID]
		savedByMe := room.Kept && f.keepVotes[roomID][accountID]
		activeChat := !room.Kept && room.EndedAt == nil && time.Since(room.CreatedAt) <= store.ActiveDirectRoomWindow
		if room.Type == roomTypeGroup || savedByMe || activeChat {
			for id, in := range f.members[roomID] {
				if in {
					room.MemberAccountIDs = append(room.MemberAccountIDs, id)
				}
			}
			sort.Strings(room.MemberAccountIDs)
			rooms = append(rooms, room)
		}
	}
	return rooms, nil
}

func (f *fakeStore) CloseDirectRoom(_ context.Context, roomID string) error {
	room, ok := f.rooms[roomID]
	if !ok || room.EndedAt != nil {
		return nil
	}
	now := time.Now()
	room.EndedAt = &now
	f.rooms[roomID] = room
	return nil
}

func (f *fakeStore) VoteKeep(_ context.Context, roomID, accountID string) (bool, error) {
	room, ok := f.rooms[roomID]
	if !ok {
		return false, store.ErrNotFound
	}
	if f.keepVotes[roomID] == nil {
		f.keepVotes[roomID] = map[string]bool{}
	}
	f.keepVotes[roomID][accountID] = true

	members := 0
	for _, isCurrent := range f.members[roomID] {
		if isCurrent {
			members++
		}
	}
	kept := members > 0 && len(f.keepVotes[roomID]) >= members
	if !kept && room.EndedAt != nil && time.Since(*room.EndedAt) <= store.VoteKeepGraceWindow {
		kept = len(f.keepVotes[roomID]) >= 1
	}
	if kept {
		room.Kept = true
		f.rooms[roomID] = room
	}
	return kept, nil
}

func (f *fakeStore) EndDirectRoom(_ context.Context, roomID string) error {
	room, ok := f.rooms[roomID]
	if !ok || room.Kept || room.EndedAt != nil {
		return nil
	}
	now := time.Now()
	room.EndedAt = &now
	f.rooms[roomID] = room
	return nil
}

func (f *fakeStore) ListMessages(_ context.Context, roomID, beforeMessageID string, limit int) ([]store.Message, error) {
	all := f.messages[roomID]
	// Oldest-first in storage; the real store returns newest-first.
	var newestFirst []store.Message
	for i := len(all) - 1; i >= 0; i-- {
		newestFirst = append(newestFirst, all[i])
	}

	start := 0
	if beforeMessageID != "" {
		for i, m := range newestFirst {
			if m.MessageID == beforeMessageID {
				start = i + 1
				break
			}
		}
	}
	end := min(start+limit, len(newestFirst))
	if start >= end {
		return nil, nil
	}
	return newestFirst[start:end], nil
}

// fakeProducer records every event it is asked to publish, or returns a
// fixed error if one is set - used to test SendMessage's "fail loudly on
// a failed produce" path (DEC-0047).
type fakeProducer struct {
	published []*eventsv1.ChatEvent
	err       error
}

func (f *fakeProducer) Publish(_ context.Context, _ string, event *eventsv1.ChatEvent) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, event)
	return nil
}

func (f *fakeProducer) Close() error { return nil }

// fakeDeduper is an in-memory Deduper, mirroring what Redis's SETNX gives
// the real one: first claim wins, every later claim for the same key gets
// the first claim's result back.
type fakeDeduper struct {
	claims map[string]dedup.Result
}

func newFakeDeduper() *fakeDeduper {
	return &fakeDeduper{claims: map[string]dedup.Result{}}
}

func (f *fakeDeduper) Claim(_ context.Context, roomID, senderAccountID, clientMsgID string, result dedup.Result) (dedup.Result, bool, error) {
	key := roomID + "|" + senderAccountID + "|" + clientMsgID
	if existing, ok := f.claims[key]; ok {
		return existing, false, nil
	}
	f.claims[key] = result
	return result, true, nil
}
