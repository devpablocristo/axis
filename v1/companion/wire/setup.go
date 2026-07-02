package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/agentfleet"
	"github.com/devpablocristo/companion/internal/agentprofiles"
	"github.com/devpablocristo/companion/internal/assist"
	commonaudit "github.com/devpablocristo/companion/internal/audit"
	"github.com/devpablocristo/companion/internal/business"
	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/handoffs"
	"github.com/devpablocristo/companion/internal/jobroles"
	"github.com/devpablocristo/companion/internal/jobs"
	"github.com/devpablocristo/companion/internal/mcpgovernance"
	"github.com/devpablocristo/companion/internal/mcpserver"
	"github.com/devpablocristo/companion/internal/memories"
	"github.com/devpablocristo/companion/internal/memory"
	nexusassist "github.com/devpablocristo/companion/internal/nexus_assist"
	"github.com/devpablocristo/companion/internal/ops"
	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/securityevals"
	"github.com/devpablocristo/companion/internal/tasks"
	"github.com/devpablocristo/companion/internal/virployees"
	virployeehttp "github.com/devpablocristo/companion/internal/virployees/inbound/http"
	"github.com/devpablocristo/companion/internal/watchers"
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

type virployeeRuntimeResolver struct {
	uc *virployees.Usecases
}

type agentProfileRuntimeResolver struct {
	uc *agentprofiles.Usecases
}

type taskOwnershipAdapter struct {
	repo tasks.Repository
}

// profileCheckerAdapter lets agentfleet validate profile_id references against
// the agent_profiles store (no physical FK exists).
type profileCheckerAdapter struct {
	repo *agentprofiles.PostgresRepository
}

func (a profileCheckerAdapter) ProfileExists(ctx context.Context, profileID string) (bool, error) {
	if _, err := a.repo.GetProfile(ctx, profileID); err != nil {
		if errors.Is(err, agentprofiles.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

type businessMemoryProjector struct {
	uc *memory.Usecases
}

type productGuardObservabilityRecorder struct {
	recorder runtime.ObservabilityRecorder
}

func (r productGuardObservabilityRecorder) RecordProductInstallationGuardrail(ctx context.Context, event products.GuardrailEvent) error {
	if r.recorder == nil {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"org_id":          event.OrgID,
		"product_surface": event.ProductSurface,
		"reason":          event.Reason,
		"error":           event.Error,
	})
	if err != nil {
		payload = json.RawMessage(`{}`)
	}
	return r.recorder.RecordObservabilityEvent(ctx, runtime.ObservabilityEvent{
		OrgID:          event.OrgID,
		ProductSurface: event.ProductSurface,
		EventType:      "guardrail",
		EventName:      "product_installation_required",
		Severity:       "warn",
		Payload:        payload,
		Redacted:       true,
		OccurredAt:     time.Now().UTC(),
	})
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
		LifecycleStatus:     agent.LifecycleStatus,
		ReviewStatus:        agent.ReviewStatus,
		MaxAutonomy:         runtime.AutonomyLevel(agent.MaxAutonomy),
		AllowedTools:        append([]string(nil), agent.AllowedTools...),
		AllowedCapabilities: append([]string(nil), agent.AllowedCapabilities...),
		MemoryScopeID:       agent.MemoryScopeID,
		SharedMemoryPolicy:  agent.SharedMemoryPolicy,
		Limits:              agent.Limits,
		SLA:                 agent.SLA,
		Version:             agent.Version,
	}, nil
}

func (r virployeeRuntimeResolver) ResolveRuntimeVirployee(ctx context.Context, tenantID, orgID, productSurface, virployeeID string) (runtime.RuntimeVirployeeConfig, error) {
	virployee, err := r.uc.GetVirployee(ctx, tenantID, orgID, productSurface, virployeeID)
	if err != nil {
		return runtime.RuntimeVirployeeConfig{}, err
	}
	capabilityIDs := make([]string, 0, len(virployee.CapabilityIDs))
	for _, id := range virployee.CapabilityIDs {
		if id != uuid.Nil {
			capabilityIDs = append(capabilityIDs, id.String())
		}
	}
	memoryID := ""
	if virployee.MemoryID != nil && *virployee.MemoryID != uuid.Nil {
		memoryID = virployee.MemoryID.String()
	}
	return runtime.RuntimeVirployeeConfig{
		VirployeeID:   virployee.VirployeeID.String(),
		TenantID:      virployee.TenantID.String(),
		Name:          virployee.Name,
		Status:        string(virployee.Status),
		ProfileID:     virployee.ProfileID.String(),
		Autonomy:      runtime.AutonomyLevel(virployee.Autonomy),
		CapabilityIDs: capabilityIDs,
		MemoryID:      memoryID,
	}, nil
}

func (r agentProfileRuntimeResolver) ResolveRuntimeAgentProfile(ctx context.Context, profileID string) (runtime.RuntimeAgentProfileConfig, error) {
	profile, err := r.uc.GetProfile(ctx, profileID)
	if err != nil {
		return runtime.RuntimeAgentProfileConfig{}, err
	}
	return runtime.RuntimeAgentProfileConfig{
		ProfileID:           profile.ProfileID,
		FamilyID:            profile.FamilyID,
		VersionLabel:        profile.VersionLabel,
		Name:                profile.Name,
		Description:         profile.Description,
		SystemPrompt:        profile.SystemPrompt,
		MaxAutonomy:         runtime.AutonomyLevel(profile.MaxAutonomy),
		AllowedTools:        append([]string(nil), profile.AllowedTools...),
		AllowedCapabilities: append([]string(nil), profile.AllowedCapabilities...),
		MemoryPolicy:        profile.MemoryPolicy,
		LLM:                 runtimeLLMConfigFromMap(profile.LLMConfig),
		Enabled:             profile.Enabled,
		Archived:            profile.ArchivedAt != nil || profile.TrashedAt != nil,
		SnapshotID:          profile.ID.String(),
	}, nil
}

// runtimeLLMConfigFromMap traduce el LLMConfig (map JSON libre) de un perfil de
// agente a la config tipada que consume el runtime. Las claves siguen la
// convención del seed/console: "model", "max_tokens", "temperature". Los números
// JSON llegan como float64; valores ausentes o inválidos quedan en cero/empty,
// lo que el runtime interpreta como "usar el default".
func runtimeLLMConfigFromMap(cfg map[string]any) runtime.RuntimeLLMConfig {
	out := runtime.RuntimeLLMConfig{}
	if len(cfg) == 0 {
		return out
	}
	if model, ok := cfg["model"].(string); ok {
		out.Model = strings.TrimSpace(model)
	}
	if maxTokens, ok := numericConfigValue(cfg["max_tokens"]); ok && maxTokens > 0 {
		out.MaxTokens = int(maxTokens)
	}
	if temperature, ok := numericConfigValue(cfg["temperature"]); ok {
		out.Temperature = temperature
	}
	return out
}

// numericConfigValue normaliza valores numéricos de un map JSON deserializado,
// que pueden llegar como float64 (lo habitual) o como int según el origen.
func numericConfigValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
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
	plan, err := a.uc.GetTaskPlan(ctx, taskID)
	if err != nil {
		return runtime.PlannerTaskPlanCompensationExecutionResult{}, err
	}
	step := taskdomain.TaskPlanStep{ID: stepID, TaskID: taskID, Status: taskdomain.TaskPlanStepStatusBlocked}
	for _, candidate := range plan.Steps {
		if candidate.ID == stepID {
			step = candidate
			break
		}
	}
	return runtime.PlannerTaskPlanCompensationExecutionResult{
		Plan:                plan,
		Step:                step,
		Status:              "execution_unavailable",
		Reason:              "no outbound adapter configured",
		NexusRequestID:      strings.TrimSpace(in.NexusRequestID),
		ExecutionStatus:     "skipped",
		VerificationStatus:  "skipped",
		VerificationSummary: "No outbound adapter is configured for task plan compensation execution.",
		ApprovalRequired:    true,
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
	ProductJWTKeys      string
	NexusBaseURL        string
	NexusAPIKey         string
	LLMProvider         string
	LLMModel            string
	LLMVertexProject    string
	LLMVertexLocation   string
	EmbeddingProvider   string
	EmbeddingModel      string
	EmbeddingProject    string
	EmbeddingLocation   string
	EmbeddingDimensions int
	OpsAlertWebhookURL  string
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
	auditRepo := commonaudit.NewPostgresRepository(db)
	auditHandler := commonaudit.NewHandler(auditRepo)

	repo := tasks.NewPostgresRepository(db)
	uc := tasks.NewUsecases(repo, nexusGateway)
	uc.SetNexusSyncInterval(nexusSyncInterval())
	h := tasks.NewHandler(uc)

	// Watchers module
	watcherRepo := watchers.NewPostgresRepository(db)
	watcherUC := watchers.NewUsecases(watcherRepo, nexusGateway)
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
	memoryContainerRepo := memories.NewPostgresRepository(db)
	memoryContainerRepo.SetAuditRecorder(auditRepo)
	memoryContainerUC := memories.NewUsecases(memoryContainerRepo)
	memoryContainerHandler := memories.NewHandler(memoryContainerUC)
	businessRepo := business.NewPostgresRepository(db)
	businessUC := business.NewUsecases(businessRepo).WithMemoryProjector(businessMemoryProjector{uc: memUC})
	businessHandler := business.NewHandler(businessUC)
	productRepo := products.NewPostgresRepository(db)
	productUC := products.NewUsecases(productRepo)
	productHandler := products.NewHandler(productUC)
	productGuard := products.NewInstallationGuard(productUC)
	productRateLimiter := productlimits.NewMemoryLimiter()
	watcherUC.SetProductInstallationGuard(productGuard)
	watcherUC.SetRateLimiter(productRateLimiter)
	memUC.WithProductInstallationGuard(productGuard)
	agentRepo := agentfleet.NewPostgresRepository(db)
	agentProfileRepo := agentprofiles.NewPostgresRepository(db)
	agentProfileUC := agentprofiles.NewUsecases(agentProfileRepo)
	agentProfileHandler := agentprofiles.NewHandler(agentProfileUC)
	jobRoleRepo := jobroles.NewPostgresRepository(db)
	jobRoleRepo.SetAuditRecorder(auditRepo)
	jobRoleUC := jobroles.NewUsecases(jobRoleRepo)
	jobRoleHandler := jobroles.NewHandler(jobRoleUC)
	virployeeRepo := virployees.NewPostgresRepository(db)
	virployeeRepo.SetAuditRecorder(auditRepo)
	virployeeUC := virployees.NewUsecases(virployeeRepo)
	virployeeHandler := virployeehttp.NewHandler(virployeeUC)
	handoffRepo := handoffs.NewPostgresRepository(db)
	handoffRepo.SetAuditRecorder(auditRepo)
	handoffUC := handoffs.NewUsecases(handoffRepo)
	handoffHandler := handoffs.NewHandler(handoffUC)
	agentUC := agentfleet.NewUsecases(agentRepo).
		WithTaskOwnership(taskOwnershipAdapter{repo: repo}).
		WithProfileChecker(profileCheckerAdapter{repo: agentProfileRepo})
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

	capabilityRepo := capabilities.NewPostgresRepository(db)
	capabilityUC := capabilities.NewUsecases(capabilityRepo).
		WithProductRegistry(productUC).
		WithManifestSourceFetcher(capabilities.NewHTTPManifestSourceFetcher())
	capabilityHandler := capabilities.NewHandler(capabilityUC)
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
	productGuard.WithRecorder(productGuardObservabilityRecorder{recorder: observabilityRepo})
	orchestrator.SetTraceRepository(traceRepo)
	orchestrator.SetObservabilityRecorder(observabilityRepo)
	orchestrator.SetCostLedger(observabilityRepo)
	orchestrator.SetRuntimeControls(runtimeControlsRepo)
	orchestrator.SetAgentResolver(agentRuntimeResolver{uc: agentUC})
	orchestrator.SetVirployeeResolver(virployeeRuntimeResolver{uc: virployeeUC})
	orchestrator.SetAgentProfileResolver(agentProfileRuntimeResolver{uc: agentProfileUC})
	orchestrator.SetProductInstallationGuard(productGuard)
	orchestrator.SetRateLimiter(productRateLimiter)
	traceHandler := runtime.NewTraceHandler(traceRepo)
	observabilityHandler := runtime.NewObservabilityHandler(observabilityRepo)
	runtimeControlsHandler := runtime.NewRuntimeControlsHandler(runtimeControlsRepo)
	securityEvalRepo := securityevals.NewPostgresRepository(db)
	securityEvalUC := securityevals.NewUsecases(securityEvalRepo)
	securityEvalUC.SetRateLimiter(productRateLimiter)
	securityEvalHandler := securityevals.NewHandler(securityEvalUC)
	opsUC := ops.NewUsecases(ops.Deps{
		Products:        productUC,
		Capabilities:    capabilityUC,
		Evals:           securityEvalUC,
		Observability:   observabilityRepo,
		Costs:           observabilityRepo,
		RuntimeControls: runtimeControlsRepo,
		Jobs:            jobRepo,
		AlertSink:       ops.NewWebhookAlertSink(cfg.OpsAlertWebhookURL, 5*time.Second),
	})
	opsHandler := ops.NewHandler(opsUC)
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

	mcpRegistry, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("configure mcp registry: %w", err)
	}
	mcpGateway := mcpgovernance.NewGateway(mcpRegistry, nexusGateway)
	mcpHandler := mcpserver.NewHandler(mcpserver.Deps{
		Registry:      mcpRegistry,
		Authorizer:    mcpGateway,
		Products:      productUC,
		Capabilities:  capabilityUC,
		Ops:           opsUC,
		Costs:         observabilityRepo,
		Traces:        observabilityRepo,
		Evals:         securityEvalUC,
		Tasks:         uc,
		Nexus:         rc,
		RateLimiter:   productRateLimiter,
		RuntimePolicy: runtimeControlsRepo,
		Observability: observabilityRepo,
	})

	assistRepo := assist.NewPostgresRepository(db)
	assistUC, err := assist.NewUsecases(assistRepo, llmProvider, auditRepo)
	if err != nil {
		return nil, nil, fmt.Errorf("build assist usecases: %w", err)
	}
	assistHandler := assist.NewHandler(assistUC)

	mux := http.NewServeMux()
	health.RegisterEndpoints(mux, func(c context.Context) error {
		return db.Ping(c)
	})
	h.Register(mux)
	watcherHandler.Register(mux)
	memHandler.Register(mux)
	memoryContainerHandler.Register(mux)
	businessHandler.Register(mux)
	productHandler.Register(mux)
	agentHandler.Register(mux)
	virployeeHandler.Register(mux)
	handoffHandler.Register(mux)
	agentProfileHandler.Register(mux)
	jobRoleHandler.Register(mux)
	chatHandler.Register(mux)
	capabilityHandler.Register(mux)
	traceHandler.Register(mux)
	observabilityHandler.Register(mux)
	runtimeControlsHandler.Register(mux)
	securityEvalHandler.Register(mux)
	opsHandler.Register(mux)
	jobHandler.Register(mux)
	nexusAssistHandler.Register(mux)
	mcpHandler.Register(mux)
	auditHandler.Register(mux)
	assistHandler.Register(mux)

	authMW, err := newAuthMiddleware(cfg.APIKeys, cfg.AuthIssuerURL, cfg.AuthAudience, internaljwt.Config{
		Secret:   cfg.InternalJWTSecret,
		Issuer:   cfg.InternalJWTIssuer,
		Audience: cfg.InternalJWTAudience,
	}, cfg.ProductJWTKeys)
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
