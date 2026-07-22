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
	Interval            time.Duration
	StaleAssistAfter    time.Duration
	StaleExecutionAfter time.Duration
	Lease               time.Duration
	ReportBackoff       time.Duration
	BatchSize           int
	MaxRecoveryAttempts int
	MaxReportAttempts   int
	WorkerID            string
}

func (u *UseCases) RunOperationalWatchers(ctx context.Context, config WatcherConfig) {
	if config.Interval <= 0 {
		return
	}
	run := func() {
		if err := u.RunOperationalWatchersOnce(ctx, config); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "operational watcher failed", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (u *UseCases) RunOperationalWatchersOnce(ctx context.Context, config WatcherConfig) error {
	repo, ok := u.executionRepo.(OperationalRepositoryPort)
	if !ok {
		return fmt.Errorf("operational watcher repository is not configured")
	}
	if config.BatchSize <= 0 || config.MaxRecoveryAttempts <= 0 || config.MaxReportAttempts <= 0 {
		return fmt.Errorf("operational watcher limits must be positive")
	}
	if config.StaleAssistAfter <= 0 || config.StaleExecutionAfter <= 0 || config.Lease <= 0 || config.ReportBackoff <= 0 {
		return fmt.Errorf("operational watcher durations must be positive")
	}
	now := time.Now().UTC()
	assists, err := repo.FailStaleAssistRuns(ctx, now.Add(-config.StaleAssistAfter), config.BatchSize)
	if err != nil {
		return fmt.Errorf("expire stale assists: %w", err)
	}
	for _, item := range assists {
		u.emitWatcherAudit(ctx, item.TenantID, item.VirployeeID.String(), "assist_run", item.ID.String(), "assist_timed_out", "stale assist run finalized", map[string]any{
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
	reports, err := repo.ClaimDueNexusReports(ctx, now.Add(-config.StaleExecutionAfter), config.BatchSize, config.WorkerID, config.Lease, config.MaxReportAttempts)
	if err != nil {
		return fmt.Errorf("claim nexus reports: %w", err)
	}
	for _, work := range reports {
		u.retryNexusReport(ctx, repo, work, config)
	}
	return nil
}

func (u *UseCases) recoverExecution(ctx context.Context, repo OperationalRepositoryPort, work ExecutionWork, config WatcherConfig) {
	attempt, prepared := work.Attempt, work.Action
	fail := func(err error) {
		if attempt.RecoveryAttempts+1 >= config.MaxRecoveryAttempts {
			message := "execution recovery exhausted"
			failed, completeErr := u.executionRepo.CompleteExecution(ctx, attempt.TenantID, attempt.ID, "failed", "", map[string]any{"watcher": "recovery_exhausted"}, message, time.Since(attempt.StartedAt).Milliseconds())
			if completeErr == nil {
				u.emitExecutionAudit(ctx, attempt.TenantID, attempt.VirployeeID, prepared.BindingHash, prepared.Action.Action, prepared.GovernanceCheckID.String(), failed)
				return
			}
		}
		_ = repo.ReleaseExecutionRecovery(ctx, attempt.TenantID, attempt.ID, runtraces.RedactText(err.Error()))
	}
	if u.approvals == nil {
		fail(fmt.Errorf("approval reader is not configured"))
		return
	}
	approval, err := u.approvals.GetApproval(ctx, attempt.TenantID, prepared.ApprovalID)
	if err != nil {
		fail(err)
		return
	}
	if strings.TrimSpace(approval.Status) != "approved" || approval.RequesterID != attempt.VirployeeID.String() || approval.BindingHash != prepared.BindingHash || approval.GovernanceCheckID != prepared.GovernanceCheckID.String() {
		fail(fmt.Errorf("approval is no longer executable"))
		return
	}
	executor := u.executors[prepared.Action.Action]
	if executor == nil {
		fail(fmt.Errorf("executor is not configured"))
		return
	}
	started := time.Now()
	outcome, executeErr := executor.Execute(ctx, attempt.TenantID, attempt.VirployeeID, attempt, prepared.Action)
	result := outcome.Result
	if result == nil {
		result = map[string]any{}
	}
	result["mode"], result["external_effects"] = outcome.Mode, outcome.ExternalEffects
	status, message := "succeeded", ""
	if executeErr != nil {
		status, message = "failed", runtraces.RedactText(executeErr.Error())
	}
	completed, err := u.executionRepo.CompleteExecution(ctx, attempt.TenantID, attempt.ID, status, outcome.ResourceID, result, message, time.Since(started).Milliseconds())
	if err != nil {
		fail(err)
		return
	}
	u.emitExecutionAudit(ctx, attempt.TenantID, attempt.VirployeeID, prepared.BindingHash, prepared.Action.Action, prepared.GovernanceCheckID.String(), completed)
	u.emitWatcherAudit(ctx, attempt.TenantID, attempt.VirployeeID.String(), "execution_attempt", attempt.ID.String(), "execution_recovered", "stale execution recovered idempotently", map[string]any{
		"binding_hash": prepared.BindingHash, "status": completed.Status,
	})
}

func (u *UseCases) retryNexusReport(ctx context.Context, repo OperationalRepositoryPort, work ExecutionWork, config WatcherConfig) {
	attempt, prepared := work.Attempt, work.Action
	currentAttempt := attempt.ReportAttempts + 1
	status, next, watcherError := "reported", time.Now().UTC(), ""
	if u.resultReporter == nil {
		status, watcherError = "failed", "result reporter is not configured"
	} else if err := u.resultReporter.ReportExecutionResult(ctx, attempt.TenantID, prepared.GovernanceCheckID.String(), attempt.IdempotencyKey, prepared.BindingHash, attempt.Status, attempt.DurationMS, attempt.Result); err != nil {
		status, watcherError = "failed", runtraces.RedactText(err.Error())
	}
	if status == "failed" {
		if currentAttempt >= config.MaxReportAttempts {
			status = "dead"
		} else {
			next = next.Add(exponentialBackoff(config.ReportBackoff, currentAttempt-1))
		}
	}
	if err := repo.RecordNexusReportAttempt(ctx, attempt.TenantID, attempt.ID, status, next, watcherError); err != nil {
		slog.ErrorContext(ctx, "persist nexus report retry failed", "error", err, "execution_attempt_id", attempt.ID)
		return
	}
	eventType, summary := "nexus_report_retried", "execution result reported to Nexus"
	if status != "reported" {
		eventType, summary = "nexus_report_retry_failed", "execution result report retry failed"
	}
	u.emitWatcherAudit(ctx, attempt.TenantID, attempt.VirployeeID.String(), "execution_attempt", attempt.ID.String(), eventType, summary, map[string]any{
		"binding_hash": prepared.BindingHash, "report_status": status, "attempt": currentAttempt,
	})
}

func exponentialBackoff(base time.Duration, exponent int) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	for i := 0; i < exponent && i < 10; i++ {
		base *= 2
	}
	return base
}

func (u *UseCases) emitWatcherAudit(ctx context.Context, tenantID, virployeeID, subjectType, subjectID, eventType, summary string, data map[string]any) {
	if u.auditEmitter == nil {
		return
	}
	if err := u.auditEmitter.AppendAuditEvent(ctx, AuditEventInput{TenantID: tenantID, VirployeeID: virployeeID, ActorType: "service", ActorID: "companion-watcher", SubjectType: subjectType, SubjectID: subjectID, EventType: eventType, Summary: summary, Data: data}); err != nil {
		slog.ErrorContext(ctx, "watcher audit emit failed", "error", err, "subject_id", subjectID)
	}
}
