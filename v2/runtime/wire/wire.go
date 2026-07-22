package wire

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	observability "github.com/devpablocristo/platform/observability/go"
	cfg "github.com/devpablocristo/runtime-v2/cmd/config"
	"github.com/devpablocristo/runtime-v2/internal/embeddings"
	"github.com/devpablocristo/runtime-v2/internal/planner"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Dependencies struct {
	Config         cfg.Config
	Router         *gin.Engine
	Server         *http.Server
	tracerShutdown func(context.Context) error
}

func Initialize(ctx context.Context) (*Dependencies, error) {
	config := cfg.Load()
	if config.InternalAuthSecret == "" {
		return nil, fmt.Errorf("RUNTIME_V2_INTERNAL_AUTH_SECRET is required")
	}

	logger := observability.NewJSONLogger("runtime-v2")
	tracerShutdown, err := observability.NewTracerProvider(ctx, observability.TracingConfig{
		ServiceName:    "runtime-v2",
		ServiceVersion: config.ServiceVersion,
		Environment:    config.Environment,
		Exporter:       config.OTelExporter,
		OTLPEndpoint:   config.OTelEndpoint,
		OTLPInsecure:   config.OTelInsecure,
	})
	if err != nil {
		return nil, err
	}

	provider := buildProvider(ctx, config)
	plannerHandler := planner.NewHandler(planner.New(provider, config.LLMModel))
	embeddingHandler := embeddings.NewHandler(buildEmbeddingProvider(ctx, config))

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ginmw.NewBodySizeLimit(config.MaxBodyBytes))
	router.Use(ginmw.NewCORS(ginmw.CORSConfig{
		Origins:      config.CORSOrigins,
		AllowHeaders: []string{"Content-Type", "X-Actor-ID", "X-Tenant-ID"},
	}))
	ginmw.RegisterHealthEndpoints(router, func(context.Context) error { return nil })

	api := router.Group("/v1")
	api.Use(internalAuthMiddleware(config.InternalAuthSecret))
	plannerHandler.Routes(api)
	embeddingHandler.Routes(api)

	server := &http.Server{
		Addr:    config.Addr(),
		Handler: tracedServerHandler("runtime-v2", observability.Middleware(logger, router)),
	}

	return &Dependencies{
		Config:         config,
		Router:         router,
		Server:         server,
		tracerShutdown: tracerShutdown,
	}, nil
}

// tracedServerHandler wraps the handler with an OTel server span per request,
// extracting incoming trace context. Health probes are excluded.
func tracedServerHandler(service string, h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, service,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
		otelhttp.WithFilter(func(r *http.Request) bool {
			p := r.URL.Path
			return p != "/readyz" && p != "/healthz"
		}),
	)
}

func (d *Dependencies) Close() {
	if d == nil {
		return
	}
	if d.tracerShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.tracerShutdown(shutdownCtx)
		cancel()
	}
}
