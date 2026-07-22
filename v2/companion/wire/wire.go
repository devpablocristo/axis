package wire

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/companion-v2/cmd/config"
	"github.com/devpablocristo/companion-v2/internal/capabilities"
	"github.com/devpablocristo/companion-v2/internal/executionstats"
	"github.com/devpablocristo/companion-v2/internal/infra/migrations"
	"github.com/devpablocristo/companion-v2/internal/jobroles"
	"github.com/devpablocristo/companion-v2/internal/learning"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/nexusclient"
	"github.com/devpablocristo/companion-v2/internal/profiletemplates"
	"github.com/devpablocristo/companion-v2/internal/runtimeclient"
	"github.com/devpablocristo/companion-v2/internal/virployees"
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
}

// runtimeAnswererAdapter adapts the runtime client's Answer to the virployees
// RuntimeAnswererPort, keeping the virployees package free of runtimeclient.
type runtimeAnswererAdapter struct{ client *runtimeclient.Client }

func (a runtimeAnswererAdapter) Answer(ctx context.Context, in virployees.AnswerInput) (virployees.AnswerOutput, error) {
	res, err := a.client.Answer(ctx, runtimeclient.AnswerRequest{
		SystemPrompt:   in.SystemPrompt,
		JobRole:        in.JobRole,
		InputJSON:      in.InputJSON,
		ResponseSchema: in.ResponseSchema,
	})
	if err != nil {
		return virployees.AnswerOutput{}, err
	}
	return virployees.AnswerOutput{
		OutputText:    res.OutputText,
		OutputJSON:    res.OutputJSON,
		Answered:      res.Answered,
		ModelID:       res.ModelID,
		PromptVersion: res.PromptVersion,
	}, nil
}

func Initialize(ctx context.Context) (*Dependencies, error) {
	config := cfg.Load()
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("COMPANION_V2_DATABASE_URL or DATABASE_URL is required")
	}
	if config.InternalAuthSecret == "" {
		return nil, fmt.Errorf("COMPANION_V2_INTERNAL_AUTH_SECRET is required")
	}

	dbConfig, err := postgres.ConfigFromEnv("COMPANION_V2_DB", "companion_v2")
	if err != nil {
		return nil, err
	}
	db, err := postgres.OpenWithConfig(ctx, config.DatabaseURL, dbConfig)
	if err != nil {
		return nil, err
	}

	if config.RunMigrations {
		if err := postgres.MigrateUp(ctx, db, "companion_v2", migrations.Files, migrations.Dir); err != nil {
			db.Close()
			return nil, err
		}
	}

	logger := observability.NewJSONLogger("companion-v2")
	tracerShutdown, err := observability.NewTracerProvider(ctx, observability.TracingConfig{
		ServiceName:    "companion-v2",
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

	jobRolesRepo := jobroles.NewRepository(db.Pool())
	jobRolesUsecases, err := jobroles.NewUseCases(jobRolesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	jobRolesHandler := jobroles.NewHandler(jobRolesUsecases)

	capabilitiesRepo := capabilities.NewRepository(db.Pool())
	capabilitiesUsecases, err := capabilities.NewUseCases(capabilitiesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	capabilitiesHandler := capabilities.NewHandler(capabilitiesUsecases)

	profileTemplatesRepo := profiletemplates.NewRepository(db.Pool())
	profileTemplatesUsecases, err := profiletemplates.NewUseCases(profileTemplatesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	profileTemplatesHandler := profiletemplates.NewHandler(profileTemplatesUsecases)

	virployeesRepo := virployees.NewRepository(db.Pool())
	virployeesUsecases, err := virployees.NewUseCases(virployeesRepo, jobRolesUsecases)
	if err != nil {
		db.Close()
		return nil, err
	}
	virployeesUsecases.SetCapabilityValidator(capabilitiesUsecases)
	virployeesUsecases.SetProfileTemplateReader(profileTemplatesUsecases)
	if config.NexusBaseURL != "" {
		nexusClient := nexusclient.New(config.NexusBaseURL, &http.Client{Timeout: 5 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}, config.InternalAuthSecret)
		virployeesUsecases.SetGovernanceChecker(nexusClient)
		virployeesUsecases.SetApprovalReader(nexusClient)
		virployeesUsecases.SetExecutionResultReporter(nexusClient)
	}
	if config.RuntimeBaseURL != "" {
		runtimePlanner := runtimeclient.New(config.RuntimeBaseURL, &http.Client{Timeout: 30 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}, config.InternalAuthSecret)
		virployeesUsecases.SetRuntimePlanner(runtimePlanner)
		virployeesUsecases.SetRuntimeAnswerer(runtimeAnswererAdapter{client: runtimePlanner})
		virployeesUsecases.SetDocumentFetcher(virployees.NewHTTPDocumentFetcher(&http.Client{Timeout: 15 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}))
	}
	if config.ExecutionMode == "local" {
		virployeesUsecases.RegisterExecutor("calendar.events.create", virployees.NewLocalCalendarExecutor(virployeesRepo))
	}
	memoriesUsecases := memories.NewUseCases(memories.NewRepository(db.Pool()))
	virployeesUsecases.SetMemoryReader(memoriesUsecases)
	virployeesHandler := virployees.NewHandler(virployeesUsecases)
	memoriesHandler := memories.NewHandler(memoriesUsecases)
	executionStatsHandler := executionstats.NewHandler(executionstats.NewUseCases(executionstats.NewRepository(db.Pool())))
	learningUsecases := learning.NewUseCases(learning.NewRepository(db.Pool()))
	learningUsecases.SetMinExecutions(config.LearningMinExecutions)
	learningUsecases.SetCapabilityChecker(learning.NewCapabilityChecker(capabilitiesUsecases))
	learningUsecases.SetMemoryInstaller(learning.NewMemoriesInstaller(memoriesUsecases))
	learningUsecases.SetAuthorizer(memoriesUsecases)
	// Optional LLM procedure enricher (PR5): double opt-in — needs the runtime
	// configured AND the flag on. Off by default, so CI/dev keep the deterministic
	// distillation and never call the model.
	if config.RuntimeBaseURL != "" && config.LearningEnricherEnabled {
		enrichClient := runtimeclient.New(config.RuntimeBaseURL, &http.Client{Timeout: 30 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}, config.InternalAuthSecret)
		learningUsecases.SetProcedureEnricher(learning.NewRuntimeEnricher(enrichClient))
	}
	learningHandler := learning.NewHandler(learningUsecases)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ginmw.NewBodySizeLimit(config.MaxBodyBytes))
	router.Use(ginmw.NewCORS(ginmw.CORSConfig{
		Origins:      config.CORSOrigins,
		AllowHeaders: []string{"X-Actor-ID", "X-Tenant-ID", "X-Axis-Tenant-Role"},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)
	api := router.Group("/v1")
	api.Use(internalAuthMiddleware(config.InternalAuthSecret))
	jobRolesHandler.Routes(api)
	capabilitiesHandler.Routes(api)
	profileTemplatesHandler.Routes(api)
	virployeesHandler.Routes(api)
	memoriesHandler.Routes(api)
	executionStatsHandler.Routes(api)
	learningHandler.Routes(api)

	server := &http.Server{
		Addr:    config.Addr(),
		Handler: tracedServerHandler("companion-v2", observability.Middleware(logger, router)),
	}

	return &Dependencies{
		Config:         config,
		DB:             db,
		Router:         router,
		Server:         server,
		tracerShutdown: tracerShutdown,
	}, nil
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
	if d.tracerShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.tracerShutdown(shutdownCtx)
		cancel()
	}
	if d.DB != nil {
		d.DB.Close()
	}
}
