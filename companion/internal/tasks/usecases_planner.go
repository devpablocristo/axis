package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

type SetTaskPlanInput struct {
	Objective       string
	Status          string
	Strategy        string
	AssumptionsJSON json.RawMessage
	ConstraintsJSON json.RawMessage
	CheckpointJSON  json.RawMessage
	NextAction      string
	Blocker         string
	CreatedBy       string
	Steps           []SetTaskPlanStepInput
}

type SetTaskPlanStepInput struct {
	ID              uuid.UUID
	StepKey         string
	Title           string
	Status          string
	DependsOnJSON   json.RawMessage
	ToolName        string
	Capability      string
	ExpectedOutcome string
	Postcondition   string
	EvidenceJSON    json.RawMessage
	Observation     string
	Blocker         string
	ErrorMessage    string
	AttemptCount    int
	SortOrder       int
}

type UpdateTaskPlanStepInput struct {
	Status         string
	EvidenceJSON   json.RawMessage
	Observation    string
	Blocker        string
	ErrorMessage   string
	CheckpointJSON json.RawMessage
	NextAction     string
}

type RecordTaskPlanCheckpointInput struct {
	Status         string
	CheckpointJSON json.RawMessage
	NextAction     string
	Blocker        string
}

type PrepareTaskPlanCompensationInput struct {
	Reason string
}

type TaskPlanCompensationOutput struct {
	Plan                domain.TaskPlan
	Step                domain.TaskPlanStep
	Status              string
	Reason              string
	Compensation        map[string]any
	NexusRequestID      string
	NexusStatus         string
	NexusDecision       string
	NexusBindingHash    string
	ApprovalRequired    bool
	ApprovalUnavailable bool
}

type ExecuteTaskPlanCompensationInput struct {
	NexusRequestID string
}

type TaskPlanCompensationExecutionOutput struct {
	Plan             domain.TaskPlan
	Step             domain.TaskPlanStep
	Status           string
	Reason           string
	Compensation     map[string]any
	NexusRequestID   string
	NexusStatus      string
	Execution        connectordomain.ExecutionResult
	Verification     domain.TaskVerificationResult
	ApprovalRequired bool
}

type taskExecutionGraphRepository interface {
	ListTaskExecutionGraph(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskExecutionGraphEvent, error)
}

func (u *Usecases) SetTaskPlan(ctx context.Context, taskID uuid.UUID, in SetTaskPlanInput) (domain.TaskPlan, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	objective := strings.TrimSpace(in.Objective)
	if objective == "" {
		objective = strings.TrimSpace(t.Goal)
	}
	if objective == "" {
		objective = strings.TrimSpace(t.Title)
	}
	if objective == "" {
		return domain.TaskPlan{}, fmt.Errorf("objective is required")
	}
	if len(in.Steps) == 0 {
		return domain.TaskPlan{}, fmt.Errorf("at least one plan step is required")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = domain.TaskPlanStatusActive
	}
	if !validTaskPlanStatus(status) {
		return domain.TaskPlan{}, fmt.Errorf("invalid plan status")
	}
	plan := domain.TaskPlan{
		TaskID:          taskID,
		OrgID:           t.OrgID,
		Objective:       objective,
		Status:          status,
		Strategy:        strings.TrimSpace(in.Strategy),
		AssumptionsJSON: jsonOrDefault(in.AssumptionsJSON, `[]`),
		ConstraintsJSON: jsonOrDefault(in.ConstraintsJSON, `[]`),
		CheckpointJSON:  jsonOrDefault(in.CheckpointJSON, `{}`),
		NextAction:      strings.TrimSpace(in.NextAction),
		Blocker:         strings.TrimSpace(in.Blocker),
		CreatedBy:       strings.TrimSpace(in.CreatedBy),
		Steps:           make([]domain.TaskPlanStep, 0, len(in.Steps)),
	}
	for i, inputStep := range in.Steps {
		step, err := buildTaskPlanStep(t.OrgID, taskID, i, inputStep)
		if err != nil {
			return domain.TaskPlan{}, err
		}
		plan.Steps = append(plan.Steps, step)
	}
	if plan.NextAction == "" {
		plan.NextAction = nextActionFromSteps(plan.Steps)
	}
	applyPlanCompletion(&plan)
	saved, err := u.repo.UpsertTaskPlan(ctx, plan)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionSetDurablePlan,
		Payload:    taskPlanActionPayload(saved),
	}); insertErr != nil {
		slog.Warn("companion set durable plan action failed", "task_id", taskID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "set_durable_plan")
	return saved, nil
}

func (u *Usecases) ListTaskExecutionGraph(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskExecutionGraphEvent, error) {
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return nil, err
	}
	repo, ok := u.repo.(taskExecutionGraphRepository)
	if !ok {
		return nil, fmt.Errorf("task execution graph repository is not configured")
	}
	return repo.ListTaskExecutionGraph(ctx, taskID, limit)
}

func (u *Usecases) UpdateTaskPlanStep(ctx context.Context, taskID, stepID uuid.UUID, in UpdateTaskPlanStepInput) (domain.TaskPlan, error) {
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return domain.TaskPlan{}, err
	}
	var step *domain.TaskPlanStep
	for i := range plan.Steps {
		if plan.Steps[i].ID == stepID {
			step = &plan.Steps[i]
			break
		}
	}
	if step == nil {
		return domain.TaskPlan{}, ErrNotFound
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		if !validTaskPlanStepStatus(status) {
			return domain.TaskPlan{}, fmt.Errorf("invalid plan step status")
		}
		step.Status = status
	}
	if len(in.EvidenceJSON) > 0 {
		step.EvidenceJSON = jsonOrDefault(in.EvidenceJSON, `{}`)
	}
	if strings.TrimSpace(in.Observation) != "" {
		step.Observation = strings.TrimSpace(in.Observation)
	}
	if strings.TrimSpace(in.Blocker) != "" {
		step.Blocker = strings.TrimSpace(in.Blocker)
	}
	if strings.TrimSpace(in.ErrorMessage) != "" {
		step.ErrorMessage = strings.TrimSpace(in.ErrorMessage)
	}
	if step.Status == domain.TaskPlanStepStatusRunning {
		step.AttemptCount++
	}
	if isTerminalTaskPlanStepStatus(step.Status) {
		now := time.Now().UTC()
		step.CompletedAt = &now
	}
	if _, err := u.repo.UpdateTaskPlanStep(ctx, *step); err != nil {
		return domain.TaskPlan{}, err
	}
	updated, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if len(in.CheckpointJSON) > 0 {
		updated.CheckpointJSON = jsonOrDefault(in.CheckpointJSON, `{}`)
	}
	if strings.TrimSpace(in.NextAction) != "" {
		updated.NextAction = strings.TrimSpace(in.NextAction)
	} else {
		updated.NextAction = nextActionFromSteps(updated.Steps)
	}
	updated.Blocker = firstPlanBlocker(updated.Steps)
	updated.Status = statusFromPlanSteps(updated.Steps, updated.Status)
	applyPlanCompletion(&updated)
	updated, err = u.repo.UpdateTaskPlan(ctx, updated)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionUpdatePlanStep,
		Payload: marshalOrEmpty("task_plan_step_action", map[string]any{
			"step_id":       stepID.String(),
			"status":        step.Status,
			"observation":   step.Observation,
			"blocker":       step.Blocker,
			"error_message": step.ErrorMessage,
		}),
	}); insertErr != nil {
		slog.Warn("companion update plan step action failed", "task_id", taskID.String(), "step_id", stepID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "update_plan_step")
	return updated, nil
}

func (u *Usecases) RecordTaskPlanCheckpoint(ctx context.Context, taskID uuid.UUID, in RecordTaskPlanCheckpointInput) (domain.TaskPlan, error) {
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return domain.TaskPlan{}, err
	}
	if len(in.CheckpointJSON) > 0 {
		plan.CheckpointJSON = jsonOrDefault(in.CheckpointJSON, `{}`)
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		if !validTaskPlanStatus(status) {
			return domain.TaskPlan{}, fmt.Errorf("invalid plan status")
		}
		plan.Status = status
	}
	if strings.TrimSpace(in.NextAction) != "" {
		plan.NextAction = strings.TrimSpace(in.NextAction)
	}
	if strings.TrimSpace(in.Blocker) != "" {
		plan.Blocker = strings.TrimSpace(in.Blocker)
	}
	applyPlanCompletion(&plan)
	updated, err := u.repo.UpdateTaskPlan(ctx, plan)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionPlanCheckpoint,
		Payload: marshalOrEmpty("task_plan_checkpoint_action", map[string]any{
			"status":      updated.Status,
			"next_action": updated.NextAction,
			"blocker":     updated.Blocker,
			"checkpoint":  json.RawMessage(updated.CheckpointJSON),
		}),
	}); insertErr != nil {
		slog.Warn("companion plan checkpoint action failed", "task_id", taskID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "plan_checkpoint")
	return updated, nil
}

func (u *Usecases) GetTaskPlan(ctx context.Context, taskID uuid.UUID) (domain.TaskPlan, error) {
	return u.repo.GetTaskPlan(ctx, taskID)
}

func (u *Usecases) PrepareTaskPlanCompensation(ctx context.Context, taskID, stepID uuid.UUID, in PrepareTaskPlanCompensationInput) (TaskPlanCompensationOutput, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return TaskPlanCompensationOutput{}, err
	}
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return TaskPlanCompensationOutput{}, err
	}
	step, ok := findTaskPlanStep(plan, stepID)
	if !ok {
		return TaskPlanCompensationOutput{}, ErrNotFound
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return TaskPlanCompensationOutput{}, fmt.Errorf("reason is required")
	}

	compensation, supported := compensationFromTaskPlanStepEvidence(step.EvidenceJSON)
	status := "compensation_unavailable"
	planStatus := domain.TaskPlanStatusBlocked
	blocker := "compensation is not declared for this step"
	nextAction := "review step manually"
	checkpoint := map[string]any{
		"source":            "prepare_task_plan_compensation",
		"status":            status,
		"step_id":           step.ID.String(),
		"step_key":          step.StepKey,
		"reason":            reason,
		"approval_required": true,
		"compensation":      compensation,
	}

	var submitOut nexusclient.SubmitResponse
	if supported {
		if u.executor == nil {
			checkpoint["status"] = "compensation_contract_invalid"
			checkpoint["error"] = "task execution is not configured"
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "configure connector execution before compensation", "compensation execution is not configured")
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "compensation_contract_invalid", Reason: reason, Compensation: compensation, ApprovalRequired: true, ApprovalUnavailable: true}, nil
		}
		spec, specErr := compensationExecutionSpec(t, step, compensation, reason, nil)
		if specErr != nil {
			checkpoint["status"] = "compensation_contract_invalid"
			checkpoint["error"] = specErr.Error()
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "fix compensation contract", "compensation contract invalid: "+specErr.Error())
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "compensation_contract_invalid", Reason: reason, Compensation: compensation, ApprovalRequired: true}, nil
		}
		binding, bindingHash, bindingErr := u.executor.BuildActionBinding(ctx, spec)
		if bindingErr != nil {
			checkpoint["status"] = "compensation_contract_invalid"
			checkpoint["error"] = bindingErr.Error()
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "fix compensation contract", "compensation action binding failed: "+bindingErr.Error())
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "compensation_contract_invalid", Reason: reason, Compensation: compensation, ApprovalRequired: true}, nil
		}
		if u.nexus == nil {
			checkpoint["status"] = "approval_unavailable"
			checkpoint["error"] = "nexus not configured"
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, planStatus, checkpoint, "review compensation manually", "compensation requires approval but Nexus is not configured")
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{
				Plan:                updated,
				Step:                step,
				Status:              "approval_unavailable",
				Reason:              reason,
				Compensation:        compensation,
				ApprovalRequired:    true,
				ApprovalUnavailable: true,
			}, nil
		}
		targetSystem := stringFromBinding(binding, "target_system", "capability")
		targetResource := stringFromBinding(binding, "target_resource", step.ID.String())
		idempotencyKey := stringFromBinding(binding, "idempotency_key", defaultCompensationIdempotencyKey(taskID, stepID))
		params := map[string]any{
			"org_id":               t.OrgID,
			"task_id":              taskID.String(),
			"plan_step_id":         step.ID.String(),
			"step_key":             step.StepKey,
			"reason":               reason,
			"compensation":         compensation,
			"compensation_payload": json.RawMessage(spec.Payload),
			"original_tool":        step.ToolName,
			"original_status":      step.Status,
			"action_binding":       binding,
			"action_binding_hash":  bindingHash,
		}
		if step.EvidenceJSON != nil {
			params["step_evidence"] = json.RawMessage(step.EvidenceJSON)
		}
		submitBody := nexusclient.SubmitRequestBody{
			RequesterType:  "agent",
			RequesterID:    CompanionRequesterID,
			RequesterName:  CompanionRequesterName,
			ActionType:     nexusclient.ActionTypeAgentCapabilityCompensate,
			TargetSystem:   targetSystem,
			TargetResource: targetResource,
			ActionBinding:  binding,
			Params:         params,
			Reason:         "Compensate task plan step: " + reason,
			Context: string(marshalOrEmpty("task_plan_compensation_context", map[string]any{
				"task_title":       t.Title,
				"task_goal":        t.Goal,
				"plan_objective":   plan.Objective,
				"step_title":       step.Title,
				"expected_outcome": step.ExpectedOutcome,
				"postcondition":    step.Postcondition,
			})),
		}
		var subErr error
		submitOut, subErr = u.nexus.SubmitRequest(ctx, idempotencyKey, submitBody)
		if subErr != nil {
			checkpoint["status"] = "approval_submit_failed"
			checkpoint["error"] = subErr.Error()
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "retry compensation approval request", "compensation approval request failed: "+subErr.Error())
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "approval_submit_failed", Reason: reason, Compensation: compensation, ApprovalRequired: true, ApprovalUnavailable: true}, fmt.Errorf("%w: %v", ErrNexusSubmit, subErr)
		}
		status = "compensation_approval_requested"
		planStatus = domain.TaskPlanStatusEscalated
		blocker = "compensation requires approval: " + reason
		nextAction = "await compensation approval decision"
		checkpoint["status"] = status
		checkpoint["nexus_request_id"] = submitOut.RequestID
		checkpoint["nexus_status"] = submitOut.Status
		checkpoint["nexus_decision"] = submitOut.Decision
		checkpoint["nexus_binding_hash"] = submitOut.BindingHash
		if submitOut.Status == nexusclient.StatusAllowed || submitOut.Status == nexusclient.StatusApproved {
			nextAction = "execute approved compensation under governed capability path"
			blocker = ""
		}
	}

	updated, err := u.recordCompensationCheckpoint(ctx, taskID, planStatus, checkpoint, nextAction, blocker)
	if err != nil {
		return TaskPlanCompensationOutput{}, err
	}
	return TaskPlanCompensationOutput{
		Plan:             updated,
		Step:             step,
		Status:           status,
		Reason:           reason,
		Compensation:     compensation,
		NexusRequestID:   submitOut.RequestID,
		NexusStatus:      submitOut.Status,
		NexusDecision:    submitOut.Decision,
		NexusBindingHash: submitOut.BindingHash,
		ApprovalRequired: true,
	}, nil
}

func (u *Usecases) ExecuteTaskPlanCompensation(ctx context.Context, taskID, stepID uuid.UUID, in ExecuteTaskPlanCompensationInput) (TaskPlanCompensationExecutionOutput, error) {
	var out TaskPlanCompensationExecutionOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}
	if u.nexus == nil {
		return out, fmt.Errorf("nexus is not configured")
	}
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return out, err
	}
	step, ok := findTaskPlanStep(plan, stepID)
	if !ok {
		return out, ErrNotFound
	}
	action, reason, compensation, err := u.latestCompensationAction(ctx, taskID, stepID, strings.TrimSpace(in.NexusRequestID))
	if err != nil {
		return out, err
	}
	nexusRequestID, err := compensationNexusRequestID(action, in.NexusRequestID)
	if err != nil {
		return out, err
	}
	sum, statusCode, err := u.nexus.GetRequest(ctx, nexusRequestID.String())
	if err != nil {
		return out, fmt.Errorf("nexus get compensation request: %w", err)
	}
	if statusCode == nexusHTTPStatusNotFound {
		return out, fmt.Errorf("nexus compensation request not found")
	}
	nexusStatus := normalizeNexusStatus(sum.Status)
	if !isApprovedNexusStatus(nexusStatus) {
		return out, u.nexusBlockedError(nexusRequestID.String(), nexusStatus, "execute_compensation")
	}

	spec, err := compensationExecutionSpec(t, step, compensation, reason, &nexusRequestID)
	if err != nil {
		return out, err
	}
	result, execErr := u.executor.Execute(ctx, spec)
	if execErr != nil {
		result = connectordomain.ExecutionResult{
			ID:             uuid.New(),
			ConnectorID:    spec.ConnectorID,
			OrgID:          spec.OrgID,
			ActorID:        spec.ActorID,
			Operation:      spec.Operation,
			Status:         connectordomain.ExecFailure,
			Payload:        spec.Payload,
			ResultJSON:     json.RawMessage(`{}`),
			ErrorMessage:   execErr.Error(),
			Retryable:      true,
			IdempotencyKey: spec.IdempotencyKey,
			TaskID:         spec.TaskID,
			NexusRequestID: spec.NexusRequestID,
			CreatedAt:      time.Now().UTC(),
		}
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	u.reportExecutionToNexus(ctx, &nexusRequestID, result)
	verification := verifyExecutionResult(result)
	status := "compensation_executed"
	planStatus := domain.TaskPlanStatusCompleted
	nextAction := "compensation executed"
	blocker := ""
	if result.Status != connectordomain.ExecSuccess || verification.Status != domain.VerificationStatusVerified {
		status = "compensation_failed"
		planStatus = domain.TaskPlanStatusFailed
		nextAction = "review failed compensation"
		blocker = firstNonEmptyString(result.ErrorMessage, verification.Summary, "compensation execution failed")
	}

	payload := buildConnectorExecutionPayload(result)
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionExecuteComp,
		Payload:        payload,
		NexusRequestID: &nexusRequestID,
		ErrorMessage:   result.ErrorMessage,
	}); insertErr != nil {
		slog.Warn("companion execute compensation action failed", "task_id", taskID.String(), "error", insertErr)
	}
	artifactKind := TaskArtifactConnectorExecution
	if result.Status != connectordomain.ExecSuccess {
		artifactKind = TaskArtifactExecutionError
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  taskID,
		Kind:    artifactKind,
		URI:     result.ExternalRef,
		Payload: payload,
	}); artifactErr != nil {
		slog.Warn("companion execute compensation artifact failed", "task_id", taskID.String(), "error", artifactErr)
	}

	checkpoint := map[string]any{
		"source":           "execute_task_plan_compensation",
		"status":           status,
		"step_id":          step.ID.String(),
		"step_key":         step.StepKey,
		"reason":           reason,
		"nexus_request_id": nexusRequestID.String(),
		"nexus_status":     nexusStatus,
		"compensation":     compensation,
		"execution":        json.RawMessage(payload),
		"verification":     buildVerificationPayload(result, verification),
		"approval_passed":  true,
	}
	updated, err := u.RecordTaskPlanCheckpoint(ctx, taskID, RecordTaskPlanCheckpointInput{
		Status:         planStatus,
		CheckpointJSON: marshalOrEmpty("task_plan_compensation_execution_checkpoint", checkpoint),
		NextAction:     nextAction,
		Blocker:        blocker,
	})
	if err != nil {
		return out, err
	}
	u.syncTaskMemory(ctx, taskID, "execute_compensation")
	return TaskPlanCompensationExecutionOutput{
		Plan:             updated,
		Step:             step,
		Status:           status,
		Reason:           reason,
		Compensation:     compensation,
		NexusRequestID:   nexusRequestID.String(),
		NexusStatus:      nexusStatus,
		Execution:        result,
		Verification:     verification,
		ApprovalRequired: true,
	}, nil
}

func (u *Usecases) recordCompensationCheckpoint(ctx context.Context, taskID uuid.UUID, status string, checkpoint map[string]any, nextAction, blocker string) (domain.TaskPlan, error) {
	updated, err := u.RecordTaskPlanCheckpoint(ctx, taskID, RecordTaskPlanCheckpointInput{
		Status:         status,
		CheckpointJSON: marshalOrEmpty("task_plan_compensation_checkpoint", checkpoint),
		NextAction:     nextAction,
		Blocker:        blocker,
	})
	if err != nil {
		return domain.TaskPlan{}, err
	}
	nexusRequestID := uuidFromAny(checkpoint["nexus_request_id"])
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionPrepareComp,
		NexusRequestID: nexusRequestID,
		Payload: marshalOrEmpty("task_plan_compensation_action", map[string]any{
			"status":           checkpoint["status"],
			"step_id":          checkpoint["step_id"],
			"reason":           checkpoint["reason"],
			"nexus_request_id": checkpoint["nexus_request_id"],
			"nexus_status":     checkpoint["nexus_status"],
			"compensation":     checkpoint["compensation"],
		}),
	}); insertErr != nil {
		slog.Warn("companion prepare compensation action failed", "task_id", taskID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "prepare_compensation")
	return updated, nil
}

func findTaskPlanStep(plan domain.TaskPlan, stepID uuid.UUID) (domain.TaskPlanStep, bool) {
	for _, step := range plan.Steps {
		if step.ID == stepID {
			return step, true
		}
	}
	return domain.TaskPlanStep{}, false
}

func (u *Usecases) latestCompensationAction(ctx context.Context, taskID, stepID uuid.UUID, requestedNexusID string) (domain.TaskAction, string, map[string]any, error) {
	actions, err := u.repo.ListActionsByTaskID(ctx, taskID)
	if err != nil {
		return domain.TaskAction{}, "", nil, err
	}
	requestedNexusID = strings.TrimSpace(requestedNexusID)
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		if action.ActionType != TaskActionPrepareComp {
			continue
		}
		payload := map[string]any{}
		if len(action.Payload) > 0 {
			_ = json.Unmarshal(action.Payload, &payload)
		}
		if strings.TrimSpace(fmt.Sprint(payload["step_id"])) != stepID.String() {
			continue
		}
		actionNexusID := strings.TrimSpace(fmt.Sprint(payload["nexus_request_id"]))
		if action.NexusRequestID != nil {
			actionNexusID = action.NexusRequestID.String()
		}
		if requestedNexusID != "" && actionNexusID != requestedNexusID {
			continue
		}
		compensation, _ := mapAnyFrom(payload["compensation"])
		if len(compensation) == 0 {
			return domain.TaskAction{}, "", nil, fmt.Errorf("prepared compensation action has no compensation payload")
		}
		return action, strings.TrimSpace(fmt.Sprint(payload["reason"])), compensation, nil
	}
	return domain.TaskAction{}, "", nil, ErrNotFound
}

func compensationNexusRequestID(action domain.TaskAction, override string) (uuid.UUID, error) {
	if id, err := uuid.Parse(strings.TrimSpace(override)); err == nil && id != uuid.Nil {
		return id, nil
	}
	if action.NexusRequestID != nil && *action.NexusRequestID != uuid.Nil {
		return *action.NexusRequestID, nil
	}
	var payload map[string]any
	if len(action.Payload) > 0 && json.Unmarshal(action.Payload, &payload) == nil {
		if id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(payload["nexus_request_id"]))); err == nil && id != uuid.Nil {
			return id, nil
		}
	}
	return uuid.Nil, fmt.Errorf("prepared compensation has no nexus_request_id")
}

func compensationFromTaskPlanStepEvidence(raw json.RawMessage) (map[string]any, bool) {
	var evidence map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &evidence) != nil {
		return map[string]any{"supported": false}, false
	}
	if compensation, ok := mapAnyFrom(evidence["compensation"]); ok {
		supported := boolAny(compensation["supported"])
		return compensation, supported
	}
	if metadata, ok := mapAnyFrom(evidence["tool_metadata"]); ok && boolAny(metadata["rollback_supported"]) {
		compensation := map[string]any{
			"supported":      true,
			"capability_id":  strings.TrimSpace(fmt.Sprint(metadata["rollback_capability_id"])),
			"requires_nexus": true,
		}
		return compensation, true
	}
	return map[string]any{"supported": false}, false
}

func compensationExecutionSpec(t domain.Task, step domain.TaskPlanStep, compensation map[string]any, reason string, nexusRequestID *uuid.UUID) (connectordomain.ExecutionSpec, error) {
	originalBinding := originalActionBindingFromTaskPlanStepEvidence(step.EvidenceJSON)
	connectorIDRaw := firstNonEmptyString(
		strings.TrimSpace(fmt.Sprint(compensation["connector_id"])),
		stringFromBinding(originalBinding, "connector_id", ""),
	)
	connectorID, err := uuid.Parse(connectorIDRaw)
	if err != nil || connectorID == uuid.Nil {
		return connectordomain.ExecutionSpec{}, fmt.Errorf("valid compensation connector_id is required")
	}
	operation := firstNonEmptyString(
		strings.TrimSpace(fmt.Sprint(compensation["operation"])),
		strings.TrimSpace(fmt.Sprint(compensation["capability_id"])),
	)
	if operation == "" {
		return connectordomain.ExecutionSpec{}, fmt.Errorf("compensation operation is required")
	}
	payload := compensationExecutionPayload(t, step, compensation, reason, originalBinding)
	return connectordomain.ExecutionSpec{
		ConnectorID:        connectorID,
		OrgID:              t.OrgID,
		ActorID:            CompanionRequesterID,
		ActorType:          "agent",
		CompanionPrincipal: CompanionRequesterID,
		OnBehalfOf:         executionOnBehalfOf(t),
		ServicePrincipal:   true,
		ProductSurface:     firstNonEmptyString(stringFromBinding(originalBinding, "product_surface", ""), "companion"),
		RunID:              t.ID.String(),
		ToolInvocationID:   "compensation:" + step.ID.String(),
		Operation:          operation,
		Payload:            payload,
		IdempotencyKey:     defaultCompensationIdempotencyKey(t.ID, step.ID),
		TaskID:             &t.ID,
		NexusRequestID:     nexusRequestID,
	}, nil
}

func compensationExecutionPayload(t domain.Task, step domain.TaskPlanStep, compensation map[string]any, reason string, originalBinding map[string]any) json.RawMessage {
	payload := map[string]any{
		"org_id":                  t.OrgID,
		"task_id":                 t.ID.String(),
		"plan_step_id":            step.ID.String(),
		"step_key":                step.StepKey,
		"reason":                  reason,
		"compensation":            compensation,
		"original_tool_name":      step.ToolName,
		"original_action_binding": originalBinding,
	}
	if supplied, ok := mapAnyFrom(compensation["payload"]); ok {
		payload = cloneAnyMap(supplied)
		payload["org_id"] = t.OrgID
		payload["task_id"] = t.ID.String()
		payload["plan_step_id"] = step.ID.String()
		if reason != "" {
			payload["reason"] = reason
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func originalActionBindingFromTaskPlanStepEvidence(raw json.RawMessage) map[string]any {
	var evidence map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &evidence) != nil {
		return nil
	}
	if binding, ok := mapAnyFrom(evidence["action_binding"]); ok {
		return cloneAnyMap(binding)
	}
	if toolResult, ok := mapAnyFrom(evidence["tool_result"]); ok {
		if binding, ok := mapAnyFrom(toolResult["action_binding"]); ok {
			return cloneAnyMap(binding)
		}
		if innerEvidence, ok := mapAnyFrom(toolResult["evidence"]); ok {
			if binding, ok := mapAnyFrom(innerEvidence["action_binding"]); ok {
				return cloneAnyMap(binding)
			}
		}
	}
	return nil
}

func mapAnyFrom(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]json.RawMessage:
		out := make(map[string]any, len(typed))
		for key, raw := range typed {
			var decoded any
			if json.Unmarshal(raw, &decoded) == nil {
				out[key] = decoded
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func boolAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultCompensationIdempotencyKey(taskID, stepID uuid.UUID) string {
	return fmt.Sprintf("task-plan-compensation-%s-%s", taskID.String(), stepID.String())
}

func uuidFromAny(value any) *uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func buildTaskPlanStep(orgID string, taskID uuid.UUID, index int, in SetTaskPlanStepInput) (domain.TaskPlanStep, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return domain.TaskPlanStep{}, fmt.Errorf("plan step title is required")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = domain.TaskPlanStepStatusPending
	}
	if !validTaskPlanStepStatus(status) {
		return domain.TaskPlanStep{}, fmt.Errorf("invalid plan step status")
	}
	stepKey := strings.TrimSpace(in.StepKey)
	if stepKey == "" {
		stepKey = fmt.Sprintf("step-%d", index+1)
	}
	sortOrder := in.SortOrder
	if sortOrder == 0 {
		sortOrder = index + 1
	}
	step := domain.TaskPlanStep{
		ID:              in.ID,
		TaskID:          taskID,
		OrgID:           orgID,
		StepKey:         stepKey,
		Title:           title,
		Status:          status,
		DependsOnJSON:   jsonOrDefault(in.DependsOnJSON, `[]`),
		ToolName:        strings.TrimSpace(in.ToolName),
		Capability:      strings.TrimSpace(in.Capability),
		ExpectedOutcome: strings.TrimSpace(in.ExpectedOutcome),
		Postcondition:   strings.TrimSpace(in.Postcondition),
		EvidenceJSON:    jsonOrDefault(in.EvidenceJSON, `{}`),
		Observation:     strings.TrimSpace(in.Observation),
		Blocker:         strings.TrimSpace(in.Blocker),
		ErrorMessage:    strings.TrimSpace(in.ErrorMessage),
		AttemptCount:    in.AttemptCount,
		SortOrder:       sortOrder,
	}
	if isTerminalTaskPlanStepStatus(step.Status) {
		now := time.Now().UTC()
		step.CompletedAt = &now
	}
	return step, nil
}

func validTaskPlanStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.TaskPlanStatusDraft, domain.TaskPlanStatusActive, domain.TaskPlanStatusBlocked,
		domain.TaskPlanStatusCompleted, domain.TaskPlanStatusFailed, domain.TaskPlanStatusEscalated:
		return true
	default:
		return false
	}
}

func validTaskPlanStepStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.TaskPlanStepStatusPending, domain.TaskPlanStepStatusReady, domain.TaskPlanStepStatusRunning,
		domain.TaskPlanStepStatusBlocked, domain.TaskPlanStepStatusDone,
		domain.TaskPlanStepStatusFailed, domain.TaskPlanStepStatusSkipped:
		return true
	default:
		return false
	}
}

func isTerminalTaskPlanStepStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.TaskPlanStepStatusDone, domain.TaskPlanStepStatusFailed, domain.TaskPlanStepStatusSkipped:
		return true
	default:
		return false
	}
}

func jsonOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func nextActionFromSteps(steps []domain.TaskPlanStep) string {
	for _, step := range steps {
		switch step.Status {
		case domain.TaskPlanStepStatusPending, domain.TaskPlanStepStatusReady, domain.TaskPlanStepStatusRunning:
			return step.Title
		case domain.TaskPlanStepStatusBlocked:
			if step.Blocker != "" {
				return "resolve blocker: " + step.Blocker
			}
			return "resolve blocker for " + step.Title
		}
	}
	return "closed"
}

func firstPlanBlocker(steps []domain.TaskPlanStep) string {
	for _, step := range steps {
		if step.Status == domain.TaskPlanStepStatusBlocked && strings.TrimSpace(step.Blocker) != "" {
			return strings.TrimSpace(step.Blocker)
		}
	}
	return ""
}

func statusFromPlanSteps(steps []domain.TaskPlanStep, fallback string) string {
	if len(steps) == 0 {
		return fallback
	}
	allTerminal := true
	hasFailed := false
	hasBlocked := false
	hasRunning := false
	for _, step := range steps {
		switch step.Status {
		case domain.TaskPlanStepStatusFailed:
			hasFailed = true
		case domain.TaskPlanStepStatusBlocked:
			hasBlocked = true
			allTerminal = false
		case domain.TaskPlanStepStatusRunning:
			hasRunning = true
			allTerminal = false
		default:
			if !isTerminalTaskPlanStepStatus(step.Status) {
				allTerminal = false
			}
		}
	}
	switch {
	case hasFailed:
		return domain.TaskPlanStatusFailed
	case hasBlocked:
		return domain.TaskPlanStatusBlocked
	case allTerminal:
		return domain.TaskPlanStatusCompleted
	case hasRunning:
		return domain.TaskPlanStatusActive
	default:
		return domain.TaskPlanStatusActive
	}
}

func applyPlanCompletion(plan *domain.TaskPlan) {
	if plan == nil {
		return
	}
	switch plan.Status {
	case domain.TaskPlanStatusCompleted, domain.TaskPlanStatusFailed, domain.TaskPlanStatusEscalated:
		if plan.CompletedAt == nil {
			now := time.Now().UTC()
			plan.CompletedAt = &now
		}
	default:
		plan.CompletedAt = nil
	}
}

func taskPlanActionPayload(plan domain.TaskPlan) json.RawMessage {
	steps := make([]map[string]any, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		steps = append(steps, map[string]any{
			"id":               step.ID.String(),
			"step_key":         step.StepKey,
			"title":            step.Title,
			"status":           step.Status,
			"tool_name":        step.ToolName,
			"capability":       step.Capability,
			"expected_outcome": step.ExpectedOutcome,
			"postcondition":    step.Postcondition,
			"sort_order":       step.SortOrder,
			"depends_on":       json.RawMessage(step.DependsOnJSON),
			"attempt_count":    step.AttemptCount,
			"completed_at":     formatOptionalTime(step.CompletedAt),
		})
	}
	return marshalOrEmpty("task_plan_action", map[string]any{
		"objective":   plan.Objective,
		"status":      plan.Status,
		"strategy":    plan.Strategy,
		"next_action": plan.NextAction,
		"blocker":     plan.Blocker,
		"steps":       steps,
	})
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
