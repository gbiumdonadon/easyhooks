package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/db"
	"github.com/easyhooks/easyhooks/internal/db/queries"
	"github.com/easyhooks/easyhooks/internal/handler"
	internalkafka "github.com/easyhooks/easyhooks/internal/kafka"
	appmiddleware "github.com/easyhooks/easyhooks/internal/middleware"
	"github.com/easyhooks/easyhooks/internal/observability"
	appredis "github.com/easyhooks/easyhooks/internal/redis"
	"github.com/easyhooks/easyhooks/internal/security"
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

	// Tracing
	shutdownTracing, err := observability.InitTracing(ctx, cfg)
	if err != nil {
		slog.Warn("Failed to init tracing", "error", err)
	}
	defer shutdownTracing(context.Background()) //nolint:errcheck

	// Database
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Run migrations
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		slog.Error("Migration failed", "error", err)
		os.Exit(1)
	}

	store := queries.New(pool)

	// Seed admin user
	if err := seedAdmin(ctx, store, cfg); err != nil {
		slog.Warn("Admin seed failed", "error", err)
	}

	// Redis
	rdb, err := appredis.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// Kafka producer
	producer, err := internalkafka.NewProducer(cfg)
	if err != nil {
		slog.Error("Failed to create Kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	// Fan-out manager
	fanoutMgr := service.NewFanoutManager()

	// Router
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.RealIP)

	// CORS — allow Docusaurus playground on localhost:3001
	origins := strings.Split(cfg.CORSOrigins, ",")
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type", "X-Webhook-Signature", "X-Event-Id"},
		AllowCredentials: true,
	}))

	// Prometheus HTTP metrics middleware
	r.Use(appmiddleware.HTTPMetrics)

	// Routes
	r.Get("/health", handler.Health)
	r.Handle("/metrics", handler.Metrics())

	// Admin routes (protected by AdminAuth)
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.AdminAuth(store))
		r.Post("/admin/tenants", handler.CreateTenant(store, rdb, cfg))
	})

	// Tenant routes (protected by TenantAuth)
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.TenantAuth(rdb, cfg))
		r.Post("/v1/webhooks/{tenant_id}", handler.IngestWebhook(producer, cfg))
		r.Post("/v1/tokens/{tenant_id}", handler.IssueToken(cfg))
	})

	// WebSocket (auth via token query param — no middleware needed)
	r.Get("/ws/events/{tenant_id}", handler.WSEvents(rdb, cfg, fanoutMgr))

	srv := &http.Server{
		Addr:         ":8000",
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // 0 = no timeout (WebSocket connections are long-lived)
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		slog.Info("API server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	slog.Info("Shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Graceful shutdown failed", "error", err)
	}
	slog.Info("API server stopped")
}

// runMigrations applies pending migrations using golang-migrate.
func runMigrations(databaseURL string) error {
	migrationPath, err := resolveMigrationsPath()
	if err != nil {
		return err
	}
	fsrc, err := (&file.File{}).Open(migrationPath)
	if err != nil {
		return fmt.Errorf("open migrations source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("file", fsrc, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	slog.Info("Migrations applied")
	return nil
}

// resolveMigrationsPath returns the golang-migrate file:// URL for the migrations directory.
// In Docker the migrations are copied to /migrations/; in local dev they live at ../migrations
// relative to the go-api/ working directory.
func resolveMigrationsPath() (string, error) {
	if _, err := os.Stat("/migrations"); err == nil {
		return "file:///migrations", nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	abs, err := filepath.Abs(filepath.Join(cwd, "..", "migrations"))
	if err != nil {
		return "", fmt.Errorf("resolve migrations path: %w", err)
	}
	return "file://" + filepath.ToSlash(abs), nil
}

// seedAdmin creates the bootstrap admin user from ADMIN_SEED_TOKEN if not present.
func seedAdmin(ctx context.Context, store *queries.Store, cfg *config.Config) error {
	count, err := store.AdminUserCount(ctx)
	if err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if count > 0 {
		return nil // already seeded
	}

	tokenHash, err := security.HashSecret(cfg.AdminSeedToken)
	if err != nil {
		return fmt.Errorf("hash admin token: %w", err)
	}
	if err := store.CreateAdminUser(ctx, uuid.New(), "superadmin", tokenHash, "admin"); err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}
	slog.Info("Seeded admin user 'superadmin'")
	return nil
}
