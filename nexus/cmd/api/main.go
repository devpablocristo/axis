package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/devpablocristo/nexus/migrations"
	"github.com/devpablocristo/nexus/wire"
	"github.com/devpablocristo/platform/http/go/httpserver"
	sharedobservability "github.com/devpablocristo/platform/observability/go"
)

func main() {
	logger := sharedobservability.NewJSONLogger("nexus")
	addr := os.Getenv("PORT")
	if addr == "" {
		addr = "8080"
	}
	if addr[0] != ':' {
		addr = ":" + addr
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	approvalTTL := time.Hour
	if ttlStr := os.Getenv("APPROVAL_DEFAULT_TTL"); ttlStr != "" {
		if secs, err := strconv.Atoi(ttlStr); err == nil && secs > 0 {
			approvalTTL = time.Duration(secs) * time.Second
		}
	}

	cfg := wire.Config{
		DatabaseURL:          databaseURL,
		APIKeys:              os.Getenv("NEXUS_API_KEYS"),
		AuthIssuerURL:        os.Getenv("NEXUS_AUTH_ISSUER_URL"),
		AuthAudience:         os.Getenv("NEXUS_AUTH_AUDIENCE"),
		InternalJWTSecret:    os.Getenv("NEXUS_INTERNAL_JWT_SECRET"),
		InternalJWTIssuer:    os.Getenv("NEXUS_INTERNAL_JWT_ISSUER"),
		InternalJWTAudience:  os.Getenv("NEXUS_INTERNAL_JWT_AUDIENCE"),
		ApprovalTTL:          approvalTTL,
		SigningKey:           os.Getenv("NEXUS_SIGNING_KEY"),
		CallbackToken:        strings.TrimSpace(os.Getenv("NEXUS_CALLBACK_TOKEN")),
		PendingCallbackURLs:  splitCSV(os.Getenv("NEXUS_APPROVAL_PENDING_CALLBACK_URLS")),
		ResolvedCallbackURLs: splitCSV(os.Getenv("NEXUS_APPROVAL_RESOLVED_CALLBACK_URLS")),
		MigrationFiles:       migrations.Files,
	}
	if cfg.APIKeys == "" {
		logger.Error("NEXUS_API_KEYS is required")
		os.Exit(1)
	}

	handler, cleanup, err := wire.NewServer(cfg)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	// Limitar tamaño de request body a 1MB
	const maxBodySize = 1 << 20
	limitedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		handler.ServeHTTP(w, r)
	})

	metrics := sharedobservability.NewMetrics(sharedobservability.DefaultMetricsConfig("nexus"))
	appHandler := sharedobservability.WithMetricsEndpoint(limitedHandler, metrics.Handler())
	securedHandler := httpserver.SecurityMiddleware(
		httpserver.SecurityConfigFromEnv("NEXUS"),
		sharedobservability.MiddlewareWithMetrics(logger, metrics, appHandler),
	)
	server := httpserver.New(addr, securedHandler)

	logger.Info("http server listening", "addr", addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := httpserver.Serve(ctx, server, logger); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}
