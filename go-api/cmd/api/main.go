package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/handler"
	appmiddleware "github.com/easyhooks/easyhooks/internal/middleware"
	"github.com/easyhooks/easyhooks/internal/observability"
	appredis "github.com/easyhooks/easyhooks/internal/redis"
	"github.com/easyhooks/easyhooks/internal/redisstore"
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

	// Tracing
	shutdownTracing, err := observability.InitTracing(ctx, cfg)
	if err != nil {
		slog.Warn("Failed to init tracing", "error", err)
	}
	defer shutdownTracing(context.Background()) //nolint:errcheck

	// Redis (sole datastore)
	rdb, err := appredis.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// Bootstrap admin token (idempotent — only writes on first start).
	if err := redisstore.SeedSuperAdmin(ctx, rdb, cfg.AdminSeedToken); err != nil {
		slog.Error("Failed to seed admin token", "error", err)
		os.Exit(1)
	}

	// Ensure the work-queue stream + consumer group exist before the worker
	// starts pushing or pulling from it.
	if err := streams.EnsureGroup(ctx, rdb, cfg.EventStreamKey, cfg.ConsumerGroup); err != nil {
		slog.Error("Failed to ensure consumer group", "error", err)
		os.Exit(1)
	}

	// Defensive one-shot trim at boot to enforce the configured memory cap on
	// streams that may have grown unbounded in older deployments. The load
	// shedder no longer depends on this (it reads consumer-group backlog,
	// not XLEN), so the trim is purely a memory guard-rail.
	bootTrimStream(ctx, rdb, cfg.EventStreamKey, int64(cfg.IngestStreamMaxLen))
	bootTrimStream(ctx, rdb, cfg.DLQStreamKey, int64(cfg.DLQStreamMaxLen))

	// Per-tenant fan-out manager (used by the WebSocket handler).
	fanoutMgr := service.NewFanoutManager()

	// Queue depth monitor: samples consumer-group backlog (lag + pending) every
	// cfg.QueueDepthPollMs so that IngestWebhook can shed load (HTTP 429)
	// without doing a Redis round-trip per request. Hysteresis between
	// high/low watermarks prevents flapping.
	queueMonitor := streams.NewQueueDepthMonitor(
		rdb,
		cfg.EventStreamKey,
		cfg.ConsumerGroup,
		int64(cfg.IngestMaxQueueDepth),
		cfg.QueueDepthLowWaterPct,
	)
	go queueMonitor.Run(ctx, time.Duration(cfg.QueueDepthPollMs)*time.Millisecond)

	// Router
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.RealIP)

	origins := strings.Split(cfg.CORSOrigins, ",")
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Webhook-Signature", "X-Event-Id"},
		AllowCredentials: true,
	}))

	r.Use(appmiddleware.HTTPMetrics)

	r.Get("/health", handler.Health)
	r.Handle("/metrics", handler.Metrics())

	// Admin routes (protected by AdminAuth — Redis-backed bcrypt verify)
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.AdminAuth(rdb))
		r.Post("/admin/tenants", handler.CreateTenant(rdb, cfg))
	})

	// Tenant routes (protected by TenantAuth)
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.TenantAuth(rdb, cfg))
		r.Post("/v1/webhooks/{tenant_id}", handler.IngestWebhook(rdb, cfg, queueMonitor))
		r.Post("/v1/tokens/{tenant_id}", handler.IssueToken(cfg))
	})

	// WebSocket (auth via short-lived token query param).
	r.Get("/ws/events/{tenant_id}", handler.WSEvents(rdb, cfg, fanoutMgr))

	srv := &http.Server{
		Addr:         ":8000",
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // 0 = no timeout (WebSocket connections are long-lived)
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("API server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Graceful shutdown failed", "error", err)
	}
	slog.Info("API server stopped")
}

// bootTrimStream runs the defensive XTRIM described in main(). Failures are
// logged but never block startup: the trim is a recovery aid, not a critical
// dependency, and the API can still serve traffic with an oversized stream.
func bootTrimStream(ctx context.Context, rdb *goredis.Client, stream string, maxLen int64) {
	if maxLen <= 0 {
		return
	}
	trimmed, err := streams.TrimMaxLenApprox(ctx, rdb, stream, maxLen)
	if err != nil {
		slog.Warn("Boot-time stream trim failed (continuing)",
			"stream", stream, "max_len", maxLen, "error", err,
		)
		return
	}
	if trimmed > 0 {
		slog.Info("Boot-time stream trim removed legacy entries",
			"stream", stream, "max_len", maxLen, "trimmed", trimmed,
		)
	}
}

// applyMemoryLimit wires GOMEMLIMIT (Go 1.19+) so the GC works harder before
// the container hits the Docker mem_limit and the OOM killer fires. We honour
// an explicit GOMEMLIMIT env var (parsed by the runtime before main runs) and
// only apply the profile-derived value when the user did not set their own.
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
