package virployees

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
)

type WatcherConfig struct {
	StaleAssistAfter    time.Duration
	StaleExecutionAfter time.Duration
	Lease               time.Duration
	BatchSize           int
	MaxRecoveryAttempts int
	WorkerID            string
}

func (u *UseCases) RunOperationalWatchersOnce(ctx context.Context, config WatcherConfig) error {
	repo, ok := u.executionRepo.(OperationalRepositoryPort)
	if !ok {
		return fmt.Errorf("operational watcher repository is not configured")
	}
	if config.BatchSize <= 0 || config.MaxRecoveryAttempts <= 0 {
		return fmt.Errorf("operational watcher limits must be positive")
	}
	if config.StaleAssistAfter <= 0 || config.StaleExecutionAfter <= 0 || config.Lease <= 0 {
		return fmt.Errorf("operational watcher durations must be positive")
	}
	now := time.Now().UTC()
	assists, err := repo.ReconcileStaleAssistRuns(ctx, now.Add(-config.StaleAssistAfter), config.BatchSize, config.MaxRecoveryAttempts)
	if err != nil {
		return fmt.Errorf("expire stale assists: %w", err)
	}
	for _, item := range assists {
		eventType, summary := "assist_timed_out", "stale assist run finalized"
		if item.Outcome == "recovered" {
			eventType, summary = "assist_recovered", "stale pre-answer assist run queued for recovery"
		}
		u.emitWatcherAudit(ctx, item.OrgID, item.VirployeeID.String(), "assist_run", item.ID.String(), eventType, summary, map[string]any{
			"run_id": item.ID.String(), "input_hash": item.InputHash,
		})
	}
	works, err := repo.ClaimStaleExecutions(ctx, now.Add(-config.StaleExecutionAfter), config.BatchSize, config.WorkerID, config.Lease, config.MaxRecoveryAttempts)
	if err != nil {
		return fmt.Errorf("claim stale executions: %w", err)
	}
	for _, work := range works {
		u.recoverExecution(ctx, repo, work, config)
	}
	return nil
}

func (u *UseCases) recoverExecution(ctx context.Context, repo OperationalRepositoryPort, work ExecutionWork, config WatcherConfig) {
	attempt, prepared := work.Attempt, work.Action
	operation := prepared.Action.Action
	if prepared.ActionV2 != nil {
		operation = prepared.ActionV2.Operation
	}
	fail := func(errorCode string) {
		if attempt.RecoveryAttempts+1 >= config.MaxRecoveryAttempts {
			message := "execution recovery exhausted"
			failed, completeErr := u.executionRepo.CompleteExecution(ctx, attempt.OrgID, attempt.ID, "failed", "", map[string]any{"watcher": "recovery_exhausted"}, message, time.Since(attempt.StartedAt).Milliseconds())
			if completeErr == nil {
				u.emitExecutionAudit(ctx, attempt.OrgID, attempt.VirployeeID, prepared.BindingHash, operation, prepared.GovernanceCheckID.String(), failed)
				return
			}
		}
		_ = repo.ReleaseExecutionRecovery(ctx, attempt.OrgID, attempt.ID, errorCode)
	}
	if u.approvals == nil {
		fail("approval_reader_unconfigured")
		return
	}
	approval, err := u.approvals.GetApproval(ctx, attempt.OrgID, prepared.ApprovalID)
	if err != nil {
		fail("approval_read_failed")
		return
	}
	if strings.TrimSpace(approval.Status) != "approved" || approval.RequesterID != attempt.VirployeeID.String() || approval.BindingHash != prepared.BindingHash || approval.GovernanceCheckID != prepared.GovernanceCheckID.String() {
		fail("approval_not_executable")
		return
	}
	mcpContext := prepared.Action.MCPContext
	if prepared.ActionV2 != nil {
		mcpContext = prepared.ActionV2.MCPContext
	}
	if mcpContext != nil {
		if u.mcpContext == nil || u.mcpContext.ValidateMCPExecutionContext(ctx, *mcpContext) != nil {
			fail("mcp_context_revalidation_failed")
			return
		}
	}
	var execute func(context.Context) (ExecutionOutcome, error)
	if prepared.ActionV2 != nil {
		executor := u.executorBindings[prepared.ActionV2.ExecutorBindingID]
		if executor != nil {
			execute = func(execCtx context.Context) (ExecutionOutcome, error) {
				return executor.ExecuteV2(execCtx, attempt.OrgID, attempt.VirployeeID, attempt, *prepared.ActionV2)
			}
		}
	} else {
		executor := u.executors[prepared.Action.Action]
		if executor != nil {
			execute = func(execCtx context.Context) (ExecutionOutcome, error) {
				return executor.Execute(execCtx, attempt.OrgID, attempt.VirployeeID, attempt, prepared.Action)
			}
		}
	}
	if execute == nil {
		fail("executor_unconfigured")
		return
	}
	if err := u.consumeQuota(ctx, quotaKey(attempt.OrgID, "axis", "executors"), attempt.IdempotencyKey, "execution_attempt", attempt.ID.String(), 1); err != nil {
		fail("executor_quota_exceeded")
		return
	}
	started := time.Now()
	outcome, executeErr := execute(ctx)
	result := outcome.Result
	if result == nil {
		result = map[string]any{}
	}
	result["mode"], result["external_effects"] = outcome.Mode, outcome.ExternalEffects
	status, message := "succeeded", ""
	if executeErr != nil {
		status, message = "failed", runtraces.RedactText(executeErr.Error())
	}
	completed, err := u.executionRepo.CompleteExecution(ctx, attempt.OrgID, attempt.ID, status, outcome.ResourceID, result, message, time.Since(started).Milliseconds())
	if err != nil {
		fail("execution_completion_failed")
		return
	}
	u.emitExecutionAudit(ctx, attempt.OrgID, attempt.VirployeeID, prepared.BindingHash, operation, prepared.GovernanceCheckID.String(), completed)
	u.emitWatcherAudit(ctx, attempt.OrgID, attempt.VirployeeID.String(), "execution_attempt", attempt.ID.String(), "execution_recovered", "stale execution recovered idempotently", map[string]any{
		"binding_hash": prepared.BindingHash, "status": completed.Status,
	})
}

func (u *UseCases) emitWatcherAudit(ctx context.Context, orgID, virployeeID, subjectType, subjectID, eventType, summary string, data map[string]any) {
	if u.auditEmitter == nil {
		return
	}
	if err := u.auditEmitter.AppendAuditEvent(ctx, AuditEventInput{OrgID: orgID, VirployeeID: virployeeID, ActorType: "service", ActorID: "companion-watcher", SubjectType: subjectType, SubjectID: subjectID, EventType: eventType, Summary: summary, Data: data}); err != nil {
		slog.ErrorContext(ctx, "watcher audit emit failed", "error", err, "subject_id", subjectID)
	}
}
