package httpapi

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
)

var errNotConfigured = status.Error(codes.Unimplemented, "fake: method not configured for this test")

// fakeIdentity implements identityv1.IdentityServiceClient with one
// overridable function field per RPC. A test sets only the methods it
// exercises; every other call returns errNotConfigured, which also
// doubles as a correctness check - a handler calling an RPC the test
// didn't expect fails loudly instead of silently returning a zero value.
type fakeIdentity struct {
	createAccount    func(context.Context, *identityv1.CreateAccountRequest) (*identityv1.CreateAccountResponse, error)
	getAccount       func(context.Context, *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error)
	updatePseudonym  func(context.Context, *identityv1.UpdatePseudonymRequest) (*identityv1.UpdatePseudonymResponse, error)
	signInWithGoogle func(context.Context, *identityv1.SignInWithGoogleRequest) (*identityv1.SignInWithGoogleResponse, error)
	setGender        func(context.Context, *identityv1.SetGenderRequest) (*identityv1.SetGenderResponse, error)
}

func (f *fakeIdentity) CreateAccount(ctx context.Context, in *identityv1.CreateAccountRequest, _ ...grpc.CallOption) (*identityv1.CreateAccountResponse, error) {
	if f.createAccount == nil {
		return nil, errNotConfigured
	}
	return f.createAccount(ctx, in)
}

func (f *fakeIdentity) GetAccount(ctx context.Context, in *identityv1.GetAccountRequest, _ ...grpc.CallOption) (*identityv1.GetAccountResponse, error) {
	if f.getAccount == nil {
		return nil, errNotConfigured
	}
	return f.getAccount(ctx, in)
}

func (f *fakeIdentity) UpdatePseudonym(ctx context.Context, in *identityv1.UpdatePseudonymRequest, _ ...grpc.CallOption) (*identityv1.UpdatePseudonymResponse, error) {
	if f.updatePseudonym == nil {
		return nil, errNotConfigured
	}
	return f.updatePseudonym(ctx, in)
}

func (f *fakeIdentity) SignInWithGoogle(ctx context.Context, in *identityv1.SignInWithGoogleRequest, _ ...grpc.CallOption) (*identityv1.SignInWithGoogleResponse, error) {
	if f.signInWithGoogle == nil {
		return nil, errNotConfigured
	}
	return f.signInWithGoogle(ctx, in)
}

func (f *fakeIdentity) SetGender(ctx context.Context, in *identityv1.SetGenderRequest, _ ...grpc.CallOption) (*identityv1.SetGenderResponse, error) {
	if f.setGender == nil {
		return nil, errNotConfigured
	}
	return f.setGender(ctx, in)
}

func (f *fakeIdentity) AdminSetGender(context.Context, *identityv1.AdminSetGenderRequest, ...grpc.CallOption) (*identityv1.AdminSetGenderResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeIdentity) TouchAccount(context.Context, *identityv1.TouchAccountRequest, ...grpc.CallOption) (*identityv1.TouchAccountResponse, error) {
	return nil, errNotConfigured
}

// fakeChat implements chatv1.ChatServiceClient the same way.
type fakeChat struct {
	createGroupRoom  func(context.Context, *chatv1.CreateGroupRoomRequest) (*chatv1.CreateGroupRoomResponse, error)
	joinRoomByCode   func(context.Context, *chatv1.JoinRoomByCodeRequest) (*chatv1.JoinRoomByCodeResponse, error)
	leaveRoom        func(context.Context, *chatv1.LeaveRoomRequest) (*chatv1.LeaveRoomResponse, error)
	rotateInviteCode func(context.Context, *chatv1.RotateInviteCodeRequest) (*chatv1.RotateInviteCodeResponse, error)
	listMyRooms      func(context.Context, *chatv1.ListMyRoomsRequest) (*chatv1.ListMyRoomsResponse, error)
	listMessages     func(context.Context, *chatv1.ListMessagesRequest) (*chatv1.ListMessagesResponse, error)
	closeDirectRoom  func(context.Context, *chatv1.CloseDirectRoomRequest) (*chatv1.CloseDirectRoomResponse, error)
}

func (f *fakeChat) CreateGroupRoom(ctx context.Context, in *chatv1.CreateGroupRoomRequest, _ ...grpc.CallOption) (*chatv1.CreateGroupRoomResponse, error) {
	if f.createGroupRoom == nil {
		return nil, errNotConfigured
	}
	return f.createGroupRoom(ctx, in)
}

func (f *fakeChat) CreateDirectRoom(context.Context, *chatv1.CreateDirectRoomRequest, ...grpc.CallOption) (*chatv1.CreateDirectRoomResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) JoinRoomByCode(ctx context.Context, in *chatv1.JoinRoomByCodeRequest, _ ...grpc.CallOption) (*chatv1.JoinRoomByCodeResponse, error) {
	if f.joinRoomByCode == nil {
		return nil, errNotConfigured
	}
	return f.joinRoomByCode(ctx, in)
}

func (f *fakeChat) LeaveRoom(ctx context.Context, in *chatv1.LeaveRoomRequest, _ ...grpc.CallOption) (*chatv1.LeaveRoomResponse, error) {
	if f.leaveRoom == nil {
		return nil, errNotConfigured
	}
	return f.leaveRoom(ctx, in)
}

func (f *fakeChat) RotateInviteCode(ctx context.Context, in *chatv1.RotateInviteCodeRequest, _ ...grpc.CallOption) (*chatv1.RotateInviteCodeResponse, error) {
	if f.rotateInviteCode == nil {
		return nil, errNotConfigured
	}
	return f.rotateInviteCode(ctx, in)
}

func (f *fakeChat) ListMyRooms(ctx context.Context, in *chatv1.ListMyRoomsRequest, _ ...grpc.CallOption) (*chatv1.ListMyRoomsResponse, error) {
	if f.listMyRooms == nil {
		return nil, errNotConfigured
	}
	return f.listMyRooms(ctx, in)
}

func (f *fakeChat) ListMessages(ctx context.Context, in *chatv1.ListMessagesRequest, _ ...grpc.CallOption) (*chatv1.ListMessagesResponse, error) {
	if f.listMessages == nil {
		return nil, errNotConfigured
	}
	return f.listMessages(ctx, in)
}

func (f *fakeChat) SendMessage(context.Context, *chatv1.SendMessageRequest, ...grpc.CallOption) (*chatv1.SendMessageResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) VoteKeep(context.Context, *chatv1.VoteKeepRequest, ...grpc.CallOption) (*chatv1.VoteKeepResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) CloseDirectRoom(ctx context.Context, in *chatv1.CloseDirectRoomRequest, _ ...grpc.CallOption) (*chatv1.CloseDirectRoomResponse, error) {
	if f.closeDirectRoom == nil {
		return nil, errNotConfigured
	}
	return f.closeDirectRoom(ctx, in)
}

func (f *fakeChat) EndDirectRoom(context.Context, *chatv1.EndDirectRoomRequest, ...grpc.CallOption) (*chatv1.EndDirectRoomResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeChat) RemoveMember(context.Context, *chatv1.RemoveMemberRequest, ...grpc.CallOption) (*chatv1.RemoveMemberResponse, error) {
	return nil, errNotConfigured
}

// fakePresence implements presencev1.PresenceServiceClient.
type fakePresence struct {
	getLiveCounts func(context.Context, *presencev1.GetLiveCountsRequest) (*presencev1.GetLiveCountsResponse, error)
}

func (f *fakePresence) Heartbeat(context.Context, *presencev1.HeartbeatRequest, ...grpc.CallOption) (*presencev1.HeartbeatResponse, error) {
	return nil, errNotConfigured
}

func (f *fakePresence) GetLiveCounts(ctx context.Context, in *presencev1.GetLiveCountsRequest, _ ...grpc.CallOption) (*presencev1.GetLiveCountsResponse, error) {
	if f.getLiveCounts == nil {
		return nil, errNotConfigured
	}
	return f.getLiveCounts(ctx, in)
}

// fakeModeration implements moderationv1.ModerationServiceClient.
type fakeModeration struct {
	fileReport          func(context.Context, *moderationv1.FileReportRequest) (*moderationv1.FileReportResponse, error)
	blockUser           func(context.Context, *moderationv1.BlockUserRequest) (*moderationv1.BlockUserResponse, error)
	listBlocks          func(context.Context, *moderationv1.ListBlocksRequest) (*moderationv1.ListBlocksResponse, error)
	requestGenderChange func(context.Context, *moderationv1.RequestGenderChangeRequest) (*moderationv1.RequestGenderChangeResponse, error)
}

func (f *fakeModeration) FileReport(ctx context.Context, in *moderationv1.FileReportRequest, _ ...grpc.CallOption) (*moderationv1.FileReportResponse, error) {
	if f.fileReport == nil {
		return nil, errNotConfigured
	}
	return f.fileReport(ctx, in)
}

func (f *fakeModeration) BlockUser(ctx context.Context, in *moderationv1.BlockUserRequest, _ ...grpc.CallOption) (*moderationv1.BlockUserResponse, error) {
	if f.blockUser == nil {
		return nil, errNotConfigured
	}
	return f.blockUser(ctx, in)
}

func (f *fakeModeration) ListBlocks(ctx context.Context, in *moderationv1.ListBlocksRequest, _ ...grpc.CallOption) (*moderationv1.ListBlocksResponse, error) {
	if f.listBlocks == nil {
		return nil, errNotConfigured
	}
	return f.listBlocks(ctx, in)
}

func (f *fakeModeration) RequestGenderChange(ctx context.Context, in *moderationv1.RequestGenderChangeRequest, _ ...grpc.CallOption) (*moderationv1.RequestGenderChangeResponse, error) {
	if f.requestGenderChange == nil {
		return nil, errNotConfigured
	}
	return f.requestGenderChange(ctx, in)
}

func (f *fakeModeration) ListReports(context.Context, *moderationv1.ListReportsRequest, ...grpc.CallOption) (*moderationv1.ListReportsResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeModeration) ResolveReport(context.Context, *moderationv1.ResolveReportRequest, ...grpc.CallOption) (*moderationv1.ResolveReportResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeModeration) BanAccount(context.Context, *moderationv1.BanAccountRequest, ...grpc.CallOption) (*moderationv1.BanAccountResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeModeration) UnbanAccount(context.Context, *moderationv1.UnbanAccountRequest, ...grpc.CallOption) (*moderationv1.UnbanAccountResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeModeration) ListGenderChangeRequests(context.Context, *moderationv1.ListGenderChangeRequestsRequest, ...grpc.CallOption) (*moderationv1.ListGenderChangeRequestsResponse, error) {
	return nil, errNotConfigured
}

func (f *fakeModeration) ResolveGenderChangeRequest(context.Context, *moderationv1.ResolveGenderChangeRequestRequest, ...grpc.CallOption) (*moderationv1.ResolveGenderChangeRequestResponse, error) {
	return nil, errNotConfigured
}

// fakeSessions implements Sessions without any real signing - issue
// returns a token that is just "session-for-<accountID>", and verify
// parses that back out, so tests don't need a real HMAC key.
type fakeSessions struct {
	// verifyErr, if set, makes every Verify call fail - used to test the
	// withAuth middleware's rejection path.
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

// fakeRateLimiter implements RateLimiter, always allowing unless denyNext
// is set - set it to test the 429 path without needing a real Redis.
type fakeRateLimiter struct {
	deny bool
}

func (f *fakeRateLimiter) Allow(context.Context, string, string, int, time.Duration) (bool, error) {
	return !f.deny, nil
}

// fakeBanned implements BannedChecker, always reporting not banned
// unless banned is set.
type fakeBanned struct {
	banned bool
}

func (f *fakeBanned) IsBanned(context.Context, string) (bool, error) {
	return f.banned, nil
}

// fakeTurnstile implements turnstile.Verifier.
type fakeTurnstile struct {
	ok bool
}

func (f *fakeTurnstile) Verify(context.Context, string, string) (bool, error) {
	return f.ok, nil
}

// fakeExchanger implements googleauth.Exchanger. By default it returns
// the code unchanged, matching googleauth.Fake's real behavior exactly -
// so a test that also uses a fakeIdentity.signInWithGoogle wired for
// "fake:<sub>:<email>"-shaped tokens can exercise the whole redirect
// path without a real Google exchange.
type fakeExchanger struct {
	idToken string
	err     error
}

func (f *fakeExchanger) Exchange(_ context.Context, code string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.idToken != "" {
		return f.idToken, nil
	}
	return code, nil
}

// fakeOAuthState implements OAuthStateStore in memory - no TTL
// enforcement, since no test here needs to wait one out.
type fakeOAuthState struct {
	stored map[string]string
}

func newFakeOAuthState() *fakeOAuthState { return &fakeOAuthState{stored: map[string]string{}} }

func (f *fakeOAuthState) Set(_ context.Context, state, guestAccountID string, _ time.Duration) error {
	f.stored[state] = guestAccountID
	return nil
}

func (f *fakeOAuthState) TakeAndDelete(_ context.Context, state string) (string, bool, error) {
	v, ok := f.stored[state]
	delete(f.stored, state)
	return v, ok, nil
}
