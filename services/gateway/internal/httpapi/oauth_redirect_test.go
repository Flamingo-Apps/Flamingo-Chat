package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

func TestHandleGoogleStartRedirectsToGoogleWithState(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/start", nil)
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Host != "accounts.google.com" {
		t.Fatalf("expected a redirect to accounts.google.com, got %s", loc)
	}
	if loc.Query().Get("client_id") != "test-client-id" {
		t.Fatalf("expected client_id=test-client-id, got %+v", loc.Query())
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("expected a non-empty state param")
	}
	if _, found := ts.oauthState.stored[state]; !found {
		t.Fatal("expected the state to have been stored server-side")
	}
}

func TestHandleGoogleStartStoresGuestAccountIDWhenGuestTokenValid(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/start?guest_token=session-for-acc-1", nil)
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	loc, _ := url.Parse(rec.Header().Get("Location"))
	state := loc.Query().Get("state")
	if got := ts.oauthState.stored[state]; got != "acc-1" {
		t.Fatalf("expected the stored state to carry guest account acc-1, got %q", got)
	}
}

func TestHandleGoogleStartIgnoresInvalidGuestToken(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/start?guest_token=garbage", nil)
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 even with an invalid guest_token, got %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	state := loc.Query().Get("state")
	if got, found := ts.oauthState.stored[state]; !found || got != "" {
		t.Fatalf("expected an empty guest account id for an invalid guest_token, got %q (found=%v)", got, found)
	}
}

func TestHandleGoogleCallbackCompletesSignInAndRedirectsWithToken(t *testing.T) {
	ts := newTestServer()
	ts.oauthState.stored["good-state"] = ""
	ts.identity.signInWithGoogle = func(_ context.Context, in *identityv1.SignInWithGoogleRequest) (*identityv1.SignInWithGoogleResponse, error) {
		if in.GetIdToken() != "fake:sub-1:a@kiit.ac.in" {
			t.Fatalf("expected the exchanged id_token to reach identity, got %q", in.GetIdToken())
		}
		return &identityv1.SignInWithGoogleResponse{
			Account: &identityv1.Account{Id: "acc-1", Pseudonym: "Fox"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback?code=fake:sub-1:a@kiit.ac.in&state=good-state", nil)
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "http://localhost:8000/#auth_token=") {
		t.Fatalf("expected a redirect back to the frontend carrying a session in the fragment, got %q", loc)
	}
	if _, found := ts.oauthState.stored["good-state"]; found {
		t.Fatal("expected the state to be consumed (deleted) after a successful callback")
	}
}

func TestHandleGoogleCallbackRejectsUnknownState(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback?code=whatever&state=never-issued", nil)
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "#auth_error=") {
		t.Fatalf("expected an #auth_error redirect for an unknown state, got %q", loc)
	}
}

// TestHandleGoogleStartInDevFakeModeSkipsRealGoogle proves the whole
// start-to-callback round trip is exercisable without ever reaching
// accounts.google.com, the same way identity's DEV_FAKE_GOOGLE already
// lets SignInWithGoogle be tested without a real token (DEC-0049,
// mirrored here as DEC-0092).
func TestHandleGoogleStartInDevFakeModeSkipsRealGoogle(t *testing.T) {
	identity := &fakeIdentity{}
	var gotIDToken string
	identity.signInWithGoogle = func(_ context.Context, in *identityv1.SignInWithGoogleRequest) (*identityv1.SignInWithGoogleResponse, error) {
		gotIDToken = in.GetIdToken()
		return &identityv1.SignInWithGoogleResponse{Account: &identityv1.Account{Id: "acc-1"}}, nil
	}
	oauthState := newFakeOAuthState()
	s := NewServer(
		identity, &fakeChat{}, &fakePresence{}, &fakeModeration{},
		&fakeSessions{}, &fakeRateLimiter{}, &fakeBanned{}, &fakeTurnstile{ok: true},
		&fakeExchanger{}, oauthState,
		GoogleOAuthConfig{ClientID: "test-client-id", RedirectURI: "http://localhost:8080/v1/auth/google/callback", FrontendURL: "http://localhost:8000"},
		true, // devFakeGoogle
	)
	mux := http.NewServeMux()
	s.Routes(mux)

	// One request through /start should land on a 302 straight back to
	// THIS gateway's own callback, not accounts.google.com, and that
	// second hop should complete the sign-in end to end.
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/start", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	startLoc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if startLoc.Host == "accounts.google.com" {
		t.Fatal("expected dev-fake mode to skip the real Google redirect entirely")
	}
	if !strings.HasPrefix(startLoc.Path, "/v1/auth/google/callback") {
		t.Fatalf("expected a redirect straight to this gateway's own callback, got %s", startLoc)
	}

	callbackReq := httptest.NewRequest(http.MethodGet, startLoc.RequestURI(), nil)
	callbackRec := httptest.NewRecorder()
	mux.ServeHTTP(callbackRec, callbackReq)
	finalLoc := callbackRec.Header().Get("Location")
	if !strings.HasPrefix(finalLoc, "http://localhost:8000/#auth_token=") {
		t.Fatalf("expected the fake flow to complete sign-in and redirect with a session, got %q", finalLoc)
	}
	if !strings.HasPrefix(gotIDToken, "fake:") {
		t.Fatalf("expected a fake:-shaped id_token to reach identity, got %q", gotIDToken)
	}
}

func TestHandleGoogleCallbackPassesGuestAccountIDThrough(t *testing.T) {
	ts := newTestServer()
	ts.oauthState.stored["state-with-guest"] = "guest-acc-9"
	var gotGuestID string
	ts.identity.signInWithGoogle = func(_ context.Context, in *identityv1.SignInWithGoogleRequest) (*identityv1.SignInWithGoogleResponse, error) {
		gotGuestID = in.GetGuestAccountId()
		return &identityv1.SignInWithGoogleResponse{Account: &identityv1.Account{Id: "guest-acc-9"}}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback?code=fake:s:e@kiit.ac.in&state=state-with-guest", nil)
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	if gotGuestID != "guest-acc-9" {
		t.Fatalf("expected the guest account id stashed at /start to reach SignInWithGoogleRequest, got %q", gotGuestID)
	}
}
