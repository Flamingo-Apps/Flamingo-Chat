package ws

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
)

var errNotConfigured = status.Error(codes.Unimplemented, "fake: method not configured for this test")

// fakeIdentity implements identityv1.IdentityServiceClient - see
// internal/httpapi's identical pattern for why only configured methods
// do anything.
type fakeIdentity struct {
	getAccount   func(context.Context, *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error)
	touchAccount func(context.Context, *identityv1.TouchAccountRequest) (*identityv1.TouchAccountResponse, error)
}

func (f *fakeIdentity) CreateAccount(context.Context, *identityv1.CreateAccountRequest, ...grpc.CallOption) (*identityv1.CreateAccountResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeIdentity) GetAccount(ctx context.Context, in *identityv1.GetAccountRequest, _ ...grpc.CallOption) (*identityv1.GetAccountResponse, error) {
	if f.getAccount == nil {
		return nil, errNotConfigured
	}
	return f.getAccount(ctx, in)
}

func (f *fakeIdentity) UpdatePseudonym(context.Context, *identityv1.UpdatePseudonymRequest, ...grpc.CallOption) (*identityv1.UpdatePseudonymResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeIdentity) SignInWithGoogle(context.Context, *identityv1.SignInWithGoogleRequest, ...grpc.CallOption) (*identityv1.SignInWithGoogleResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeIdentity) SetGender(context.Context, *identityv1.SetGenderRequest, ...grpc.CallOption) (*identityv1.SetGenderResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeIdentity) AdminSetGender(context.Context, *identityv1.AdminSetGenderRequest, ...grpc.CallOption) (*identityv1.AdminSetGenderResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeIdentity) TouchAccount(ctx context.Context, in *identityv1.TouchAccountRequest, _ ...grpc.CallOption) (*identityv1.TouchAccountResponse, error) {
	if f.touchAccount == nil {
		return &identityv1.TouchAccountResponse{}, nil
	}
	return f.touchAccount(ctx, in)
}

// fakeChat implements chatv1.ChatServiceClient.
type fakeChat struct {
	listMyRooms   func(context.Context, *chatv1.ListMyRoomsRequest) (*chatv1.ListMyRoomsResponse, error)
	sendMessage   func(context.Context, *chatv1.SendMessageRequest) (*chatv1.SendMessageResponse, error)
	voteKeep      func(context.Context, *chatv1.VoteKeepRequest) (*chatv1.VoteKeepResponse, error)
	endDirectRoom func(context.Context, *chatv1.EndDirectRoomRequest) (*chatv1.EndDirectRoomResponse, error)
}

func (f *fakeChat) CloseDirectRoom(context.Context, *chatv1.CloseDirectRoomRequest, ...grpc.CallOption) (*chatv1.CloseDirectRoomResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) CreateGroupRoom(context.Context, *chatv1.CreateGroupRoomRequest, ...grpc.CallOption) (*chatv1.CreateGroupRoomResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) CreateDirectRoom(context.Context, *chatv1.CreateDirectRoomRequest, ...grpc.CallOption) (*chatv1.CreateDirectRoomResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) JoinRoomByCode(context.Context, *chatv1.JoinRoomByCodeRequest, ...grpc.CallOption) (*chatv1.JoinRoomByCodeResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) LeaveRoom(context.Context, *chatv1.LeaveRoomRequest, ...grpc.CallOption) (*chatv1.LeaveRoomResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) RotateInviteCode(context.Context, *chatv1.RotateInviteCodeRequest, ...grpc.CallOption) (*chatv1.RotateInviteCodeResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) ListMyRooms(ctx context.Context, in *chatv1.ListMyRoomsRequest, _ ...grpc.CallOption) (*chatv1.ListMyRoomsResponse, error) {
	if f.listMyRooms == nil {
		return &chatv1.ListMyRoomsResponse{}, nil
	}
	return f.listMyRooms(ctx, in)
}

func (f *fakeChat) ListMessages(context.Context, *chatv1.ListMessagesRequest, ...grpc.CallOption) (*chatv1.ListMessagesResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) SendMessage(ctx context.Context, in *chatv1.SendMessageRequest, _ ...grpc.CallOption) (*chatv1.SendMessageResponse, error) {
	if f.sendMessage == nil {
		return nil, errNotConfigured
	}
	return f.sendMessage(ctx, in)
}

func (f *fakeChat) VoteKeep(ctx context.Context, in *chatv1.VoteKeepRequest, _ ...grpc.CallOption) (*chatv1.VoteKeepResponse, error) {
	if f.voteKeep == nil {
		return nil, errNotConfigured
	}
	return f.voteKeep(ctx, in)
}

func (f *fakeChat) EndDirectRoom(ctx context.Context, in *chatv1.EndDirectRoomRequest, _ ...grpc.CallOption) (*chatv1.EndDirectRoomResponse, error) {
	if f.endDirectRoom == nil {
		return nil, errNotConfigured
	}
	return f.endDirectRoom(ctx, in)
}

func (f *fakeChat) RemoveMember(context.Context, *chatv1.RemoveMemberRequest, ...grpc.CallOption) (*chatv1.RemoveMemberResponse, error) {
	return nil, errNotConfigured
}

// fakeMatching implements matchingv1.MatchingServiceClient.
type fakeMatching struct {
	requestMatch func(context.Context, *matchingv1.RequestMatchRequest) (*matchingv1.RequestMatchResponse, error)
	cancelMatch  func(context.Context, *matchingv1.CancelMatchRequest) (*matchingv1.CancelMatchResponse, error)
	next         func(context.Context, *matchingv1.NextRequest) (*matchingv1.NextResponse, error)
}

func (f *fakeMatching) RequestMatch(ctx context.Context, in *matchingv1.RequestMatchRequest, _ ...grpc.CallOption) (*matchingv1.RequestMatchResponse, error) {
	if f.requestMatch == nil {
		return nil, errNotConfigured
	}
	return f.requestMatch(ctx, in)
}

func (f *fakeMatching) Next(ctx context.Context, in *matchingv1.NextRequest, _ ...grpc.CallOption) (*matchingv1.NextResponse, error) {
	if f.next == nil {
		return nil, errNotConfigured
	}
	return f.next(ctx, in)
}

func (f *fakeMatching) CancelMatch(ctx context.Context, in *matchingv1.CancelMatchRequest, _ ...grpc.CallOption) (*matchingv1.CancelMatchResponse, error) {
	if f.cancelMatch == nil {
		return nil, errNotConfigured
	}
	return f.cancelMatch(ctx, in)
}

// fakeSessions mirrors internal/httpapi's identical fake.
type fakeSessions struct {
	verifyErr error
}

func (f *fakeSessions) Issue(accountID string) (string, error) {
	return "session-for-" + accountID, nil
}

func (f *fakeSessions) Verify(tokenString string) (string, bool, error) {
	if f.verifyErr != nil {
		return "", false, f.verifyErr
	}
	const prefix = "session-for-"
	if len(tokenString) <= len(prefix) || tokenString[:len(prefix)] != prefix {
		return "", false, status.Error(codes.Unauthenticated, "fake: malformed test token")
	}
	return tokenString[len(prefix):], false, nil
}

// fakeRateLimiter always allows.
type fakeRateLimiter struct{}

func (f *fakeRateLimiter) Allow(context.Context, string, string, int, time.Duration) (bool, error) {
	return true, nil
}

// fakeBanned implements BannedChecker, always reporting not banned
// unless banned is set.
type fakeBanned struct {
	banned bool
}

func (f *fakeBanned) IsBanned(context.Context, string) (bool, error) {
	return f.banned, nil
}
