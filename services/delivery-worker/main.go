package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/metrics"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/delivery-worker/internal/consumer"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/delivery-worker/internal/router"
)

// redisPublisher adapts *redis.Client to consumer.Publisher.
type redisPublisher struct {
	client *redis.Client
}

func (p *redisPublisher) Publish(ctx context.Context, channel string, payload []byte) error {
	return p.client.Publish(ctx, channel, payload).Err()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	kafkaBrokers := strings.Split(config.String("KAFKA_BROKERS", "kafka:9092"), ",")
	redisURL := config.String("REDIS_URL", "redis://redis:6379/0")
	// A distinct consumer group from any other consumer of these topics
	// (the persistence worker, built in the next PR in T6, needs its own
	// group so both see every message independently - that's the whole
	// point of a consumer group per logical consumer).
	groupID := config.String("KAFKA_CONSUMER_GROUP", "delivery-worker")
	metricsPort := config.String("METRICS_PORT", "9090")

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

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V4_3_1_0
	cfg.Consumer.Return.Errors = true

	group, err := sarama.NewConsumerGroup(kafkaBrokers, groupID, cfg)
	if err != nil {
		log.Fatalf("sarama.NewConsumerGroup: %v", err)
	}
	defer func() {
		if err := group.Close(); err != nil {
			log.Printf("closing consumer group: %v", err)
		}
	}()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	go metrics.Serve(":"+metricsPort, logger)
	handler := consumer.New(map[string]consumer.RouteFunc{
		"chat.events":       router.RouteChatEvent,
		"matching.events":   router.RouteMatchingEvent,
		"moderation.events": router.RouteModerationEvent,
	}, &redisPublisher{client: redisClient}, logger)

	topics := []string{"chat.events", "matching.events", "moderation.events"}

	go func() {
		for err := range group.Errors() {
			logger.Error("consumer group", "err", err)
		}
	}()

	log.Printf("delivery-worker consuming %v", topics)
	// Consume returns at the end of every rebalance; the caller is
	// expected to call it again to join the next session (sarama's own
	// documented pattern). The loop exits once ctx is canceled.
	for ctx.Err() == nil {
		if err := group.Consume(ctx, topics, handler); err != nil {
			logger.Error("consume", "err", err)
		}
	}
	log.Println("delivery-worker shutting down")
}
