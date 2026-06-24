package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

type SetExecutionPlanInput struct {
	ConnectorID    uuid.UUID
	Operation      string
	Payload        json.RawMessage
	IdempotencyKey string
}

func (u *Usecases) SetExecutionPlan(ctx context.Context, taskID uuid.UUID, in SetExecutionPlanInput) (domain.TaskExecutionPlan, error) {
	if in.ConnectorID == uuid.Nil {
		return domain.TaskExecutionPlan{}, fmt.Errorf("connector_id is required")
	}
	if in.Operation == "" {
		return domain.TaskExecutionPlan{}, fmt.Errorf("operation is required")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.TaskExecutionPlan{}, err
	}
	switch t.Status {
	case domain.TaskStatusDone, domain.TaskStatusFailed, domain.TaskStatusExecuting, domain.TaskStatusVerifying:
		return domain.TaskExecutionPlan{}, ErrInvalidTaskState
	}

	if u.executor != nil {
		if _, err := u.executor.GetConnector(ctx, in.ConnectorID); err != nil {
			return domain.TaskExecutionPlan{}, fmt.Errorf("get connector: %w", err)
		}
	}

	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage(`{}`)
	}

	var prevPlan *domain.TaskExecutionPlan
	currentPlan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		currentCopy := currentPlan
		prevPlan = &currentCopy
	} else if !domainerr.IsNotFound(err) {
		return domain.TaskExecutionPlan{}, err
	}

	plan, err := u.repo.UpsertExecutionPlan(ctx, domain.TaskExecutionPlan{
		TaskID:         taskID,
		ConnectorID:    in.ConnectorID,
		Operation:      in.Operation,
		Payload:        in.Payload,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return domain.TaskExecutionPlan{}, err
	}

	if executionPlanChanged(prevPlan, plan) {
		payload := marshalOrEmpty("execution_plan_action", map[string]any{
			"connector_id":    plan.ConnectorID.String(),
			"operation":       plan.Operation,
			"payload":         json.RawMessage(plan.Payload),
			"idempotency_key": plan.IdempotencyKey,
		})
		if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
			TaskID:     taskID,
			ActionType: TaskActionSetExecutionPlan,
			Payload:    payload,
		}); insertErr != nil {
			slog.Warn("companion set execution plan action failed", "task_id", taskID.String(), "error", insertErr)
		}
	}
	u.syncTaskMemory(ctx, taskID, "set_execution_plan")

	return plan, nil
}

type ExecuteTaskOutput struct {
	Task           domain.Task
	Plan           domain.TaskExecutionPlan
	Execution      connectordomain.ExecutionResult
	ExecutionState domain.TaskExecutionState
}

func buildConnectorExecutionPayload(result connectordomain.ExecutionResult) json.RawMessage {
	return marshalOrEmpty("connector_execution_payload", map[string]any{
		"id":              result.ID.String(),
		"connector_id":    result.ConnectorID.String(),
		"org_id":          result.OrgID,
		"actor_id":        result.ActorID,
		"operation":       result.Operation,
		"status":          result.Status,
		"external_ref":    result.ExternalRef,
		"payload":         json.RawMessage(result.Payload),
		"result":          json.RawMessage(result.ResultJSON),
		"evidence":        json.RawMessage(result.EvidenceJSON),
		"error_message":   result.ErrorMessage,
		"retryable":       result.Retryable,
		"duration_ms":     result.DurationMS,
		"idempotency_key": result.IdempotencyKey,
		"nexus_request_id": func() string {
			if result.NexusRequestID != nil {
				return result.NexusRequestID.String()
			}
			return ""
		}(),
	})
}

func buildVerificationPayload(result connectordomain.ExecutionResult, verification domain.TaskVerificationResult) json.RawMessage {
	return marshalOrEmpty("verification_payload", map[string]any{
		"execution_id":        result.ID.String(),
		"execution_status":    result.Status,
		"verification_status": verification.Status,
		"summary":             verification.Summary,
		"checked_at":          verification.CheckedAt,
		"details":             json.RawMessage(verification.Details),
		"retryable":           result.Retryable,
	})
}

func hasResultPayload(result json.RawMessage) bool {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 {
		return false
	}
	switch string(trimmed) {
	case "{}", "null", "[]":
		return false
	default:
		return true
	}
}

func hasVerificationEvidence(result connectordomain.ExecutionResult) bool {
	if strings.TrimSpace(result.ExternalRef) != "" {
		return true
	}
	return hasResultPayload(result.ResultJSON)
}

func verifyExecutionResult(result connectordomain.ExecutionResult) domain.TaskVerificationResult {
	checkedAt := time.Now().UTC()
	details := marshalOrEmpty("verification_details", map[string]any{
		"execution_status":       result.Status,
		"external_ref_present":   strings.TrimSpace(result.ExternalRef) != "",
		"result_payload_present": hasResultPayload(result.ResultJSON),
		"retryable":              result.Retryable,
		"error_message":          result.ErrorMessage,
	})

	switch result.Status {
	case connectordomain.ExecSuccess:
		if hasVerificationEvidence(result) {
			return domain.TaskVerificationResult{
				Status:    domain.VerificationStatusVerified,
				Summary:   "connector execution verified from returned evidence",
				CheckedAt: checkedAt,
				Details:   details,
			}
		}
		return domain.TaskVerificationResult{
			Status:    domain.VerificationStatusFailed,
			Summary:   "verification failed: connector returned no evidence",
			CheckedAt: checkedAt,
			Details:   details,
		}
	default:
		summary := "execution failed before verification"
		if result.ErrorMessage != "" {
			summary = result.ErrorMessage
		}
		return domain.TaskVerificationResult{
			Status:    domain.VerificationStatusFailed,
			Summary:   summary,
			CheckedAt: checkedAt,
			Details:   details,
		}
	}
}

func buildExecutionState(prev *domain.TaskExecutionState, taskID uuid.UUID, result connectordomain.ExecutionResult, verification domain.TaskVerificationResult, isRetry bool) domain.TaskExecutionState {
	retryCount := 0
	createdAt := time.Now().UTC()
	if prev != nil {
		retryCount = prev.RetryCount
		createdAt = prev.CreatedAt
	}
	if isRetry {
		retryCount++
	}
	lastError := result.ErrorMessage
	if lastError == "" && verification.Status == domain.VerificationStatusFailed {
		lastError = verification.Summary
	}
	retryable := result.Retryable
	if verification.Status == domain.VerificationStatusFailed {
		retryable = true
	}
	if verification.Status == domain.VerificationStatusVerified {
		retryable = false
		lastError = ""
	}
	return domain.TaskExecutionState{
		TaskID:              taskID,
		LastExecutionID:     result.ID,
		LastExecutionStatus: result.Status,
		Retryable:           retryable,
		RetryCount:          retryCount,
		LastError:           lastError,
		LastAttemptedAt:     result.CreatedAt,
		VerificationResult:  verification,
		CreatedAt:           createdAt,
	}
}

func defaultExecutionIdempotencyKey(taskID uuid.UUID, nexusRequestID *uuid.UUID) string {
	return fmt.Sprintf("task-execute-%s", taskID.String())
}

func stringFromBinding(binding map[string]any, key, defaultValue string) string {
	if value := strings.TrimSpace(fmt.Sprint(binding[key])); value != "" && value != "<nil>" {
		return value
	}
	return defaultValue
}

func executionActorID(t domain.Task) string {
	if actor := strings.TrimSpace(t.AssignedTo); actor != "" {
		return actor
	}
	if actor := strings.TrimSpace(t.CreatedBy); actor != "" {
		return actor
	}
	return CompanionRequesterID
}

func executionActorType(t domain.Task) string {
	if executionActorID(t) == CompanionRequesterID {
		return "agent"
	}
	return "human"
}

func executionOnBehalfOf(t domain.Task) string {
	if actor := strings.TrimSpace(t.CreatedBy); actor != "" && actor != CompanionRequesterID {
		return actor
	}
	return ""
}

func (u *Usecases) refreshNexusSnapshot(ctx context.Context, taskID uuid.UUID, origin string) (*domain.TaskNexusSyncState, error) {
	var prevState *domain.TaskNexusSyncState
	currentState, err := u.repo.GetNexusSyncState(ctx, taskID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return nil, err
	}

	nexusRequestID, err := u.latestNexusRequestIDForTask(ctx, taskID, prevState)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskNexusSyncState{
		TaskID:         taskID,
		NexusRequestID: nexusRequestID,
		LastCheckedAt:  now,
		NextCheckAt:    nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0),
	}
	if prevState != nil {
		nextState.CreatedAt = prevState.CreatedAt
		nextState.LastNexusStatus = prevState.LastNexusStatus
		nextState.LastNexusHTTPStatus = prevState.LastNexusHTTPStatus
		nextState.LastError = prevState.LastError
		nextState.ConsecutiveFailures = prevState.ConsecutiveFailures
	}

	sum, statusCode, getErr := u.nexus.GetRequest(ctx, nexusRequestID.String())
	if getErr != nil {
		nextState.LastNexusHTTPStatus = statusCode
		nextState.LastError = getErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upsertErr := u.repo.UpsertNexusSyncState(ctx, nextState)
		if upsertErr != nil {
			return nil, upsertErr
		}
		return &stateOut, fmt.Errorf("nexus get request: %w", getErr)
	}

	nextState.LastNexusHTTPStatus = statusCode
	nextState.LastNexusStatus = normalizeNexusStatus(sum.Status)
	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0)

	stateOut, upsertErr := u.repo.UpsertNexusSyncState(ctx, nextState)
	if upsertErr != nil {
		return nil, upsertErr
	}
	if nexusSnapshotChanged(prevState, stateOut) {
		u.persistNexusSyncAction(ctx, taskID, nexusRequestID, origin, prevState, stateOut, "", "", "")
	}
	return &stateOut, nil
}

func (u *Usecases) runTaskExecution(ctx context.Context, t domain.Task, plan domain.TaskExecutionPlan, prevState *domain.TaskExecutionState, startEvent string) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput

	t, err := u.applyTaskEvent(ctx, t, startEvent)
	if err != nil {
		return out, err
	}

	var nexusRequestID *uuid.UUID
	if syncState, syncErr := u.repo.GetNexusSyncState(ctx, t.ID); syncErr == nil && syncState.NexusRequestID != uuid.Nil {
		nexusRequestID = &syncState.NexusRequestID
	}
	idempotencyKey := plan.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = defaultExecutionIdempotencyKey(t.ID, nexusRequestID)
	}

	result, execErr := u.executor.Execute(ctx, connectordomain.ExecutionSpec{
		ConnectorID:        plan.ConnectorID,
		OrgID:              t.OrgID,
		ActorID:            executionActorID(t),
		ActorType:          executionActorType(t),
		CompanionPrincipal: CompanionRequesterID,
		OnBehalfOf:         executionOnBehalfOf(t),
		ServicePrincipal:   true,
		ProductSurface:     "companion",
		Operation:          plan.Operation,
		Payload:            plan.Payload,
		IdempotencyKey:     idempotencyKey,
		TaskID:             &t.ID,
		NexusRequestID:     nexusRequestID,
	})
	if execErr != nil {
		result = connectordomain.ExecutionResult{
			ID:             uuid.New(),
			ConnectorID:    plan.ConnectorID,
			OrgID:          t.OrgID,
			ActorID:        executionActorID(t),
			Operation:      plan.Operation,
			Status:         connectordomain.ExecFailure,
			Payload:        plan.Payload,
			ResultJSON:     json.RawMessage(`{}`),
			ErrorMessage:   execErr.Error(),
			Retryable:      true,
			IdempotencyKey: idempotencyKey,
			TaskID:         &t.ID,
			NexusRequestID: nexusRequestID,
			CreatedAt:      time.Now().UTC(),
		}
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	u.reportExecutionToNexus(ctx, nexusRequestID, result)

	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         t.ID,
		ActionType:     TaskActionExecuteConnector,
		Payload:        buildConnectorExecutionPayload(result),
		NexusRequestID: nexusRequestID,
		ErrorMessage:   result.ErrorMessage,
	}); insertErr != nil {
		slog.Warn("companion execute connector action failed", "task_id", t.ID.String(), "error", insertErr)
	}

	artifactKind := TaskArtifactConnectorExecution
	if result.Status != connectordomain.ExecSuccess {
		artifactKind = TaskArtifactExecutionError
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  t.ID,
		Kind:    artifactKind,
		URI:     result.ExternalRef,
		Payload: buildConnectorExecutionPayload(result),
	}); artifactErr != nil {
		slog.Warn("companion execute connector artifact failed", "task_id", t.ID.String(), "error", artifactErr)
	}

	verification := verifyExecutionResult(result)
	if _, verifyErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         t.ID,
		ActionType:     TaskActionVerifyExecution,
		Payload:        buildVerificationPayload(result, verification),
		NexusRequestID: nexusRequestID,
		ErrorMessage: func() string {
			if verification.Status == domain.VerificationStatusFailed {
				return verification.Summary
			}
			return ""
		}(),
	}); verifyErr != nil {
		slog.Warn("companion verify execution action failed", "task_id", t.ID.String(), "error", verifyErr)
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  t.ID,
		Kind:    TaskArtifactExecutionVerification,
		URI:     result.ExternalRef,
		Payload: buildVerificationPayload(result, verification),
	}); artifactErr != nil {
		slog.Warn("companion verify execution artifact failed", "task_id", t.ID.String(), "error", artifactErr)
	}

	executionState, stateErr := u.repo.UpsertExecutionState(ctx, buildExecutionState(prevState, t.ID, result, verification, startEvent == evRetryExecution))
	if stateErr != nil {
		return out, stateErr
	}

	switch {
	case result.Status == connectordomain.ExecSuccess && verification.Status == domain.VerificationStatusVerified:
		t, err = u.applyTaskEvent(ctx, t, evExecutionSucceeded)
		if err != nil {
			return out, err
		}
		t, err = u.applyTaskEvent(ctx, t, evExecutionVerified)
		if err != nil {
			return out, err
		}
	case result.Status == connectordomain.ExecSuccess && verification.Status == domain.VerificationStatusFailed:
		t, err = u.applyTaskEvent(ctx, t, evExecutionSucceeded)
		if err != nil {
			return out, err
		}
		t, err = u.applyTaskEvent(ctx, t, evExecutionFailed)
		if err != nil {
			return out, err
		}
	default:
		t, err = u.applyTaskEvent(ctx, t, evExecutionFailed)
		if err != nil {
			return out, err
		}
	}

	t.NexusStatus = normalizeNexusStatus(t.NexusStatus)
	out.Task = t
	out.Plan = plan
	out.Execution = result
	out.ExecutionState = executionState
	u.syncTaskMemory(ctx, t.ID, "execution")
	return out, nil
}

func (u *Usecases) reportExecutionToNexus(ctx context.Context, nexusRequestID *uuid.UUID, result connectordomain.ExecutionResult) {
	if u.nexus == nil || nexusRequestID == nil || *nexusRequestID == uuid.Nil {
		return
	}
	success := result.Status == connectordomain.ExecSuccess
	var resultPayload map[string]any
	if len(result.ResultJSON) > 0 {
		if err := json.Unmarshal(result.ResultJSON, &resultPayload); err != nil {
			resultPayload = map[string]any{"raw": string(result.ResultJSON)}
		}
	}
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	resultPayload["connector_execution_id"] = result.ID.String()
	resultPayload["connector_id"] = result.ConnectorID.String()
	resultPayload["operation"] = result.Operation
	resultPayload["external_ref"] = result.ExternalRef
	resultPayload["org_id"] = result.OrgID
	resultPayload["actor_id"] = result.ActorID
	if len(result.EvidenceJSON) > 0 {
		resultPayload["evidence"] = json.RawMessage(result.EvidenceJSON)
	}
	status, err := u.nexus.ReportResult(ctx, nexusRequestID.String(), success, resultPayload, result.DurationMS, result.ErrorMessage)
	if err != nil || status >= http.StatusBadRequest {
		slog.Warn("report execution to nexus failed",
			"nexus_request_id", nexusRequestID.String(),
			"status", status,
			"error", err)
	}
}

func (u *Usecases) ExecuteTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, fmt.Errorf("execution plan is required")
		}
		return out, err
	}

	var nexusRequestID string
	if t.Status == domain.TaskStatusWaitingForApproval {
		syncedTask, state, syncErr := u.syncTaskWithNexus(ctx, t, "execute")
		if state != nil {
			syncedTask.NexusStatus = state.LastNexusStatus
			syncedTask.NexusLastCheckedAt = &state.LastCheckedAt
			syncedTask.NexusSyncError = state.LastError
			nexusRequestID = state.NexusRequestID.String()
		}
		if syncErr != nil {
			return out, syncErr
		}
		t = syncedTask
	}

	if !isApprovedNexusStatus(t.NexusStatus) {
		return out, u.nexusBlockedError(nexusRequestID, t.NexusStatus, "execute")
	}
	if t.Status != domain.TaskStatusWaitingForInput {
		return out, ErrInvalidTaskState
	}

	prevState, stateErr := u.getExecutionState(ctx, taskID)
	if stateErr != nil {
		return out, stateErr
	}
	return u.runTaskExecution(ctx, t, plan, prevState, evStartExecution)
}

func (u *Usecases) RetryTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, fmt.Errorf("execution plan is required")
		}
		return out, err
	}
	state, err := u.repo.GetExecutionState(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, ErrInvalidTaskState
		}
		return out, err
	}
	if t.Status != domain.TaskStatusFailed || !state.Retryable {
		return out, ErrInvalidTaskState
	}

	snapshot, snapshotErr := u.refreshNexusSnapshot(ctx, taskID, "retry")
	if snapshotErr != nil {
		return out, snapshotErr
	}
	t.NexusStatus = snapshot.LastNexusStatus
	t.NexusLastCheckedAt = &snapshot.LastCheckedAt
	t.NexusSyncError = snapshot.LastError
	if !isApprovedNexusStatus(snapshot.LastNexusStatus) {
		return out, u.nexusBlockedError(snapshot.NexusRequestID.String(), snapshot.LastNexusStatus, "retry")
	}

	payload := marshalOrEmpty("retry_execution_action", map[string]any{
		"retry_count_before":    state.RetryCount,
		"last_execution_status": state.LastExecutionStatus,
		"last_error":            state.LastError,
	})
	nexusRequestID := snapshot.NexusRequestID
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionRetryExecution,
		Payload:        payload,
		NexusRequestID: &nexusRequestID,
	}); insertErr != nil {
		slog.Warn("companion retry execution action failed", "task_id", taskID.String(), "error", insertErr)
	}

	return u.runTaskExecution(ctx, t, plan, &state, evRetryExecution)
}
