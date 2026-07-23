package virployees

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type LocalCalendarExecutor struct {
	repo LegacyCalendarStorePort
}

func NewLocalCalendarExecutor(repo LegacyCalendarStorePort) *LocalCalendarExecutor {
	return &LocalCalendarExecutor{repo: repo}
}

func (e *LocalCalendarExecutor) Execute(ctx context.Context, orgID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (ExecutionOutcome, error) {
	// The local executor is a simulator with no external effects. A delete
	// (compensation) is a simulated no-op that still flows through the governed
	// path with its own binding — it just does not touch a real system.
	if action.Action == preparedactions.ActionDelete {
		return ExecutionOutcome{
			ResourceID:      action.EventID,
			Mode:            "local",
			ExternalEffects: false,
			Result: map[string]any{
				"mode":          "local",
				"operation":     "delete",
				"resource_id":   action.EventID,
				"resource_type": "calendar_event",
			},
		}, nil
	}
	resourceID, err := e.repo.CreateLocalCalendarEvent(ctx, orgID, virployeeID, attempt, action)
	if err != nil {
		return ExecutionOutcome{Mode: "local"}, err
	}
	return ExecutionOutcome{
		ResourceID:      resourceID,
		Mode:            "local",
		ExternalEffects: false,
		Result: map[string]any{
			"mode":          "local",
			"operation":     "create",
			"resource_id":   resourceID,
			"resource_type": "calendar_event",
		},
	}, nil
}

// resultString / resultBool read persisted execution metadata (stored in the
// attempt's Result map by the executor) so the run trace reflects the real mode and
// external-effects flag on both the first execution and any idempotent re-entry.
func resultString(m map[string]any, key, fallback string) string {
	if m != nil {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return fallback
}

func resultBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

func (u *UseCases) ExecuteApprovedAction(ctx context.Context, orgID string, id, approvalID uuid.UUID) (runtraces.Trace, error) {
	orgID = normalizeOrgID(orgID)
	if u.approvals == nil {
		return runtraces.Trace{}, domainerr.Conflict("approval reader is not configured")
	}
	if u.executionRepo == nil {
		return runtraces.Trace{}, domainerr.Conflict("execution repository is not configured")
	}
	if _, err := u.repo.Get(ctx, orgID, id); err != nil {
		return runtraces.Trace{}, err
	}
	approval, err := u.approvals.GetApproval(ctx, orgID, approvalID)
	if err != nil {
		return runtraces.Trace{}, err
	}
	if strings.TrimSpace(approval.Status) != "approved" || strings.TrimSpace(approval.RequesterID) != id.String() {
		return runtraces.Trace{}, domainerr.Conflict("approval is not executable for this virployee")
	}
	prepared, err := u.executionRepo.GetPreparedActionByApproval(ctx, orgID, id, approvalID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtraces.Trace{}, domainerr.Conflict("approval has no durable prepared action")
		}
		return runtraces.Trace{}, err
	}
	if prepared.BindingHash != approval.BindingHash || prepared.GovernanceCheckID.String() != approval.GovernanceCheckID {
		return runtraces.Trace{}, domainerr.Conflict("approval binding does not match prepared action")
	}
	governancePolicySnapshotHash := prepared.GovernancePolicySnapshotHash
	if governancePolicySnapshotHash == "" {
		governancePolicySnapshotHash = prepared.NexusPolicySnapshotHash
	}
	if governancePolicySnapshotHash != approval.PolicySnapshotHash {
		return runtraces.Trace{}, domainerr.Conflict("approval policy snapshot does not match prepared action")
	}
	isV2 := prepared.ActionV2 != nil
	var payloadHash string
	if isV2 {
		payloadHash, err = prepared.ActionV2.PayloadHash()
	} else {
		payloadHash, err = prepared.Action.PayloadHash()
	}
	if err != nil || payloadHash != prepared.PayloadHash {
		return runtraces.Trace{}, domainerr.Conflict("prepared action payload does not match its approved hash")
	}
	var currentVirployee domain.Virployee
	var currentCapability capabilitydomain.Capability
	var assistContext *preparedactions.AssistContextBinding
	var mcpContext *preparedactions.MCPContextBinding
	operation := prepared.Action.Action
	if isV2 {
		currentVirployee, currentCapability, err = u.verifyCurrentExecutionEligibilityV2(
			ctx, orgID, id, prepared.CapabilityID, prepared.CapabilityKey, *prepared.ActionV2,
		)
		if err == nil {
			err = u.verifyCurrentProfessionalActionScopeV2(ctx, orgID, id, currentVirployee.JobRoleID, prepared.CapabilityKey, *prepared.ActionV2)
		}
		assistContext = prepared.ActionV2.AssistContext
		mcpContext = prepared.ActionV2.MCPContext
		operation = prepared.ActionV2.Operation
	} else {
		currentVirployee, currentCapability, err = u.verifyCurrentExecutionEligibility(ctx, orgID, id, prepared.CapabilityKey, prepared.Action)
		if err == nil {
			err = u.verifyCurrentProfessionalActionScope(ctx, orgID, id, currentVirployee.JobRoleID, prepared.CapabilityKey, prepared.Action)
		}
		assistContext = prepared.Action.AssistContext
		mcpContext = prepared.Action.MCPContext
	}
	if err != nil {
		return runtraces.Trace{}, err
	}
	if err := u.verifyPreparedAssistContext(ctx, orgID, id, assistContext); err != nil {
		return runtraces.Trace{}, err
	}
	if mcpContext != nil {
		if u.mcpContext == nil {
			return runtraces.Trace{}, domainerr.Conflict("MCP execution context validator is unavailable")
		}
		if err := u.mcpContext.ValidateMCPExecutionContext(ctx, *mcpContext); err != nil {
			return runtraces.Trace{}, err
		}
	}
	var currentAuthority executiongate.AuthorityCheckResult
	if isV2 {
		currentAuthority, err = u.verifyCurrentAuthorityV2(ctx, orgID, id, currentCapability, *prepared.ActionV2, prepared.AuthorityBindingHash)
	} else {
		currentAuthority, err = u.verifyCurrentAuthority(ctx, orgID, id, currentCapability, prepared.Action, prepared.AuthorityBindingHash)
	}
	if err != nil {
		return runtraces.Trace{}, err
	}
	if strings.TrimSpace(approval.PolicySnapshotHash) != "" {
		if u.governanceRevalidator == nil {
			return runtraces.Trace{}, domainerr.Conflict("governance revalidation is unavailable")
		}
		revalidation, err := u.governanceRevalidator.Revalidate(ctx, executiongate.GovernanceRevalidationInput{
			OrgID: orgID, CheckID: prepared.GovernanceCheckID.String(), BindingHash: prepared.BindingHash,
			PolicySnapshotHash: approval.PolicySnapshotHash, AuthorityBindingHash: currentAuthority.SnapshotHash,
			ScopeRevision: currentAuthority.ScopeRevision, PolicyRevisionHash: currentAuthority.PolicyRevisionHash,
			DelegationID: currentAuthority.DelegationID, DelegationRevision: currentAuthority.DelegationRevision,
		})
		if err != nil {
			return runtraces.Trace{}, domainerr.Conflict("governance authority could not be revalidated")
		}
		if !revalidation.Valid {
			return runtraces.Trace{}, domainerr.Conflict("governance authority changed after approval")
		}
	}
	var execute func(context.Context, string, uuid.UUID, ExecutionAttempt) (ExecutionOutcome, error)
	if isV2 {
		executor := u.executorBindings[prepared.ActionV2.ExecutorBindingID]
		if executor == nil {
			return runtraces.Trace{}, domainerr.Conflict("executor binding is not configured for prepared action")
		}
		execute = func(execCtx context.Context, execOrgID string, virployeeID uuid.UUID, attempt ExecutionAttempt) (ExecutionOutcome, error) {
			return executor.ExecuteV2(execCtx, execOrgID, virployeeID, attempt, *prepared.ActionV2)
		}
	} else {
		executor := u.executors[prepared.Action.Action]
		if executor == nil {
			return runtraces.Trace{}, domainerr.Conflict("executor is not configured for legacy prepared action")
		}
		execute = func(execCtx context.Context, execOrgID string, virployeeID uuid.UUID, attempt ExecutionAttempt) (ExecutionOutcome, error) {
			return executor.Execute(execCtx, execOrgID, virployeeID, attempt, prepared.Action)
		}
	}
	idempotencyKey := runtraces.HashString(fmt.Sprintf("%s:%s:%s:%s", orgID, approvalID, prepared.BindingHash, operation))
	if err := u.consumeQuota(ctx, quotaKey(orgID, "axis", "executors"), idempotencyKey, "prepared_action", prepared.ID.String(), 1); err != nil {
		return runtraces.Trace{}, err
	}
	attempt, created, err := u.executionRepo.BeginExecution(ctx, orgID, id, prepared.ID, idempotencyKey)
	if err != nil {
		return runtraces.Trace{}, err
	}
	if !created && attempt.Status == "running" {
		return runtraces.Trace{}, domainerr.Conflict("execution is already running")
	}
	if created {
		started := time.Now()
		outcome, executeErr := execute(ctx, orgID, id, attempt)
		durationMS := time.Since(started).Milliseconds()
		status := "succeeded"
		errorMessage := ""
		resourceID := outcome.ResourceID
		result := outcome.Result
		if result == nil {
			result = map[string]any{}
		}
		if outcome.Mode != "" {
			result["mode"] = outcome.Mode
		}
		// Persist the effect flag with the attempt so the trace is accurate on
		// idempotent re-entry (where the executor is not called again).
		result["external_effects"] = outcome.ExternalEffects
		if executeErr != nil {
			status = "failed"
			errorMessage = runtraces.RedactText(executeErr.Error())
		}
		attempt, err = u.executionRepo.CompleteExecution(ctx, orgID, attempt.ID, status, resourceID, result, errorMessage, durationMS)
		if err != nil {
			return runtraces.Trace{}, err
		}
		// Record the real execution in the tamper-evident ledger (best-effort:
		// emitted once, on the created attempt, so idempotent re-entry does not
		// duplicate it). Keyed by binding_hash — no external payload/PII.
		u.emitExecutionAudit(ctx, orgID, id, prepared.BindingHash, operation, prepared.GovernanceCheckID.String(), attempt)
	}
	// Governance delivery is asynchronous. CompleteExecution atomically creates
	// the outbox message; NexusReportStatus is the backwards-compatible projection.
	reportStatus := attempt.GovernanceReportStatus
	if reportStatus == "" {
		reportStatus = attempt.NexusReportStatus
	}
	if existing, err := u.executionRepo.FindExecutionTraceByApproval(ctx, orgID, id, approvalID.String()); err == nil {
		existing.ExecutionResult.GovernanceReportStatus = reportStatus
		existing.ExecutionResult.NexusReportStatus = reportStatus
		return existing, nil
	} else if !domainerr.IsNotFound(err) {
		return runtraces.Trace{}, err
	}
	source, err := u.repo.FindExecutionGateTraceByApproval(ctx, orgID, id, approvalID.String())
	if err != nil {
		return runtraces.Trace{}, err
	}
	governanceResult := source.GovernanceResult
	if governanceResult == nil {
		governanceResult = source.NexusResult
	}
	if governanceResult != nil {
		copy := *governanceResult
		copy.ApprovalStatus = approval.Status
		governanceResult = &copy
	}
	mode := resultString(attempt.Result, "mode", "local")
	externalEffects := resultBool(attempt.Result, "external_effects")
	message := "Governed action executed."
	if !isV2 {
		message = "Local calendar event created."
	}
	if attempt.Status == "failed" {
		message = attempt.Error
	}
	return u.repo.CreateRunTrace(ctx, orgID, runtraces.CreateInput{
		VirployeeID: id, Operation: runtraces.OperationExecution, Input: source.InputPreview,
		InputHash: source.InputHash, InputPreview: source.InputPreview, Intent: source.Intent,
		CapabilityID: source.CapabilityID, CapabilityKey: source.CapabilityKey,
		DryRunDecision: source.DryRunDecision, GateDecision: "pass", GateChecks: source.GateChecks,
		GovernanceResult: governanceResult, NexusResult: governanceResult, BindingHash: prepared.BindingHash,
		MemoryReferences: source.MemoryReferences, MemoryContextHash: source.MemoryContextHash,
		ExecutionResult: &runtraces.ExecutionResult{
			Status: attempt.Status, Mode: mode, ApprovalID: approval.ID, ApprovalStatus: approval.Status,
			BindingHash: prepared.BindingHash, Message: message, ExternalEffects: externalEffects,
			ResourceID: attempt.ResourceID, DurationMS: attempt.DurationMS,
			GovernanceReportStatus: reportStatus, NexusReportStatus: reportStatus,
		},
	})
}

// emitExecutionAudit records a governed execution in the tamper-evident ledger
// (Nexus). Best-effort: an audit sink failure never fails the execution. Keyed
// by binding_hash so it chains alongside the virployee's other events.
func (u *UseCases) emitExecutionAudit(ctx context.Context, orgID string, virployeeID uuid.UUID, bindingHash, capabilityKey, governanceCheckID string, attempt ExecutionAttempt) {
	if u.auditEmitter == nil {
		return
	}
	eventType, summary := "execution_succeeded", "governed action executed"
	if attempt.Status != "succeeded" {
		eventType, summary = "execution_failed", "governed action failed"
	}
	data := map[string]any{
		"binding_hash":        bindingHash,
		"capability_key":      capabilityKey,
		"governance_check_id": governanceCheckID,
		"status":              attempt.Status,
		"duration_ms":         attempt.DurationMS,
	}
	if v, ok := attempt.Result["external_effects"]; ok {
		data["external_effects"] = v
	}
	if attempt.ResourceID != "" {
		data["resource_id"] = attempt.ResourceID
	}
	if err := u.auditEmitter.AppendAuditEvent(ctx, AuditEventInput{
		OrgID:       orgID,
		VirployeeID: virployeeID.String(),
		ActorType:   "virployee",
		ActorID:     virployeeID.String(),
		SubjectType: "binding",
		SubjectID:   bindingHash,
		EventType:   eventType,
		Summary:     summary,
		Data:        data,
	}); err != nil {
		slog.ErrorContext(ctx, "audit emit failed for execution",
			"error", err, "org_id", orgID, "virployee_id", virployeeID.String(), "binding_hash", bindingHash)
	}
}
