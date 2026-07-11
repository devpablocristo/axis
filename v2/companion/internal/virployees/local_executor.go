package virployees

import (
	"context"
	"fmt"
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

func (e *LocalCalendarExecutor) Execute(ctx context.Context, tenantID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (string, map[string]any, error) {
	resourceID, err := e.repo.CreateLocalCalendarEvent(ctx, tenantID, virployeeID, attempt, action)
	if err != nil {
		return "", nil, err
	}
	return resourceID, map[string]any{
		"mode":          "local",
		"resource_id":   resourceID,
		"resource_type": "calendar_event",
	}, nil
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
	attempt, created, err := u.executionRepo.BeginExecution(ctx, tenantID, id, prepared.ID, idempotencyKey)
	if err != nil {
		return runtraces.Trace{}, err
	}
	if !created && attempt.Status == "running" {
		return runtraces.Trace{}, domainerr.Conflict("execution is already running")
	}
	if created {
		started := time.Now()
		resourceID, result, executeErr := executor.Execute(ctx, tenantID, id, attempt, prepared.Action)
		durationMS := time.Since(started).Milliseconds()
		status := "succeeded"
		errorMessage := ""
		if executeErr != nil {
			status = "failed"
			errorMessage = runtraces.RedactText(executeErr.Error())
			result = map[string]any{"mode": "local"}
		}
		attempt, err = u.executionRepo.CompleteExecution(ctx, tenantID, attempt.ID, status, resourceID, result, errorMessage, durationMS)
		if err != nil {
			return runtraces.Trace{}, err
		}
	}
	reportStatus := attempt.NexusReportStatus
	if reportStatus != "reported" && u.resultReporter != nil {
		reportErr := u.resultReporter.ReportExecutionResult(ctx, tenantID, prepared.GovernanceCheckID.String(), attempt.IdempotencyKey, prepared.BindingHash, attempt.Status, attempt.DurationMS, attempt.Result)
		reportStatus = "reported"
		if reportErr != nil {
			reportStatus = "failed"
		}
		if err := u.executionRepo.SetNexusReportStatus(ctx, tenantID, attempt.ID, reportStatus); err != nil {
			return runtraces.Trace{}, err
		}
	}
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
		ExecutionResult: &runtraces.ExecutionResult{
			Status: attempt.Status, Mode: "local", ApprovalID: approval.ID, ApprovalStatus: approval.Status,
			BindingHash: prepared.BindingHash, Message: message, ExternalEffects: false,
			ResourceID: attempt.ResourceID, DurationMS: attempt.DurationMS, NexusReportStatus: reportStatus,
		},
	})
}
