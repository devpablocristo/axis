package virployees

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/memories"
	profiletemplatedomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
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
	DefaultTenantID       = "default"
	DefaultActorID        = "system"
)

type RepositoryPort interface {
	lifecycle.RepositoryPort

	Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.Virployee, error)
	List(ctx context.Context, tenantID string, state domain.State) ([]domain.Virployee, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Virployee, error)
	Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error)
	CreateRunTrace(ctx context.Context, tenantID string, input runtraces.CreateInput) (runtraces.Trace, error)
	ListRunTraces(ctx context.Context, tenantID string, virployeeID uuid.UUID, limit int) ([]runtraces.Trace, error)
	FindExecutionGateTraceByApproval(ctx context.Context, tenantID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error)
	FindSimulatedExecutionTraceByApproval(ctx context.Context, tenantID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error)
}

type ExecutionRepositoryPort interface {
	FindExecutionTraceByApproval(ctx context.Context, tenantID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error)
	SavePreparedAction(ctx context.Context, tenantID string, virployeeID uuid.UUID, checkID, approvalID string, capabilityKey, payloadHash, bindingHash string, action preparedactions.Action) (PreparedActionRecord, error)
	GetPreparedActionByApproval(ctx context.Context, tenantID string, virployeeID, approvalID uuid.UUID) (PreparedActionRecord, error)
	BeginExecution(ctx context.Context, tenantID string, virployeeID uuid.UUID, preparedActionID uuid.UUID, idempotencyKey string) (ExecutionAttempt, bool, error)
	GetExecutionByPreparedAction(ctx context.Context, tenantID string, preparedActionID uuid.UUID) (ExecutionAttempt, error)
	CompleteExecution(ctx context.Context, tenantID string, id uuid.UUID, status, resourceID string, result map[string]any, executionError string, durationMS int64) (ExecutionAttempt, error)
	CreateLocalCalendarEvent(ctx context.Context, tenantID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (string, error)
	SetNexusReportStatus(ctx context.Context, tenantID string, id uuid.UUID, status string) error
}

type JobRoleReaderPort interface {
	EnsureActive(ctx context.Context, tenantID string, id uuid.UUID) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (jobroledomain.JobRole, error)
}

type CapabilityValidatorPort interface {
	EnsureAssignable(ctx context.Context, tenantID string, ids []uuid.UUID, autonomy domain.AutonomyLevel) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (capabilitydomain.Capability, error)
}

type ProfileTemplateReaderPort interface {
	EnsureUsableByVirployee(ctx context.Context, tenantID string, id uuid.UUID, autonomy domain.AutonomyLevel) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (profiletemplatedomain.ProfileTemplate, error)
}

type GovernanceCheckerPort interface {
	Check(ctx context.Context, input executiongate.GovernanceCheckInput) (executiongate.GovernanceCheckResult, error)
}

type ApprovalReaderPort interface {
	GetApproval(ctx context.Context, tenantID string, id uuid.UUID) (executiongate.GovernanceApproval, error)
}

type ExecutionResultReporterPort interface {
	ReportExecutionResult(ctx context.Context, tenantID, checkID, idempotencyKey, bindingHash, status string, durationMS int64, result map[string]any) error
}

type ActionExecutorPort interface {
	Execute(ctx context.Context, tenantID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (string, map[string]any, error)
}

type MemoryReaderPort interface {
	RecallInternal(context.Context, string, uuid.UUID, string, int) ([]memories.Recalled, error)
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
	SystemPrompt   string
	JobRole        string
	InputJSON      json.RawMessage
	ResponseSchema map[string]any
}

type AnswerOutput struct {
	OutputText    string
	OutputJSON    json.RawMessage
	Answered      bool
	ModelID       string
	PromptVersion string
}

// RuntimeAnswererPort asks the runtime to process input and answer (read/explain,
// no governance decision). Unset ⇒ the Assist usecase is unavailable (fail-closed).
type RuntimeAnswererPort interface {
	Answer(ctx context.Context, in AnswerInput) (AnswerOutput, error)
}

// AssistRepositoryPort persists product assist runs (reserve-before-LLM).
type AssistRepositoryPort interface {
	BeginAssistRun(ctx context.Context, tenantID string, virployeeID uuid.UUID, assistType, idempotencyKey, inputHash, inputPreview string) (AssistRun, bool, error)
	CompleteAssistRun(ctx context.Context, tenantID string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error)
	GetAssistRunByKey(ctx context.Context, tenantID string, virployeeID uuid.UUID, idempotencyKey string) (AssistRun, error)
}

type UseCases struct {
	repo             RepositoryPort
	executionRepo    ExecutionRepositoryPort
	jobRoles         JobRoleReaderPort
	capabilities     CapabilityValidatorPort
	profileTemplates ProfileTemplateReaderPort
	governance       GovernanceCheckerPort
	approvals        ApprovalReaderPort
	resultReporter   ExecutionResultReporterPort
	executors        map[string]ActionExecutorPort
	memories         MemoryReaderPort
	runtime          RuntimePlannerPort
	answerer         RuntimeAnswererPort
	assistRepo       AssistRepositoryPort
	lifecycle        *lifecycle.Service
}

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
		lifecycle:        service,
	}
	if executionRepo, ok := repo.(ExecutionRepositoryPort); ok {
		uc.executionRepo = executionRepo
	}
	if assistRepo, ok := repo.(AssistRepositoryPort); ok {
		uc.assistRepo = assistRepo
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

func (u *UseCases) SetApprovalReader(reader ApprovalReaderPort) {
	u.approvals = reader
}

func (u *UseCases) SetExecutionResultReporter(reporter ExecutionResultReporterPort) {
	u.resultReporter = reporter
}

func (u *UseCases) SetMemoryReader(reader MemoryReaderPort) { u.memories = reader }

func (u *UseCases) SetRuntimePlanner(planner RuntimePlannerPort) { u.runtime = planner }

func (u *UseCases) SetRuntimeAnswerer(answerer RuntimeAnswererPort) { u.answerer = answerer }

func (u *UseCases) RegisterExecutor(action string, executor ActionExecutorPort) {
	action = strings.TrimSpace(action)
	if action != "" && executor != nil {
		u.executors[action] = executor
	}
}

func (u *UseCases) Create(ctx context.Context, tenantID string, input domain.CreateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := u.jobRoles.EnsureActive(ctx, normalizeTenantID(tenantID), normalized.JobRoleID); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.profileTemplates.EnsureUsableByVirployee(ctx, normalizeTenantID(tenantID), normalized.ProfileTemplateID, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.capabilities.EnsureAssignable(ctx, normalizeTenantID(tenantID), normalized.CapabilityIDs, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Create(ctx, normalizeTenantID(tenantID), normalized)
}

func (u *UseCases) ListActive(ctx context.Context, tenantID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateActive)
}

func (u *UseCases) ListArchived(ctx context.Context, tenantID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateArchived)
}

func (u *UseCases) ListTrash(ctx context.Context, tenantID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateTrashed)
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Virployee, error) {
	return u.repo.Get(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) RuntimeContext(ctx context.Context, tenantID string, id uuid.UUID) (runtimecontext.Context, error) {
	tenantID = normalizeTenantID(tenantID)
	virployee, err := u.repo.Get(ctx, tenantID, id)
	if err != nil {
		return runtimecontext.Context{}, err
	}

	jobRole, err := u.jobRoles.Get(ctx, tenantID, virployee.JobRoleID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtimecontext.Context{}, domainerr.Validation("job_role_id must reference an active job role in the same tenant")
		}
		return runtimecontext.Context{}, err
	}
	if jobRole.State() != jobroledomain.StateActive {
		return runtimecontext.Context{}, domainerr.Validation("job_role_id must reference an active job role in the same tenant")
	}

	profileTemplate, err := u.profileTemplates.Get(ctx, tenantID, virployee.ProfileTemplateID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtimecontext.Context{}, domainerr.Validation("profile_template_id must reference an active profile template in the same tenant")
		}
		return runtimecontext.Context{}, err
	}
	if profileTemplate.State() != profiletemplatedomain.StateActive {
		return runtimecontext.Context{}, domainerr.Validation("profile_template_id must reference an active profile template in the same tenant")
	}
	if !profileTemplate.MaxAutonomy.Allows(virployee.Autonomy) {
		return runtimecontext.Context{}, domainerr.Validation("profile template " + profileTemplate.Name + " allows max autonomy " + string(profileTemplate.MaxAutonomy) + "; virployee autonomy " + string(virployee.Autonomy) + " exceeds it")
	}

	capabilities := make([]capabilitydomain.Capability, 0, len(virployee.CapabilityIDs))
	for _, capabilityID := range virployee.CapabilityIDs {
		capability, err := u.capabilities.Get(ctx, tenantID, capabilityID)
		if err != nil {
			if domainerr.IsNotFound(err) {
				return runtimecontext.Context{}, domainerr.Validation("capability_ids must reference active capabilities in the same tenant")
			}
			return runtimecontext.Context{}, err
		}
		if capability.State() != capabilitydomain.StateActive {
			return runtimecontext.Context{}, domainerr.Validation("capability_ids must reference active capabilities in the same tenant")
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
		items, recallErr := u.memories.RecallInternal(ctx, tenantID, id, virployee.Name+" "+virployee.Description, 5)
		if recallErr != nil {
			return runtimecontext.Context{}, recallErr
		}
		for _, item := range items {
			result.MemoryReferences = append(result.MemoryReferences, item.Reference)
		}
		result.MemoryContextHash = memories.ContextHash(result.MemoryReferences)
	}
	return result, nil
}

func (u *UseCases) DryRun(ctx context.Context, tenantID string, id uuid.UUID, input string) (dryrun.Result, error) {
	tenantID = normalizeTenantID(tenantID)
	result, err := u.dryRun(ctx, tenantID, id, input)
	if err != nil {
		return dryrun.Result{}, err
	}
	if err := u.recordDryRunTrace(ctx, tenantID, result); err != nil {
		return dryrun.Result{}, err
	}
	return result, nil
}

func (u *UseCases) dryRun(ctx context.Context, tenantID string, id uuid.UUID, input string) (dryrun.Result, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return dryrun.Result{}, domainerr.Validation("input is required")
	}
	runtimeCtx, err := u.RuntimeContext(ctx, tenantID, id)
	if err != nil {
		return dryrun.Result{}, err
	}
	if u.memories != nil {
		items, recallErr := u.memories.RecallInternal(ctx, tenantID, id, input, 5)
		if recallErr != nil {
			return dryrun.Result{}, recallErr
		}
		runtimeCtx.MemoryReferences = runtimeCtx.MemoryReferences[:0]
		for _, item := range items {
			runtimeCtx.MemoryReferences = append(runtimeCtx.MemoryReferences, item.Reference)
		}
		runtimeCtx.MemoryContextHash = memories.ContextHash(runtimeCtx.MemoryReferences)
	}
	if u.runtime != nil {
		proposal, err := u.runtime.Propose(ctx, input, runtimeCtx)
		if err != nil {
			// Fail-closed transport: if the runtime is unavailable or errors, do
			// not act on a half-formed proposal — fall back to the deterministic
			// matcher, which is scoped to the assigned capabilities and still
			// passes through the execution gate and Nexus.
			slog.WarnContext(ctx, "runtime_propose_failed_fallback_deterministic", "error", runtraces.RedactText(err.Error()))
			return dryrun.Evaluate(input, runtimeCtx), nil
		}
		return dryrun.EvaluateWithProposal(input, runtimeCtx, proposal), nil
	}
	return dryrun.Evaluate(input, runtimeCtx), nil
}

func (u *UseCases) ExecutionGate(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	input string,
	confirmedDraft *executiongate.ConfirmedDraft,
) (executiongate.Result, error) {
	tenantID = normalizeTenantID(tenantID)
	result, err := u.dryRun(ctx, tenantID, id, input)
	if err != nil {
		return executiongate.Result{}, err
	}
	if confirmedDraft != nil {
		result, err = executiongate.ApplyConfirmedDraft(result, *confirmedDraft)
		if err != nil {
			return executiongate.Result{}, domainerr.Validation(err.Error())
		}
	}
	var preparedAction *preparedactions.Action
	if confirmedDraft != nil && result.Intent.CapabilityKey == preparedactions.ActionCreate && result.Draft.Status == dryrun.DraftStatusReady {
		prepared, prepareErr := preparedactions.FromDraft(result.Draft)
		if prepareErr != nil {
			return executiongate.Result{}, domainerr.Validation(prepareErr.Error())
		}
		preparedAction = &prepared
	}
	gate := executiongate.Evaluate(result)
	bindingHash, err := bindingHashFor(tenantID, result, preparedAction)
	if err != nil {
		return executiongate.Result{}, err
	}
	if gate.Gate.Decision != executiongate.DecisionPass {
		if err := u.recordExecutionGateTrace(ctx, tenantID, gate, nil, bindingHash); err != nil {
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
		if err := u.recordExecutionGateTrace(ctx, tenantID, gate, nexus, bindingHash); err != nil {
			return executiongate.Result{}, err
		}
		return gate, nil
	}
	governance, err := u.governance.Check(ctx, governanceInput(tenantID, result, bindingHash))
	if err != nil {
		gate = executiongate.ApplyGovernanceUnavailable(gate)
		nexus := &runtraces.NexusResult{
			Available:   false,
			BindingHash: bindingHash,
			Error:       runtraces.RedactText(err.Error()),
		}
		if err := u.recordExecutionGateTrace(ctx, tenantID, gate, nexus, bindingHash); err != nil {
			return executiongate.Result{}, err
		}
		return gate, nil
	}
	gate = executiongate.ApplyGovernance(gate, governance)
	if preparedAction != nil && governance.Decision == "require_approval" {
		payloadHash, hashErr := preparedAction.PayloadHash()
		if hashErr != nil {
			return executiongate.Result{}, hashErr
		}
		if u.executionRepo == nil {
			return executiongate.Result{}, domainerr.Conflict("execution repository is not configured")
		}
		if _, saveErr := u.executionRepo.SavePreparedAction(ctx, tenantID, id, governance.CheckID, governance.ApprovalID, result.Intent.CapabilityKey, payloadHash, bindingHash, *preparedAction); saveErr != nil {
			return executiongate.Result{}, saveErr
		}
	}
	if err := u.recordExecutionGateTrace(ctx, tenantID, gate, nexusTraceFrom(governance, bindingHash), bindingHash); err != nil {
		return executiongate.Result{}, err
	}
	return gate, nil
}

func (u *UseCases) ListRuns(ctx context.Context, tenantID string, id uuid.UUID, limit int) ([]runtraces.Trace, error) {
	tenantID = normalizeTenantID(tenantID)
	if _, err := u.repo.Get(ctx, tenantID, id); err != nil {
		return nil, err
	}
	return u.repo.ListRunTraces(ctx, tenantID, id, normalizeRunTraceLimit(limit))
}

func (u *UseCases) SimulateApprovedExecution(ctx context.Context, tenantID string, id uuid.UUID, approvalID uuid.UUID) (runtraces.Trace, error) {
	tenantID = normalizeTenantID(tenantID)
	if u.approvals == nil {
		return runtraces.Trace{}, domainerr.Conflict("approval reader is not configured")
	}
	if _, err := u.repo.Get(ctx, tenantID, id); err != nil {
		return runtraces.Trace{}, err
	}
	approval, err := u.approvals.GetApproval(ctx, tenantID, approvalID)
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
	if existing, err := u.repo.FindSimulatedExecutionTraceByApproval(ctx, tenantID, id, approvalID.String()); err == nil {
		return existing, nil
	} else if !domainerr.IsNotFound(err) {
		return runtraces.Trace{}, err
	}
	source, err := u.repo.FindExecutionGateTraceByApproval(ctx, tenantID, id, approvalID.String())
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
	return u.repo.CreateRunTrace(ctx, tenantID, runtraces.CreateInput{
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

func (u *UseCases) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := u.jobRoles.EnsureActive(ctx, normalizeTenantID(tenantID), normalized.JobRoleID); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.profileTemplates.EnsureUsableByVirployee(ctx, normalizeTenantID(tenantID), normalized.ProfileTemplateID, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.capabilities.EnsureAssignable(ctx, normalizeTenantID(tenantID), normalized.CapabilityIDs, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Update(ctx, normalizeTenantID(tenantID), id, normalized)
}

func (u *UseCases) Archive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Archive(ctx, &lifecycle.ArchiveRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Unarchive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Unarchive(ctx, &lifecycle.UnarchiveRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Trash(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Trash(ctx, &lifecycle.TrashRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Restore(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Purge(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Purge(ctx, &lifecycle.PurgeRequest{
		ResourceType:  ResourceTypeVirployee,
		ResourceID:    id,
		TenantID:      normalizeTenantID(tenantID),
		Actor:         normalizeActor(actor),
		Reason:        strings.TrimSpace(reason),
		MustBeTrashed: true,
	})
}

func normalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return DefaultTenantID
	}
	return tenantID
}

func normalizeActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return DefaultActorID
	}
	return actor
}

func governanceInput(tenantID string, result dryrun.Result, bindingHash string) executiongate.GovernanceCheckInput {
	return executiongate.GovernanceCheckInput{
		TenantID:       normalizeTenantID(tenantID),
		RequesterType:  "virployee",
		RequesterID:    result.RuntimeContext.Virployee.ID.String(),
		ActionType:     result.Intent.CapabilityKey,
		TargetSystem:   result.Intent.Domain,
		TargetResource: result.Intent.Resource,
		Params:         governanceParams(result),
		Reason:         result.Input,
		Context:        result.RuntimeContext.JobRole.Name,
		BindingHash:    bindingHash,
	}
}

func governanceParams(result dryrun.Result) map[string]any {
	fields := make(map[string]any, len(result.Draft.Fields))
	for _, field := range result.Draft.Fields {
		if field.Key == "" {
			continue
		}
		fields[field.Key] = field.Value
	}
	return map[string]any{
		"draft_status": string(result.Draft.Status),
		"draft_kind":   result.Draft.Kind,
		"fields":       fields,
	}
}

func (u *UseCases) recordDryRunTrace(ctx context.Context, tenantID string, result dryrun.Result) error {
	capabilityID, capabilityKey := capabilityTraceFields(result)
	_, err := u.repo.CreateRunTrace(ctx, tenantID, runtraces.CreateInput{
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
	tenantID string,
	result executiongate.Result,
	nexus *runtraces.NexusResult,
	bindingHash string,
) error {
	capabilityID, capabilityKey := capabilityTraceFields(result.DryRun)
	_, err := u.repo.CreateRunTrace(ctx, tenantID, runtraces.CreateInput{
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

func bindingHashFor(tenantID string, result dryrun.Result, prepared *preparedactions.Action) (string, error) {
	return runtraces.BindingHash(actionBinding(tenantID, result, prepared))
}

func actionBinding(tenantID string, result dryrun.Result, prepared *preparedactions.Action) map[string]any {
	if !result.Intent.Matched {
		return nil
	}
	proposedBy := result.Intent.ProposedBy
	if proposedBy == "" {
		proposedBy = "deterministic"
	}
	binding := map[string]any{
		"schema_version":      "tool_intent.v1",
		"tenant_id":           normalizeTenantID(tenantID),
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
