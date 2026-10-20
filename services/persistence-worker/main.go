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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/metrics"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/consumer"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/decode"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/persistence-worker/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	kafkaBrokers := strings.Split(config.String("KAFKA_BROKERS", "kafka:9092"), ",")
	postgresURL := config.String("POSTGRES_URL", "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable")
	// A distinct consumer group from delivery-worker's - each needs its
	// own group so both see every message independently (that is the
	// whole point of a consumer group per logical consumer).
	groupID := config.String("KAFKA_CONSUMER_GROUP", "persistence-worker")
	metricsPort := config.String("METRICS_PORT", "9090")

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
	handler := consumer.New(map[string]consumer.DecodeFunc{
		"chat.events":       decode.DecodeChatEvent,
		"matching.events":   decode.DecodeMatchingEvent,
		"moderation.events": decode.DecodeModerationEvent,
	}, store.NewPostgres(pool), logger)

	topics := []string{"chat.events", "matching.events", "moderation.events"}

	go func() {
		for err := range group.Errors() {
			logger.Error("consumer group", "err", err)
		}
	}()

	log.Printf("persistence-worker consuming %v", topics)
	// Consume returns at the end of every rebalance; the caller is
	// expected to call it again to join the next session (sarama's own
	// documented pattern). The loop exits once ctx is canceled.
	for ctx.Err() == nil {
		if err := group.Consume(ctx, topics, handler); err != nil {
			logger.Error("consume", "err", err)
		}
	}
	log.Println("persistence-worker shutting down")
}
