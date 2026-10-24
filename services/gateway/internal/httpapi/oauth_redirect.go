package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
)

// oauthStateTTL bounds how long a user has between clicking "Sign in
// with Google" and actually completing Google's consent screen - well
// past any real delay, short enough that an abandoned state key doesn't
// linger in Redis.
const oauthStateTTL = 10 * time.Minute

// GoogleOAuthConfig is gateway's own OAuth client identity for the
// server-side redirect flow (DEC-0092) - ClientID must match the
// COLLEGE_OAUTH_CLIENT_ID identity verifies ID token audiences against,
// since they're the same registered OAuth client. ClientSecret never
// reaches this struct: it's known only to the Exchanger that uses it to
// talk to Google's token endpoint.
type GoogleOAuthConfig struct {
	ClientID    string
	RedirectURI string
	// FrontendURL is where handleGoogleCallback sends the browser back
	// to, success or failure - the one place a user-controlled
	// "return_to" would be an open-redirect risk, so this is a single
	// server-configured value instead.
	FrontendURL string
}

// OAuthStateStore is the minimal Redis-backed capability
// handleGoogleStart and handleGoogleCallback need: stash a value keyed
// by a random, one-time state, and retrieve-and-delete it exactly once -
// the retrieve step also being the CSRF check, since only a state this
// handler itself generated and stored will be found.
type OAuthStateStore interface {
	Set(ctx context.Context, state, guestAccountID string, ttl time.Duration) error
	// TakeAndDelete reports found=false for a state that was never
	// stored, already consumed, or expired - all three are "reject the
	// callback," not errors worth telling apart.
	TakeAndDelete(ctx context.Context, state string) (guestAccountID string, found bool, err error)
}

// RedisOAuthState is the real OAuthStateStore, backed by Redis's GETDEL
// (atomic get-and-delete, so two callbacks racing on the same state
// can't both succeed).
type RedisOAuthState struct {
	client *redis.Client
}

func NewRedisOAuthState(client *redis.Client) *RedisOAuthState {
	return &RedisOAuthState{client: client}
}

func oauthStateKey(state string) string { return "oauth_state:" + state }

func (r *RedisOAuthState) Set(ctx context.Context, state, guestAccountID string, ttl time.Duration) error {
	return r.client.Set(ctx, oauthStateKey(state), guestAccountID, ttl).Err()
}

func (r *RedisOAuthState) TakeAndDelete(ctx context.Context, state string) (string, bool, error) {
	guestAccountID, err := r.client.GetDel(ctx, oauthStateKey(state)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return guestAccountID, true, nil
}

// handleGoogleStart begins the server-side OAuth redirect flow
// (DEC-0092): generates a one-time state value, stashes it (plus an
// optional guest account to upgrade in place), and sends the browser to
// Google's consent screen. This exists alongside handleAuthGoogle (the
// original in-page-widget flow) because the widget approach needs
// Google to trust whatever page it's embedded on; this one instead only
// needs Google to trust the one page - flamingo-api.aranyadutta.dev - that
// actually talks to it, regardless of where the rest of the app is
// hosted.
func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		writeError(w, fmt.Errorf("generate state: %w", err))
		return
	}

	var guestAccountID string
	if guestToken := r.URL.Query().Get("guest_token"); guestToken != "" {
		if accountID, _, err := s.sessions.Verify(guestToken); err == nil {
			guestAccountID = accountID
		}
		// An invalid or expired guest_token just means no guest account
		// to upgrade, the same as handleAuthGoogle treats a missing
		// Authorization header - not an error on this endpoint.
	}

	if err := s.oauthState.Set(r.Context(), state, guestAccountID, oauthStateTTL); err != nil {
		writeError(w, fmt.Errorf("store oauth state: %w", err))
		return
	}

	// In dev-fake mode (googleauth.Fake, DEC-0092 mirroring identity's
	// own DEV_FAKE_GOOGLE/DEC-0049), there is no real Google to redirect
	// to - go straight to this gateway's own callback with a fabricated
	// code, so the whole start-to-callback round trip is exercisable
	// from a browser on localhost, no real Google credentials needed.
	// dev_fake_account lets interactive testing pick which identity to
	// sign in as (e.g. two browser sessions wanting two different
	// accounts); it's ignored entirely outside dev-fake mode.
	if s.devFakeGoogle {
		fakeAccount := r.URL.Query().Get("dev_fake_account")
		if fakeAccount == "" {
			fakeAccount = "dev-" + state[:8] + ":dev-" + state[:8] + "@kiit.ac.in"
		}
		// Built through url.Values, not string concatenation - an
		// unescaped dev_fake_account (attacker- or fat-finger-supplied)
		// could otherwise inject extra query params or break out of the
		// intended URL shape (gosec G710).
		q := url.Values{"code": {"fake:" + fakeAccount}, "state": {state}}
		http.Redirect(w, r, s.oauthConfig.RedirectURI+"?"+q.Encode(), http.StatusFound)
		return
	}

	http.Redirect(w, r, s.googleAuthorizeURL(state), http.StatusFound)
}

func (s *Server) googleAuthorizeURL(state string) string {
	q := url.Values{
		"client_id":     {s.oauthConfig.ClientID},
		"redirect_uri":  {s.oauthConfig.RedirectURI},
		"response_type": {"code"},
		"scope":         {"openid email"},
		"state":         {state},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}

// handleGoogleCallback is where Google redirects the browser back to
// after the user picks an account. Every failure path redirects to the
// frontend with #auth_error instead of rendering an error page here -
// this handler's own page was never meant to be looked at, only passed
// through.
func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		s.redirectWithError(w, r, "missing_code_or_state")
		return
	}

	// TakeAndDelete is also the CSRF check: only a state this same
	// handleGoogleStart call generated and stored will be found, and it
	// can only be consumed once.
	guestAccountID, found, err := s.oauthState.TakeAndDelete(r.Context(), state)
	if err != nil || !found {
		s.redirectWithError(w, r, "invalid_or_expired_state")
		return
	}

	idToken, err := s.exchanger.Exchange(r.Context(), code)
	if err != nil {
		s.redirectWithError(w, r, "google_exchange_failed")
		return
	}

	sessionToken, _, err := s.signInWithGoogle(r.Context(), idToken, guestAccountID)
	if err != nil {
		s.redirectWithError(w, r, "sign_in_failed")
		return
	}

	// The session token travels in the URL fragment, not a query
	// param - a fragment is never sent to any server (this one
	// included, past this redirect) and never appears in access logs,
	// unlike the guest_token query param handleGoogleStart reads, which
	// is short-lived and far lower stakes.
	dest := s.oauthConfig.FrontendURL + "/#auth_token=" + url.QueryEscape(sessionToken)
	http.Redirect(w, r, dest, http.StatusFound)
}

func (s *Server) redirectWithError(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, s.oauthConfig.FrontendURL+"/#auth_error="+url.QueryEscape(reason), http.StatusFound)
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
