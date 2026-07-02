package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/devpablocristo/companion/migrations"
	"github.com/devpablocristo/companion/wire"
	"github.com/devpablocristo/platform/http/go/httpserver"
	sharedobservability "github.com/devpablocristo/platform/observability/go"
)

func main() {
	logger := sharedobservability.NewJSONLogger("companion")
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
	nexusBase := os.Getenv("NEXUS_BASE_URL")
	if nexusBase == "" {
		logger.Error("NEXUS_BASE_URL is required")
		os.Exit(1)
	}
	nexusKey := os.Getenv("NEXUS_API_KEY")
	if nexusKey == "" {
		logger.Error("NEXUS_API_KEY is required")
		os.Exit(1)
	}
	apiKeys := os.Getenv("COMPANION_API_KEYS")
	if apiKeys == "" {
		logger.Error("COMPANION_API_KEYS is required")
		os.Exit(1)
	}

	cfg := wire.Config{
		DatabaseURL:         databaseURL,
		APIKeys:             apiKeys,
		AuthIssuerURL:       os.Getenv("COMPANION_AUTH_ISSUER_URL"),
		AuthAudience:        os.Getenv("COMPANION_AUTH_AUDIENCE"),
		InternalJWTSecret:   os.Getenv("COMPANION_INTERNAL_JWT_SECRET"),
		InternalJWTIssuer:   os.Getenv("COMPANION_INTERNAL_JWT_ISSUER"),
		InternalJWTAudience: os.Getenv("COMPANION_INTERNAL_JWT_AUDIENCE"),
		ProductJWTKeys:      os.Getenv("COMPANION_PRODUCT_JWT_KEYS"),
		NexusBaseURL:        nexusBase,
		NexusAPIKey:         nexusKey,
		LLMProvider:         os.Getenv("COMPANION_LLM_PROVIDER"),
		LLMModel:            os.Getenv("COMPANION_LLM_MODEL"),
		LLMVertexProject:    os.Getenv("COMPANION_LLM_VERTEX_PROJECT"),
		LLMVertexLocation:   os.Getenv("COMPANION_LLM_VERTEX_LOCATION"),
		EmbeddingProvider:   os.Getenv("COMPANION_EMBEDDING_PROVIDER"),
		EmbeddingModel:      os.Getenv("COMPANION_EMBEDDING_MODEL"),
		EmbeddingProject:    os.Getenv("COMPANION_EMBEDDING_VERTEX_PROJECT"),
		EmbeddingLocation:   os.Getenv("COMPANION_EMBEDDING_VERTEX_LOCATION"),
		EmbeddingDimensions: envInt("COMPANION_EMBEDDING_DIMENSIONS"),
		OpsAlertWebhookURL:  os.Getenv("COMPANION_OPS_ALERT_WEBHOOK_URL"),
		MigrationFiles:      migrations.Files,
	}

	handler, cleanup, err := wire.NewServer(cfg)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	const maxBodySize = 1 << 20
	limitedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		handler.ServeHTTP(w, r)
	})

	metrics := sharedobservability.NewMetrics(sharedobservability.DefaultMetricsConfig("companion"))
	appHandler := sharedobservability.WithMetricsEndpoint(limitedHandler, metrics.Handler())
	securedHandler := httpserver.SecurityMiddleware(
		httpserver.SecurityConfigFromEnv("COMPANION"),
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

func envInt(key string) int {
	raw := os.Getenv(key)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}
