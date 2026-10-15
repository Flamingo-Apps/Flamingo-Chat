package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

// withAuth requires a valid session token and passes the account id it
// authenticates to next. Unlike the WebSocket auth frame
// (docs/ARCHITECTURE.md §4.3), an HTTP request that holds an old token
// is not refreshed here - there is no documented HTTP equivalent of
// auth_ok carrying a fresh token, and inventing one isn't this
// handler's call to make.
func (s *Server) withAuth(next func(w http.ResponseWriter, r *http.Request, accountID string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, status.Error(codes.Unauthenticated, "missing or malformed Authorization header"))
			return
		}
		accountID, _, err := s.sessions.Verify(token)
		if err != nil {
			writeError(w, status.Error(codes.Unauthenticated, "invalid session token"))
			return
		}
		next(w, r, accountID)
	}
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}

// clientIP is used for rate limiting and Turnstile's remoteip field.
// This reads RemoteAddr directly rather than trusting an X-Forwarded-For
// header - which proxy, if any, sits in front of the gateway is
// infrastructure that docs/ARCHITECTURE.md §12 says is still "to be
// designed", and trusting a client-settable header for this would be a
// spoofing vector until that's settled.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) handleAuthGuest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pseudonym      string `json:"pseudonym"`
		TurnstileToken string `json:"turnstile_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}

	ip := clientIP(r)
	allowed, err := s.rateLimiter.Allow(r.Context(), "guest_signup", ip, 5, time.Hour)
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "rate limit: %v", err))
		return
	}
	if !allowed {
		writeError(w, status.Error(codes.ResourceExhausted, "too many guest signups from this address"))
		return
	}

	ok, err := s.turnstile.Verify(r.Context(), req.TurnstileToken, ip)
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "turnstile: %v", err))
		return
	}
	if !ok {
		writeError(w, status.Error(codes.InvalidArgument, "turnstile verification failed"))
		return
	}

	resp, err := s.identity.CreateAccount(r.Context(), &identityv1.CreateAccountRequest{Pseudonym: req.Pseudonym})
	if err != nil {
		writeError(w, err)
		return
	}
	issueSessionAndRespond(w, s.sessions, resp.GetAccount())
}

func (s *Server) handleAuthGoogle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}

	signInReq := &identityv1.SignInWithGoogleRequest{IdToken: req.IDToken}
	// An optional guest session upgrades that guest account in place
	// (docs/ARCHITECTURE.md §4.2's case B). A missing or invalid
	// Authorization header just means case C (create fresh) applies -
	// not an error on this call.
	if token, ok := bearerToken(r); ok {
		if accountID, _, err := s.sessions.Verify(token); err == nil {
			signInReq.GuestAccountId = &accountID
		}
	}

	resp, err := s.identity.SignInWithGoogle(r.Context(), signInReq)
	if err != nil {
		writeError(w, err)
		return
	}
	token, err := s.sessions.Issue(resp.GetAccount().GetId())
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "issue session: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		SessionToken string          `json:"session_token"`
		Account      accountResponse `json:"account"`
		NeedsGender  bool            `json:"needs_gender"`
	}{token, toAccountResponse(resp.GetAccount()), resp.GetNeedsGender()})
}

// issueSessionAndRespond is the {session_token, account} response shape
// shared by every endpoint that creates or returns an account alongside
// a fresh session.
func issueSessionAndRespond(w http.ResponseWriter, sessions Sessions, acc *identityv1.Account) {
	token, err := sessions.Issue(acc.GetId())
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "issue session: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		SessionToken string          `json:"session_token"`
		Account      accountResponse `json:"account"`
	}{token, toAccountResponse(acc)})
}
