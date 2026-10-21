package main

import (
	"context"
	"log"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/grpclog"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/metrics"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/server"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/verify"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/migrations"
)

// guestRetentionDays is how long a guest account can go without activity
// before cleanup-guests removes it (DEC-0026).
const guestRetentionDays = 60

func main() {
	// A second mode on the same binary, run as a Kubernetes CronJob
	// (deploy/helm/service's cronjob.yaml) rather than as a separate
	// image - see docs/ARCHITECTURE.md §9.
	if len(os.Args) > 1 && os.Args[1] == "cleanup-guests" {
		runCleanupGuests()
		return
	}
	runServer()
}

func connectAndMigrate(ctx context.Context) *pgxpool.Pool {
	postgresURL := config.String("POSTGRES_URL", "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable")

	pool, err := pgxpool.New(ctx, postgresURL)
	if err != nil {
		log.Fatalf("pgxpool.New: %v", err)
	}

	// stdlib.OpenDBFromPool wraps the existing pool as a *sql.DB just for
	// golang-migrate's sake (it predates pgx v5's native interface and
	// still expects database/sql) - it does not open a second connection
	// pool.
	sqlDB := stdlib.OpenDBFromPool(pool)
	if err := migrations.Run(sqlDB); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		log.Printf("migrations: closing db handle: %v", err)
	}
	return pool
}

func runCleanupGuests() {
	ctx := context.Background()
	pool := connectAndMigrate(ctx)
	defer pool.Close()

	accountStore := store.NewPostgres(pool)
	n, err := accountStore.DeleteStaleGuests(ctx, guestRetentionDays*24*time.Hour)
	if err != nil {
		log.Fatalf("cleanup-guests: %v", err)
	}
	log.Printf("cleanup-guests: removed %d guest account(s) inactive for over %d days", n, guestRetentionDays)
}

func runServer() {
	ctx := context.Background()

	port := config.String("GRPC_PORT", "50051")
	metricsPort := config.String("METRICS_PORT", "9090")
	collegeDomain := config.String("COLLEGE_EMAIL_DOMAIN", "kiit.ac.in")
	oauthClientID := config.String("COLLEGE_OAUTH_CLIENT_ID", "")
	oauthIssuer := config.String("OAUTH_ISSUER_URL", "https://accounts.google.com")
	// Exactly "1", not merely non-empty: with "any value enables it", a
	// DEV_FAKE_GOOGLE of "0" or "false" - which reads as off - would accept
	// fabricated ID tokens.
	devFakeGoogle := config.String("DEV_FAKE_GOOGLE", "") == "1"

	pool := connectAndMigrate(ctx)
	defer pool.Close()

	var verifier verify.GoogleVerifier
	switch {
	case devFakeGoogle:
		// Never for anything but local development (DEC-0049) - accepts
		// tokens shaped "fake:<sub>:<email>" instead of a real
		// Google-signed JWT, so SignInWithGoogle can be exercised without
		// a browser.
		log.Println("WARNING: DEV_FAKE_GOOGLE is set - accepting fabricated sign-in tokens instead of verifying real ones. Never set this outside local development.")
		verifier = verify.Fake{}
	case oauthClientID == "":
		log.Println("WARNING: COLLEGE_OAUTH_CLIENT_ID is not set - SignInWithGoogle will reject every request until it is configured")
		verifier = verify.Disabled{}
	default:
		v, err := verify.NewOIDCVerifier(ctx, oauthIssuer, oauthClientID)
		if err != nil {
			log.Fatalf("verify.NewOIDCVerifier: %v", err)
		}
		verifier = v
	}

	accountStore := store.NewPostgres(pool)
	svc := server.New(accountStore, verifier, uuid.NewString, collegeDomain)

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	go metrics.Serve(":"+metricsPort, logger)
	srv := grpc.NewServer(grpc.UnaryInterceptor(grpclog.UnaryServerInterceptor(logger)))

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)
	identityv1.RegisterIdentityServiceServer(srv, svc)
	grpclog.InitializeMetrics(srv)

	log.Printf("identity listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
