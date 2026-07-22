package wire

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/nexus-v2/cmd/config"
	"github.com/devpablocristo/nexus-v2/internal/actiontypes"
	"github.com/devpablocristo/nexus-v2/internal/approvals"
	"github.com/devpablocristo/nexus-v2/internal/audit"
	"github.com/devpablocristo/nexus-v2/internal/evidence"
	"github.com/devpablocristo/nexus-v2/internal/governance"
	"github.com/devpablocristo/nexus-v2/internal/infra/migrations"
	"github.com/devpablocristo/nexus-v2/internal/watchers"
	postgres "github.com/devpablocristo/platform/databases/postgres/go"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	observability "github.com/devpablocristo/platform/observability/go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Dependencies struct {
	Config         cfg.Config
	DB             *postgres.DB
	Router         *gin.Engine
	Server         *http.Server
	tracerShutdown func(context.Context) error
	watcherCancel  context.CancelFunc
	watcherWG      sync.WaitGroup
}

func Initialize(ctx context.Context) (*Dependencies, error) {
	config := cfg.Load()
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("NEXUS_V2_DATABASE_URL or DATABASE_URL is required")
	}
	if config.InternalAuthSecret == "" {
		return nil, fmt.Errorf("NEXUS_V2_INTERNAL_AUTH_SECRET is required")
	}

	dbConfig, err := postgres.ConfigFromEnv("NEXUS_V2_DB", "nexus_v2")
	if err != nil {
		return nil, err
	}
	db, err := postgres.OpenWithConfig(ctx, config.DatabaseURL, dbConfig)
	if err != nil {
		return nil, err
	}

	if config.RunMigrations {
		if err := postgres.MigrateUp(ctx, db, "nexus_v2", migrations.Files, migrations.Dir); err != nil {
			db.Close()
			return nil, err
		}
	}

	logger := observability.NewJSONLogger("nexus-v2")
	tracerShutdown, err := observability.NewTracerProvider(ctx, observability.TracingConfig{
		ServiceName:    "nexus-v2",
		ServiceVersion: config.ServiceVersion,
		Environment:    config.Environment,
		Exporter:       config.OTelExporter,
		OTLPEndpoint:   config.OTelEndpoint,
		OTLPInsecure:   config.OTelInsecure,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	actionTypeRepo := actiontypes.NewRepository(db.Pool())
	actionTypeUseCases := actiontypes.NewUseCases(actionTypeRepo)
	actionTypeHandler := actiontypes.NewHandler(actionTypeUseCases)

	governanceRepo := governance.NewRepository(db.Pool(), config.ApprovalTTL)
	governanceUseCases := governance.NewUseCases(actionTypeUseCases, governanceRepo)
	governanceHandler := governance.NewHandler(governanceUseCases)

	approvalsRepo := approvals.NewRepository(db.Pool())
	approvalsUseCases := approvals.NewUseCases(approvalsRepo)
	approvalsHandler := approvals.NewHandler(approvalsUseCases)

	// Tamper-evident audit ledger (hash-chained per virployee). Signing is
	// optional: with NEXUS_V2_SIGNING_KEY unset the chain still hashes, it just
	// leaves signatures blank (local-first; production sets the key).
	auditRepo := audit.NewRepository(db.Pool(), audit.WithSigner(config.SigningKey, ""))
	auditUseCases := audit.NewUseCases(auditRepo)
	auditHandler := audit.NewHandler(auditUseCases)

	// Signed, exportable evidence packs built over the audit ledger. Shares the
	// same signing key; nil signer (no key) emits packs with algorithm "none".
	evidenceUseCases := evidence.NewUseCases(auditUseCases, evidence.NewSigner(config.SigningKey, ""))
	evidenceHandler := evidence.NewHandler(evidenceUseCases)

	watcherCtx, watcherCancel := context.WithCancel(ctx)
	approvalWatcher := watchers.New(watchers.NewRepository(db.Pool()), auditUseCases)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ginmw.NewBodySizeLimit(config.MaxBodyBytes))
	router.Use(ginmw.NewCORS(ginmw.CORSConfig{
		Origins: config.CORSOrigins,
		AllowHeaders: []string{
			"Authorization",
			"Content-Type",
			"X-Actor-ID",
			"X-Tenant-ID",
		},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)

	api := router.Group("/v1")
	api.Use(internalAuthMiddleware(config.InternalAuthSecret))
	actionTypeHandler.Routes(api)
	governanceHandler.Routes(api)
	approvalsHandler.Routes(api)
	auditHandler.Routes(api)
	evidenceHandler.Routes(api)

	server := &http.Server{
		Addr:    config.Addr(),
		Handler: tracedServerHandler("nexus-v2", observability.Middleware(logger, router)),
	}

	deps := &Dependencies{
		Config:         config,
		DB:             db,
		Router:         router,
		Server:         server,
		tracerShutdown: tracerShutdown,
		watcherCancel:  watcherCancel,
	}
	deps.watcherWG.Add(1)
	go func() {
		defer deps.watcherWG.Done()
		approvalWatcher.Run(watcherCtx, config.WatcherInterval, config.WatcherBatchSize)
	}()
	return deps, nil
}

// tracedServerHandler wraps an HTTP handler with an OTel server span per
// request, extracting incoming trace context so the trace continues across
// services. Health probes are excluded to avoid flooding traces.
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
	if d.watcherCancel != nil {
		d.watcherCancel()
		d.watcherWG.Wait()
	}
	if d.tracerShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.tracerShutdown(shutdownCtx)
		cancel()
	}
	if d.DB != nil {
		d.DB.Close()
	}
}
