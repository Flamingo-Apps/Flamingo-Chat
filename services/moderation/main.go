package main

import (
	"context"
	"log"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/grpclog"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/metrics"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/events"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/server"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/sets"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/migrations"
)

func main() {
	ctx := context.Background()

	port := config.String("GRPC_PORT", "50055")
	metricsPort := config.String("METRICS_PORT", "9090")
	postgresURL := config.String("POSTGRES_URL", "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable")
	kafkaBrokers := strings.Split(config.String("KAFKA_BROKERS", "kafka:9092"), ",")
	redisURL := config.String("REDIS_URL", "redis://redis:6379/0")
	identityAddr := config.String("IDENTITY_ADDR", "identity:50051")

	pool, err := pgxpool.New(ctx, postgresURL)
	if err != nil {
		log.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	sqlDB := stdlib.OpenDBFromPool(pool)
	if err := migrations.Run(sqlDB); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		log.Printf("migrations: closing db handle: %v", err)
	}

	producer, err := events.NewSaramaProducer(kafkaBrokers)
	if err != nil {
		log.Fatalf("events.NewSaramaProducer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			log.Printf("closing kafka producer: %v", err)
		}
	}()

	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("redis.ParseURL: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Printf("closing redis client: %v", err)
		}
	}()

	identityConn, err := grpc.NewClient(identityAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial identity: %v", err)
	}
	defer func() { _ = identityConn.Close() }()
	identityClient := identityv1.NewIdentityServiceClient(identityConn)

	moderationStore := store.NewPostgres(pool)
	moderationSets := sets.NewRedis(redisClient)
	svc := server.New(moderationStore, moderationSets, producer, identityClient, uuid.NewString)

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
	moderationv1.RegisterModerationServiceServer(srv, svc)

	log.Printf("moderation listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
