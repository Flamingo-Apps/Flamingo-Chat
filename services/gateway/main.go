package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/banned"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/httpapi"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/presence"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/ratelimit"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/session"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/turnstile"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/gateway/internal/ws"
)

// heartbeatInterval is docs/ARCHITECTURE.md §3.4's "every 15 seconds".
const heartbeatInterval = 15 * time.Second

func dialService(addr string) *grpc.ClientConn {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial %s: %v", addr, err)
	}
	return conn
}

func main() {
	port := config.String("HTTP_PORT", "8080")
	signingKey := config.String("SESSION_SIGNING_KEY", "")
	if signingKey == "" {
		// Unlike Google sign-in (optional, degrades to rejecting that one
		// feature - identity's own verify.Disabled pattern) sessions are
		// load-bearing for everything this service does, so there is no
		// sensible degraded mode to fall back to.
		log.Fatal("SESSION_SIGNING_KEY is required")
	}
	turnstileSecret := config.String("TURNSTILE_SECRET", "")
	originPatterns := strings.Split(config.String("WS_ORIGIN_PATTERNS", ""), ",")

	identityConn := dialService(config.String("IDENTITY_ADDR", "identity:50051"))
	defer func() { _ = identityConn.Close() }()
	chatConn := dialService(config.String("CHAT_ADDR", "chat:50053"))
	defer func() { _ = chatConn.Close() }()
	matchingConn := dialService(config.String("MATCHING_ADDR", "matching:50052"))
	defer func() { _ = matchingConn.Close() }()
	presenceConn := dialService(config.String("PRESENCE_ADDR", "presence:50054"))
	defer func() { _ = presenceConn.Close() }()
	moderationConn := dialService(config.String("MODERATION_ADDR", "moderation:50055"))
	defer func() { _ = moderationConn.Close() }()

	identityClient := identityv1.NewIdentityServiceClient(identityConn)
	chatClient := chatv1.NewChatServiceClient(chatConn)
	matchingClient := matchingv1.NewMatchingServiceClient(matchingConn)
	presenceClient := presencev1.NewPresenceServiceClient(presenceConn)
	moderationClient := moderationv1.NewModerationServiceClient(moderationConn)

	redisOpts, err := redis.ParseURL(config.String("REDIS_URL", "redis://redis:6379/0"))
	if err != nil {
		log.Fatalf("redis.ParseURL: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	defer func() { _ = redisClient.Close() }()

	sessions := session.NewSigner([]byte(signingKey))
	rateLimiter := ratelimit.New(redisClient)
	var turnstileVerifier turnstile.Verifier = turnstile.Disabled{}
	if turnstileSecret != "" {
		turnstileVerifier = turnstile.NewCloudflare(turnstileSecret)
	} else {
		log.Println("WARNING: TURNSTILE_SECRET is not set - guest signup will accept any (or no) Turnstile token")
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	presenceRegistry := presence.NewRegistry()
	go presence.RunHeartbeatLoop(ctx, presenceRegistry, presenceClient, heartbeatInterval, logger)

	bannedChecker := banned.NewRedis(redisClient)

	api := httpapi.NewServer(identityClient, chatClient, presenceClient, moderationClient, sessions, rateLimiter, bannedChecker, turnstileVerifier)
	wsDeps := ws.Deps{
		Identity: identityClient, Chat: chatClient, Matching: matchingClient, Presence: presenceClient,
		Sessions: sessions, RateLimiter: rateLimiter, Banned: bannedChecker, Redis: redisClient, Logger: logger,
		PresenceRegistry: presenceRegistry,
	}

	mux := http.NewServeMux()
	api.Routes(mux)
	mux.HandleFunc("GET /v1/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.Serve(wsDeps, originPatterns, w, r)
	})

	// A bare http.ListenAndServe has no read timeout, so a client that
	// sends headers slowly (by accident or on purpose) can tie up a
	// connection indefinitely. ReadHeaderTimeout only bounds the time to
	// read the request headers, so it won't cut off a long-lived
	// WebSocket connection once it's upgraded.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("gateway shutting down")
		// Shutdown stops accepting new connections and waits for in-flight
		// plain HTTP requests, but per its own documentation does not wait
		// for hijacked connections - which is exactly what every
		// WebSocket connection is once upgraded. An open WebSocket
		// connection is simply dropped when the process exits after this
		// returns, same as a hard kill would do to it; the client's own
		// documented reconnect-with-backoff (docs/ARCHITECTURE.md §5.2)
		// is what recovers from that, not anything server-side here.
		// A registry that closes every live WebSocket with a proper close
		// frame first is a real improvement but a separate piece of work,
		// not part of this pass - presenceRegistry above tracks connected
		// accounts for Heartbeat, it doesn't hold the connections
		// themselves.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("gateway listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("failed to serve: %v", err)
	}
}
