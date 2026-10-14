// Package httpapi implements the gateway's HTTP surface
// (docs/ARCHITECTURE.md §5.1): JSON over HTTPS under /v1, translating to
// gRPC calls against the backend services. Authenticated endpoints take
// a session token (internal/session) via Authorization: Bearer.
package httpapi

import (
	"context"
	"net/http"
	"time"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/turnstile"
)

// RateLimiter is the subset of internal/ratelimit.Limiter the HTTP API
// depends on, so handler tests can fake it instead of needing a real
// Redis.
type RateLimiter interface {
	Allow(ctx context.Context, action, subject string, max int, window time.Duration) (bool, error)
}

// Sessions is the subset of internal/session.Signer the HTTP API depends
// on.
type Sessions interface {
	Issue(accountID string) (string, error)
	Verify(tokenString string) (accountID string, needsRefresh bool, err error)
}

// Server holds every dependency the HTTP handlers need: gRPC clients to
// the backend services, and the gateway's own session/rate-limit/CAPTCHA
// pieces.
type Server struct {
	identity   identityv1.IdentityServiceClient
	chat       chatv1.ChatServiceClient
	presence   presencev1.PresenceServiceClient
	moderation moderationv1.ModerationServiceClient

	sessions    Sessions
	rateLimiter RateLimiter
	turnstile   turnstile.Verifier
}

func NewServer(
	identity identityv1.IdentityServiceClient,
	chat chatv1.ChatServiceClient,
	presence presencev1.PresenceServiceClient,
	moderation moderationv1.ModerationServiceClient,
	sessions Sessions,
	rateLimiter RateLimiter,
	turnstileVerifier turnstile.Verifier,
) *Server {
	return &Server{
		identity:    identity,
		chat:        chat,
		presence:    presence,
		moderation:  moderation,
		sessions:    sessions,
		rateLimiter: rateLimiter,
		turnstile:   turnstileVerifier,
	}
}

// Routes registers every HTTP handler on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleHealthz)

	mux.HandleFunc("POST /v1/auth/guest", s.handleAuthGuest)
	mux.HandleFunc("POST /v1/auth/google", s.handleAuthGoogle)

	mux.HandleFunc("GET /v1/me", s.withAuth(s.handleGetMe))
	mux.HandleFunc("PATCH /v1/me", s.withAuth(s.handleUpdateMe))
	mux.HandleFunc("POST /v1/me/gender", s.withAuth(s.handleSetGender))
	mux.HandleFunc("POST /v1/me/gender-change-requests", s.withAuth(s.handleRequestGenderChange))

	mux.HandleFunc("GET /v1/presence", s.handlePresence)

	mux.HandleFunc("GET /v1/rooms", s.withAuth(s.handleListRooms))
	mux.HandleFunc("POST /v1/rooms", s.withAuth(s.handleCreateRoom))
	mux.HandleFunc("POST /v1/rooms/join", s.withAuth(s.handleJoinRoom))
	mux.HandleFunc("POST /v1/rooms/{id}/leave", s.withAuth(s.handleLeaveRoom))
	mux.HandleFunc("POST /v1/rooms/{id}/invite-code", s.withAuth(s.handleRotateInviteCode))
	mux.HandleFunc("GET /v1/rooms/{id}/messages", s.withAuth(s.handleListMessages))

	mux.HandleFunc("POST /v1/reports", s.withAuth(s.handleFileReport))
	mux.HandleFunc("GET /v1/blocks", s.withAuth(s.handleListBlocks))
	mux.HandleFunc("POST /v1/blocks", s.withAuth(s.handleCreateBlock))
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
