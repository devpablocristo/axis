package virployees

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/invocation"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/memories"
	profiletemplatedomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

const (
	ResourceTypeVirployee = "virployee"
	DefaultOrgID          = "default"
	DefaultActorID        = "system"
)

type RepositoryPort interface {
	lifecycle.RepositoryPort

	Create(ctx context.Context, orgID string, input domain.NormalizedCreateInput) (domain.Virployee, error)
	List(ctx context.Context, orgID string, state domain.State) ([]domain.Virployee, error)
	Get(ctx context.Context, orgID string, id uuid.UUID) (domain.Virployee, error)
	Update(ctx context.Context, orgID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error)
	CreateRunTrace(ctx context.Context, orgID string, input runtraces.CreateInput) (runtraces.Trace, error)
	ListRunTraces(ctx context.Context, orgID string, virployeeID uuid.UUID, limit int) ([]runtraces.Trace, error)
	FindExecutionGateTraceByApproval(ctx context.Context, orgID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error)
	FindSimulatedExecutionTraceByApproval(ctx context.Context, orgID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error)
}

type ExecutionRepositoryPort interface {
	FindExecutionTraceByApproval(ctx context.Context, orgID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error)
	SavePreparedAction(ctx context.Context, orgID string, virployeeID uuid.UUID, checkID, approvalID string, capabilityKey, payloadHash, bindingHash string, action preparedactions.Action) (PreparedActionRecord, error)
	GetPreparedActionByApproval(ctx context.Context, orgID string, virployeeID, approvalID uuid.UUID) (PreparedActionRecord, error)
	BeginExecution(ctx context.Context, orgID string, virployeeID uuid.UUID, preparedActionID uuid.UUID, idempotencyKey string) (ExecutionAttempt, bool, error)
	GetExecutionByPreparedAction(ctx context.Context, orgID string, preparedActionID uuid.UUID) (ExecutionAttempt, error)
	CompleteExecution(ctx context.Context, orgID string, id uuid.UUID, status, resourceID string, result map[string]any, executionError string, durationMS int64) (ExecutionAttempt, error)
}

type ExecutionRepositoryV2Port interface {
	SavePreparedActionV2(ctx context.Context, orgID string, virployeeID uuid.UUID, checkID, approvalID string, capabilityID uuid.UUID, capabilityKey, payloadHash, bindingHash string, action preparedactions.PreparedActionV2) (PreparedActionRecord, error)
}

type JobRoleReaderPort interface {
	EnsureActive(ctx context.Context, orgID string, id uuid.UUID) error
	Get(ctx context.Context, orgID string, id uuid.UUID) (jobroledomain.JobRole, error)
}

type CapabilityValidatorPort interface {
	EnsureAssignable(ctx context.Context, orgID string, ids []uuid.UUID, autonomy domain.AutonomyLevel) error
	Get(ctx context.Context, orgID string, id uuid.UUID) (capabilitydomain.Capability, error)
}

type ProfileTemplateReaderPort interface {
	EnsureUsableByVirployee(ctx context.Context, orgID string, id uuid.UUID, autonomy domain.AutonomyLevel) error
	Get(ctx context.Context, orgID string, id uuid.UUID) (profiletemplatedomain.ProfileTemplate, error)
}

type GovernanceCheckerPort interface {
	Check(ctx context.Context, input executiongate.GovernanceCheckInput) (executiongate.GovernanceCheckResult, error)
}

type GovernanceRevalidatorPort interface {
	Revalidate(ctx context.Context, input executiongate.GovernanceRevalidationInput) (executiongate.GovernanceRevalidationResult, error)
}

type ApprovalReaderPort interface {
	GetApproval(ctx context.Context, orgID string, id uuid.UUID) (executiongate.GovernanceApproval, error)
}

// MCPExecutionContextValidatorPort revalidates the server-derived MCP policy,
// assignment and authority snapshot at execution time. It intentionally takes
// only metadata bound into the prepared action.
type MCPExecutionContextValidatorPort interface {
	ValidateMCPExecutionContext(context.Context, preparedactions.MCPContextBinding) error
}

// AuditEventInput is a companion-owned audit event, mapped to the Nexus audit
// ledger by the wire adapter. Data must hold only hashes + non-sensitive
// metadata (never PHI or raw content).
type AuditEventInput struct {
	OrgID       string
	VirployeeID string
	ActorType   string
	ActorID     string
	SubjectType string
	SubjectID   string
	EventType   string
	Summary     string
	Data        map[string]any
}

// AuditEmitterPort records an event in the tamper-evident ledger (Nexus). It is
// optional and best-effort: emission failures must never fail the underlying
// work. Gaps stay detectable because the ledger's subject_id is the run id.
type AuditEmitterPort interface {
	AppendAuditEvent(ctx context.Context, in AuditEventInput) error
}

// ExecutionOutcome is what an executor reports after acting. Mode identifies the
// executor that ran (e.g. "local", "google_calendar") and ExternalEffects is true
// when the action wrote to a real external system. Both are persisted with the
// attempt and surfaced verbatim in the run trace — never hardcoded at the call site.
// Executors should set Mode even when returning an error so a failed attempt still
// records which executor was responsible.
type ExecutionOutcome struct {
	ResourceID      string
	Mode            string
	ExternalEffects bool
	Result          map[string]any
}

type ActionExecutorPort interface {
	Execute(ctx context.Context, orgID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (ExecutionOutcome, error)
}

type ActionExecutorV2Port interface {
	ExecuteV2(ctx context.Context, orgID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.PreparedActionV2) (ExecutionOutcome, error)
}

type LegacyCalendarStorePort interface {
	CreateLocalCalendarEvent(ctx context.Context, orgID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (string, error)
}

type MemoryReaderPort interface {
	RecallInternal(context.Context, string, uuid.UUID, string, int) ([]memories.Recalled, error)
}

type ScopedMemoryReaderPort interface {
	RecallScopedInternal(context.Context, string, uuid.UUID, memories.Scope, string, int) ([]memories.Recalled, error)
}

type KnowledgeRetrieverPort interface {
	Retrieve(context.Context, knowledgebases.RetrievalScope, string, int) (knowledgebases.Evidence, error)
}

// GovernedReadInvokerPort routes Assist capability reads through the common
// ToolInvocationGate. Implementations must re-resolve mutable assignment,
// policy, promotion, manifest, and authority state immediately before running
// an executor.
type GovernedReadInvokerPort interface {
	SupportsGovernedRead(string) bool
	InvokeGovernedRead(context.Context, GovernedReadInvocation) (map[string]any, error)
}

type GovernedReadInvocation struct {
	OrgID                  string
	ActorID                string
	VirployeeID            uuid.UUID
	SubjectID              uuid.UUID
	CaseID                 uuid.UUID
	AssignmentID           uuid.UUID
	AssignmentVersion      int64
	ProductSurface         string
	ProductID              string
	RepositoryGeneration   string
	CapabilityID           uuid.UUID
	CapabilityKey          string
	CapabilityManifestHash string
	IdempotencyKey         string
	Arguments              map[string]any
}

type ContinuityAssignmentValidatorPort interface {
	ValidateAssistAssignment(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int64) (int64, error)
	RequiresAssistAssignment(context.Context, string, uuid.UUID, uuid.UUID) (bool, error)
}

// AssistExecutionContextValidatorPort validates mutable database state that a
// completed Assist depended on (case ownership and every cited source). The
// immutable hashes are recomputed in the usecase; this port only answers from
// current organization-scoped rows.
type AssistExecutionContextValidatorPort interface {
	ValidateAssistExecutionContext(context.Context, string, uuid.UUID, uuid.UUID, AssistRun) error
	AssistSourceAuthorizationHash(context.Context, string, uuid.UUID, uuid.UUID, AssistRun, []knowledgebases.Citation) (string, error)
}

// RuntimePlannerPort proposes an intent + draft for a natural-language input.
// The planner only proposes; Go decides on the proposal (dryrun.EvaluateWithProposal).
// When unset, dry-run falls back to the deterministic matcher.
type RuntimePlannerPort interface {
	Propose(ctx context.Context, input string, rc runtimecontext.Context) (dryrun.Proposal, error)
}

// AnswerInput/AnswerOutput are the companion-owned shapes for the "process and
// respond" path, so the runtime transport can be injected without virployees
// importing runtimeclient.
type AnswerInput struct {
	SystemPrompt        string
	JobRole             string
	ProfessionalContext ProfessionalContext
	InputJSON           json.RawMessage
	ResponseSchema      map[string]any
	ContentParts        []artifacts.ContentPart
	GroundingMode       string
}

type ProfessionalContext struct {
	JobRoleID        string                           `json:"job_role_id,omitempty"`
	Name             string                           `json:"name,omitempty"`
	Mission          string                           `json:"mission,omitempty"`
	Responsibilities []jobroledomain.Responsibility   `json:"responsibilities,omitempty"`
	SuccessCriteria  []jobroledomain.SuccessCriterion `json:"success_criteria,omitempty"`
}

type RuntimeCitation struct {
	DocumentID string          `json:"document_id"`
	SHA256     string          `json:"sha256,omitempty"`
	Locator    json.RawMessage `json:"locator,omitempty"`
}

type AnswerOutput struct {
	OutputText            string
	OutputJSON            json.RawMessage
	Answered              bool
	Status                string
	Citations             []RuntimeCitation
	ModelID               string
	PromptVersion         string
	InputTokens           int64
	OutputTokens          int64
	EstimatedCostMicroUSD int64
}

// RuntimeAnswererPort asks the runtime to process input and answer (read/explain,
// no governance decision). Unset ⇒ the Assist usecase is unavailable (fail-closed).
type RuntimeAnswererPort interface {
	Answer(ctx context.Context, in AnswerInput) (AnswerOutput, error)
}

type ArtifactIngestorPort interface {
	Ingest(context.Context, artifacts.IngestRequest) (artifacts.IngestResult, error)
}

type AssistMetadata struct {
	AssistType              string
	ProductID               string
	ProductSurface          string
	Invocation              invocation.Context
	CapabilityID            uuid.UUID
	CapabilityKey           string
	CapabilityManifestHash  string
	SubjectID               string
	CaseID                  uuid.UUID
	AssignmentID            uuid.UUID
	AssignmentVersion       int64
	RepositoryGeneration    string
	GroundingMode           string
	ContextHash             string
	MemoryContextHash       string
	JobRoleSnapshotHash     string
	SourceAuthorizationHash string
	PromptBundleHash        string
}

type PromptResolution struct {
	BundleHash string
	Content    string
	Versions   json.RawMessage
}

type PromptResolverPort interface {
	ResolvePrompt(context.Context, string, string, uuid.UUID, string) (PromptResolution, error)
}

type FinOpsUsage struct {
	OrgID, ProductID, Area, Service  string
	VirployeeID                      uuid.UUID
	CapabilityKey, CapabilityVersion string
	Model                            string
	InputUnits, OutputUnits          int64
	EstimatedCostMicroUSD            int64
	IdempotencyKey                   string
}

type FinOpsRecorderPort interface {
	RecordFinOpsUsage(context.Context, FinOpsUsage) error
}

type AssistPromptContextRepositoryPort interface {
	SetAssistPromptContext(context.Context, string, uuid.UUID, string, string, json.RawMessage) (AssistRun, error)
}

// AssistRepositoryPort persists product assist runs (reserve-before-LLM).
type AssistRepositoryPort interface {
	BeginAssistRun(ctx context.Context, orgID string, virployeeID uuid.UUID, metadata AssistMetadata, idempotencyKey, inputHash, inputPreview string, inputJSON json.RawMessage) (AssistRun, bool, error)
	ClaimAssistRun(ctx context.Context, orgID string, id uuid.UUID, recoverPreAnswer bool) (AssistRun, bool, error)
	SetAssistRunStatus(ctx context.Context, orgID string, id uuid.UUID, status string) (AssistRun, error)
	CompleteAssistRun(ctx context.Context, orgID string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error)
	GetAssistRunByKey(ctx context.Context, orgID string, virployeeID uuid.UUID, idempotencyKey string) (AssistRun, error)
	GetAssistRunByID(ctx context.Context, orgID string, id uuid.UUID) (AssistRun, error)
	ListReceivedAssistRuns(ctx context.Context, limit int) ([]AssistRun, error)
}

type AssistGroundingRepositoryPort interface {
	SetAssistGrounding(context.Context, string, uuid.UUID, string, string, string, []knowledgebases.Citation, []knowledgebases.Citation, string, []memories.Reference, string, string) (AssistRun, error)
}

// AssistCompletion is committed in one UPDATE so a terminal row can never be
// observed without the source, memory, policy, and professional context that
// justified its answer.
type AssistCompletion struct {
	Status                  string
	Output                  json.RawMessage
	OutputText              string
	Answered                bool
	Degraded                bool
	Model                   string
	PromptVersion           string
	RunError                string
	DurationMS              int64
	GroundingMode           string
	AnswerStatus            string
	ContextHash             string
	Citations               []knowledgebases.Citation
	SourceContext           []knowledgebases.Citation
	MemoryContextHash       string
	MemoryReferences        []memories.Reference
	JobRoleSnapshotHash     string
	SourceAuthorizationHash string
}

type AssistGroundedCompletionRepositoryPort interface {
	CompleteAssistRunWithGrounding(context.Context, string, uuid.UUID, AssistCompletion) (AssistRun, error)
}

// AssistQueuePort keeps the domain independent of the PostgreSQL jobs adapter.
// Payloads contain only identifiers; the input remains in companion_assist_runs.
type AssistQueuePort interface {
	EnqueueAssist(ctx context.Context, run AssistRun) error
}

type UseCases struct {
	repo                  RepositoryPort
	executionRepo         ExecutionRepositoryPort
	jobRoles              JobRoleReaderPort
	capabilities          CapabilityValidatorPort
	profileTemplates      ProfileTemplateReaderPort
	governance            GovernanceCheckerPort
	governanceRevalidator GovernanceRevalidatorPort
	approvals             ApprovalReaderPort
	authority             AuthorityEvaluatorPort
	executors             map[string]ActionExecutorPort
	executorBindings      map[string]ActionExecutorV2Port
	memories              MemoryReaderPort
	knowledge             KnowledgeRetrieverPort
	governedReads         GovernedReadInvokerPort
	continuity            ContinuityAssignmentValidatorPort
	assistExecution       AssistExecutionContextValidatorPort
	runtime               RuntimePlannerPort
	answerer              RuntimeAnswererPort
	assistRepo            AssistRepositoryPort
	assistQueue           AssistQueuePort
	coordinationRepo      CoordinationRepositoryPort
	coordinationQueue     CoordinationQueuePort
	corpusReader          ArtifactCorpusReaderPort
	docFetcher            DocumentFetcherPort
	artifactIngestor      ArtifactIngestorPort
	auditEmitter          AuditEmitterPort
	mcpContext            MCPExecutionContextValidatorPort
	quota                 quotas.QuotaPort
	usageLedger           quotas.UsageLedgerPort
	promptResolver        PromptResolverPort
	finops                FinOpsRecorderPort
	lifecycle             *lifecycle.Service
}

func (u *UseCases) SetAssistQueue(queue AssistQueuePort) { u.assistQueue = queue }

func (u *UseCases) SetPromptResolver(resolver PromptResolverPort) { u.promptResolver = resolver }

func (u *UseCases) SetFinOpsRecorder(recorder FinOpsRecorderPort) { u.finops = recorder }

func (u *UseCases) SetCoordinationQueue(queue CoordinationQueuePort) { u.coordinationQueue = queue }

func (u *UseCases) SetArtifactCorpusReader(reader ArtifactCorpusReaderPort) { u.corpusReader = reader }

func NewUseCases(repo RepositoryPort, jobRoles ...JobRoleReaderPort) (*UseCases, error) {
	policy := &lifecycle.LifecyclePolicy{
		ResourceType:  ResourceTypeVirployee,
		AllowArchive:  true,
		AllowTrash:    true,
		AllowPurge:    true,
		RequireReason: false,
		RetentionDays: 30,
	}
	service, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{ResourceTypeVirployee: repo},
		noopLifecycleAudit{},
		lifecycle.NewStaticPolicyRegistry(policy),
	)
	if err != nil {
		return nil, err
	}
	reader := JobRoleReaderPort(noopJobRoleReader{})
	if len(jobRoles) > 0 && jobRoles[0] != nil {
		reader = jobRoles[0]
	}
	uc := &UseCases{
		repo:             repo,
		jobRoles:         reader,
		capabilities:     noopCapabilityValidator{},
		profileTemplates: noopProfileTemplateReader{},
		executors:        map[string]ActionExecutorPort{},
		executorBindings: map[string]ActionExecutorV2Port{},
		lifecycle:        service,
	}
	if executionRepo, ok := repo.(ExecutionRepositoryPort); ok {
		uc.executionRepo = executionRepo
	}
	if assistRepo, ok := repo.(AssistRepositoryPort); ok {
		uc.assistRepo = assistRepo
	}
	if validator, ok := repo.(AssistExecutionContextValidatorPort); ok {
		uc.assistExecution = validator
	}
	if coordinationRepo, ok := repo.(CoordinationRepositoryPort); ok {
		uc.coordinationRepo = coordinationRepo
	}
	return uc, nil
}

func (u *UseCases) SetCapabilityValidator(validator CapabilityValidatorPort) {
	if validator == nil {
		u.capabilities = noopCapabilityValidator{}
		return
	}
	u.capabilities = validator
}

func (u *UseCases) SetProfileTemplateReader(reader ProfileTemplateReaderPort) {
	if reader == nil {
		u.profileTemplates = noopProfileTemplateReader{}
		return
	}
	u.profileTemplates = reader
}

func (u *UseCases) SetGovernanceChecker(checker GovernanceCheckerPort) {
	u.governance = checker
}

func (u *UseCases) SetGovernanceRevalidator(revalidator GovernanceRevalidatorPort) {
	u.governanceRevalidator = revalidator
}

func (u *UseCases) SetApprovalReader(reader ApprovalReaderPort) {
	u.approvals = reader
}

func (u *UseCases) SetMemoryReader(reader MemoryReaderPort) { u.memories = reader }

func (u *UseCases) SetKnowledgeRetriever(reader KnowledgeRetrieverPort) { u.knowledge = reader }

func (u *UseCases) SetGovernedReadInvoker(invoker GovernedReadInvokerPort) { u.governedReads = invoker }

func (u *UseCases) SetContinuityAssignmentValidator(validator ContinuityAssignmentValidatorPort) {
	u.continuity = validator
}

func (u *UseCases) SetAssistExecutionContextValidator(validator AssistExecutionContextValidatorPort) {
	u.assistExecution = validator
}

func (u *UseCases) SetRuntimePlanner(planner RuntimePlannerPort) { u.runtime = planner }

func (u *UseCases) SetRuntimeAnswerer(answerer RuntimeAnswererPort) { u.answerer = answerer }

func (u *UseCases) SetDocumentFetcher(fetcher DocumentFetcherPort) { u.docFetcher = fetcher }

func (u *UseCases) SetArtifactIngestor(ingestor ArtifactIngestorPort) { u.artifactIngestor = ingestor }

func (u *UseCases) SetAuditEmitter(emitter AuditEmitterPort) { u.auditEmitter = emitter }

func (u *UseCases) SetMCPExecutionContextValidator(validator MCPExecutionContextValidatorPort) {
	u.mcpContext = validator
}

func (u *UseCases) SetQuotaPorts(quota quotas.QuotaPort, ledger quotas.UsageLedgerPort) {
	u.quota, u.usageLedger = quota, ledger
}

func (u *UseCases) RegisterExecutor(action string, executor ActionExecutorPort) {
	action = strings.TrimSpace(action)
	if action != "" && executor != nil {
		u.executors[action] = executor
	}
}

// RegisterExecutorBinding installs a provider-neutral outbound adapter. V2
// prepared actions dispatch only through this registry and never by capability
// key, product surface or provider name.
func (u *UseCases) RegisterExecutorBinding(bindingID string, executor ActionExecutorV2Port) {
	bindingID = strings.ToLower(strings.TrimSpace(bindingID))
	if bindingID != "" && executor != nil {
		u.executorBindings[bindingID] = executor
	}
}

func (u *UseCases) Create(ctx context.Context, orgID string, input domain.CreateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := u.jobRoles.EnsureActive(ctx, normalizeOrgID(orgID), normalized.JobRoleID); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.profileTemplates.EnsureUsableByVirployee(ctx, normalizeOrgID(orgID), normalized.ProfileTemplateID, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.capabilities.EnsureAssignable(ctx, normalizeOrgID(orgID), normalized.CapabilityIDs, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Create(ctx, normalizeOrgID(orgID), normalized)
}

func (u *UseCases) ListActive(ctx context.Context, orgID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeOrgID(orgID), domain.StateActive)
}

func (u *UseCases) ListArchived(ctx context.Context, orgID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeOrgID(orgID), domain.StateArchived)
}

func (u *UseCases) ListTrash(ctx context.Context, orgID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeOrgID(orgID), domain.StateTrashed)
}

func (u *UseCases) Get(ctx context.Context, orgID string, id uuid.UUID) (domain.Virployee, error) {
	return u.repo.Get(ctx, normalizeOrgID(orgID), id)
}

func (u *UseCases) RuntimeContext(ctx context.Context, orgID string, id uuid.UUID) (runtimecontext.Context, error) {
	orgID = normalizeOrgID(orgID)
	virployee, err := u.repo.Get(ctx, orgID, id)
	if err != nil {
		return runtimecontext.Context{}, err
	}

	jobRole, err := u.jobRoles.Get(ctx, orgID, virployee.JobRoleID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtimecontext.Context{}, domainerr.Validation("job_role_id must reference an active job role in the same organization")
		}
		return runtimecontext.Context{}, err
	}
	if jobRole.State() != jobroledomain.StateActive {
		return runtimecontext.Context{}, domainerr.Validation("job_role_id must reference an active job role in the same organization")
	}

	profileTemplate, err := u.profileTemplates.Get(ctx, orgID, virployee.ProfileTemplateID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtimecontext.Context{}, domainerr.Validation("profile_template_id must reference an active profile template in the same organization")
		}
		return runtimecontext.Context{}, err
	}
	if profileTemplate.State() != profiletemplatedomain.StateActive {
		return runtimecontext.Context{}, domainerr.Validation("profile_template_id must reference an active profile template in the same organization")
	}
	if !profileTemplate.MaxAutonomy.Allows(virployee.Autonomy) {
		return runtimecontext.Context{}, domainerr.Validation("profile template " + profileTemplate.Name + " allows max autonomy " + string(profileTemplate.MaxAutonomy) + "; virployee autonomy " + string(virployee.Autonomy) + " exceeds it")
	}

	capabilities := make([]capabilitydomain.Capability, 0, len(virployee.CapabilityIDs))
	for _, capabilityID := range virployee.CapabilityIDs {
		capability, err := u.capabilities.Get(ctx, orgID, capabilityID)
		if err != nil {
			if domainerr.IsNotFound(err) {
				return runtimecontext.Context{}, domainerr.Validation("capability_ids must reference active capabilities in the same organization")
			}
			return runtimecontext.Context{}, err
		}
		if capability.State() != capabilitydomain.StateActive {
			return runtimecontext.Context{}, domainerr.Validation("capability_ids must reference active capabilities in the same organization")
		}
		if capability.PromotionState != capabilitydomain.PromotionActive {
			return runtimecontext.Context{}, domainerr.Validation("assigned capability is no longer conformant and promoted")
		}
		if !virployee.Autonomy.Allows(capability.RequiredAutonomy) {
			return runtimecontext.Context{}, domainerr.Validation("capability " + capability.CapabilityKey + " requires autonomy " + string(capability.RequiredAutonomy) + "; virployee autonomy " + string(virployee.Autonomy) + " does not allow it")
		}
		capabilities = append(capabilities, capability)
	}

	result := runtimecontext.Context{
		Virployee:       virployee,
		JobRole:         jobRole,
		ProfileTemplate: profileTemplate,
		Capabilities:    capabilities,
	}
	if u.memories != nil {
		items, recallErr := u.memories.RecallInternal(ctx, orgID, id, virployee.Name+" "+virployee.Description, 5)
		if recallErr != nil {
			return runtimecontext.Context{}, recallErr
		}
		for _, item := range items {
			result.MemoryReferences = append(result.MemoryReferences, item.Reference)
			result.MemoryContext = append(result.MemoryContext, memories.ContextItem{
				Title: item.Memory.Title, Type: item.Memory.Type, Content: item.Memory.Content,
			})
		}
		result.MemoryContextHash = memories.ContextHash(result.MemoryReferences)
	}
	return result, nil
}

func (u *UseCases) DryRun(ctx context.Context, orgID string, id uuid.UUID, input string) (dryrun.Result, error) {
	orgID = normalizeOrgID(orgID)
	result, err := u.dryRun(ctx, orgID, id, input)
	if err != nil {
		return dryrun.Result{}, err
	}
	if err := u.recordDryRunTrace(ctx, orgID, result); err != nil {
		return dryrun.Result{}, err
	}
	return result, nil
}

func (u *UseCases) dryRun(ctx context.Context, orgID string, id uuid.UUID, input string) (dryrun.Result, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return dryrun.Result{}, domainerr.Validation("input is required")
	}
	runtimeCtx, err := u.RuntimeContext(ctx, orgID, id)
	if err != nil {
		return dryrun.Result{}, err
	}
	if u.memories != nil {
		items, recallErr := u.memories.RecallInternal(ctx, orgID, id, input, 5)
		if recallErr != nil {
			return dryrun.Result{}, recallErr
		}
		runtimeCtx.MemoryReferences = runtimeCtx.MemoryReferences[:0]
		runtimeCtx.MemoryContext = runtimeCtx.MemoryContext[:0]
		for _, item := range items {
			runtimeCtx.MemoryReferences = append(runtimeCtx.MemoryReferences, item.Reference)
			runtimeCtx.MemoryContext = append(runtimeCtx.MemoryContext, memories.ContextItem{
				Title: item.Memory.Title, Type: item.Memory.Type, Content: item.Memory.Content,
			})
		}
		runtimeCtx.MemoryContextHash = memories.ContextHash(runtimeCtx.MemoryReferences)
	}
	if u.runtime != nil {
		quotaID := uuid.NewString()
		if err := u.consumeQuota(ctx, quotaKey(orgID, "axis", quotas.AreaLLM), quotaID, "virployee", id.String(), estimatedDryRunTokens(input, runtimeCtx)); err != nil {
			return dryrun.Result{}, err
		}
		proposal, err := u.runtime.Propose(ctx, input, runtimeCtx)
		if err != nil {
			// Fail-closed transport: if the runtime is unavailable or errors, do
			// not act on a half-formed proposal — fall back to the deterministic
			// matcher, which is scoped to the assigned capabilities and still
			// passes through the execution gate and Nexus.
			slog.WarnContext(ctx, "runtime_propose_failed_fallback_deterministic", "error", runtraces.RedactText(err.Error()))
			return attachRuntimeActionPreview(dryrun.Evaluate(input, runtimeCtx))
		}
		u.recordProposalUsage(ctx, orgID, id, quotaID, proposal)
		// Echo means no LLM is configured. Its empty proposal is transport
		// success, not a semantic decision, so retain the scoped deterministic
		// matcher used when the runtime is absent or unavailable.
		if !proposal.Intent.Matched && proposal.Intent.ModelID == "echo" {
			return attachRuntimeActionPreview(dryrun.Evaluate(input, runtimeCtx))
		}
		return attachRuntimeActionPreview(dryrun.EvaluateWithProposal(input, runtimeCtx, proposal))
	}
	return attachRuntimeActionPreview(dryrun.Evaluate(input, runtimeCtx))
}

func (u *UseCases) ExecutionGate(
	ctx context.Context,
	orgID string,
	id uuid.UUID,
	input string,
	confirmedDraft *executiongate.ConfirmedDraft,
	principalContexts ...executiongate.PrincipalContext,
) (executiongate.Result, error) {
	if len(principalContexts) > 1 {
		return executiongate.Result{}, domainerr.Validation("only one principal context may be provided")
	}
	principal := executiongate.PrincipalContext{}
	if len(principalContexts) == 1 {
		var err error
		principal, err = executiongate.NormalizePrincipalContext(principalContexts[0])
		if err != nil {
			return executiongate.Result{}, domainerr.Validation(err.Error())
		}
	}
	return u.executionGate(ctx, orgID, id, input, confirmedDraft, nil, principal, uuid.Nil, nil, "")
}

// ExecutionGateWithAssistRun is the HTTP-facing bound variant. assistRunID is
// an identifier only: all hashes and provenance are loaded server-side.
func (u *UseCases) ExecutionGateWithAssistRun(
	ctx context.Context,
	orgID string,
	id uuid.UUID,
	input string,
	confirmedDraft *executiongate.ConfirmedDraft,
	requestedPreparedAction *preparedactions.PreparedActionV2,
	principal executiongate.PrincipalContext,
	assistRunID uuid.UUID,
) (executiongate.Result, error) {
	normalized, err := executiongate.NormalizePrincipalContext(principal)
	if err != nil {
		return executiongate.Result{}, domainerr.Validation(err.Error())
	}
	return u.executionGate(ctx, orgID, id, input, confirmedDraft, requestedPreparedAction, normalized, assistRunID, nil, "")
}

// ExecutionGateFromMCP is the governed entrypoint used by the MCP facade. The
// caller supplies only a server-derived metadata binding; raw user content and
// documents are never added to it.
func (u *UseCases) ExecutionGateFromMCP(
	ctx context.Context,
	orgID string,
	id uuid.UUID,
	input string,
	confirmedDraft *executiongate.ConfirmedDraft,
	principal executiongate.PrincipalContext,
	binding preparedactions.MCPContextBinding,
) (executiongate.Result, error) {
	normalized, err := executiongate.NormalizePrincipalContext(principal)
	if err != nil {
		return executiongate.Result{}, domainerr.Validation(err.Error())
	}
	return u.executionGate(ctx, orgID, id, input, confirmedDraft, nil, normalized, uuid.Nil, &binding, binding.CapabilityKey)
}

func (u *UseCases) executionGate(
	ctx context.Context,
	orgID string,
	id uuid.UUID,
	input string,
	confirmedDraft *executiongate.ConfirmedDraft,
	requestedPreparedAction *preparedactions.PreparedActionV2,
	principal executiongate.PrincipalContext,
	assistRunID uuid.UUID,
	mcpBinding *preparedactions.MCPContextBinding,
	forcedCapabilityKey string,
) (executiongate.Result, error) {
	orgID = normalizeOrgID(orgID)
	var result dryrun.Result
	var err error
	if strings.TrimSpace(forcedCapabilityKey) != "" {
		result, err = u.dryRunForCapability(ctx, orgID, id, input, forcedCapabilityKey)
	} else {
		result, err = u.dryRun(ctx, orgID, id, input)
	}
	if err != nil {
		return executiongate.Result{}, err
	}
	var preparedAction *preparedactions.Action
	var preparedActionV2 *preparedactions.PreparedActionV2
	if requestedPreparedAction != nil && confirmedDraft != nil {
		return executiongate.Result{}, domainerr.Validation("only one prepared action confirmation may be provided")
	}
	if requestedPreparedAction != nil {
		preparedActionV2, err = validateRequestedRuntimeAction(result, *requestedPreparedAction, principal)
		if err != nil {
			return executiongate.Result{}, domainerr.Validation(err.Error())
		}
	} else if confirmedDraft != nil {
		result, preparedActionV2, err = prepareConfirmedActionV2(result, *confirmedDraft, principal)
		if err != nil {
			return executiongate.Result{}, domainerr.Validation(err.Error())
		}
		if preparedActionV2 == nil {
			result, err = executiongate.ApplyConfirmedDraft(result, *confirmedDraft)
			if err != nil {
				return executiongate.Result{}, domainerr.Validation(err.Error())
			}
		}
	}
	if preparedActionV2 == nil && confirmedDraft != nil && result.Draft.Status == dryrun.DraftStatusReady {
		prepared, prepareErr := preparedactions.FromReadyDraft(result.Draft)
		if prepareErr != nil {
			return executiongate.Result{}, domainerr.Validation(prepareErr.Error())
		}
		if prepared != nil {
			prepared.PrincipalType = principal.Type
			prepared.PrincipalID = principal.ID
		}
		preparedAction = prepared
	}
	assistBinding, err := u.resolveAssistExecutionBinding(ctx, orgID, id, assistRunID)
	if err != nil {
		return executiongate.Result{}, err
	}
	scopeResult, professionalScope, scopeEvaluated := u.evaluateProfessionalActionScope(
		ctx, orgID, id, result.RuntimeContext.Virployee.JobRoleID,
		professionalActionScopeQueryV2(result.Intent.CapabilityKey, preparedAction, preparedActionV2),
	)
	if preparedAction != nil {
		preparedAction.AssistContext = assistBinding
		preparedAction.ProfessionalScope = professionalScope
		preparedAction.MCPContext = mcpBinding
	}
	if preparedActionV2 != nil {
		preparedActionV2.AssistContext = assistBinding
		preparedActionV2.ProfessionalScope = professionalScope
		preparedActionV2.MCPContext = mcpBinding
	}
	gate := executiongate.Evaluate(result)
	if scopeEvaluated {
		gate = executiongate.ApplyProfessionalScope(gate, scopeResult)
	}
	var authority *executiongate.AuthorityCheckResult
	if gate.Gate.Decision == executiongate.DecisionPass && u.authority != nil {
		capability, ok := runtimeCapabilityForIntent(
			result.RuntimeContext.Capabilities,
			result.Intent.CapabilityID,
			result.Intent.CapabilityKey,
		)
		if !ok {
			gate = executiongate.ApplyAuthorityUnavailable(gate)
		} else {
			evaluated, authorityErr := u.evaluateAuthority(ctx, orgID, id, result.RuntimeContext.Virployee.JobRoleID, capability, principal, mcpBinding)
			if authorityErr != nil {
				gate = executiongate.ApplyAuthorityUnavailable(gate)
			} else {
				authority = &evaluated
				gate = executiongate.ApplyAuthority(gate, evaluated)
			}
		}
	}
	bindingHash, err := bindingHashForAuthorityContextV2(orgID, result, preparedAction, preparedActionV2, authority, assistBinding, professionalScope)
	if err != nil {
		return executiongate.Result{}, err
	}
	gate.BindingHash = bindingHash
	if gate.Gate.Decision != executiongate.DecisionPass {
		if err := u.recordExecutionGateTrace(ctx, orgID, gate, nil, bindingHash); err != nil {
			return executiongate.Result{}, err
		}
		return gate, nil
	}
	if u.governance == nil {
		gate = executiongate.ApplyGovernanceUnavailable(gate)
		nexus := &runtraces.NexusResult{
			Available:   false,
			BindingHash: bindingHash,
			Error:       "governance checker is not configured",
		}
		if err := u.recordExecutionGateTrace(ctx, orgID, gate, nexus, bindingHash); err != nil {
			return executiongate.Result{}, err
		}
		return gate, nil
	}
	governance, err := u.governance.Check(ctx, governanceInput(orgID, result, bindingHash, mcpBinding, authority))
	if err != nil {
		gate = executiongate.ApplyGovernanceUnavailable(gate)
		nexus := &runtraces.NexusResult{
			Available:   false,
			BindingHash: bindingHash,
			Error:       runtraces.RedactText(err.Error()),
		}
		if err := u.recordExecutionGateTrace(ctx, orgID, gate, nexus, bindingHash); err != nil {
			return executiongate.Result{}, err
		}
		return gate, nil
	}
	gate.Governance = &governance
	gate = executiongate.ApplyGovernance(gate, governance)
	if (preparedAction != nil || preparedActionV2 != nil) && governance.Decision == "require_approval" {
		var payloadHash string
		var hashErr error
		if preparedActionV2 != nil {
			payloadHash, hashErr = preparedActionV2.PayloadHash()
		} else {
			payloadHash, hashErr = preparedAction.PayloadHash()
		}
		if hashErr != nil {
			return executiongate.Result{}, hashErr
		}
		if u.executionRepo == nil {
			return executiongate.Result{}, domainerr.Conflict("execution repository is not configured")
		}
		if preparedActionV2 != nil {
			v2Repo, ok := u.executionRepo.(ExecutionRepositoryV2Port)
			if !ok {
				return executiongate.Result{}, domainerr.Conflict("execution repository cannot persist prepared action v2")
			}
			capabilityID, parseErr := uuid.Parse(preparedActionV2.CapabilityID)
			if parseErr != nil {
				return executiongate.Result{}, domainerr.Conflict("prepared action capability id is invalid")
			}
			if _, saveErr := v2Repo.SavePreparedActionV2(ctx, orgID, id, governance.CheckID, governance.ApprovalID, capabilityID, result.Intent.CapabilityKey, payloadHash, bindingHash, *preparedActionV2); saveErr != nil {
				return executiongate.Result{}, saveErr
			}
		} else if _, saveErr := u.executionRepo.SavePreparedAction(ctx, orgID, id, governance.CheckID, governance.ApprovalID, result.Intent.CapabilityKey, payloadHash, bindingHash, *preparedAction); saveErr != nil {
			return executiongate.Result{}, saveErr
		}
		var parsedApprovalID uuid.UUID
		if strings.TrimSpace(governance.PolicySnapshotHash) != "" || authority != nil {
			var parseErr error
			parsedApprovalID, parseErr = uuid.Parse(governance.ApprovalID)
			if parseErr != nil {
				return executiongate.Result{}, domainerr.Conflict("governance returned an invalid approval id")
			}
		}
		if strings.TrimSpace(governance.PolicySnapshotHash) != "" {
			var bindErr error
			if policyRepo, ok := u.executionRepo.(GovernancePolicyPreparedActionRepositoryPort); ok {
				bindErr = policyRepo.BindPreparedActionGovernancePolicy(ctx, orgID, id, parsedApprovalID, governance.PolicySnapshotHash)
			} else if legacyRepo, ok := u.executionRepo.(NexusPolicyPreparedActionRepositoryPort); ok {
				bindErr = legacyRepo.BindPreparedActionNexusPolicy(ctx, orgID, id, parsedApprovalID, governance.PolicySnapshotHash)
			} else {
				return executiongate.Result{}, domainerr.Conflict("execution repository cannot bind governance policy authority")
			}
			if bindErr != nil {
				return executiongate.Result{}, bindErr
			}
		}
		if authority != nil {
			authorityRepo, ok := u.executionRepo.(AuthorityPreparedActionRepositoryPort)
			if !ok {
				return executiongate.Result{}, domainerr.Conflict("execution repository cannot bind professional authority")
			}
			if bindErr := authorityRepo.BindPreparedActionAuthority(ctx, orgID, id, parsedApprovalID, authority.SnapshotHash); bindErr != nil {
				return executiongate.Result{}, bindErr
			}
		}
	}
	if err := u.recordExecutionGateTrace(ctx, orgID, gate, nexusTraceFrom(governance, bindingHash), bindingHash); err != nil {
		return executiongate.Result{}, err
	}
	return gate, nil
}

func runtimeCapability(capabilities []capabilitydomain.Capability, key string) (capabilitydomain.Capability, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, capability := range capabilities {
		if capability.CapabilityKey == key {
			return capability, true
		}
	}
	return capabilitydomain.Capability{}, false
}

func runtimeCapabilityForIntent(
	capabilities []capabilitydomain.Capability,
	capabilityID string,
	key string,
) (capabilitydomain.Capability, bool) {
	capabilityID = strings.TrimSpace(capabilityID)
	if capabilityID != "" {
		for _, capability := range capabilities {
			if capability.ID.String() == capabilityID {
				return capability, true
			}
		}
		return capabilitydomain.Capability{}, false
	}
	return runtimeCapability(capabilities, key)
}

// dryRunForCapability skips intent inference for a server-selected MCP tool.
// The capability still passes through the regular deterministic assignment,
// autonomy, draft, authority and governance checks.
func (u *UseCases) dryRunForCapability(ctx context.Context, orgID string, id uuid.UUID, input, capabilityKey string) (dryrun.Result, error) {
	runtimeCtx, err := u.RuntimeContext(ctx, orgID, id)
	if err != nil {
		return dryrun.Result{}, err
	}
	capabilityKey = strings.ToLower(strings.TrimSpace(capabilityKey))
	parts := strings.Split(capabilityKey, ".")
	if len(parts) < 3 {
		return dryrun.Result{}, domainerr.Validation("MCP capability key is invalid")
	}
	var required domain.AutonomyLevel
	for _, capability := range runtimeCtx.Capabilities {
		if capability.CapabilityKey == capabilityKey {
			required = capability.RequiredAutonomy
			break
		}
	}
	if required == "" {
		return dryrun.Result{}, domainerr.Forbidden("MCP capability is not assigned to the Virployee")
	}
	proposal := dryrun.Proposal{
		Intent: dryrun.Intent{
			Matched: true, CapabilityKey: capabilityKey,
			Domain: parts[0], Resource: parts[1], Action: parts[len(parts)-1],
			Confidence: 1, ProposedBy: "mcp",
		},
		RequiredAutonomy: required,
	}
	return dryrun.EvaluateWithProposal(input, runtimeCtx, proposal), nil
}

func (u *UseCases) ListRuns(ctx context.Context, orgID string, id uuid.UUID, limit int) ([]runtraces.Trace, error) {
	orgID = normalizeOrgID(orgID)
	if _, err := u.repo.Get(ctx, orgID, id); err != nil {
		return nil, err
	}
	return u.repo.ListRunTraces(ctx, orgID, id, normalizeRunTraceLimit(limit))
}

func (u *UseCases) SimulateApprovedExecution(ctx context.Context, orgID string, id uuid.UUID, approvalID uuid.UUID) (runtraces.Trace, error) {
	orgID = normalizeOrgID(orgID)
	if u.approvals == nil {
		return runtraces.Trace{}, domainerr.Conflict("approval reader is not configured")
	}
	if _, err := u.repo.Get(ctx, orgID, id); err != nil {
		return runtraces.Trace{}, err
	}
	approval, err := u.approvals.GetApproval(ctx, orgID, approvalID)
	if err != nil {
		return runtraces.Trace{}, err
	}
	if strings.TrimSpace(approval.Status) != "approved" {
		return runtraces.Trace{}, domainerr.Conflict("approval is not approved")
	}
	if strings.TrimSpace(approval.RequesterID) != id.String() {
		return runtraces.Trace{}, domainerr.Conflict("approval does not belong to virployee")
	}
	if strings.TrimSpace(approval.BindingHash) == "" {
		return runtraces.Trace{}, domainerr.Conflict("approval has no binding hash")
	}
	if existing, err := u.repo.FindSimulatedExecutionTraceByApproval(ctx, orgID, id, approvalID.String()); err == nil {
		return existing, nil
	} else if !domainerr.IsNotFound(err) {
		return runtraces.Trace{}, err
	}
	source, err := u.repo.FindExecutionGateTraceByApproval(ctx, orgID, id, approvalID.String())
	if err != nil {
		return runtraces.Trace{}, err
	}
	if source.NexusResult == nil || source.NexusResult.Decision != "require_approval" {
		return runtraces.Trace{}, domainerr.Conflict("approval trace is not a require_approval gate")
	}
	if source.BindingHash != approval.BindingHash {
		return runtraces.Trace{}, domainerr.Conflict("approval binding does not match evaluated action")
	}
	nexus := *source.NexusResult
	nexus.ApprovalStatus = approval.Status
	nexus.BindingHash = approval.BindingHash
	return u.repo.CreateRunTrace(ctx, orgID, runtraces.CreateInput{
		VirployeeID:    id,
		Operation:      runtraces.OperationSimulatedExecution,
		Input:          source.InputPreview,
		InputHash:      source.InputHash,
		InputPreview:   source.InputPreview,
		Intent:         source.Intent,
		CapabilityID:   source.CapabilityID,
		CapabilityKey:  source.CapabilityKey,
		DryRunDecision: source.DryRunDecision,
		GateDecision:   "pass",
		GateChecks: []runtraces.GateCheck{
			{Key: "approval_status", Status: "pass", Reason: "approval is approved"},
			{Key: "binding_hash", Status: "pass", Reason: "approval binding matches evaluated action"},
			{Key: "external_effects", Status: "pass", Reason: "no external effects were performed"},
		},
		NexusResult: &nexus,
		ExecutionResult: &runtraces.ExecutionResult{
			Status:          "simulated_executed",
			Mode:            "simulation",
			ApprovalID:      approval.ID,
			ApprovalStatus:  approval.Status,
			BindingHash:     approval.BindingHash,
			Message:         "Simulated execution completed; no external effects were performed.",
			ExternalEffects: false,
		},
		BindingHash:       approval.BindingHash,
		MemoryReferences:  source.MemoryReferences,
		MemoryContextHash: source.MemoryContextHash,
	})
}

func (u *UseCases) Update(ctx context.Context, orgID string, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := u.jobRoles.EnsureActive(ctx, normalizeOrgID(orgID), normalized.JobRoleID); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.profileTemplates.EnsureUsableByVirployee(ctx, normalizeOrgID(orgID), normalized.ProfileTemplateID, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.capabilities.EnsureAssignable(ctx, normalizeOrgID(orgID), normalized.CapabilityIDs, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Update(ctx, normalizeOrgID(orgID), id, normalized)
}

func (u *UseCases) Archive(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Archive(ctx, &lifecycle.ArchiveRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Unarchive(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Unarchive(ctx, &lifecycle.UnarchiveRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Trash(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Trash(ctx, &lifecycle.TrashRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Restore(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Purge(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Purge(ctx, &lifecycle.PurgeRequest{
		ResourceType:  ResourceTypeVirployee,
		ResourceID:    id,
		TenantID:      normalizeOrgID(orgID),
		Actor:         normalizeActor(actor),
		Reason:        strings.TrimSpace(reason),
		MustBeTrashed: true,
	})
}

func normalizeOrgID(orgID string) string {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return DefaultOrgID
	}
	return orgID
}

func normalizeActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return DefaultActorID
	}
	return actor
}

func governanceInput(orgID string, result dryrun.Result, bindingHash string, mcpBinding *preparedactions.MCPContextBinding, authority ...*executiongate.AuthorityCheckResult) executiongate.GovernanceCheckInput {
	productSurface := "companion"
	if capability, ok := runtimeCapability(result.RuntimeContext.Capabilities, result.Intent.CapabilityKey); ok && strings.TrimSpace(capability.Manifest.ProductSurface) != "" {
		productSurface = capability.Manifest.ProductSurface
	}
	out := executiongate.GovernanceCheckInput{
		OrgID:            normalizeOrgID(orgID),
		ProductSurface:   productSurface,
		RequesterType:    "virployee",
		RequesterID:      result.RuntimeContext.Virployee.ID.String(),
		SupervisorUserID: result.RuntimeContext.Virployee.SupervisorUserID,
		ActionType:       result.Intent.CapabilityKey,
		TargetSystem:     result.Intent.Domain,
		TargetResource:   result.Intent.Resource,
		ResourceType:     result.Intent.Resource,
		Reason:           "Virployee capability invocation",
		BindingHash:      bindingHash,
	}
	if mcpBinding != nil {
		// MCP governance is metadata-only. Nexus receives stable hashes and
		// opaque internal references, never raw arguments, conversations or
		// subject documents.
		out.Reason = "MCP capability invocation"
		out.ResourceType = "work_subject"
		out.TargetResource = mcpBinding.SubjectID
		if strings.TrimSpace(mcpBinding.CaseID) != "" {
			out.ResourceType = "case"
			out.TargetResource = mcpBinding.CaseID
		}
	}
	if len(authority) > 0 && authority[0] != nil {
		out.AuthorityBindingHash = authority[0].SnapshotHash
		out.ScopeRevision = authority[0].ScopeRevision
		out.PolicyRevisionHash = authority[0].PolicyRevisionHash
		out.DelegationRequired = authority[0].DelegationRequired
		out.DelegationID = authority[0].DelegationID
		out.DelegationRevision = authority[0].DelegationRevision
	}
	return out
}

func (u *UseCases) recordDryRunTrace(ctx context.Context, orgID string, result dryrun.Result) error {
	capabilityID, capabilityKey := capabilityTraceFields(result)
	_, err := u.repo.CreateRunTrace(ctx, orgID, runtraces.CreateInput{
		VirployeeID:       result.RuntimeContext.Virployee.ID,
		Operation:         runtraces.OperationDryRun,
		Input:             result.Input,
		Intent:            intentTrace(result.Intent),
		CapabilityID:      capabilityID,
		CapabilityKey:     capabilityKey,
		DryRunDecision:    string(result.Decision),
		GateChecks:        []runtraces.GateCheck{},
		MemoryReferences:  result.RuntimeContext.MemoryReferences,
		MemoryContextHash: result.RuntimeContext.MemoryContextHash,
	})
	return err
}

func (u *UseCases) recordExecutionGateTrace(
	ctx context.Context,
	orgID string,
	result executiongate.Result,
	nexus *runtraces.NexusResult,
	bindingHash string,
) error {
	capabilityID, capabilityKey := capabilityTraceFields(result.DryRun)
	_, err := u.repo.CreateRunTrace(ctx, orgID, runtraces.CreateInput{
		VirployeeID:       result.DryRun.RuntimeContext.Virployee.ID,
		Operation:         runtraces.OperationExecutionGate,
		Input:             result.Input,
		Intent:            intentTrace(result.DryRun.Intent),
		CapabilityID:      capabilityID,
		CapabilityKey:     capabilityKey,
		DryRunDecision:    string(result.DryRun.Decision),
		GateDecision:      string(result.Gate.Decision),
		GateChecks:        gateChecksTrace(result.Gate.Checks),
		NexusResult:       nexus,
		BindingHash:       bindingHash,
		MemoryReferences:  result.DryRun.RuntimeContext.MemoryReferences,
		MemoryContextHash: result.DryRun.RuntimeContext.MemoryContextHash,
	})
	return err
}

func normalizeRunTraceLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func capabilityTraceFields(result dryrun.Result) (string, string) {
	if result.RequiredCapability == nil {
		return "", result.Intent.CapabilityKey
	}
	return result.RequiredCapability.ID, result.RequiredCapability.CapabilityKey
}

func intentTrace(intent dryrun.Intent) map[string]any {
	rules := make([]any, 0, len(intent.Rules))
	for _, rule := range intent.Rules {
		rules = append(rules, map[string]any{
			"type":   rule.Type,
			"target": rule.Target,
			"value":  rule.Value,
		})
	}
	return map[string]any{
		"matched":        intent.Matched,
		"capability_id":  intent.CapabilityID,
		"capability_key": intent.CapabilityKey,
		"domain":         intent.Domain,
		"resource":       intent.Resource,
		"action":         intent.Action,
		"confidence":     intent.Confidence,
		"matched_by":     intent.MatchedBy,
		"rules":          rules,
	}
}

func gateChecksTrace(checks []executiongate.Check) []runtraces.GateCheck {
	out := make([]runtraces.GateCheck, 0, len(checks))
	for _, check := range checks {
		out = append(out, runtraces.GateCheck{
			Key:    check.Key,
			Status: string(check.Status),
			Reason: check.Reason,
		})
	}
	return out
}

func bindingHashForAuthority(orgID string, result dryrun.Result, prepared *preparedactions.Action, authority *executiongate.AuthorityCheckResult) (string, error) {
	return bindingHashForAuthorityContext(orgID, result, prepared, authority, nil, nil)
}

func bindingHashForAuthorityContext(
	orgID string,
	result dryrun.Result,
	prepared *preparedactions.Action,
	authority *executiongate.AuthorityCheckResult,
	assist *preparedactions.AssistContextBinding,
	professionalScope *preparedactions.ProfessionalScopeBinding,
) (string, error) {
	binding := actionBinding(orgID, result, prepared)
	if binding != nil && authority != nil {
		binding["professional_authority"] = map[string]any{
			"snapshot_hash":        authority.SnapshotHash,
			"scope_revision":       authority.ScopeRevision,
			"policy_revision_hash": authority.PolicyRevisionHash,
			"delegation_required":  authority.DelegationRequired,
			"delegation_id":        authority.DelegationID,
			"delegation_revision":  authority.DelegationRevision,
		}
	}
	if binding != nil && assist != nil {
		binding["assist_context"] = assist
	}
	if binding != nil && professionalScope != nil {
		binding["professional_scope"] = professionalScope
	}
	return runtraces.BindingHash(binding)
}

func bindingHashForAuthorityContextV2(
	orgID string,
	result dryrun.Result,
	legacy *preparedactions.Action,
	prepared *preparedactions.PreparedActionV2,
	authority *executiongate.AuthorityCheckResult,
	assist *preparedactions.AssistContextBinding,
	professionalScope *preparedactions.ProfessionalScopeBinding,
) (string, error) {
	if prepared == nil {
		return bindingHashForAuthorityContext(orgID, result, legacy, authority, assist, professionalScope)
	}
	binding := actionBindingV2(orgID, result, *prepared)
	if authority != nil {
		binding["professional_authority"] = map[string]any{
			"snapshot_hash":        authority.SnapshotHash,
			"scope_revision":       authority.ScopeRevision,
			"policy_revision_hash": authority.PolicyRevisionHash,
			"delegation_required":  authority.DelegationRequired,
			"delegation_id":        authority.DelegationID,
			"delegation_revision":  authority.DelegationRevision,
		}
	}
	if assist != nil {
		binding["assist_context"] = assist
	}
	if professionalScope != nil {
		binding["professional_scope"] = professionalScope
	}
	return runtraces.BindingHash(binding)
}

func actionBindingV2(orgID string, result dryrun.Result, prepared preparedactions.PreparedActionV2) map[string]any {
	proposedBy := result.Intent.ProposedBy
	if proposedBy == "" {
		proposedBy = "deterministic"
	}
	payloadHash, _ := prepared.PayloadHash()
	return map[string]any{
		"schema_version":       preparedactions.V2SchemaVersion,
		"org_id":               normalizeOrgID(orgID),
		"virployee_id":         result.RuntimeContext.Virployee.ID.String(),
		"operation":            prepared.Operation,
		"capability_id":        prepared.CapabilityID,
		"capability_key_alias": result.Intent.CapabilityKey,
		"manifest_hash":        prepared.ManifestHash,
		"executor_binding_id":  prepared.ExecutorBindingID,
		"input_schema_hash":    prepared.InputSchemaHash,
		"output_schema_hash":   prepared.OutputSchemaHash,
		"prepared_action_hash": payloadHash,
		"input_hash":           runtraces.HashString(result.Input),
		"memory_context_hash":  result.RuntimeContext.MemoryContextHash,
		"proposed_by":          proposedBy,
		"model_id":             result.Intent.ModelID,
		"prompt_version":       result.Intent.PromptVersion,
		"intent_confidence":    result.Intent.Confidence,
	}
}

func actionBinding(orgID string, result dryrun.Result, prepared *preparedactions.Action) map[string]any {
	if !result.Intent.Matched {
		return nil
	}
	proposedBy := result.Intent.ProposedBy
	if proposedBy == "" {
		proposedBy = "deterministic"
	}
	binding := map[string]any{
		"schema_version":      "tool_intent.v1",
		"org_id":              normalizeOrgID(orgID),
		"virployee_id":        result.RuntimeContext.Virployee.ID.String(),
		"operation":           "execution_gate",
		"capability_key":      result.Intent.CapabilityKey,
		"action":              result.Intent.Action,
		"target_system":       result.Intent.Domain,
		"target_resource":     result.Intent.Resource,
		"input_hash":          runtraces.HashString(result.Input),
		"memory_context_hash": result.RuntimeContext.MemoryContextHash,
		// Proposal provenance: a change of proposer, model or prompt version
		// changes the binding, so an approval cannot be replayed against a
		// different runtime than the one that produced it.
		"proposed_by":       proposedBy,
		"model_id":          result.Intent.ModelID,
		"prompt_version":    result.Intent.PromptVersion,
		"intent_confidence": result.Intent.Confidence,
	}
	if prepared != nil {
		payloadHash, err := prepared.PayloadHash()
		if err == nil {
			binding["prepared_action_schema"] = prepared.SchemaVersion
			binding["prepared_action_hash"] = payloadHash
		}
	}
	return binding
}

func nexusTraceFrom(governance executiongate.GovernanceCheckResult, bindingHash string) *runtraces.NexusResult {
	if governance.BindingHash != "" {
		bindingHash = governance.BindingHash
	}
	return &runtraces.NexusResult{
		CheckID:              governance.CheckID,
		Available:            true,
		Decision:             governance.Decision,
		RiskLevel:            governance.RiskLevel,
		Status:               governance.Status,
		DecisionReason:       governance.DecisionReason,
		WouldRequireApproval: governance.WouldRequireApproval,
		BindingHash:          bindingHash,
		ApprovalID:           governance.ApprovalID,
		ApprovalStatus:       governance.ApprovalStatus,
	}
}

type noopLifecycleAudit struct{}

func (noopLifecycleAudit) Append(context.Context, lifecycle.AuditEvent) error {
	return nil
}

type noopJobRoleReader struct{}

func (noopJobRoleReader) EnsureActive(context.Context, string, uuid.UUID) error {
	return nil
}

func (noopJobRoleReader) Get(context.Context, string, uuid.UUID) (jobroledomain.JobRole, error) {
	return jobroledomain.JobRole{}, domainerr.NotFound("job role not found")
}

type noopCapabilityValidator struct{}

func (noopCapabilityValidator) EnsureAssignable(context.Context, string, []uuid.UUID, domain.AutonomyLevel) error {
	return nil
}

func (noopCapabilityValidator) Get(context.Context, string, uuid.UUID) (capabilitydomain.Capability, error) {
	return capabilitydomain.Capability{}, domainerr.NotFound("capability not found")
}

type noopProfileTemplateReader struct{}

func (noopProfileTemplateReader) EnsureUsableByVirployee(context.Context, string, uuid.UUID, domain.AutonomyLevel) error {
	return nil
}

func (noopProfileTemplateReader) Get(context.Context, string, uuid.UUID) (profiletemplatedomain.ProfileTemplate, error) {
	return profiletemplatedomain.ProfileTemplate{}, domainerr.NotFound("profile template not found")
}
