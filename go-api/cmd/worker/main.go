package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	goredis "github.com/redis/go-redis/v9"

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

	applyMemoryLimit(cfg)
	observability.RecordProfileInfo(cfg.Profile)

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

	// Check Redis memory at startup (same as API): warns if unconfigured or
	// already under pressure before the worker begins consuming messages.
	bootCheckRedisMemory(ctx, rdb)

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

// bootCheckRedisMemory mirrors cmd/api: logs Redis memory state at startup and
// warns if maxmemory is unconfigured or usage is already above 70%.
func bootCheckRedisMemory(ctx context.Context, rdb *goredis.Client) {
	info, err := appredis.InfoMemory(ctx, rdb)
	if err != nil {
		slog.Warn("Redis memory check failed at boot (continuing)", "error", err)
		return
	}
	attrs := []any{
		"used_bytes", info.UsedMemory,
		"max_bytes", info.MaxMemory,
		"policy", info.Policy,
	}
	if info.MaxMemory == 0 {
		slog.Warn("Redis maxmemory is not configured — set --maxmemory to prevent OOM", attrs...)
		return
	}
	usedPct := info.UsedMemory * 100 / info.MaxMemory
	attrs = append(attrs, "used_pct", usedPct)
	if usedPct > 70 {
		slog.Warn("Redis memory already above 70% at startup", attrs...)
	} else {
		slog.Info("Redis memory OK at startup", attrs...)
	}
}

// applyMemoryLimit mirrors cmd/api: honour an explicit GOMEMLIMIT, otherwise
// apply the profile-derived value so the worker stays within the container
// budget under heavy retry/backoff fan-out.
func applyMemoryLimit(cfg *config.Config) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		slog.Info("Memory limit honoured from GOMEMLIMIT env var", "profile", cfg.Profile)
		return
	}
	if cfg.GoMemLimitBytes <= 0 {
		slog.Warn("No memory limit set; profile=custom without GOMEMLIMIT", "profile", cfg.Profile)
		return
	}
	debug.SetMemoryLimit(cfg.GoMemLimitBytes)
	slog.Info("Memory limit set",
		"profile", cfg.Profile,
		"gomemlimit_bytes", cfg.GoMemLimitBytes,
		"gomemlimit_mib", cfg.GoMemLimitBytes/(1024*1024),
	)
}
