package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

type testServer struct {
	identity   *fakeIdentity
	chat       *fakeChat
	moderation *fakeModeration
	banned     *fakeBanned
	oauthState *fakeOAuthState
	mux        *http.ServeMux
}

func newTestServer() *testServer {
	ts := &testServer{
		identity:   &fakeIdentity{},
		chat:       &fakeChat{},
		moderation: &fakeModeration{},
		banned:     &fakeBanned{},
		oauthState: newFakeOAuthState(),
	}
	s := NewServer(
		ts.identity, ts.chat, &fakePresence{}, ts.moderation,
		&fakeSessions{}, &fakeRateLimiter{}, ts.banned, &fakeTurnstile{ok: true},
		&fakeExchanger{}, ts.oauthState,
		GoogleOAuthConfig{ClientID: "test-client-id", RedirectURI: "http://localhost:8080/v1/auth/google/callback", FrontendURL: "http://localhost:8000"},
		false,
	)
	ts.mux = http.NewServeMux()
	s.Routes(ts.mux)
	return ts
}

func (ts *testServer) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestHandleAuthGuestSuccess(t *testing.T) {
	ts := newTestServer()
	ts.identity.createAccount = func(_ context.Context, req *identityv1.CreateAccountRequest) (*identityv1.CreateAccountResponse, error) {
		if req.GetPseudonym() != "shadow_fox" {
			t.Fatalf("unexpected pseudonym: %q", req.GetPseudonym())
		}
		return &identityv1.CreateAccountResponse{Account: &identityv1.Account{Id: "acc-1", Pseudonym: "shadow_fox"}}, nil
	}

	rec := ts.do(t, http.MethodPost, "/v1/auth/guest", "", map[string]string{"pseudonym": "shadow_fox", "turnstile_token": "tok"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody[struct {
		SessionToken string          `json:"session_token"`
		Account      accountResponse `json:"account"`
	}](t, rec)
	if resp.SessionToken == "" || resp.Account.ID != "acc-1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleAuthGuestRejectsFailedTurnstile(t *testing.T) {
	ts := newTestServer()
	s := NewServer(ts.identity, ts.chat, &fakePresence{}, ts.moderation, &fakeSessions{}, &fakeRateLimiter{}, ts.banned, &fakeTurnstile{ok: false}, &fakeExchanger{}, ts.oauthState, GoogleOAuthConfig{ClientID: "test-client-id", RedirectURI: "http://localhost:8080/v1/auth/google/callback", FrontendURL: "http://localhost:8000"}, false)
	mux := http.NewServeMux()
	s.Routes(mux)
	ts.mux = mux

	rec := ts.do(t, http.MethodPost, "/v1/auth/guest", "", map[string]string{"pseudonym": "shadow_fox", "turnstile_token": "bad"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a failed Turnstile check, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthGuestRejectsOverRateLimit(t *testing.T) {
	ts := newTestServer()
	s := NewServer(ts.identity, ts.chat, &fakePresence{}, ts.moderation, &fakeSessions{}, &fakeRateLimiter{deny: true}, ts.banned, &fakeTurnstile{ok: true}, &fakeExchanger{}, ts.oauthState, GoogleOAuthConfig{ClientID: "test-client-id", RedirectURI: "http://localhost:8080/v1/auth/google/callback", FrontendURL: "http://localhost:8000"}, false)
	mux := http.NewServeMux()
	s.Routes(mux)
	ts.mux = mux

	rec := ts.do(t, http.MethodPost, "/v1/auth/guest", "", map[string]string{"pseudonym": "shadow_fox"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthGooglePassesAlongAnExistingGuestSession(t *testing.T) {
	ts := newTestServer()
	var gotGuestID string
	ts.identity.signInWithGoogle = func(_ context.Context, req *identityv1.SignInWithGoogleRequest) (*identityv1.SignInWithGoogleResponse, error) {
		gotGuestID = req.GetGuestAccountId()
		return &identityv1.SignInWithGoogleResponse{
			Account:     &identityv1.Account{Id: "acc-2", Pseudonym: "shadow_fox"},
			NeedsGender: true,
		}, nil
	}

	rec := ts.do(t, http.MethodPost, "/v1/auth/google", "session-for-acc-1", map[string]string{"id_token": "tok"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotGuestID != "acc-1" {
		t.Fatalf("expected the guest session's account id to be passed along, got %q", gotGuestID)
	}
	resp := decodeBody[struct {
		NeedsGender bool `json:"needs_gender"`
	}](t, rec)
	if !resp.NeedsGender {
		t.Fatalf("expected needs_gender to be true")
	}
}

func TestHandleAuthGoogleRejectsBannedAccount(t *testing.T) {
	ts := newTestServer()
	ts.identity.signInWithGoogle = func(context.Context, *identityv1.SignInWithGoogleRequest) (*identityv1.SignInWithGoogleResponse, error) {
		return &identityv1.SignInWithGoogleResponse{Account: &identityv1.Account{Id: "acc-banned"}}, nil
	}
	ts.banned.banned = true

	rec := ts.do(t, http.MethodPost, "/v1/auth/google", "", map[string]string{"id_token": "tok"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a banned account, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWithAuth_RejectsBannedAccount(t *testing.T) {
	ts := newTestServer()
	ts.banned.banned = true

	rec := ts.do(t, http.MethodGet, "/v1/me", "session-for-acc-1", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a banned account, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetMeRequiresAuth(t *testing.T) {
	ts := newTestServer()
	rec := ts.do(t, http.MethodGet, "/v1/me", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no Authorization header, got %d", rec.Code)
	}

	rec = ts.do(t, http.MethodGet, "/v1/me", "not-a-real-token", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a malformed token, got %d", rec.Code)
	}
}

func TestHandleGetMeSuccess(t *testing.T) {
	ts := newTestServer()
	ts.identity.getAccount = func(_ context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
		if req.GetAccountId() != "acc-1" {
			t.Fatalf("unexpected account id: %q", req.GetAccountId())
		}
		return &identityv1.GetAccountResponse{Account: &identityv1.Account{Id: "acc-1", Pseudonym: "shadow_fox", Gender: identityv1.Gender_GENDER_MALE}}, nil
	}

	rec := ts.do(t, http.MethodGet, "/v1/me", "session-for-acc-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody[accountResponse](t, rec)
	if resp.ID != "acc-1" || resp.Gender != "male" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestErrorMappingReflectsGRPCCode(t *testing.T) {
	ts := newTestServer()
	ts.identity.getAccount = func(context.Context, *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
		return nil, status.Error(codes.NotFound, "account not found")
	}

	rec := ts.do(t, http.MethodGet, "/v1/me", "session-for-acc-1", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a NotFound gRPC error, got %d", rec.Code)
	}
	resp := decodeBody[errorResponse](t, rec)
	if resp.Error.Code != "not_found" || resp.Error.Message != "account not found" {
		t.Fatalf("unexpected error body: %+v", resp)
	}
}

func TestHandleCreateRoomEnforcesRateLimit(t *testing.T) {
	ts := newTestServer()
	s := NewServer(ts.identity, ts.chat, &fakePresence{}, ts.moderation, &fakeSessions{}, &fakeRateLimiter{deny: true}, ts.banned, &fakeTurnstile{ok: true}, &fakeExchanger{}, ts.oauthState, GoogleOAuthConfig{ClientID: "test-client-id", RedirectURI: "http://localhost:8080/v1/auth/google/callback", FrontendURL: "http://localhost:8000"}, false)
	mux := http.NewServeMux()
	s.Routes(mux)
	ts.mux = mux

	rec := ts.do(t, http.MethodPost, "/v1/rooms", "session-for-acc-1", map[string]string{"name": "Study group"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateRoomSuccess(t *testing.T) {
	ts := newTestServer()
	ts.chat.createGroupRoom = func(_ context.Context, req *chatv1.CreateGroupRoomRequest) (*chatv1.CreateGroupRoomResponse, error) {
		if req.GetOwnerAccountId() != "acc-1" || req.GetName() != "Study group" {
			t.Fatalf("unexpected request: %+v", req)
		}
		return &chatv1.CreateGroupRoomResponse{Room: &chatv1.Room{Id: "room-1", Type: chatv1.RoomType_ROOM_TYPE_GROUP, Name: "Study group"}}, nil
	}

	rec := ts.do(t, http.MethodPost, "/v1/rooms", "session-for-acc-1", map[string]string{"name": "Study group"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody[roomResponse](t, rec)
	if resp.ID != "room-1" || resp.Type != "group" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleListMessagesPassesQueryParameters(t *testing.T) {
	ts := newTestServer()
	ts.chat.listMessages = func(_ context.Context, req *chatv1.ListMessagesRequest) (*chatv1.ListMessagesResponse, error) {
		if req.GetRoomId() != "room-1" || req.GetBeforeMessageId() != "msg-5" || req.GetLimit() != 10 {
			t.Fatalf("unexpected request: %+v", req)
		}
		return &chatv1.ListMessagesResponse{Messages: []*chatv1.Message{{MessageId: "msg-1", Body: "hi"}}}, nil
	}

	rec := ts.do(t, http.MethodGet, "/v1/rooms/room-1/messages?before=msg-5&limit=10", "session-for-acc-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody[struct {
		Messages []messageResponse `json:"messages"`
	}](t, rec)
	if len(resp.Messages) != 1 || resp.Messages[0].MessageID != "msg-1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleFileReportEnforcesRateLimit(t *testing.T) {
	ts := newTestServer()
	s := NewServer(ts.identity, ts.chat, &fakePresence{}, ts.moderation, &fakeSessions{}, &fakeRateLimiter{deny: true}, ts.banned, &fakeTurnstile{ok: true}, &fakeExchanger{}, ts.oauthState, GoogleOAuthConfig{ClientID: "test-client-id", RedirectURI: "http://localhost:8080/v1/auth/google/callback", FrontendURL: "http://localhost:8000"}, false)
	mux := http.NewServeMux()
	s.Routes(mux)
	ts.mux = mux

	rec := ts.do(t, http.MethodPost, "/v1/reports", "session-for-acc-1", map[string]string{"reported_account_id": "acc-2"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileReportSuccess(t *testing.T) {
	ts := newTestServer()
	ts.moderation.fileReport = func(_ context.Context, req *moderationv1.FileReportRequest) (*moderationv1.FileReportResponse, error) {
		if req.GetReporterAccountId() != "acc-1" || req.GetReportedAccountId() != "acc-2" {
			t.Fatalf("unexpected request: %+v", req)
		}
		return &moderationv1.FileReportResponse{ReportId: "report-1"}, nil
	}

	rec := ts.do(t, http.MethodPost, "/v1/reports", "session-for-acc-1", map[string]string{"reported_account_id": "acc-2", "reason": "spam"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
