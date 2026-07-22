package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/companion-v2/cmd/config"
	"github.com/devpablocristo/companion-v2/internal/artifactindex"
	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/capabilities"
	"github.com/devpablocristo/companion-v2/internal/executionstats"
	"github.com/devpablocristo/companion-v2/internal/infra/migrations"
	"github.com/devpablocristo/companion-v2/internal/jobroles"
	"github.com/devpablocristo/companion-v2/internal/jobs"
	"github.com/devpablocristo/companion-v2/internal/learning"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/nexusclient"
	"github.com/devpablocristo/companion-v2/internal/outbox"
	"github.com/devpablocristo/companion-v2/internal/profiletemplates"
	"github.com/devpablocristo/companion-v2/internal/runtimeclient"
	"github.com/devpablocristo/companion-v2/internal/virployees"
	postgres "github.com/devpablocristo/platform/databases/postgres/go"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	observability "github.com/devpablocristo/platform/observability/go"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2/google"
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

// runtimeAnswererAdapter adapts the runtime client's Answer to the virployees
// RuntimeAnswererPort, keeping the virployees package free of runtimeclient.
type runtimeAnswererAdapter struct{ client *runtimeclient.Client }

func (a runtimeAnswererAdapter) Answer(ctx context.Context, in virployees.AnswerInput) (virployees.AnswerOutput, error) {
	parts := make([]runtimeclient.ContentPart, 0, len(in.ContentParts))
	for _, part := range in.ContentParts {
		locator, _ := json.Marshal(part.Locator)
		parts = append(parts, runtimeclient.ContentPart{
			Kind: string(part.Kind), Text: part.Text, Data: part.Data, URI: part.URI,
			MIMEType: part.MIMEType, Name: part.Name, SHA256: part.SHA256,
			DocumentID: part.DocumentID, Locator: locator,
		})
	}
	res, err := a.client.Answer(ctx, runtimeclient.AnswerRequest{
		SystemPrompt:   in.SystemPrompt,
		JobRole:        in.JobRole,
		InputJSON:      in.InputJSON,
		ResponseSchema: in.ResponseSchema,
		ContentParts:   parts,
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

// auditEmitterAdapter maps the companion-owned audit event onto the Nexus audit
// client, keeping the virployees package free of nexusclient.
type auditEmitterAdapter struct{ client *nexusclient.Client }

func (a auditEmitterAdapter) AppendAuditEvent(ctx context.Context, in virployees.AuditEventInput) error {
	return a.client.AppendAuditEvent(ctx, in.TenantID, nexusclient.AuditEvent{
		VirployeeID: in.VirployeeID,
		ActorType:   in.ActorType,
		ActorID:     in.ActorID,
		SubjectType: in.SubjectType,
		SubjectID:   in.SubjectID,
		EventType:   in.EventType,
		Summary:     in.Summary,
		Data:        in.Data,
	})
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
	var nexusClient *nexusclient.Client
	if config.NexusBaseURL != "" {
		nexusClient = nexusclient.New(config.NexusBaseURL, &http.Client{Timeout: 5 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}, config.InternalAuthSecret)
		virployeesUsecases.SetGovernanceChecker(nexusClient)
		virployeesUsecases.SetApprovalReader(nexusClient)
		// Emit tamper-evident audit events (assist + governed executions) into the
		// Nexus ledger. Best-effort: emission failures never fail the work.
		virployeesUsecases.SetAuditEmitter(auditEmitterAdapter{client: nexusClient})
	}
	var runtimePlanner *runtimeclient.Client
	if config.RuntimeBaseURL != "" {
		runtimePlanner = runtimeclient.New(config.RuntimeBaseURL, &http.Client{Timeout: 30 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}, config.InternalAuthSecret)
		virployeesUsecases.SetRuntimePlanner(runtimePlanner)
		virployeesUsecases.SetRuntimeAnswerer(runtimeAnswererAdapter{client: runtimePlanner})
		virployeesUsecases.SetDocumentFetcher(virployees.NewHTTPDocumentFetcher(&http.Client{Timeout: 15 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}))
	}
	if config.ArtifactStagingBucket != "" {
		if runtimePlanner == nil {
			db.Close()
			return nil, fmt.Errorf("artifact ingestion requires COMPANION_V2_RUNTIME_BASE_URL")
		}
		tokens, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/devstorage.read_write")
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("artifact staging credentials: %w", err)
		}
		store, err := artifacts.NewGCSStore(artifacts.GCSStoreConfig{
			Bucket: config.ArtifactStagingBucket, CMEKKey: config.ArtifactCMEKKey,
			Prefix: config.ArtifactStagingPrefix, RequireCMEK: config.Environment == "production",
		}, tokens, &http.Client{Timeout: 10 * time.Minute, Transport: otelhttp.NewTransport(http.DefaultTransport)})
		if err != nil {
			db.Close()
			return nil, err
		}
		var scanner artifacts.MalwareScannerPort = artifacts.DevelopmentScanner{}
		if config.MalwareScannerAddress != "" {
			scanner = artifacts.ClamAVScanner{Address: config.MalwareScannerAddress, Timeout: 2 * time.Minute}
		} else if config.Environment == "production" {
			db.Close()
			return nil, fmt.Errorf("production artifact ingestion requires COMPANION_V2_MALWARE_SCANNER_ADDRESS")
		}
		fetcher, err := artifacts.NewHTTPFetcher(
			&http.Client{Timeout: 5 * time.Minute, Transport: otelhttp.NewTransport(http.DefaultTransport)},
			config.ArtifactFetchAllowedHosts,
		)
		if err != nil {
			db.Close()
			return nil, err
		}
		artifactPipeline := artifacts.NewPipeline(
			artifacts.NewRepository(db.Pool()),
			fetcher,
			scanner,
			store,
			artifacts.TextFormatAdapter{}, artifacts.PDFFormatAdapter{}, artifacts.NativeMediaAdapter{},
		)
		indexService, err := artifactindex.NewService(
			artifactindex.NewChunker(), artifactindex.NewRuntimeEmbedder(runtimePlanner), artifactindex.NewRepository(db.Pool()),
		)
		if err != nil {
			db.Close()
			return nil, err
		}
		artifactPipeline.SetIndexer(indexService)
		virployeesUsecases.SetArtifactIngestor(artifactPipeline)
	}
	// Executors are wired per enabled mode (COMPANION_V2_EXECUTION_MODE is a set).
	// The local simulator and a real external executor can coexist on different
	// capabilities; with no mode enabled, execution stays simulation-only. When
	// both are enabled for calendar.events.create, google_calendar is wired last
	// and wins (real effects over simulation).
	if config.HasExecutionMode("local") {
		localExecutor := virployees.NewLocalCalendarExecutor(virployeesRepo)
		virployeesUsecases.RegisterExecutor("calendar.events.create", localExecutor)
		virployeesUsecases.RegisterExecutor("calendar.events.delete", localExecutor)
	}
	if config.HasExecutionMode("google_calendar") {
		if config.GoogleCalendarID == "" {
			db.Close()
			return nil, fmt.Errorf("google_calendar execution mode requires COMPANION_V2_GOOGLE_CALENDAR_ID")
		}
		calendarAPI, err := virployees.NewGoogleCalendarAPI(ctx)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("google calendar executor: %w", err)
		}
		// Both the forward action and its compensation (delete) run through the
		// same executor and the same governed path; the delete carries its own
		// binding (G3.5).
		googleExecutor := virployees.NewGoogleCalendarExecutor(calendarAPI, config.GoogleCalendarID)
		virployeesUsecases.RegisterExecutor("calendar.events.create", googleExecutor)
		virployeesUsecases.RegisterExecutor("calendar.events.delete", googleExecutor)
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
	backgroundCtx, backgroundCancel := context.WithCancel(ctx)
	jobsRepository := jobs.NewPostgresRepository(db.Pool())
	virployeesUsecases.SetAssistQueue(assistQueueAdapter{repository: jobsRepository})
	jobsWorker := jobs.NewWorker(jobsRepository, jobs.WorkerConfig{
		WorkerID: "companion-jobs-" + uuid.NewString(), Concurrency: config.JobWorkerConcurrency,
		PollInterval: config.JobPollInterval, LeaseDuration: config.WatcherLease,
		DefaultTimeout: config.JobTimeout, RecoveryBatch: config.WatcherBatchSize,
	})
	watcherConfig := virployees.WatcherConfig{
		StaleAssistAfter: config.StaleAssistAfter, StaleExecutionAfter: config.StaleExecutionAfter,
		Lease:     config.WatcherLease,
		BatchSize: config.WatcherBatchSize, MaxRecoveryAttempts: config.WatcherMaxRecoveries,
		WorkerID: "companion-watchers-" + uuid.NewString(),
	}
	jobsWorker.Register("operational.reconcile", func(jobCtx context.Context, _ jobs.Job) (json.RawMessage, error) {
		if err := virployeesUsecases.RunOperationalWatchersOnce(jobCtx, watcherConfig); err != nil {
			return nil, jobs.Retryable("operational_reconcile_failed", err)
		}
		requeued, err := virployeesUsecases.RequeueReceivedAssistRuns(jobCtx, watcherConfig.BatchSize)
		if err != nil {
			return nil, jobs.Retryable("assist_reconcile_failed", err)
		}
		evidence, _ := json.Marshal(map[string]any{"reconciled": true, "assist_runs_requeued": requeued})
		return evidence, nil
	})
	jobsWorker.Register(assistProcessJobKind, func(jobCtx context.Context, job jobs.Job) (json.RawMessage, error) {
		var payload assistJobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, jobs.Permanent("invalid_assist_job", err)
		}
		runID, err := uuid.Parse(payload.RunID)
		if err != nil {
			return nil, jobs.Permanent("invalid_assist_job", err)
		}
		run, processErr := virployeesUsecases.ProcessAssistRun(jobCtx, job.TenantID, runID, job.Attempts > 1)
		evidence, _ := json.Marshal(map[string]any{"run_id": runID.String(), "status": run.Status})
		if processErr != nil {
			if run.Status == "failed" || run.Status == "done" {
				return evidence, nil
			}
			return evidence, jobs.Retryable("assist_processing_failed", processErr)
		}
		return evidence, nil
	})
	var nexusOutbox *outbox.Dispatcher
	if nexusClient != nil {
		nexusOutbox = outbox.NewDispatcher(outbox.NewRepository(db.Pool()), outbox.SenderFunc(func(deliveryCtx context.Context, message outbox.Message) error {
			var payload outbox.NexusExecutionResult
			if err := json.Unmarshal(message.Payload, &payload); err != nil {
				return outbox.Permanent("invalid_outbox_payload", err)
			}
			if payload.GovernanceCheckID == "" || payload.IdempotencyKey == "" || payload.BindingHash == "" || payload.Status == "" {
				return outbox.Permanent("invalid_outbox_payload", fmt.Errorf("required delivery metadata is missing"))
			}
			if err := nexusClient.ReportExecutionResult(deliveryCtx, message.TenantID, payload.GovernanceCheckID, payload.IdempotencyKey, payload.BindingHash, payload.Status, payload.DurationMS, payload.Result); err != nil {
				return outbox.Retryable("nexus_unavailable", err)
			}
			return nil
		}), outbox.DispatcherConfig{
			WorkerID:    "companion-nexus-outbox-" + uuid.NewString(),
			Concurrency: config.JobWorkerConcurrency, PollInterval: config.JobPollInterval,
			Lease: config.WatcherLease, Timeout: 10 * time.Second,
			RecoveryBatch: config.WatcherBatchSize, BaseBackoff: config.OutboxBaseBackoff,
		})
	}

	deps := &Dependencies{
		Config:         config,
		DB:             db,
		Router:         router,
		Server:         server,
		tracerShutdown: tracerShutdown,
		watcherCancel:  backgroundCancel,
	}
	deps.watcherWG.Add(2)
	go func() {
		defer deps.watcherWG.Done()
		jobsWorker.Run(backgroundCtx)
	}()
	if nexusOutbox != nil {
		deps.watcherWG.Add(1)
		go func() {
			defer deps.watcherWG.Done()
			nexusOutbox.Run(backgroundCtx)
		}()
	}
	go func() {
		defer deps.watcherWG.Done()
		jobs.RunRecurringScheduler(backgroundCtx, jobsRepository, jobs.RecurringConfig{
			TenantID: "system", ProductSurface: "companion", Kind: "operational.reconcile",
			Interval: config.WatcherInterval, Timeout: config.JobTimeout,
			MaxAttempts: config.WatcherMaxRecoveries,
		})
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
