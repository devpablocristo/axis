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
	"github.com/devpablocristo/companion-v2/internal/attestation"
	"github.com/devpablocristo/companion-v2/internal/capabilities"
	"github.com/devpablocristo/companion-v2/internal/executionstats"
	"github.com/devpablocristo/companion-v2/internal/infra/migrations"
	"github.com/devpablocristo/companion-v2/internal/jobroles"
	"github.com/devpablocristo/companion-v2/internal/jobs"
	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/learning"
	"github.com/devpablocristo/companion-v2/internal/mcpgovernance"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/nexusclient"
	"github.com/devpablocristo/companion-v2/internal/outbox"
	"github.com/devpablocristo/companion-v2/internal/professionalauthority"
	"github.com/devpablocristo/companion-v2/internal/profiletemplates"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/runtimeclient"
	"github.com/devpablocristo/companion-v2/internal/virployees"
	"github.com/devpablocristo/companion-v2/internal/workforcerouting"
	postgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/errors/go/domainerr"
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

type memoryEmbeddingAdapter struct{ client *runtimeclient.Client }

func (a memoryEmbeddingAdapter) EmbedDocument(ctx context.Context, text string) ([]float32, string, error) {
	return a.embed(ctx, text, runtimeclient.EmbeddingTaskDocument)
}

func (a memoryEmbeddingAdapter) EmbedQuery(ctx context.Context, text string) ([]float32, string, error) {
	return a.embed(ctx, text, runtimeclient.EmbeddingTaskQuery)
}

func (a memoryEmbeddingAdapter) embed(ctx context.Context, input, task string) ([]float32, string, error) {
	result, err := a.client.Embed(ctx, runtimeclient.EmbedRequest{Texts: []string{input}, TaskType: task})
	if err != nil {
		return nil, "", err
	}
	if result.Dimensions != memories.EmbeddingDimensions || len(result.Embeddings) != 1 || len(result.Embeddings[0]) != memories.EmbeddingDimensions {
		return nil, "", fmt.Errorf("runtime returned invalid memory embedding shape")
	}
	return result.Embeddings[0], result.Model, nil
}

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
	professional := runtimeclient.ProfessionalContext{
		JobRoleID: in.ProfessionalContext.JobRoleID,
		Name:      in.ProfessionalContext.Name,
		Mission:   in.ProfessionalContext.Mission,
	}
	for _, responsibility := range in.ProfessionalContext.Responsibilities {
		professional.Responsibilities = append(professional.Responsibilities, runtimeclient.ProfessionalResponsibility{
			Title: responsibility.Title, Description: responsibility.Description,
			ExpectedOutcome: responsibility.ExpectedOutcome, Priority: responsibility.Priority,
		})
	}
	for _, criterion := range in.ProfessionalContext.SuccessCriteria {
		professional.SuccessCriteria = append(professional.SuccessCriteria, runtimeclient.ProfessionalSuccessCriterion{
			Title: criterion.Title, Description: criterion.Description,
			TargetValue: criterion.TargetValue, Priority: criterion.Priority,
		})
	}
	res, err := a.client.Answer(ctx, runtimeclient.AnswerRequest{
		SystemPrompt:        in.SystemPrompt,
		JobRole:             in.JobRole,
		ProfessionalContext: professional,
		InputJSON:           in.InputJSON,
		ResponseSchema:      in.ResponseSchema,
		ContentParts:        parts,
		GroundingMode:       in.GroundingMode,
	})
	if err != nil {
		return virployees.AnswerOutput{}, err
	}
	citations := make([]virployees.RuntimeCitation, 0, len(res.Citations))
	for _, citation := range res.Citations {
		citations = append(citations, virployees.RuntimeCitation{DocumentID: citation.DocumentID, SHA256: citation.SHA256, Locator: citation.Locator})
	}
	return virployees.AnswerOutput{
		OutputText:    res.OutputText,
		OutputJSON:    res.OutputJSON,
		Answered:      res.Answered,
		Status:        res.Status,
		Citations:     citations,
		ModelID:       res.ModelID,
		PromptVersion: res.PromptVersion,
		InputTokens:   res.Usage.InputTokens, OutputTokens: res.Usage.OutputTokens,
		EstimatedCostMicroUSD: res.Usage.EstimatedCostMicroUSD,
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
	workforceUsecases := workforcerouting.NewUseCases(workforcerouting.NewRepository(db.Pool()))
	workforceHandler := workforcerouting.NewHandler(workforceUsecases)
	knowledgeRepository := knowledgebases.NewRepository(db.Pool())
	knowledgeUsecases := knowledgebases.NewUseCases(knowledgeRepository)
	knowledgeHandler := knowledgebases.NewHandler(knowledgeUsecases)

	capabilitiesRepo := capabilities.NewRepository(db.Pool())
	capabilitiesUsecases, err := capabilities.NewUseCases(capabilitiesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	capabilitiesHandler := capabilities.NewHandler(capabilitiesUsecases)
	quotaRepository := quotas.NewRepository(db.Pool(), config.Environment == "production")
	quotaHandler := quotas.NewHandler(quotaRepository)
	capabilitiesUsecases.SetQuotaPolicyChecker(quotaRepository)

	profileTemplatesRepo := profiletemplates.NewRepository(db.Pool())
	profileTemplatesUsecases, err := profiletemplates.NewUseCases(profileTemplatesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	profileTemplatesHandler := profiletemplates.NewHandler(profileTemplatesUsecases)

	virployeesRepo := virployees.NewRepository(db.Pool())
	attestationKey, err := resolveAttestationKey(ctx, config)
	if err != nil {
		db.Close()
		return nil, err
	}
	attestor, err := attestation.NewSigner(attestationKey, config.ServiceVersion)
	for i := range attestationKey {
		attestationKey[i] = 0
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	virployeesRepo.SetExecutionAttestor(attestor)
	virployeesUsecases, err := virployees.NewUseCases(virployeesRepo, jobRolesUsecases)
	if err != nil {
		db.Close()
		return nil, err
	}
	virployeesUsecases.SetContinuityAssignmentValidator(workforceUsecases)
	virployeesUsecases.SetCapabilityValidator(capabilitiesUsecases)
	virployeesUsecases.SetProfileTemplateReader(profileTemplatesUsecases)
	virployeesUsecases.SetQuotaPorts(quotaRepository, quotaRepository)
	authorityUsecases := professionalauthority.NewUseCases(professionalauthority.NewRepository(db.Pool()))
	virployeesUsecases.SetAuthorityEvaluator(authorityUsecases)
	mcpRepository := mcpgovernance.NewRepository(db.Pool())
	mcpUsecases := mcpgovernance.NewToolInvocationGate(
		mcpRepository, capabilitiesUsecases, virployeesUsecases, authorityUsecases,
		mcpWriteGateAdapter{virployees: virployeesUsecases},
	)
	virployeesUsecases.SetMCPExecutionContextValidator(mcpUsecases)
	mcpHandler := mcpgovernance.NewHandler(mcpUsecases)
	var nexusClient *nexusclient.Client
	if config.NexusBaseURL != "" {
		nexusClient = nexusclient.New(config.NexusBaseURL, &http.Client{Timeout: 5 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}, config.InternalAuthSecret)
		authorityUsecases.SetDelegationAuthorizer(nexusClient)
		virployeesUsecases.SetGovernanceChecker(nexusClient)
		virployeesUsecases.SetGovernanceRevalidator(nexusClient)
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
	production := config.Environment == "production"
	var artifactStore artifacts.ArtifactStorePort
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
		artifactStore, err = artifacts.NewGCSStore(artifacts.GCSStoreConfig{
			Bucket: config.ArtifactStagingBucket, CMEKKey: config.ArtifactCMEKKey,
			Prefix: config.ArtifactStagingPrefix, RequireCMEK: production,
		}, tokens, &http.Client{Timeout: 10 * time.Minute, Transport: otelhttp.NewTransport(http.DefaultTransport)})
		if err != nil {
			db.Close()
			return nil, err
		}
	} else if production {
		db.Close()
		return nil, fmt.Errorf("production artifact ingestion requires COMPANION_V2_ARTIFACT_STAGING_BUCKET with CMEK")
	} else if config.ArtifactLocalStagingDir != "" {
		artifactStore, err = artifacts.NewLocalStore(artifacts.LocalStoreConfig{
			RootDir: config.ArtifactLocalStagingDir, MaxBytes: config.ArtifactLocalMaxBytes,
		})
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("local artifact staging: %w", err)
		}
	}
	if artifactStore != nil {
		if runtimePlanner == nil {
			db.Close()
			return nil, fmt.Errorf("artifact ingestion requires COMPANION_V2_RUNTIME_BASE_URL")
		}
		var scanner artifacts.MalwareScannerPort = artifacts.DevelopmentScanner{}
		if config.MalwareScannerAddress != "" {
			scanner = artifacts.ClamAVScanner{Address: config.MalwareScannerAddress, Timeout: 2 * time.Minute}
		} else if production {
			db.Close()
			return nil, fmt.Errorf("production artifact ingestion requires COMPANION_V2_MALWARE_SCANNER_ADDRESS")
		}
		var fetcher artifacts.ArtifactFetcherPort
		if len(config.ArtifactFetchAllowedHosts) > 0 {
			fetcher, err = artifacts.NewHTTPFetcher(
				&http.Client{Timeout: 5 * time.Minute, Transport: otelhttp.NewTransport(http.DefaultTransport)},
				config.ArtifactFetchAllowedHosts,
			)
			if err != nil {
				db.Close()
				return nil, err
			}
		}
		var extractor artifacts.ExtractionPort
		if config.ArtifactExtractorBaseURL != "" {
			extractor, err = artifacts.NewHTTPExtractionClient(
				config.ArtifactExtractorBaseURL,
				&http.Client{Timeout: 10 * time.Minute, Transport: otelhttp.NewTransport(http.DefaultTransport)},
				config.InternalAuthSecret,
			)
			if err != nil {
				db.Close()
				return nil, err
			}
		}
		artifactRepository := artifacts.NewRepository(db.Pool())
		artifactPipeline := artifacts.NewPipeline(
			artifactRepository,
			fetcher,
			scanner,
			artifactStore,
			artifacts.OfficeFormatAdapter{Extractor: extractor},
			artifacts.DICOMFormatAdapter{Extractor: extractor},
			artifacts.PDFFormatAdapter{Extractor: extractor},
			artifacts.ImageFormatAdapter{Extractor: extractor},
			artifacts.AudioFormatAdapter{Extractor: extractor},
			artifacts.VideoFormatAdapter{Extractor: extractor},
			artifacts.TextFormatAdapter{},
		)
		artifactEmbedder := artifactindex.NewRuntimeEmbedder(runtimePlanner)
		artifactEmbedder.SetQuotaPorts(quotaRepository, quotaRepository)
		indexService, err := artifactindex.NewService(
			artifactindex.NewChunker(), artifactEmbedder, artifactindex.NewRepository(db.Pool()),
		)
		if err != nil {
			db.Close()
			return nil, err
		}
		artifactPipeline.SetIndexer(indexService)
		knowledgeUsecases.SetArtifactIngestor(artifactPipeline)
		virployeesUsecases.SetArtifactIngestor(artifactPipeline)
		virployeesUsecases.SetArtifactCorpusReader(artifacts.NewCorpusReader(artifactRepository, indexService))
		knowledgeRetriever, err := knowledgebases.NewRetriever(knowledgeRepository, indexService)
		if err != nil {
			db.Close()
			return nil, err
		}
		virployeesUsecases.SetKnowledgeRetriever(knowledgeRetriever)
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
		calendarAPI, err := resolveGoogleCalendarAPI(ctx, config)
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
	memoriesUsecases.SetQuotaPorts(quotaRepository, quotaRepository)
	if runtimePlanner != nil {
		memoriesUsecases.SetEmbedder(memoryEmbeddingAdapter{client: runtimePlanner})
	}
	virployeesUsecases.SetMemoryReader(memoriesUsecases)
	virployeesHandler := virployees.NewHandler(virployeesUsecases)
	authorityHandler := professionalauthority.NewHandler(authorityUsecases)
	coordinationHandler := virployees.NewCoordinationHandler(virployeesUsecases)
	memoriesHandler := memories.NewHandler(memoriesUsecases)
	executionStatsHandler := executionstats.NewHandler(executionstats.NewUseCases(executionstats.NewRepository(db.Pool())))
	learningUsecases := learning.NewUseCases(learning.NewRepository(db.Pool()))
	learningUsecases.SetQuotaPorts(quotaRepository, quotaRepository)
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
	router.Use(routeAwareBodySizeLimit(config.MaxBodyBytes, config.KnowledgeUploadMaxBodyBytes))
	router.Use(ginmw.NewCORS(ginmw.CORSConfig{
		Origins:      config.CORSOrigins,
		AllowHeaders: []string{"X-Actor-ID", "X-Tenant-ID", "X-Axis-Tenant-Role", "X-Axis-Virployee-ID", "X-Axis-Subject-ID", "X-Axis-Case-ID", "X-Idempotency-Key"},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)
	api := router.Group("/v1")
	api.Use(internalAuthMiddleware(config.InternalAuthSecret))
	mcp := router.Group("")
	mcp.Use(internalAuthMiddleware(config.InternalAuthSecret))
	mcpHandler.MCPRoutes(mcp)
	mcpHandler.MCPRoutes(api)
	jobRolesHandler.Routes(api)
	workforceHandler.Routes(api)
	knowledgeHandler.Routes(api)
	capabilitiesHandler.Routes(api)
	quotaHandler.Routes(api)
	profileTemplatesHandler.Routes(api)
	virployeesHandler.Routes(api)
	authorityHandler.Routes(api)
	coordinationHandler.Routes(api)
	memoriesHandler.Routes(api)
	executionStatsHandler.Routes(api)
	learningHandler.Routes(api)
	mcpHandler.AdminRoutes(api)

	server := &http.Server{
		Addr:    config.Addr(),
		Handler: tracedServerHandler("companion-v2", observability.Middleware(logger, router)),
	}
	backgroundCtx, backgroundCancel := context.WithCancel(ctx)
	jobsRepository := jobs.NewPostgresRepository(db.Pool())
	virployeesUsecases.SetAssistQueue(assistQueueAdapter{repository: jobsRepository})
	virployeesUsecases.SetCoordinationQueue(coordinationQueueAdapter{repository: jobsRepository})
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
		coordination, err := virployeesUsecases.RequeueCoordinationWork(jobCtx, watcherConfig.BatchSize)
		if err != nil {
			return nil, jobs.Retryable("coordination_reconcile_failed", err)
		}
		evidence, _ := json.Marshal(map[string]any{"reconciled": true, "assist_runs_requeued": requeued, "coordination": coordination})
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
	jobsWorker.Register(virployees.JobKindSpecialistConsult, func(jobCtx context.Context, job jobs.Job) (json.RawMessage, error) {
		var payload struct {
			ConsultationID string `json:"consultation_id"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, jobs.Permanent("invalid_specialist_consult_job", err)
		}
		id, err := uuid.Parse(payload.ConsultationID)
		if err != nil {
			return nil, jobs.Permanent("invalid_specialist_consult_job", err)
		}
		item, processErr := virployeesUsecases.ProcessSpecialistConsultation(jobCtx, job.TenantID, id, job.Attempts)
		evidence, _ := json.Marshal(map[string]any{"consultation_id": id.String(), "status": item.Status, "output_hash": item.OutputHash})
		if processErr != nil {
			return evidence, jobs.Retryable("specialist_consult_failed", processErr)
		}
		return evidence, nil
	})
	jobsWorker.Register(virployees.JobKindOrchestrationReconcile, func(jobCtx context.Context, job jobs.Job) (json.RawMessage, error) {
		var payload struct {
			PlanID string `json:"plan_id"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, jobs.Permanent("invalid_orchestration_reconcile_job", err)
		}
		id, err := uuid.Parse(payload.PlanID)
		if err != nil {
			return nil, jobs.Permanent("invalid_orchestration_reconcile_job", err)
		}
		plan, reconcileErr := virployeesUsecases.ReconcileOrchestration(jobCtx, job.TenantID, id)
		evidence, _ := json.Marshal(map[string]any{"plan_id": id.String(), "status": plan.Status, "completed": plan.CompletedCount, "failed": plan.FailedCount})
		if reconcileErr != nil {
			return evidence, jobs.Retryable("orchestration_reconcile_failed", reconcileErr)
		}
		return evidence, nil
	})
	jobsWorker.Register(virployees.JobKindOrchestrationSynthesis, func(jobCtx context.Context, job jobs.Job) (json.RawMessage, error) {
		var payload struct {
			PlanID string `json:"plan_id"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, jobs.Permanent("invalid_orchestration_synthesis_job", err)
		}
		id, err := uuid.Parse(payload.PlanID)
		if err != nil {
			return nil, jobs.Permanent("invalid_orchestration_synthesis_job", err)
		}
		run, synthesisErr := virployeesUsecases.SynthesizeOrchestration(jobCtx, job.TenantID, id, job.Attempts)
		evidence, _ := json.Marshal(map[string]any{"plan_id": id.String(), "run_id": run.ID.String(), "status": run.Status})
		if synthesisErr != nil {
			return evidence, jobs.Retryable("orchestration_synthesis_failed", synthesisErr)
		}
		return evidence, nil
	})
	jobsWorker.Register("handoff.expire", func(jobCtx context.Context, _ jobs.Job) (json.RawMessage, error) {
		count, err := virployeesUsecases.ExpireHandoffs(jobCtx, config.WatcherBatchSize)
		if err != nil {
			return nil, jobs.Retryable("handoff_expire_failed", err)
		}
		evidence, _ := json.Marshal(map[string]any{"expired": count})
		return evidence, nil
	})
	jobsWorker.Register("memory.index", func(jobCtx context.Context, job jobs.Job) (json.RawMessage, error) {
		var payload memories.IndexJobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, jobs.Permanent("invalid_memory_index_job", err)
		}
		memoryID, err := uuid.Parse(payload.MemoryID)
		if err != nil || payload.Version <= 0 {
			return nil, jobs.Permanent("invalid_memory_index_job", err)
		}
		indexed, err := memoriesUsecases.IndexMemory(jobCtx, job.TenantID, memoryID, payload.Version)
		if err != nil {
			if domainerr.IsNotFound(err) || domainerr.IsConflict(err) {
				return nil, jobs.Permanent("memory_version_stale", err)
			}
			return nil, jobs.Retryable("memory_index_failed", err)
		}
		evidence, _ := json.Marshal(map[string]any{
			"memory_id": indexed.ID, "version": indexed.Version,
			"content_hash": indexed.ContentHash, "embedding_model": indexed.EmbeddingModel,
			"embedding_version": indexed.EmbeddingVersion,
		})
		return evidence, nil
	})
	jobsWorker.Register("memory.decay", func(jobCtx context.Context, _ jobs.Job) (json.RawMessage, error) {
		count, err := memoriesUsecases.DecayDue(jobCtx, config.WatcherBatchSize)
		if err != nil {
			return nil, jobs.Retryable("memory_decay_failed", err)
		}
		evidence, _ := json.Marshal(map[string]any{"processed": count})
		return evidence, nil
	})
	var nexusOutbox *outbox.Dispatcher
	if nexusClient != nil {
		nexusOutbox = outbox.NewDispatcher(outbox.NewRepository(db.Pool()), newNexusOutboxSender(nexusClient), outbox.DispatcherConfig{
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
	deps.watcherWG.Add(4)
	go func() {
		defer deps.watcherWG.Done()
		jobsWorker.Run(backgroundCtx)
	}()
	go func() {
		defer deps.watcherWG.Done()
		jobs.RunRecurringScheduler(backgroundCtx, jobsRepository, jobs.RecurringConfig{
			TenantID: "system", ProductSurface: "companion", Kind: "handoff.expire",
			Interval: config.WatcherInterval, Timeout: config.JobTimeout,
			MaxAttempts: config.WatcherMaxRecoveries,
		})
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
	go func() {
		defer deps.watcherWG.Done()
		jobs.RunRecurringScheduler(backgroundCtx, jobsRepository, jobs.RecurringConfig{
			TenantID: "system", ProductSurface: "companion", Kind: "memory.decay",
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
