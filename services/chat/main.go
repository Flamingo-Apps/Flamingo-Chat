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
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/grpclog"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/metrics"
	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/dedup"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/events"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/server"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/chat/migrations"
)

func main() {
	ctx := context.Background()

	port := config.String("GRPC_PORT", "50053")
	metricsPort := config.String("METRICS_PORT", "9090")
	postgresURL := config.String("POSTGRES_URL", "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable")
	kafkaBrokers := strings.Split(config.String("KAFKA_BROKERS", "kafka:9092"), ",")
	redisURL := config.String("REDIS_URL", "redis://redis:6379/0")

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

	roomStore := store.NewPostgres(pool)
	deduper := dedup.NewRedis(redisClient)
	svc := server.New(roomStore, producer, deduper, uuid.NewString)

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
	chatv1.RegisterChatServiceServer(srv, svc)

	log.Printf("chat listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
