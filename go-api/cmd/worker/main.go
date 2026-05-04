package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/easyhooks/easyhooks/internal/config"
	internalkafka "github.com/easyhooks/easyhooks/internal/kafka"
	"github.com/easyhooks/easyhooks/internal/observability"
	appredis "github.com/easyhooks/easyhooks/internal/redis"
	"github.com/easyhooks/easyhooks/internal/service"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Tracing (worker service name = "easyhooks-worker")
	workerCfg := *cfg
	workerCfg.OTELServiceName = cfg.OTELServiceName + "-worker"
	shutdownTracing, err := observability.InitTracing(ctx, &workerCfg)
	if err != nil {
		slog.Warn("Failed to init tracing", "error", err)
	}
	defer shutdownTracing(context.Background()) //nolint:errcheck

	// Redis
	rdb, err := appredis.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// Kafka consumer
	consumer, err := internalkafka.NewConsumer(cfg)
	if err != nil {
		slog.Error("Failed to create Kafka consumer", "error", err)
		os.Exit(1)
	}
	defer consumer.Close()

	// Kafka DLQ producer
	dlqClient, err := internalkafka.NewDLQProducer(cfg)
	if err != nil {
		slog.Error("Failed to create DLQ producer", "error", err)
		os.Exit(1)
	}
	defer dlqClient.Close()

	// Business handler: publish events to Redis Streams
	handler := service.MakeRedisStreamsHandler(rdb, cfg)

	slog.Info("Worker started",
		"topic", cfg.KafkaWebhookTopic,
		"dlq_topic", cfg.KafkaDLQTopic,
		"group_id", cfg.KafkaConsumerGroup,
	)

	// Poll loop — fetch and process records until shutdown
	for {
		fetches := consumer.PollFetches(ctx)
		if ctx.Err() != nil {
			break
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				slog.Error("Fetch error", "error", e.Err, "topic", e.Topic, "partition", e.Partition)
			}
		}

		fetches.EachRecord(func(record *kgo.Record) {
			observability.KafkaConsumeTotal.WithLabelValues(cfg.KafkaWebhookTopic, cfg.KafkaConsumerGroup).Inc()

			if err := service.ProcessRecord(ctx, record, rdb, dlqClient, cfg, handler); err != nil {
				slog.Error("Unrecoverable error processing record", "error", err)
			}

			// Manual commit after each record
			if err := consumer.CommitRecords(ctx, record); err != nil {
				slog.Warn("Failed to commit offset", "error", err)
			}
		})
	}

	slog.Info("Worker stopped")
}
