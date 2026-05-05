package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

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

	// Per-tenant fan-out manager (used by the WebSocket handler).
	fanoutMgr := service.NewFanoutManager()

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
		r.Post("/v1/webhooks/{tenant_id}", handler.IngestWebhook(rdb, cfg))
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
