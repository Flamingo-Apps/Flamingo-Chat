// Package server implements the ChatService gRPC contract, translating
// between the wire types (chatv1) and the store/events/dedup boundaries.
package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/dedup"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/events"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/store"
)

// IDGenerator generates a new room, message or event ID. A function type
// rather than a concrete uuid.NewString reference so tests can supply
// deterministic IDs - the same pattern identity's server package uses.
type IDGenerator func() string

// defaultMessageLimit and maxMessageLimit bound ListMessages' page size -
// a client that asks for everything at once would otherwise be free to
// make Chat scan and return an unbounded number of rows.
const (
	defaultMessageLimit = 50
	maxMessageLimit     = 200
)

type Server struct {
	chatv1.UnimplementedChatServiceServer

	store    store.RoomStore
	producer events.Producer
	dedup    dedup.Deduper
	genID    IDGenerator
}

func New(roomStore store.RoomStore, producer events.Producer, deduper dedup.Deduper, genID IDGenerator) *Server {
	return &Server{
		store:    roomStore,
		producer: producer,
		dedup:    deduper,
		genID:    genID,
	}
}

func (s *Server) CreateGroupRoom(ctx context.Context, req *chatv1.CreateGroupRoomRequest) (*chatv1.CreateGroupRoomResponse, error) {
	if strings.TrimSpace(req.GetOwnerAccountId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_account_id is required")
	}
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	var room store.Room
	for range maxInviteCodeAttempts {
		code, err := generateInviteCode()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "create group room: generate invite code: %v", err)
		}
		room, err = s.store.CreateGroupRoom(ctx, s.genID(), req.GetOwnerAccountId(), name, code)
		if err == nil {
			break
		}
		if !errors.Is(err, store.ErrAlreadyExists) {
			return nil, status.Errorf(codes.Internal, "create group room: %v", err)
		}
		// An invite code collision - vanishingly unlikely at 32^8
		// combinations, but retrying with a freshly generated code
		// converges the same way identity's pseudonym retry does.
	}
	if room.ID == "" {
		return nil, status.Errorf(codes.Internal, "create group room: could not generate a unique invite code after %d attempts", maxInviteCodeAttempts)
	}

	return &chatv1.CreateGroupRoomResponse{Room: toProtoRoom(room)}, nil
}

// CreateDirectRoom is called only by Matching when it pairs two users
// (DEC-0044), never by a client directly. It deliberately does not
// produce a chat.events MemberJoined event: Matching's own MatchFound
// event, not yet built (T6 in .claude/build-plan.md), is what tells the
// delivery worker about the new room - a second, redundant event here
// would just be noise for the same fact.
func (s *Server) CreateDirectRoom(ctx context.Context, req *chatv1.CreateDirectRoomRequest) (*chatv1.CreateDirectRoomResponse, error) {
	members := req.GetMemberAccountIds()
	if len(members) != 2 {
		return nil, status.Error(codes.InvalidArgument, "a direct room requires exactly two members")
	}

	room, err := s.store.CreateDirectRoom(ctx, s.genID(), members)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create direct room: %v", err)
	}
	return &chatv1.CreateDirectRoomResponse{Room: toProtoRoom(room)}, nil
}

func (s *Server) JoinRoomByCode(ctx context.Context, req *chatv1.JoinRoomByCodeRequest) (*chatv1.JoinRoomByCodeResponse, error) {
	room, err := s.store.GetRoomByInviteCode(ctx, req.GetInviteCode())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "no room has this invite code")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "join room by code: %v", err)
	}

	if err := s.store.AddMember(ctx, room.ID, req.GetAccountId()); err != nil {
		return nil, status.Errorf(codes.Internal, "join room by code: %v", err)
	}
	if err := s.publish(ctx, room.ID, &eventsv1.ChatEvent_MemberJoined{
		MemberJoined: &eventsv1.MemberJoined{RoomId: room.ID, AccountId: req.GetAccountId()},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "join room by code: %v", err)
	}
	return &chatv1.JoinRoomByCodeResponse{Room: toProtoRoom(room)}, nil
}

func (s *Server) LeaveRoom(ctx context.Context, req *chatv1.LeaveRoomRequest) (*chatv1.LeaveRoomResponse, error) {
	isMember, err := s.store.IsCurrentMember(ctx, req.GetRoomId(), req.GetAccountId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "leave room: %v", err)
	}
	if !isMember {
		return nil, status.Error(codes.NotFound, "not a member of this room")
	}

	// Deleting a saved chat (or leaving any direct room) closes it for the
	// other person too: with one side gone nobody is left to talk to, and a
	// room that still accepted messages would swallow them silently
	// (DEC-0106).
	room, err := s.store.GetRoom(ctx, req.GetRoomId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "leave room: %v", err)
	}
	if room.Type == chatv1.RoomType_ROOM_TYPE_DIRECT && room.EndedAt == nil {
		if err := s.store.CloseDirectRoom(ctx, req.GetRoomId()); err != nil {
			return nil, status.Errorf(codes.Internal, "leave room: %v", err)
		}
		if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_RoomEnded{
			RoomEnded: &eventsv1.RoomEnded{RoomId: req.GetRoomId(), Reason: "left", EndedByAccountId: req.GetAccountId()},
		}); err != nil {
			return nil, status.Errorf(codes.Internal, "leave room: %v", err)
		}
	}

	if err := s.store.RemoveMember(ctx, req.GetRoomId(), req.GetAccountId()); err != nil {
		return nil, status.Errorf(codes.Internal, "leave room: %v", err)
	}
	if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_MemberLeft{
		MemberLeft: &eventsv1.MemberLeft{RoomId: req.GetRoomId(), AccountId: req.GetAccountId()},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "leave room: %v", err)
	}
	return &chatv1.LeaveRoomResponse{}, nil
}

func (s *Server) RotateInviteCode(ctx context.Context, req *chatv1.RotateInviteCodeRequest) (*chatv1.RotateInviteCodeResponse, error) {
	room, err := s.store.GetRoom(ctx, req.GetRoomId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "room not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rotate invite code: %v", err)
	}
	if room.Type != chatv1.RoomType_ROOM_TYPE_GROUP {
		return nil, status.Error(codes.InvalidArgument, "only group rooms have an invite code")
	}
	if room.OwnerAccountID == nil || *room.OwnerAccountID != req.GetRequestedByAccountId() {
		return nil, status.Error(codes.PermissionDenied, "only the room's owner can rotate its invite code")
	}

	for range maxInviteCodeAttempts {
		code, err := generateInviteCode()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "rotate invite code: generate: %v", err)
		}
		room, err = s.store.RotateInviteCode(ctx, req.GetRoomId(), code)
		if err == nil {
			return &chatv1.RotateInviteCodeResponse{InviteCode: *room.InviteCode}, nil
		}
		if !errors.Is(err, store.ErrAlreadyExists) {
			return nil, status.Errorf(codes.Internal, "rotate invite code: %v", err)
		}
	}
	return nil, status.Errorf(codes.Internal, "rotate invite code: could not generate a unique code after %d attempts", maxInviteCodeAttempts)
}

func (s *Server) ListMyRooms(ctx context.Context, req *chatv1.ListMyRoomsRequest) (*chatv1.ListMyRoomsResponse, error) {
	rooms, err := s.store.ListRoomsForAccount(ctx, req.GetAccountId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list my rooms: %v", err)
	}
	protoRooms := make([]*chatv1.Room, len(rooms))
	for i, r := range rooms {
		protoRooms[i] = toProtoRoom(r)
	}
	return &chatv1.ListMyRoomsResponse{Rooms: protoRooms}, nil
}

func (s *Server) ListMessages(ctx context.Context, req *chatv1.ListMessagesRequest) (*chatv1.ListMessagesResponse, error) {
	wasMember, err := s.store.HasEverBeenMember(ctx, req.GetRoomId(), req.GetAccountId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list messages: %v", err)
	}
	if !wasMember {
		return nil, status.Error(codes.PermissionDenied, "not a current or former member of this room")
	}

	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = defaultMessageLimit
	}
	if limit > maxMessageLimit {
		limit = maxMessageLimit
	}

	messages, err := s.store.ListMessages(ctx, req.GetRoomId(), req.GetBeforeMessageId(), limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list messages: %v", err)
	}
	protoMessages := make([]*chatv1.Message, len(messages))
	for i, m := range messages {
		protoMessages[i] = toProtoMessage(m)
	}
	return &chatv1.ListMessagesResponse{Messages: protoMessages}, nil
}

// SendMessage never writes to Postgres itself - it only produces a
// MessageSent event to Kafka (docs/ARCHITECTURE.md §3.1, DEC-0047). The
// persistence worker, not yet built, is what actually inserts the row
// ListMessages later reads.
func (s *Server) SendMessage(ctx context.Context, req *chatv1.SendMessageRequest) (*chatv1.SendMessageResponse, error) {
	if strings.TrimSpace(req.GetBody()) == "" {
		return nil, status.Error(codes.InvalidArgument, "body is required")
	}
	if strings.TrimSpace(req.GetClientMsgId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "client_msg_id is required")
	}

	isMember, roomOpen, err := s.store.MembershipState(ctx, req.GetRoomId(), req.GetSenderAccountId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "send message: %v", err)
	}
	if !isMember {
		return nil, status.Error(codes.PermissionDenied, "not a member of this room")
	}
	// An ended room can be read but takes no more messages: a chat saved
	// after the other person skipped, or one closed with "stop messaging".
	if !roomOpen {
		return nil, status.Error(codes.FailedPrecondition, "this conversation is closed")
	}

	candidate := dedup.Result{MessageID: s.genID(), SentAtUnix: time.Now().Unix()}
	result, claimed, err := s.dedup.Claim(ctx, req.GetRoomId(), req.GetSenderAccountId(), req.GetClientMsgId(), candidate)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "send message: dedup: %v", err)
	}
	if !claimed {
		// A retry of a call we already produced for - DEC-0047's
		// "never recorded twice" - so the Kafka event isn't produced
		// again; we just hand back the original result.
		return &chatv1.SendMessageResponse{MessageId: result.MessageID, SentAtUnix: result.SentAtUnix}, nil
	}

	if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_MessageSent{
		MessageSent: &eventsv1.MessageSent{
			MessageId:       result.MessageID,
			RoomId:          req.GetRoomId(),
			SenderAccountId: req.GetSenderAccountId(),
			SenderPseudonym: req.GetSenderPseudonym(),
			Body:            req.GetBody(),
			SentAtUnix:      result.SentAtUnix,
		},
	}); err != nil {
		// DEC-0047: a failed produce must fail loudly rather than tell
		// the caller a message was sent that nobody will ever see.
		return nil, status.Errorf(codes.Internal, "send message: %v", err)
	}
	return &chatv1.SendMessageResponse{MessageId: result.MessageID, SentAtUnix: result.SentAtUnix}, nil
}

func (s *Server) VoteKeep(ctx context.Context, req *chatv1.VoteKeepRequest) (*chatv1.VoteKeepResponse, error) {
	isMember, err := s.store.IsCurrentMember(ctx, req.GetRoomId(), req.GetAccountId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "vote keep: %v", err)
	}
	if !isMember {
		return nil, status.Error(codes.PermissionDenied, "not a member of this room")
	}

	kept, err := s.store.VoteKeep(ctx, req.GetRoomId(), req.GetAccountId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "room not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "vote keep: %v", err)
	}

	if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_KeepVote{
		KeepVote: &eventsv1.KeepVote{RoomId: req.GetRoomId(), AccountId: req.GetAccountId(), Kept: kept},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "vote keep: %v", err)
	}
	if kept {
		if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_RoomKept{
			RoomKept: &eventsv1.RoomKept{RoomId: req.GetRoomId()},
		}); err != nil {
			return nil, status.Errorf(codes.Internal, "vote keep: %v", err)
		}
	}
	return &chatv1.VoteKeepResponse{Kept: kept}, nil
}

func (s *Server) EndDirectRoom(ctx context.Context, req *chatv1.EndDirectRoomRequest) (*chatv1.EndDirectRoomResponse, error) {
	isMember, err := s.store.IsCurrentMember(ctx, req.GetRoomId(), req.GetAccountId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "end direct room: %v", err)
	}
	if !isMember {
		return nil, status.Error(codes.PermissionDenied, "not a member of this room")
	}

	if err := s.store.EndDirectRoom(ctx, req.GetRoomId()); err != nil {
		return nil, status.Errorf(codes.Internal, "end direct room: %v", err)
	}
	if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_RoomEnded{
		RoomEnded: &eventsv1.RoomEnded{RoomId: req.GetRoomId(), Reason: req.GetReason(), EndedByAccountId: req.GetAccountId()},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "end direct room: %v", err)
	}
	return &chatv1.EndDirectRoomResponse{}, nil
}

// RemoveMember currently only implements the room owner removing a
// member. docs/ARCHITECTURE.md §5.3 also describes the admin tool using
// this same RPC to act on a ban, but tools/admin does not exist yet
// (T9 in .claude/build-plan.md) and the contract has no field to tell
// the two callers apart - that authorization gap is deferred until the
// admin tool is actually built, rather than guessed at now.
// CloseDirectRoom stops further messages in a direct room, kept or not
// ("stop messaging" in the Inbox, DEC-0106). Everyone can still read it.
func (s *Server) CloseDirectRoom(ctx context.Context, req *chatv1.CloseDirectRoomRequest) (*chatv1.CloseDirectRoomResponse, error) {
	isMember, err := s.store.IsCurrentMember(ctx, req.GetRoomId(), req.GetAccountId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "close direct room: %v", err)
	}
	if !isMember {
		return nil, status.Error(codes.PermissionDenied, "not a member of this room")
	}
	room, err := s.store.GetRoom(ctx, req.GetRoomId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "room not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "close direct room: %v", err)
	}
	if room.Type != chatv1.RoomType_ROOM_TYPE_DIRECT {
		return nil, status.Error(codes.InvalidArgument, "only a direct conversation can be closed this way")
	}
	if room.EndedAt != nil {
		return &chatv1.CloseDirectRoomResponse{}, nil
	}

	if err := s.store.CloseDirectRoom(ctx, req.GetRoomId()); err != nil {
		return nil, status.Errorf(codes.Internal, "close direct room: %v", err)
	}
	if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_RoomEnded{
		RoomEnded: &eventsv1.RoomEnded{RoomId: req.GetRoomId(), Reason: "closed", EndedByAccountId: req.GetAccountId()},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "close direct room: %v", err)
	}
	return &chatv1.CloseDirectRoomResponse{}, nil
}

func (s *Server) RemoveMember(ctx context.Context, req *chatv1.RemoveMemberRequest) (*chatv1.RemoveMemberResponse, error) {
	room, err := s.store.GetRoom(ctx, req.GetRoomId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "room not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "remove member: %v", err)
	}
	if room.OwnerAccountID == nil || *room.OwnerAccountID != req.GetRequestedByAccountId() {
		return nil, status.Error(codes.PermissionDenied, "only the room's owner can remove a member")
	}

	if err := s.store.RemoveMember(ctx, req.GetRoomId(), req.GetAccountId()); err != nil {
		return nil, status.Errorf(codes.Internal, "remove member: %v", err)
	}
	if err := s.publish(ctx, req.GetRoomId(), &eventsv1.ChatEvent_MemberLeft{
		MemberLeft: &eventsv1.MemberLeft{RoomId: req.GetRoomId(), AccountId: req.GetAccountId()},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "remove member: %v", err)
	}
	return &chatv1.RemoveMemberResponse{}, nil
}

// publish wraps payload (one of the eventsv1.ChatEvent_* oneof wrapper
// types) in a fresh envelope and produces it to chat.events keyed by
// roomID. payload is typed any, not eventsv1's own oneof interface,
// because that interface's marker method is unexported and so cannot be
// named from this package - the type switch below is what actually
// enforces which types are accepted.
func (s *Server) publish(ctx context.Context, roomID string, payload any) error {
	event := &eventsv1.ChatEvent{
		EventId:        s.genID(),
		OccurredAtUnix: time.Now().Unix(),
	}
	switch p := payload.(type) {
	case *eventsv1.ChatEvent_MessageSent:
		event.Payload = p
	case *eventsv1.ChatEvent_MemberJoined:
		event.Payload = p
	case *eventsv1.ChatEvent_MemberLeft:
		event.Payload = p
	case *eventsv1.ChatEvent_KeepVote:
		event.Payload = p
	case *eventsv1.ChatEvent_RoomKept:
		event.Payload = p
	case *eventsv1.ChatEvent_RoomEnded:
		event.Payload = p
	default:
		return fmt.Errorf("publish: unhandled payload type %T", payload)
	}
	return s.producer.Publish(ctx, roomID, event)
}

func toProtoRoom(r store.Room) *chatv1.Room {
	room := &chatv1.Room{
		Id:            r.ID,
		Type:          r.Type,
		Kept:          r.Kept,
		CreatedAtUnix: r.CreatedAt.Unix(),
	}
	if r.Name != nil {
		room.Name = *r.Name
	}
	if r.OwnerAccountID != nil {
		room.OwnerAccountId = *r.OwnerAccountID
	}
	if r.InviteCode != nil {
		room.InviteCode = *r.InviteCode
	}
	if r.EndedAt != nil {
		room.EndedAtUnix = r.EndedAt.Unix()
	}
	room.MemberAccountIds = r.MemberAccountIDs
	return room
}

func toProtoMessage(m store.Message) *chatv1.Message {
	return &chatv1.Message{
		MessageId:       m.MessageID,
		RoomId:          m.RoomID,
		SenderAccountId: m.SenderAccountID,
		SenderPseudonym: m.SenderPseudonym,
		Body:            m.Body,
		SentAtUnix:      m.SentAt.Unix(),
	}
}
