package main

import (
	"log"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/grpclog"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/metrics"
	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/events"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/pairing"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/queue"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/server"
)

func main() {
	port := config.String("GRPC_PORT", "50052")
	metricsPort := config.String("METRICS_PORT", "9090")
	chatAddr := config.String("CHAT_ADDR", "chat:50053")
	kafkaBrokers := strings.Split(config.String("KAFKA_BROKERS", "kafka:9092"), ",")
	redisURL := config.String("REDIS_URL", "redis://redis:6379/0")

	chatConn, err := grpc.NewClient(chatAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial chat: %v", err)
	}
	defer func() { _ = chatConn.Close() }()
	chatClient := chatv1.NewChatServiceClient(chatConn)

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

	matchQueue := queue.NewRedis(redisClient)
	pairingCache := pairing.NewRedis(redisClient)
	svc := server.New(matchQueue, pairingCache, chatClient, producer, uuid.NewString)

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
	matchingv1.RegisterMatchingServiceServer(srv, svc)
	grpclog.InitializeMetrics(srv)

	log.Printf("matching listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
