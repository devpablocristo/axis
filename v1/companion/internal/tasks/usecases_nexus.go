package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/devpablocristo/platform/concurrency/go/worker"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// HTTP status codes returned by the Nexus gateway that the task-sync logic
// branches on. Declared here so the usecase interprets gateway results without
// importing the net/http transport package (the gateway port returns a plain
// int status; see usecases.go nexusGateway).
const (
	nexusHTTPStatusCreated    = 201
	nexusHTTPStatusBadRequest = 400
	nexusHTTPStatusNotFound   = 404
)

type InvestigateInput struct {
	Note string
}

func (u *Usecases) applyTaskEvent(ctx context.Context, t domain.Task, event string) (domain.Task, error) {
	to, err := companionTaskMachine().Transition(t.Status, event)
	if err != nil {
		return domain.Task{}, ErrInvalidTaskState
	}
	t.Status = to
	if to == domain.TaskStatusDone || to == domain.TaskStatusFailed {
		now := time.Now().UTC()
		t.ClosedAt = &now
	} else {
		t.ClosedAt = nil
	}
	return u.repo.UpdateTask(ctx, t)
}

func (u *Usecases) nexusSyncIntervalOrDefault() time.Duration {
	if u.nexusSyncInterval <= 0 {
		return defaultNexusSyncInterval
	}
	return u.nexusSyncInterval
}

func nextNexusSyncAt(now time.Time, interval time.Duration, consecutiveFailures int) time.Time {
	if interval <= 0 {
		interval = defaultNexusSyncInterval
	}
	if consecutiveFailures <= 0 {
		return now.Add(interval)
	}
	delay := interval
	for i := 1; i < consecutiveFailures; i++ {
		if delay >= maxNexusSyncBackoff/2 {
			delay = maxNexusSyncBackoff
			break
		}
		delay *= 2
	}
	if delay > maxNexusSyncBackoff {
		delay = maxNexusSyncBackoff
	}
	return now.Add(delay)
}

func nexusSnapshotChanged(prev *domain.TaskNexusSyncState, next domain.TaskNexusSyncState) bool {
	if prev == nil {
		return next.NexusRequestID != uuid.Nil ||
			next.LastNexusStatus != "" ||
			next.LastNexusHTTPStatus != 0 ||
			next.LastError != ""
	}
	return prev.NexusRequestID != next.NexusRequestID ||
		prev.LastNexusStatus != next.LastNexusStatus ||
		prev.LastNexusHTTPStatus != next.LastNexusHTTPStatus ||
		prev.LastError != next.LastError
}

func isApprovedNexusStatus(status string) bool {
	switch normalizeNexusStatus(status) {
	case "allowed", "approved", "executed":
		return true
	default:
		return false
	}
}

type taskMemorySnapshot struct {
	Task        domain.Task
	NexusSync   *domain.TaskNexusSyncState
	DurablePlan *domain.TaskPlan
}

func (u *Usecases) loadTaskMemorySnapshot(ctx context.Context, taskID uuid.UUID) (taskMemorySnapshot, error) {
	task, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return taskMemorySnapshot{}, err
	}
	snapshot := taskMemorySnapshot{Task: task}

	nexusSync, err := u.repo.GetNexusSyncState(ctx, taskID)
	if err == nil {
		snapshot.NexusSync = &nexusSync
		snapshot.Task.NexusStatus = nexusSync.LastNexusStatus
		snapshot.Task.NexusLastCheckedAt = &nexusSync.LastCheckedAt
		snapshot.Task.NexusSyncError = nexusSync.LastError
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	durablePlan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err == nil {
		snapshot.DurablePlan = &durablePlan
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	return snapshot, nil
}

func nextTaskStep(snapshot taskMemorySnapshot) string {
	if snapshot.DurablePlan != nil && strings.TrimSpace(snapshot.DurablePlan.NextAction) != "" {
		return snapshot.DurablePlan.NextAction
	}
	switch snapshot.Task.Status {
	case domain.TaskStatusNew, domain.TaskStatusInvestigating:
		return "propose to nexus"
	case domain.TaskStatusWaitingForApproval:
		return "wait for nexus resolution or sync from nexus"
	case domain.TaskStatusWaitingForInput:
		return "provide the missing execution input"
	case domain.TaskStatusExecuting, domain.TaskStatusVerifying:
		return "observe execution and verification"
	case domain.TaskStatusFailed:
		if snapshot.Task.NexusStatus == "rejected" || snapshot.Task.NexusStatus == "denied" {
			return "inspect nexus decision and adjust the task"
		}
		return "inspect failure details"
	case domain.TaskStatusDone:
		return "closed"
	default:
		return "inspect task status"
	}
}

func buildTaskSummary(snapshot taskMemorySnapshot) string {
	title := strings.TrimSpace(snapshot.Task.Title)
	if title == "" {
		title = snapshot.Task.ID.String()
	}
	prefix := fmt.Sprintf("Task %q", title)
	if snapshot.DurablePlan != nil && strings.TrimSpace(snapshot.DurablePlan.NextAction) != "" {
		return fmt.Sprintf("%s has an active durable plan (%s). Next action: %s.", prefix, formatStatusForMemory(snapshot.DurablePlan.Status), snapshot.DurablePlan.NextAction)
	}

	switch snapshot.Task.Status {
	case domain.TaskStatusNew:
		return fmt.Sprintf("%s was created and is ready for investigation.", prefix)
	case domain.TaskStatusInvestigating:
		return fmt.Sprintf("%s is under investigation. Next step: %s.", prefix, nextTaskStep(snapshot))
	case domain.TaskStatusWaitingForApproval:
		if snapshot.NexusSync != nil && snapshot.NexusSync.NexusRequestID != uuid.Nil {
			return fmt.Sprintf("%s is waiting for Nexus. Request %s is currently %s.", prefix, snapshot.NexusSync.NexusRequestID.String(), formatStatusForMemory(snapshot.NexusSync.LastNexusStatus))
		}
		return fmt.Sprintf("%s is waiting for Nexus approval.", prefix)
	case domain.TaskStatusWaitingForInput:
		return fmt.Sprintf("%s is approved and waiting for additional input.", prefix)
	case domain.TaskStatusExecuting:
		return fmt.Sprintf("%s is executing.", prefix)
	case domain.TaskStatusVerifying:
		return fmt.Sprintf("%s finished execution and is being verified.", prefix)
	case domain.TaskStatusDone:
		if isApprovedNexusStatus(snapshot.Task.NexusStatus) {
			return fmt.Sprintf("%s completed successfully after Nexus resolved %s.", prefix, formatStatusForMemory(snapshot.Task.NexusStatus))
		}
		return fmt.Sprintf("%s completed successfully.", prefix)
	case domain.TaskStatusFailed:
		if snapshot.Task.NexusStatus != "" {
			return fmt.Sprintf("%s failed because Nexus resolved %s.", prefix, formatStatusForMemory(snapshot.Task.NexusStatus))
		}
		return fmt.Sprintf("%s failed and needs operator attention.", prefix)
	default:
		return fmt.Sprintf("%s is in status %s.", prefix, formatStatusForMemory(snapshot.Task.Status))
	}
}

func formatStatusForMemory(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "unknown"
	}
	return strings.ReplaceAll(status, "_", " ")
}

func buildTaskFactsPayload(snapshot taskMemorySnapshot, reason string) json.RawMessage {
	payload := map[string]any{
		"projection_reason":  reason,
		"task_id":            snapshot.Task.ID.String(),
		"title":              snapshot.Task.Title,
		"goal":               snapshot.Task.Goal,
		"status":             snapshot.Task.Status,
		"priority":           snapshot.Task.Priority,
		"created_by":         snapshot.Task.CreatedBy,
		"assigned_to":        snapshot.Task.AssignedTo,
		"channel":            snapshot.Task.Channel,
		"summary":            snapshot.Task.Summary,
		"next_step":          nextTaskStep(snapshot),
		"attention_required": snapshot.Task.Status == domain.TaskStatusWaitingForApproval || snapshot.Task.Status == domain.TaskStatusWaitingForInput || snapshot.Task.Status == domain.TaskStatusFailed,
		"updated_at":         snapshot.Task.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if snapshot.Task.CreatedAt.IsZero() {
		payload["created_at"] = ""
	} else {
		payload["created_at"] = snapshot.Task.CreatedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.ClosedAt != nil {
		payload["closed_at"] = snapshot.Task.ClosedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.NexusStatus != "" {
		payload["nexus_status"] = snapshot.Task.NexusStatus
	}
	if snapshot.Task.NexusLastCheckedAt != nil {
		payload["nexus_last_checked_at"] = snapshot.Task.NexusLastCheckedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.NexusSyncError != "" {
		payload["nexus_sync_error"] = snapshot.Task.NexusSyncError
	}
	if snapshot.NexusSync != nil {
		payload["nexus"] = map[string]any{
			"nexus_request_id":     snapshot.NexusSync.NexusRequestID.String(),
			"status":               snapshot.NexusSync.LastNexusStatus,
			"http_status":          snapshot.NexusSync.LastNexusHTTPStatus,
			"last_checked_at":      snapshot.NexusSync.LastCheckedAt.UTC().Format(time.RFC3339),
			"next_check_at":        snapshot.NexusSync.NextCheckAt.UTC().Format(time.RFC3339),
			"consecutive_failures": snapshot.NexusSync.ConsecutiveFailures,
			"last_error":           snapshot.NexusSync.LastError,
		}
	}
	if snapshot.DurablePlan != nil {
		steps := make([]map[string]any, 0, len(snapshot.DurablePlan.Steps))
		for _, step := range snapshot.DurablePlan.Steps {
			steps = append(steps, map[string]any{
				"id":               step.ID.String(),
				"step_key":         step.StepKey,
				"title":            step.Title,
				"status":           step.Status,
				"expected_outcome": step.ExpectedOutcome,
				"postcondition":    step.Postcondition,
				"observation":      step.Observation,
				"blocker":          step.Blocker,
				"error_message":    step.ErrorMessage,
				"attempt_count":    step.AttemptCount,
				"sort_order":       step.SortOrder,
				"completed_at":     formatOptionalTime(step.CompletedAt),
			})
		}
		payload["durable_plan"] = map[string]any{
			"objective":   snapshot.DurablePlan.Objective,
			"status":      snapshot.DurablePlan.Status,
			"strategy":    snapshot.DurablePlan.Strategy,
			"next_action": snapshot.DurablePlan.NextAction,
			"blocker":     snapshot.DurablePlan.Blocker,
			"checkpoint":  json.RawMessage(snapshot.DurablePlan.CheckpointJSON),
			"steps":       steps,
			"updated_at":  snapshot.DurablePlan.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	return marshalOrEmpty("task_facts", payload)
}

func (u *Usecases) syncTaskMemory(ctx context.Context, taskID uuid.UUID, reason string) {
	if u.taskMemory == nil {
		return
	}
	snapshot, err := u.loadTaskMemorySnapshot(ctx, taskID)
	if err != nil {
		slog.Warn("companion project task memory failed", "task_id", taskID.String(), "reason", reason, "error", err)
		return
	}
	summaryPayload := marshalOrEmpty("task_summary", map[string]any{
		"projection_reason": reason,
		"status":            snapshot.Task.Status,
		"nexus_status":      snapshot.Task.NexusStatus,
		"next_step":         nextTaskStep(snapshot),
	})
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindSummary, taskMemoryCurrentKey, buildTaskSummary(snapshot), summaryPayload); err != nil {
		slog.Warn("companion upsert task summary failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindFacts, taskMemoryCurrentKey, "", buildTaskFactsPayload(snapshot, reason)); err != nil {
		slog.Warn("companion upsert task facts failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
}

func buildNexusSyncActionPayload(origin string, prev *domain.TaskNexusSyncState, next domain.TaskNexusSyncState, beforeStatus, afterStatus, event string) json.RawMessage {
	type syncSnapshot struct {
		NexusRequestID string `json:"nexus_request_id,omitempty"`
		Status         string `json:"status,omitempty"`
		HTTPStatus     int    `json:"http_status,omitempty"`
		Error          string `json:"error,omitempty"`
	}
	payload := map[string]any{
		"origin":             origin,
		"task_status_before": beforeStatus,
		"task_status_after":  afterStatus,
	}
	if event != "" {
		payload["transition_event"] = event
	}
	current := syncSnapshot{
		Status:     next.LastNexusStatus,
		HTTPStatus: next.LastNexusHTTPStatus,
		Error:      next.LastError,
	}
	if next.NexusRequestID != uuid.Nil {
		current.NexusRequestID = next.NexusRequestID.String()
	}
	payload["current"] = current
	if prev != nil {
		previous := syncSnapshot{
			Status:     prev.LastNexusStatus,
			HTTPStatus: prev.LastNexusHTTPStatus,
			Error:      prev.LastError,
		}
		if prev.NexusRequestID != uuid.Nil {
			previous.NexusRequestID = prev.NexusRequestID.String()
		}
		payload["previous"] = previous
	}
	return marshalOrEmpty("nexus_sync_payload", payload)
}

func (u *Usecases) latestNexusRequestIDForTask(ctx context.Context, taskID uuid.UUID, state *domain.TaskNexusSyncState) (uuid.UUID, error) {
	if state != nil && state.NexusRequestID != uuid.Nil {
		return state.NexusRequestID, nil
	}
	return u.repo.LatestProposeNexusRequestID(ctx, taskID)
}

func (u *Usecases) persistNexusSyncAction(ctx context.Context, taskID uuid.UUID, nexusRequestID uuid.UUID, origin string, prev *domain.TaskNexusSyncState, next domain.TaskNexusSyncState, beforeStatus, afterStatus, event string) {
	payload := buildNexusSyncActionPayload(origin, prev, next, beforeStatus, afterStatus, event)
	nexusRequestIDCopy := nexusRequestID
	if _, err := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionSyncNexus,
		Payload:        payload,
		NexusRequestID: &nexusRequestIDCopy,
	}); err != nil {
		slog.Warn("companion sync_nexus action failed", "task_id", taskID.String(), "nexus_request_id", nexusRequestID.String(), "error", err)
	}
}

func (u *Usecases) syncTaskWithNexus(ctx context.Context, t domain.Task, origin string) (domain.Task, *domain.TaskNexusSyncState, error) {
	if t.Status != domain.TaskStatusWaitingForApproval {
		return t, nil, nil
	}

	var prevState *domain.TaskNexusSyncState
	currentState, err := u.repo.GetNexusSyncState(ctx, t.ID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return domain.Task{}, nil, err
	}

	rid, err := u.latestNexusRequestIDForTask(ctx, t.ID, prevState)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return t, prevState, nil
		}
		return domain.Task{}, prevState, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskNexusSyncState{
		TaskID:         t.ID,
		NexusRequestID: rid,
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

	sum, st, gErr := u.nexus.GetRequest(ctx, rid.String())
	beforeStatus := t.Status
	appliedEvent := ""

	if gErr != nil {
		nextState.LastNexusHTTPStatus = st
		nextState.LastError = gErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertNexusSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if nexusSnapshotChanged(prevState, stateOut) {
			u.persistNexusSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "nexus_sync_error")
		}
		return domain.Task{}, &stateOut, fmt.Errorf("nexus get request: %w", gErr)
	}

	nextState.LastNexusHTTPStatus = st

	if st == nexusHTTPStatusNotFound {
		nextState.LastError = "nexus request not found"
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertNexusSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if nexusSnapshotChanged(prevState, stateOut) {
			u.persistNexusSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "nexus_sync_not_found")
		}
		t.NexusStatus = stateOut.LastNexusStatus
		t.NexusLastCheckedAt = &stateOut.LastCheckedAt
		t.NexusSyncError = stateOut.LastError
		return t, &stateOut, nil
	}
	if normalizedStatus := normalizeNexusStatus(sum.Status); normalizedStatus != "" {
		nextState.LastNexusStatus = normalizedStatus
	}

	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0)

	ev, apply := eventFromNexusRequestStatus(sum.Status)
	if apply {
		appliedEvent = ev
		t, err = u.applyTaskEvent(ctx, t, ev)
		if err != nil {
			return domain.Task{}, prevState, err
		}
	}

	stateOut, upErr := u.repo.UpsertNexusSyncState(ctx, nextState)
	if upErr != nil {
		return domain.Task{}, prevState, upErr
	}
	if nexusSnapshotChanged(prevState, stateOut) || beforeStatus != t.Status {
		u.persistNexusSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
		u.syncTaskMemory(ctx, t.ID, "nexus_sync")
	}
	t.NexusStatus = stateOut.LastNexusStatus
	t.NexusLastCheckedAt = &stateOut.LastCheckedAt
	t.NexusSyncError = stateOut.LastError

	slog.Info("companion task synced from nexus",
		"task_id", t.ID.String(),
		"nexus_request_id", rid.String(),
		"nexus_status", stateOut.LastNexusStatus,
		"task_status", t.Status,
		"origin", origin,
	)
	return t, &stateOut, nil
}

func (u *Usecases) Investigate(ctx context.Context, taskID uuid.UUID, in InvestigateInput) (domain.Task, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	t, err = u.applyTaskEvent(ctx, t, evInvestigate)
	if err != nil {
		return domain.Task{}, err
	}
	if in.Note != "" {
		_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
			TaskID:     taskID,
			AuthorType: "system",
			AuthorID:   CompanionRequesterID,
			Body:       in.Note,
		})
		if err != nil {
			return domain.Task{}, err
		}
	}
	u.syncTaskMemory(ctx, taskID, "investigate")
	return t, nil
}

type ProposeInput struct {
	Note           string
	TargetSystem   string
	TargetResource string
	SessionID      string
}

func (u *Usecases) Propose(ctx context.Context, taskID uuid.UUID, in ProposeInput) (domain.Task, domain.TaskAction, nexusclient.SubmitResponse, error) {
	var zeroA domain.TaskAction
	var zeroSub nexusclient.SubmitResponse
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, zeroA, zeroSub, err
	}
	switch t.Status {
	case domain.TaskStatusDone, domain.TaskStatusFailed:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	case domain.TaskStatusWaitingForApproval:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	case domain.TaskStatusNew, domain.TaskStatusInvestigating:
		// ok
	default:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	}

	targetSystem := strings.TrimSpace(in.TargetSystem)
	if targetSystem == "" {
		targetSystem = "axis"
	}
	targetResource := strings.TrimSpace(in.TargetResource)
	if targetResource == "" {
		targetResource = t.ID.String()
	}
	binding := map[string]any{
		"target_system":   targetSystem,
		"target_resource": targetResource,
		"org_id":          t.OrgID,
		"task_id":         taskID.String(),
		"task_title":      t.Title,
	}
	if in.Note != "" {
		binding["note"] = in.Note
	}
	if in.SessionID != "" {
		binding["session_id"] = in.SessionID
	}

	payload := map[string]any{
		"note":            in.Note,
		"target_system":   targetSystem,
		"target_resource": targetResource,
	}
	pj := marshalOrEmpty("propose_action_payload", payload)
	action, err := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionPropose,
		Payload:    pj,
	})
	if err != nil {
		return domain.Task{}, zeroA, zeroSub, err
	}

	nexusMeta := map[string]any{
		"origin":      "companion",
		"task_id":     taskID.String(),
		"proposed_by": CompanionRequesterID,
		"human_owner": t.CreatedBy,
		"action_id":   action.ID.String(),
	}
	if in.SessionID != "" {
		nexusMeta["session_id"] = in.SessionID
	}
	params := map[string]any{
		"org_id":         t.OrgID,
		"nexus":          nexusMeta,
		"action_binding": binding,
	}

	ctxJSON := map[string]any{
		"task_title": t.Title,
		"task_goal":  t.Goal,
		"note":       in.Note,
	}
	ctxStr := marshalOrEmpty("propose_context", ctxJSON)

	reason := t.Title
	if in.Note != "" {
		reason = t.Title + ": " + in.Note
	}

	idem := fmt.Sprintf("companion-propose-%s", action.ID.String())
	submitBody := nexusclient.SubmitRequestBody{
		RequesterType:  CompanionRequesterType,
		RequesterID:    CompanionRequesterID,
		RequesterName:  CompanionRequesterName,
		ActionType:     ActionTypePropose,
		TargetSystem:   stringFromBinding(binding, "target_system", in.TargetSystem),
		TargetResource: stringFromBinding(binding, "target_resource", in.TargetResource),
		ActionBinding:  binding,
		Params:         params,
		Reason:         reason,
		Context:        string(ctxStr),
	}

	submitOut, subErr := u.nexus.SubmitRequest(ctx, idem, submitBody)
	if subErr != nil {
		slog.Warn("companion propose nexus submit failed",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"error", subErr,
		)
		_ = u.repo.UpdateActionNexusResult(ctx, action.ID, nil, subErr.Error())
		t2, ge := u.repo.GetTaskByID(ctx, taskID)
		if ge != nil {
			return domain.Task{}, action, zeroSub, ge
		}
		return t2, action, zeroSub, fmt.Errorf("%w: %v", ErrNexusSubmit, subErr)
	}
	reqUUID, perr := uuid.Parse(submitOut.RequestID)
	if perr != nil {
		_ = u.repo.UpdateActionNexusResult(ctx, action.ID, nil, "invalid request_id from nexus")
		return domain.Task{}, action, zeroSub, fmt.Errorf("parse request_id: %w", perr)
	}
	if err := u.repo.UpdateActionNexusResult(ctx, action.ID, &reqUUID, ""); err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	now := time.Now().UTC()
	state, err := u.repo.UpsertNexusSyncState(ctx, domain.TaskNexusSyncState{
		TaskID:              taskID,
		NexusRequestID:      reqUUID,
		LastNexusStatus:     normalizeNexusStatus(submitOut.Status),
		LastNexusHTTPStatus: nexusHTTPStatusCreated,
		LastCheckedAt:       now,
		LastError:           "",
		ConsecutiveFailures: 0,
		NextCheckAt:         nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0),
	})
	if err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	ev, evErr := eventFromSubmitResponse(submitOut)
	if evErr != nil {
		slog.Error("companion propose unexpected nexus status",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"nexus_status", submitOut.Status,
			"error", evErr,
		)
		return domain.Task{}, action, submitOut, evErr
	}
	t, err = u.applyTaskEvent(ctx, t, ev)
	if err != nil {
		return domain.Task{}, action, submitOut, err
	}
	t.NexusStatus = state.LastNexusStatus
	t.NexusLastCheckedAt = &state.LastCheckedAt
	t.NexusSyncError = state.LastError
	action.NexusRequestID = &reqUUID
	slog.Info("companion propose submitted to nexus",
		"task_id", taskID.String(),
		"action_id", action.ID.String(),
		"nexus_request_id", reqUUID.String(),
		"nexus_decision", submitOut.Decision,
		"nexus_status", submitOut.Status,
		"task_status", t.Status,
	)
	u.syncTaskMemory(ctx, taskID, "propose")
	return t, action, submitOut, nil
}

// SyncTaskNexus consulta Nexus y aplica transición si el request ya resolvió (tareas en espera).
func (u *Usecases) SyncTaskNexus(ctx context.Context, taskID uuid.UUID) (domain.Task, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	t, state, err := u.syncTaskWithNexus(ctx, t, "manual")
	if state != nil {
		t.NexusStatus = state.LastNexusStatus
		t.NexusLastCheckedAt = &state.LastCheckedAt
		t.NexusSyncError = state.LastError
	}
	if err != nil {
		return domain.Task{}, err
	}
	return t, nil
}

// SyncPendingNexusTasks sincroniza un lote de tareas en waiting_for_approval.
func (u *Usecases) SyncPendingNexusTasks(ctx context.Context, limit int) {
	if limit <= 0 {
		limit = 50
	}
	list, err := u.repo.ListTasksPendingNexusSync(ctx, time.Now().UTC(), limit)
	if err != nil {
		slog.Error("companion sync list waiting tasks", "error", err)
		return
	}
	for _, item := range list {
		if _, _, sErr := u.syncTaskWithNexus(ctx, item, "loop"); sErr != nil {
			slog.Warn("companion sync task failed", "task_id", item.ID.String(), "error", sErr)
		}
	}
}

// RunNexusSyncLoop ejecuta SyncPendingNexusTasks periódicamente hasta que ctx termina.
func (u *Usecases) RunNexusSyncLoop(ctx context.Context, interval time.Duration, batch int) {
	if batch <= 0 {
		return
	}
	worker.RunPeriodic(ctx, interval, "nexus-sync", func(c context.Context) {
		runCtx, cancel := context.WithTimeout(c, 2*time.Minute)
		u.SyncPendingNexusTasks(runCtx, batch)
		cancel()
	})
}

// ErrInvalidStatus para handlers.
func IsNotFound(err error) bool {
	return domainerr.IsNotFound(err)
}

// IsInvalidTaskState indica conflicto de estado (FSM / reglas de negocio).
func IsInvalidTaskState(err error) bool {
	return errors.Is(err, ErrInvalidTaskState)
}

// nexusBlockedError devuelve un error estructurado cuando una operación de
// task se bloquea porque la nexus en Nexus no está aprobada.
func (u *Usecases) nexusBlockedError(nexusRequestID, nexusStatus, reason string) error {
	return &NexusBlockedError{
		NexusRequestID: nexusRequestID,
		NexusStatus:    nexusStatus,
		Reason:         reason,
	}
}

// NotifyAlert implementa watchers.ChatNotifier.
// Crea una tarea-alerta y agrega el mensaje como sistema.
func (u *Usecases) NotifyAlert(ctx context.Context, orgID, message string) error {
	title := message
	if len(title) > 80 {
		title = title[:80]
	}
	t, err := u.repo.CreateTask(ctx, domain.Task{
		Title:     title,
		OrgID:     orgID,
		Status:    domain.TaskStatusNew,
		Priority:  "high",
		CreatedBy: orgID,
		Channel:   "watcher",
	})
	if err != nil {
		return fmt.Errorf("create alert task: %w", err)
	}
	_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     t.ID,
		AuthorType: "system",
		AuthorID:   "nexus-watcher",
		Body:       message,
	})
	if err != nil {
		return fmt.Errorf("insert alert message: %w", err)
	}
	slog.Info("watcher alert pushed to chat", "task_id", t.ID, "org_id", orgID)
	return nil
}
