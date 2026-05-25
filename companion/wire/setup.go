package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/agentfleet"
	"github.com/devpablocristo/companion/internal/assist"
	"github.com/devpablocristo/companion/internal/business"
	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/connectors"
	"github.com/devpablocristo/companion/internal/connectors/registry"
	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/jobs"
	"github.com/devpablocristo/companion/internal/memory"
	nexusassist "github.com/devpablocristo/companion/internal/nexus_assist"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/securityevals"
	"github.com/devpablocristo/companion/internal/tasks"
	"github.com/devpablocristo/companion/internal/watchers"
	"github.com/devpablocristo/companion/internal/watchers/pymesclient"
	"github.com/devpablocristo/platform/authn/go/internaljwt"
	"github.com/devpablocristo/platform/config/go/envconfig"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/http/go/health"

	memdomain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

type taskMemoryAdapter struct {
	uc   *memory.Usecases
	repo tasks.Repository
}

type taskPlannerAdapter struct {
	uc *tasks.Usecases
}

// taskOrgGetter resuelve el org_id de una task para que el handler de
// memoria pueda autorizar memorias scope=task contra el principal.
type taskOrgGetter struct {
	repo tasks.Repository
}

type agentRuntimeResolver struct {
	uc *agentfleet.Usecases
}

type taskOwnershipAdapter struct {
	repo tasks.Repository
}

type businessMemoryProjector struct {
	uc *memory.Usecases
}

func (r agentRuntimeResolver) ResolveRuntimeAgent(ctx context.Context, orgID, productSurface, agentID string) (runtime.RuntimeAgentConfig, error) {
	agent, err := r.uc.GetAgent(ctx, orgID, productSurface, agentID)
	if err != nil {
		return runtime.RuntimeAgentConfig{}, err
	}
	return runtime.RuntimeAgentConfig{
		AgentID:             agent.AgentID,
		ProfileID:           agent.ProfileID,
		Role:                agent.Role,
		Status:              agent.Status,
		MaxAutonomy:         runtime.AutonomyLevel(agent.MaxAutonomy),
		AllowedTools:        append([]string(nil), agent.AllowedTools...),
		AllowedCapabilities: append([]string(nil), agent.AllowedCapabilities...),
		AllowedConnectors:   append([]string(nil), agent.AllowedConnectors...),
		MemoryScopeID:       agent.MemoryScopeID,
		SharedMemoryPolicy:  agent.SharedMemoryPolicy,
		Limits:              agent.Limits,
		SLA:                 agent.SLA,
		Version:             agent.Version,
	}, nil
}

func (a taskOwnershipAdapter) TransferTaskOwnership(ctx context.Context, orgID, taskID, agentID string) error {
	id, err := uuid.Parse(taskID)
	if err != nil || id == uuid.Nil {
		return fmt.Errorf("invalid task_id")
	}
	task, err := a.repo.GetTaskByID(ctx, id)
	if err != nil {
		return err
	}
	if task.OrgID != orgID {
		return fmt.Errorf("task belongs to a different customer org")
	}
	task.AssignedTo = agentID
	_, err = a.repo.UpdateTask(ctx, task)
	return err
}

func (p businessMemoryProjector) ProjectBusinessModel(ctx context.Context, model business.Model) error {
	if p.uc == nil {
		return nil
	}
	_, err := p.uc.Upsert(ctx, memory.UpsertInput{
		OrgID:           model.OrgID,
		ProductSurface:  model.ProductSurface,
		Kind:            memdomain.MemoryBusinessContext,
		MemoryType:      memdomain.MemoryTypeBusinessContext,
		ScopeType:       memdomain.ScopeOrg,
		ScopeID:         model.OrgID,
		Key:             "business_model:active",
		PayloadJSON:     business.BusinessModelMemoryPayload(model),
		ContentText:     model.Summary(),
		ProvenanceJSON:  json.RawMessage(`{"source":"business_model","projection":"active_summary"}`),
		Confidence:      1,
		RetentionPolicy: "business_model",
		Source:          "business_model",
		Supersede:       true,
	})
	return err
}

func (g taskOrgGetter) GetTaskOrg(ctx context.Context, taskID uuid.UUID) (string, error) {
	t, err := g.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return "", err
	}
	return t.OrgID, nil
}

func (a taskPlannerAdapter) GetTaskPlan(ctx context.Context, taskID uuid.UUID) (taskdomain.TaskPlan, error) {
	return a.uc.GetTaskPlan(ctx, taskID)
}

func (a taskPlannerAdapter) SetTaskPlan(ctx context.Context, taskID uuid.UUID, in runtime.PlannerSetTaskPlanInput) (taskdomain.TaskPlan, error) {
	steps := make([]tasks.SetTaskPlanStepInput, 0, len(in.Steps))
	for _, step := range in.Steps {
		steps = append(steps, tasks.SetTaskPlanStepInput{
			ID:              step.ID,
			StepKey:         step.StepKey,
			Title:           step.Title,
			Status:          step.Status,
			DependsOnJSON:   step.DependsOnJSON,
			ToolName:        step.ToolName,
			Capability:      step.Capability,
			ExpectedOutcome: step.ExpectedOutcome,
			Postcondition:   step.Postcondition,
			EvidenceJSON:    step.EvidenceJSON,
			Observation:     step.Observation,
			Blocker:         step.Blocker,
			ErrorMessage:    step.ErrorMessage,
			AttemptCount:    step.AttemptCount,
			SortOrder:       step.SortOrder,
		})
	}
	return a.uc.SetTaskPlan(ctx, taskID, tasks.SetTaskPlanInput{
		Objective:       in.Objective,
		Status:          in.Status,
		Strategy:        in.Strategy,
		AssumptionsJSON: in.AssumptionsJSON,
		ConstraintsJSON: in.ConstraintsJSON,
		CheckpointJSON:  in.CheckpointJSON,
		NextAction:      in.NextAction,
		Blocker:         in.Blocker,
		CreatedBy:       in.CreatedBy,
		Steps:           steps,
	})
}

func (a taskPlannerAdapter) UpdateTaskPlanStep(ctx context.Context, taskID, stepID uuid.UUID, in runtime.PlannerUpdateTaskPlanStepInput) (taskdomain.TaskPlan, error) {
	return a.uc.UpdateTaskPlanStep(ctx, taskID, stepID, tasks.UpdateTaskPlanStepInput{
		Status:         in.Status,
		EvidenceJSON:   in.EvidenceJSON,
		Observation:    in.Observation,
		Blocker:        in.Blocker,
		ErrorMessage:   in.ErrorMessage,
		CheckpointJSON: in.CheckpointJSON,
		NextAction:     in.NextAction,
	})
}

func (a taskPlannerAdapter) RecordTaskPlanCheckpoint(ctx context.Context, taskID uuid.UUID, in runtime.PlannerRecordTaskPlanCheckpointInput) (taskdomain.TaskPlan, error) {
	return a.uc.RecordTaskPlanCheckpoint(ctx, taskID, tasks.RecordTaskPlanCheckpointInput{
		Status:         in.Status,
		CheckpointJSON: in.CheckpointJSON,
		NextAction:     in.NextAction,
		Blocker:        in.Blocker,
	})
}

func (a taskPlannerAdapter) PrepareTaskPlanCompensation(ctx context.Context, taskID, stepID uuid.UUID, in runtime.PlannerPrepareTaskPlanCompensationInput) (runtime.PlannerTaskPlanCompensationResult, error) {
	out, err := a.uc.PrepareTaskPlanCompensation(ctx, taskID, stepID, tasks.PrepareTaskPlanCompensationInput{
		Reason: in.Reason,
	})
	if err != nil {
		return runtime.PlannerTaskPlanCompensationResult{}, err
	}
	return runtime.PlannerTaskPlanCompensationResult{
		Plan:                out.Plan,
		Step:                out.Step,
		Status:              out.Status,
		Reason:              out.Reason,
		Compensation:        out.Compensation,
		NexusRequestID:      out.NexusRequestID,
		NexusStatus:         out.NexusStatus,
		NexusDecision:       out.NexusDecision,
		NexusBindingHash:    out.NexusBindingHash,
		ApprovalRequired:    out.ApprovalRequired,
		ApprovalUnavailable: out.ApprovalUnavailable,
	}, nil
}

func (a taskPlannerAdapter) ExecuteTaskPlanCompensation(ctx context.Context, taskID, stepID uuid.UUID, in runtime.PlannerExecuteTaskPlanCompensationInput) (runtime.PlannerTaskPlanCompensationExecutionResult, error) {
	out, err := a.uc.ExecuteTaskPlanCompensation(ctx, taskID, stepID, tasks.ExecuteTaskPlanCompensationInput{
		NexusRequestID: in.NexusRequestID,
	})
	if err != nil {
		return runtime.PlannerTaskPlanCompensationExecutionResult{}, err
	}
	return runtime.PlannerTaskPlanCompensationExecutionResult{
		Plan:                out.Plan,
		Step:                out.Step,
		Status:              out.Status,
		Reason:              out.Reason,
		Compensation:        out.Compensation,
		NexusRequestID:      out.NexusRequestID,
		NexusStatus:         out.NexusStatus,
		ExecutionID:         out.Execution.ID.String(),
		ExecutionStatus:     out.Execution.Status,
		VerificationStatus:  out.Verification.Status,
		VerificationSummary: out.Verification.Summary,
		ExternalRef:         out.Execution.ExternalRef,
		ApprovalRequired:    out.ApprovalRequired,
	}, nil
}

func (a taskMemoryAdapter) UpsertTaskMemory(ctx context.Context, taskID uuid.UUID, kind, key string, contentText string, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	task, err := a.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	_, err = a.uc.Upsert(ctx, memory.UpsertInput{
		OrgID:           task.OrgID,
		UserID:          task.CreatedBy,
		ProductSurface:  "companion",
		Kind:            memdomain.MemoryKind(kind),
		MemoryType:      memdomain.TypeForKind(memdomain.MemoryKind(kind)),
		ScopeType:       memdomain.ScopeTask,
		ScopeID:         taskID.String(),
		Key:             key,
		PayloadJSON:     payload,
		ContentText:     contentText,
		ProvenanceJSON:  json.RawMessage(`{"source":"task_projection"}`),
		Confidence:      1,
		RetentionPolicy: "task",
	})
	return err
}

func nexusSyncInterval() time.Duration {
	return envconfig.Duration("COMPANION_NEXUS_SYNC_INTERVAL_SEC", 30*time.Second)
}

func watcherInterval() time.Duration {
	return envconfig.Duration("COMPANION_WATCHER_INTERVAL_SEC", 0)
}

func watcherSyncInterval() time.Duration {
	return envconfig.Duration("COMPANION_WATCHER_SYNC_INTERVAL_SEC", watcherInterval())
}

func jobWorkerCount() int {
	raw := envconfig.Get("COMPANION_JOB_WORKERS", "2")
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return 2
	}
	return count
}

func jobPollInterval() time.Duration {
	return envconfig.Duration("COMPANION_JOB_POLL_INTERVAL_SEC", time.Second)
}

func jobLeaseDuration() time.Duration {
	return envconfig.Duration("COMPANION_JOB_LEASE_SEC", 30*time.Second)
}

func jobTimeout() time.Duration {
	return envconfig.Duration("COMPANION_JOB_TIMEOUT_SEC", 5*time.Minute)
}

func activeManifestRegistry(ctx context.Context, uc *capabilities.Usecases) *capabilities.Registry {
	records, err := uc.ListManifests(ctx, capabilities.ManifestFilter{Status: capabilities.ManifestStatusActive, Limit: 500})
	if err != nil {
		slog.Error("load active capability manifests", "error", err)
		return nil
	}
	manifests := make([]capabilities.Manifest, 0, len(records))
	for _, record := range records {
		manifests = append(manifests, record.Manifest)
	}
	registry, err := capabilities.NewRegistry(manifests)
	if err != nil {
		slog.Error("build active capability manifest registry", "error", err)
		return nil
	}
	return registry
}

// defaultAutonomyLevel lee el nivel de autonomía base del runtime desde env
// (COMPANION_DEFAULT_AUTONOMY_LEVEL). Acepta A0..A5; cualquier otro valor causa
// fail-fast en boot para evitar arrancar con configuración ambigua. Default A2.
func defaultAutonomyLevel() (runtime.AutonomyLevel, error) {
	raw := envconfig.Get("COMPANION_DEFAULT_AUTONOMY_LEVEL", "A2")
	switch runtime.AutonomyLevel(raw) {
	case runtime.AutonomyA0, runtime.AutonomyA1, runtime.AutonomyA2,
		runtime.AutonomyA3, runtime.AutonomyA4, runtime.AutonomyA5:
		return runtime.AutonomyLevel(raw), nil
	default:
		return "", fmt.Errorf("invalid COMPANION_DEFAULT_AUTONOMY_LEVEL=%q (expected A0..A5)", raw)
	}
}

// Config arranque del servicio Companion.
type Config struct {
	DatabaseURL         string
	APIKeys             string
	AuthIssuerURL       string
	AuthAudience        string
	InternalJWTSecret   string
	InternalJWTIssuer   string
	InternalJWTAudience string
	NexusBaseURL        string
	NexusAPIKey         string
	PymesBaseURL        string
	PymesAPIKey         string
	PontiBaseURL        string
	PontiAPIKey         string
	LLMProvider         string
	LLMModel            string
	LLMVertexProject    string
	LLMVertexLocation   string
	EmbeddingProvider   string
	EmbeddingModel      string
	EmbeddingProject    string
	EmbeddingLocation   string
	EmbeddingDimensions int
	MigrationFiles      fs.FS
}

// NewServer abre DB, migra, monta mux y auth.
func NewServer(cfg Config) (http.Handler, func(), error) {
	ctx := context.Background()

	db, err := sharedpostgres.OpenWithConfig(ctx, cfg.DatabaseURL, sharedpostgres.DefaultConfig("companion"))
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	if err := sharedpostgres.MigrateUp(ctx, db, "companion", cfg.MigrationFiles, "."); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	nexusGateway := newNexusGateway(cfg.NexusBaseURL, cfg.NexusAPIKey)
	rc := nexusGateway.client
	pymesClient := pymesclient.NewClient(cfg.PymesBaseURL, cfg.PymesAPIKey)

	// Connectors module
	connReg := registry.NewRegistry()
	connReg.Register(registry.NewMockConnector())
	if cfg.PymesBaseURL != "" {
		connReg.Register(registry.NewPymesConnector(pymesClient))
	}
	if cfg.PontiBaseURL != "" {
		pontiClient := registry.NewPontiClient(cfg.PontiBaseURL, cfg.PontiAPIKey)
		connReg.Register(registry.NewPontiConnector(pontiClient))
	}
	connRepo := connectors.NewPostgresRepository(db)
	nexusChecker := connectors.NewNexusCheckerAdapter(func(c context.Context, id uuid.UUID) (connectors.NexusRequestMeta, int, error) {
		return nexusGateway.GetRequestMeta(c, id.String())
	})
	connUC := connectors.NewUsecases(connRepo, connReg, nexusChecker)
	connHandler := connectors.NewHandler(connUC)

	repo := tasks.NewPostgresRepository(db)
	uc := tasks.NewUsecases(repo, nexusGateway)
	uc.SetNexusSyncInterval(nexusSyncInterval())
	uc.SetExecutor(connUC)
	h := tasks.NewHandler(uc)

	// Watchers module
	watcherRepo := watchers.NewPostgresRepository(db)
	watcherUC := watchers.NewUsecases(watcherRepo, nexusGateway)
	watcherUC.SetConnectorExecutor(connUC)
	watcherHandler := watchers.NewHandler(watcherUC)
	jobRepo := jobs.NewPostgresRepository(db)
	watcherUC.SetJobQueue(jobRepo)

	// Memory module
	memRepo := memory.NewPostgresRepository(db)
	embeddingProvider, err := memory.NewEmbeddingProvider(memory.EmbeddingProviderConfig{
		Provider:       cfg.EmbeddingProvider,
		Model:          cfg.EmbeddingModel,
		VertexProject:  firstNonEmpty(cfg.EmbeddingProject, cfg.LLMVertexProject),
		VertexLocation: firstNonEmpty(cfg.EmbeddingLocation, cfg.LLMVertexLocation),
		Dimensions:     cfg.EmbeddingDimensions,
	})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("configure embedding provider: %w", err)
	}
	memUC := memory.NewUsecases(memRepo).WithEmbeddingProvider(embeddingProvider).WithVectorStore(memory.NewPostgresVectorStore(db))
	memHandler := memory.NewHandler(memUC, taskOrgGetter{repo: repo})
	businessRepo := business.NewPostgresRepository(db)
	businessUC := business.NewUsecases(businessRepo).WithMemoryProjector(businessMemoryProjector{uc: memUC})
	businessHandler := business.NewHandler(businessUC)
	agentRepo := agentfleet.NewPostgresRepository(db)
	agentUC := agentfleet.NewUsecases(agentRepo).WithTaskOwnership(taskOwnershipAdapter{repo: repo})
	agentHandler := agentfleet.NewHandler(agentUC)
	uc.SetTaskMemory(taskMemoryAdapter{uc: memUC, repo: repo})

	// Agent memory (conversation history durable per user). El repo de memory
	// implementa los métodos agent_* — pasamos el mismo *PostgresRepository.
	agentMemUC := memory.NewAgentMemoryUC(memRepo)
	uc.SetAgentMemory(agentMemUC)
	chatHandler := memory.NewChatHandler(agentMemUC)

	// Runtime del compañero (LLM + tools + context)
	llmProvider, err := runtime.NewProvider(runtime.ProviderConfig{
		Provider:       cfg.LLMProvider,
		Model:          cfg.LLMModel,
		VertexProject:  cfg.LLMVertexProject,
		VertexLocation: cfg.LLMVertexLocation,
	})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("configure Gemini provider: %w", err)
	}
	toolkit := runtime.NewToolKit(rc, memUC, watcherUC)
	runtimeControlsRepo := runtime.NewPostgresRuntimeControlsRepository(db)

	// Bridge LLM ↔ connectors: expone cada capability declarada por los
	// connector types registrados como runtime tool (LLM-callable). Reads van
	// directo al executor; writes controlled pasan por Nexus antes de ejecutar.
	connectorViews := make([]runtime.ConnectorTypeView, 0)
	for _, c := range connReg.List() {
		connectorViews = append(connectorViews, c)
	}
	capabilityRepo := capabilities.NewPostgresRepository(db)
	capabilityUC := capabilities.NewUsecases(capabilityRepo)
	if generatedManifests, err := connUC.CapabilityManifests(connectordomain.CapabilityFilter{IncludeWrites: true}); err == nil {
		if err := capabilityUC.SyncGenerated(ctx, generatedManifests); err != nil {
			slog.Error("sync generated capability manifests", "error", err)
		}
	} else {
		slog.Error("load generated capability manifests", "error", err)
	}
	capabilityHandler := capabilities.NewHandler(capabilityUC)
	activeCapabilityRegistry := activeManifestRegistry(ctx, capabilityUC)
	runtime.RegisterConnectorCapabilities(toolkit, runtime.CapabilityBridgeDeps{
		Connectors:       connectorViews,
		Executor:         connUC,
		Submitter:        nexusGateway,
		Controls:         runtimeControlsRepo,
		ManifestRegistry: activeCapabilityRegistry,
	})
	runtime.RegisterTaskPlannerTools(toolkit, taskPlannerAdapter{uc: uc})
	contextPorts := runtime.ContextPorts{
		NexusClient: rc,
		MemoryFind: func(c context.Context, orgID, userID, productSurface string, st memdomain.ScopeType, sid string, k memdomain.MemoryKind, limit int) ([]memdomain.MemoryEntry, error) {
			return memUC.Find(c, memory.FindQuery{OrgID: orgID, UserID: userID, ProductSurface: productSurface, ScopeType: st, ScopeID: sid, Kind: k, Limit: limit})
		},
		TaskPlanGet: func(c context.Context, taskID uuid.UUID) (taskdomain.TaskPlan, error) {
			return repo.GetTaskPlan(c, taskID)
		},
		BusinessModelSummary: func(c context.Context, orgID, productSurface string) (string, error) {
			model, err := businessUC.Get(c, orgID, productSurface)
			if err != nil {
				return "", err
			}
			return model.Summary(), nil
		},
	}
	orchestrator := runtime.NewOrchestrator(llmProvider, toolkit, contextPorts)
	orchestrator.SetModel(cfg.LLMModel)
	autonomy, err := defaultAutonomyLevel()
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	orchestrator.SetDefaultAutonomy(autonomy)
	traceRepo := runtime.NewPostgresTraceRepository(db)
	observabilityRepo := runtime.NewPostgresObservabilityRepository(db, traceRepo)
	orchestrator.SetTraceRepository(traceRepo)
	orchestrator.SetObservabilityRecorder(observabilityRepo)
	orchestrator.SetCostLedger(observabilityRepo)
	orchestrator.SetRuntimeControls(runtimeControlsRepo)
	orchestrator.SetAgentResolver(agentRuntimeResolver{uc: agentUC})
	traceHandler := runtime.NewTraceHandler(traceRepo)
	observabilityHandler := runtime.NewObservabilityHandler(observabilityRepo)
	runtimeControlsHandler := runtime.NewRuntimeControlsHandler(runtimeControlsRepo)
	securityEvalRepo := securityevals.NewPostgresRepository(db)
	securityEvalUC := securityevals.NewUsecases(securityEvalRepo)
	securityEvalHandler := securityevals.NewHandler(securityEvalUC)
	jobHandler := jobs.NewHandler(jobRepo)
	adapter := runtime.NewOrchestratorAdapter(orchestrator)
	uc.SetOrchestrator(adapter)
	// Watchers empujan alertas al chat del suscriptor
	watcherUC.SetNotifier(uc)
	jobWorker := jobs.NewWorker(jobRepo, jobs.WorkerConfig{
		WorkerID:       "companion-" + uuid.NewString(),
		Concurrency:    jobWorkerCount(),
		PollInterval:   jobPollInterval(),
		LeaseDuration:  jobLeaseDuration(),
		DefaultTimeout: jobTimeout(),
	})
	watcherUC.RegisterJobHandlers(jobWorker)
	memUC.RegisterJobHandlers(jobWorker)
	slog.Info("companion runtime initialized", "llm_provider", cfg.LLMProvider)

	// Nexus-assist: lee Nexus + arma proposals/summaries con Gemini.
	// Le pasamos el mismo provider del runtime para no duplicar config.
	nexusAssistProposer := nexusassist.NewProposer(rc, llmProvider)
	nexusAssistContextualizer := nexusassist.NewContextualizer(rc, llmProvider)
	nexusAssistHandler := nexusassist.NewHandler(nexusAssistProposer, nexusAssistContextualizer)

	assistRepo := assist.NewPostgresRepository(db)
	assistUC := assist.NewUsecases(assistRepo, llmProvider)
	assistHandler := assist.NewHandler(assistUC)

	mux := http.NewServeMux()
	health.RegisterEndpoints(mux, func(c context.Context) error {
		return db.Ping(c)
	})
	h.Register(mux)
	watcherHandler.Register(mux)
	memHandler.Register(mux)
	businessHandler.Register(mux)
	agentHandler.Register(mux)
	chatHandler.Register(mux)
	connHandler.Register(mux)
	capabilityHandler.Register(mux)
	traceHandler.Register(mux)
	observabilityHandler.Register(mux)
	runtimeControlsHandler.Register(mux)
	securityEvalHandler.Register(mux)
	jobHandler.Register(mux)
	nexusAssistHandler.Register(mux)
	assistHandler.Register(mux)

	// Seed conectores por defecto
	if err := connUC.SeedDefaultConnectors(ctx); err != nil {
		slog.Error("seed default connectors", "error", err)
	}

	authMW, err := newAuthMiddleware(cfg.APIKeys, cfg.AuthIssuerURL, cfg.AuthAudience, internaljwt.Config{
		Secret:   cfg.InternalJWTSecret,
		Issuer:   cfg.InternalJWTIssuer,
		Audience: cfg.InternalJWTAudience,
	})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("create authenticator: %w", err)
	}

	cleanup := func() {
		db.Close()
	}
	if d := nexusSyncInterval(); d > 0 {
		syncCtx, syncCancel := context.WithCancel(context.Background())
		go uc.RunNexusSyncLoop(syncCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			syncCancel()
			prev()
		}
	}
	if d := watcherInterval(); d > 0 {
		watcherCtx, watcherCancel := context.WithCancel(context.Background())
		go watcherUC.RunWatcherLoop(watcherCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			watcherCancel()
			prev()
		}
	}
	if d := watcherSyncInterval(); d > 0 {
		watcherSyncCtx, watcherSyncCancel := context.WithCancel(context.Background())
		go watcherUC.RunPendingProposalSyncLoop(watcherSyncCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			watcherSyncCancel()
			prev()
		}
	}
	if jobWorkerCount() > 0 {
		jobsCtx, jobsCancel := context.WithCancel(context.Background())
		go jobWorker.Run(jobsCtx)
		prev := cleanup
		cleanup = func() {
			jobsCancel()
			prev()
		}
	}

	// Memory purge loop: limpia entradas expiradas cada hora
	{
		purgeCtx, purgeCancel := context.WithCancel(context.Background())
		go memUC.RunPurgeLoop(purgeCtx, 1*time.Hour)
		prev := cleanup
		cleanup = func() {
			purgeCancel()
			prev()
		}
	}

	return authMW(mux), cleanup, nil
}
