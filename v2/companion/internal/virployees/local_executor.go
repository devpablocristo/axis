package virployees

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type LocalCalendarExecutor struct {
	repo ExecutionRepositoryPort
}

func NewLocalCalendarExecutor(repo ExecutionRepositoryPort) *LocalCalendarExecutor {
	return &LocalCalendarExecutor{repo: repo}
}

func (e *LocalCalendarExecutor) Execute(ctx context.Context, tenantID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (ExecutionOutcome, error) {
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
	resourceID, err := e.repo.CreateLocalCalendarEvent(ctx, tenantID, virployeeID, attempt, action)
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

func (u *UseCases) ExecuteApprovedAction(ctx context.Context, tenantID string, id, approvalID uuid.UUID) (runtraces.Trace, error) {
	tenantID = normalizeTenantID(tenantID)
	if u.approvals == nil {
		return runtraces.Trace{}, domainerr.Conflict("approval reader is not configured")
	}
	if u.executionRepo == nil {
		return runtraces.Trace{}, domainerr.Conflict("execution repository is not configured")
	}
	if _, err := u.repo.Get(ctx, tenantID, id); err != nil {
		return runtraces.Trace{}, err
	}
	approval, err := u.approvals.GetApproval(ctx, tenantID, approvalID)
	if err != nil {
		return runtraces.Trace{}, err
	}
	if strings.TrimSpace(approval.Status) != "approved" || strings.TrimSpace(approval.RequesterID) != id.String() {
		return runtraces.Trace{}, domainerr.Conflict("approval is not executable for this virployee")
	}
	prepared, err := u.executionRepo.GetPreparedActionByApproval(ctx, tenantID, id, approvalID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtraces.Trace{}, domainerr.Conflict("approval has no durable prepared action")
		}
		return runtraces.Trace{}, err
	}
	if prepared.BindingHash != approval.BindingHash || prepared.GovernanceCheckID.String() != approval.GovernanceCheckID {
		return runtraces.Trace{}, domainerr.Conflict("approval binding does not match prepared action")
	}
	executor := u.executors[prepared.Action.Action]
	if executor == nil {
		return runtraces.Trace{}, domainerr.Conflict("executor is not configured for prepared action")
	}
	idempotencyKey := runtraces.HashString(fmt.Sprintf("%s:%s:%s:%s", tenantID, approvalID, prepared.BindingHash, prepared.Action.Action))
	if err := u.consumeQuota(ctx, quotaKey(tenantID, "axis", "executors"), idempotencyKey, "prepared_action", prepared.ID.String(), 1); err != nil {
		return runtraces.Trace{}, err
	}
	attempt, created, err := u.executionRepo.BeginExecution(ctx, tenantID, id, prepared.ID, idempotencyKey)
	if err != nil {
		return runtraces.Trace{}, err
	}
	if !created && attempt.Status == "running" {
		return runtraces.Trace{}, domainerr.Conflict("execution is already running")
	}
	if created {
		started := time.Now()
		outcome, executeErr := executor.Execute(ctx, tenantID, id, attempt, prepared.Action)
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
		attempt, err = u.executionRepo.CompleteExecution(ctx, tenantID, attempt.ID, status, resourceID, result, errorMessage, durationMS)
		if err != nil {
			return runtraces.Trace{}, err
		}
		// Record the real execution in the tamper-evident ledger (best-effort:
		// emitted once, on the created attempt, so idempotent re-entry does not
		// duplicate it). Keyed by binding_hash — no external payload/PII.
		u.emitExecutionAudit(ctx, tenantID, id, prepared.BindingHash, prepared.Action.Action, prepared.GovernanceCheckID.String(), attempt)
	}
	// Delivery to Nexus is asynchronous. CompleteExecution atomically created the
	// outbox message and NexusReportStatus is its backwards-compatible projection.
	reportStatus := attempt.NexusReportStatus
	if existing, err := u.executionRepo.FindExecutionTraceByApproval(ctx, tenantID, id, approvalID.String()); err == nil {
		existing.ExecutionResult.NexusReportStatus = reportStatus
		return existing, nil
	} else if !domainerr.IsNotFound(err) {
		return runtraces.Trace{}, err
	}
	source, err := u.repo.FindExecutionGateTraceByApproval(ctx, tenantID, id, approvalID.String())
	if err != nil {
		return runtraces.Trace{}, err
	}
	nexus := source.NexusResult
	if nexus != nil {
		copy := *nexus
		copy.ApprovalStatus = approval.Status
		nexus = &copy
	}
	mode := resultString(attempt.Result, "mode", "local")
	externalEffects := resultBool(attempt.Result, "external_effects")
	message := "Local calendar event created."
	if attempt.Status == "failed" {
		message = attempt.Error
	}
	return u.repo.CreateRunTrace(ctx, tenantID, runtraces.CreateInput{
		VirployeeID: id, Operation: runtraces.OperationExecution, Input: source.InputPreview,
		InputHash: source.InputHash, InputPreview: source.InputPreview, Intent: source.Intent,
		CapabilityID: source.CapabilityID, CapabilityKey: source.CapabilityKey,
		DryRunDecision: source.DryRunDecision, GateDecision: "pass", GateChecks: source.GateChecks,
		NexusResult: nexus, BindingHash: prepared.BindingHash,
		MemoryReferences: source.MemoryReferences, MemoryContextHash: source.MemoryContextHash,
		ExecutionResult: &runtraces.ExecutionResult{
			Status: attempt.Status, Mode: mode, ApprovalID: approval.ID, ApprovalStatus: approval.Status,
			BindingHash: prepared.BindingHash, Message: message, ExternalEffects: externalEffects,
			ResourceID: attempt.ResourceID, DurationMS: attempt.DurationMS, NexusReportStatus: reportStatus,
		},
	})
}

// emitExecutionAudit records a governed execution in the tamper-evident ledger
// (Nexus). Best-effort: an audit sink failure never fails the execution. Keyed
// by binding_hash so it chains alongside the virployee's other events.
func (u *UseCases) emitExecutionAudit(ctx context.Context, tenantID string, virployeeID uuid.UUID, bindingHash, capabilityKey, governanceCheckID string, attempt ExecutionAttempt) {
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
		TenantID:    tenantID,
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
			"error", err, "tenant_id", tenantID, "virployee_id", virployeeID.String(), "binding_hash", bindingHash)
	}
}
