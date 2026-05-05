package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/observability"
	appredis "github.com/easyhooks/easyhooks/internal/redis"
	"github.com/easyhooks/easyhooks/internal/service"
	"github.com/easyhooks/easyhooks/internal/streams"
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

	rdb, err := appredis.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// The API also calls EnsureGroup on startup; doing it here too lets the
	// worker run standalone (e.g. tests, isolated deployments).
	if err := streams.EnsureGroup(ctx, rdb, cfg.EventStreamKey, cfg.ConsumerGroup); err != nil {
		slog.Error("Failed to ensure consumer group", "error", err)
		os.Exit(1)
	}

	consumer, err := os.Hostname()
	if err != nil || consumer == "" {
		consumer = "worker"
	}
	reader := streams.NewReader(rdb, cfg.EventStreamKey, cfg.ConsumerGroup, consumer, cfg.StreamBlockMs, cfg.StreamCount)

	handler := service.MakeRedisStreamsHandler(rdb, cfg)

	slog.Info("Worker started",
		"stream", cfg.EventStreamKey,
		"dlq_stream", cfg.DLQStreamKey,
		"group", cfg.ConsumerGroup,
		"consumer", consumer,
	)

	for {
		if ctx.Err() != nil {
			break
		}
		msgs, err := reader.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			slog.Error("Stream read failed", "error", err)
			continue
		}

		for _, m := range msgs {
			observability.StreamConsumeTotal.WithLabelValues(cfg.EventStreamKey, cfg.ConsumerGroup).Inc()

			if procErr := service.ProcessMessage(ctx, m, rdb, cfg, handler); procErr != nil {
				// ProcessMessage already routes terminal failures to the DLQ
				// stream; an error here means we could not even reach Redis,
				// so the message will stay pending and be reclaimed manually
				// (XAUTOCLAIM is currently a TODO).
				slog.Error("Unrecoverable error processing message",
					"stream_id", m.ID, "error", procErr,
				)
			}

			if ackErr := reader.Ack(ctx, m.ID); ackErr != nil {
				slog.Warn("Failed to ack stream message", "stream_id", m.ID, "error", ackErr)
			}
		}
	}

	slog.Info("Worker stopped")
}
