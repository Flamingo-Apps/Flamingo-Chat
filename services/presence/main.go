package main

import (
	"context"
	"log"
	"log/slog"
	"net"
	"os"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/grpclog"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/metrics"
	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/presence/internal/broadcast"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/presence/internal/server"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/presence/internal/store"
)

func main() {
	port := config.String("GRPC_PORT", "50054")
	metricsPort := config.String("METRICS_PORT", "9090")
	redisURL := config.String("REDIS_URL", "redis://redis:6379/0")

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

	presenceStore := store.NewRedis(redisClient)
	svc := server.New(presenceStore)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	go metrics.Serve(":"+metricsPort, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broadcast.Run(ctx, presenceStore, redisClient, logger)

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer(grpc.UnaryInterceptor(grpclog.UnaryServerInterceptor(logger)))

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)
	presencev1.RegisterPresenceServiceServer(srv, svc)
	grpclog.InitializeMetrics(srv)

	log.Printf("presence listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
