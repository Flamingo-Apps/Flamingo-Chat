package main

import (
	"context"
	"log"
	"net"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/server"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/verify"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/migrations"
)

func main() {
	ctx := context.Background()

	port := config.String("GRPC_PORT", "50051")
	postgresURL := config.String("POSTGRES_URL", "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable")
	collegeDomain := config.String("COLLEGE_EMAIL_DOMAIN", "kiit.ac.in")
	oauthClientID := config.String("COLLEGE_OAUTH_CLIENT_ID", "")
	oauthIssuer := config.String("OAUTH_ISSUER_URL", "https://accounts.google.com")

	pool, err := pgxpool.New(ctx, postgresURL)
	if err != nil {
		log.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	// stdlib.OpenDBFromPool wraps the existing pool as a *sql.DB just for
	// golang-migrate's sake (it predates pgx v5's native interface and
	// still expects database/sql) - it does not open a second connection
	// pool.
	sqlDB := stdlib.OpenDBFromPool(pool)
	if err := migrations.Run(sqlDB); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	sqlDB.Close()

	var verifier verify.BadgeVerifier
	if oauthClientID == "" {
		log.Println("WARNING: COLLEGE_OAUTH_CLIENT_ID is not set - VerifyBadge will reject every request until it is configured")
		verifier = verify.Disabled{}
	} else {
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

	srv := grpc.NewServer()

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)
	identityv1.RegisterIdentityServiceServer(srv, svc)

	log.Printf("identity listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
