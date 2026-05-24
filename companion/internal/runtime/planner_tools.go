package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

type TaskPlanner interface {
	GetTaskPlan(ctx context.Context, taskID uuid.UUID) (taskdomain.TaskPlan, error)
	SetTaskPlan(ctx context.Context, taskID uuid.UUID, in PlannerSetTaskPlanInput) (taskdomain.TaskPlan, error)
	UpdateTaskPlanStep(ctx context.Context, taskID, stepID uuid.UUID, in PlannerUpdateTaskPlanStepInput) (taskdomain.TaskPlan, error)
	RecordTaskPlanCheckpoint(ctx context.Context, taskID uuid.UUID, in PlannerRecordTaskPlanCheckpointInput) (taskdomain.TaskPlan, error)
}

const (
	toolSetTaskPlan              = "set_task_plan"
	toolUpdateTaskPlanStep       = "update_task_plan_step"
	toolRecordTaskPlanCheckpoint = "record_task_plan_checkpoint"
	toolExecuteTaskPlanStep      = "execute_task_plan_step"
	toolPrepareTaskPlanComp      = "prepare_task_plan_compensation"
)

type PlannerSetTaskPlanInput struct {
	Objective       string
	Status          string
	Strategy        string
	AssumptionsJSON json.RawMessage
	ConstraintsJSON json.RawMessage
	CheckpointJSON  json.RawMessage
	NextAction      string
	Blocker         string
	CreatedBy       string
	Steps           []PlannerSetTaskPlanStepInput
}

type PlannerSetTaskPlanStepInput struct {
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

type PlannerUpdateTaskPlanStepInput struct {
	Status         string
	EvidenceJSON   json.RawMessage
	Observation    string
	Blocker        string
	ErrorMessage   string
	CheckpointJSON json.RawMessage
	NextAction     string
}

type PlannerRecordTaskPlanCheckpointInput struct {
	Status         string          `json:"status"`
	CheckpointJSON json.RawMessage `json:"checkpoint"`
	NextAction     string          `json:"next_action"`
	Blocker        string          `json:"blocker"`
}

func RegisterTaskPlannerTools(tk *ToolKit, planner TaskPlanner) {
	if tk == nil || planner == nil {
		return
	}
	tk.add(ToolSchema{
		Name:        toolSetTaskPlan,
		Description: "Crea o reemplaza el plan durable de la task actual con objetivo, estrategia, pasos, postconditions, blockers y próxima acción.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"objective":   map[string]any{"type": "string"},
				"status":      map[string]any{"type": "string", "enum": []string{"draft", "active", "blocked", "completed", "failed", "escalated"}},
				"strategy":    map[string]any{"type": "string"},
				"assumptions": map[string]any{"type": "array", "items": map[string]any{}},
				"constraints": map[string]any{"type": "array", "items": map[string]any{}},
				"checkpoint":  map[string]any{"type": "object"},
				"next_action": map[string]any{"type": "string"},
				"blocker":     map[string]any{"type": "string"},
				"steps": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"step_key":         map[string]any{"type": "string"},
							"title":            map[string]any{"type": "string"},
							"status":           map[string]any{"type": "string", "enum": []string{"pending", "ready", "running", "blocked", "done", "failed", "skipped"}},
							"depends_on":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"tool_name":        map[string]any{"type": "string"},
							"capability":       map[string]any{"type": "string"},
							"expected_outcome": map[string]any{"type": "string"},
							"postcondition":    map[string]any{"type": "string"},
							"evidence":         map[string]any{"type": "object"},
							"observation":      map[string]any{"type": "string"},
							"blocker":          map[string]any{"type": "string"},
							"error_message":    map[string]any{"type": "string"},
							"attempt_count":    map[string]any{"type": "integer"},
							"sort_order":       map[string]any{"type": "integer"},
						},
						"required": []string{"title"},
					},
				},
			},
			"required": []string{"steps"},
		},
	}, toolPolicy{RequiresTenant: true, RequiresUser: true, RequiresTask: true}, func(ctx context.Context, args json.RawMessage) (string, error) {
		taskID, ok := taskIDFromContext(ctx)
		if !ok {
			return `{"error":"task context required"}`, nil
		}
		var input plannerSetTaskPlanArgs
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("parse planner args: %w", err)
		}
		id := IdentityFromContext(ctx)
		plan, err := planner.SetTaskPlan(ctx, taskID, input.toInput(id.UserID))
		if err != nil {
			return "", fmt.Errorf("set task plan: %w", err)
		}
		return taskPlanToolResult(plan), nil
	})

	tk.add(ToolSchema{
		Name:        toolUpdateTaskPlanStep,
		Description: "Actualiza estado, evidencia, observación o blocker de un paso del plan durable de la task actual.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"step_id":       map[string]any{"type": "string"},
				"status":        map[string]any{"type": "string", "enum": []string{"pending", "ready", "running", "blocked", "done", "failed", "skipped"}},
				"evidence":      map[string]any{"type": "object"},
				"observation":   map[string]any{"type": "string"},
				"blocker":       map[string]any{"type": "string"},
				"error_message": map[string]any{"type": "string"},
				"checkpoint":    map[string]any{"type": "object"},
				"next_action":   map[string]any{"type": "string"},
			},
			"required": []string{"step_id"},
		},
	}, toolPolicy{RequiresTenant: true, RequiresUser: true, RequiresTask: true}, func(ctx context.Context, args json.RawMessage) (string, error) {
		taskID, ok := taskIDFromContext(ctx)
		if !ok {
			return `{"error":"task context required"}`, nil
		}
		var input plannerUpdateTaskPlanStepArgs
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("parse planner step args: %w", err)
		}
		stepID, err := uuid.Parse(strings.TrimSpace(input.StepID))
		if err != nil {
			return `{"error":"invalid step_id"}`, nil
		}
		plan, err := planner.UpdateTaskPlanStep(ctx, taskID, stepID, input.toInput())
		if err != nil {
			return "", fmt.Errorf("update task plan step: %w", err)
		}
		return taskPlanToolResult(plan), nil
	})

	tk.add(ToolSchema{
		Name:        toolRecordTaskPlanCheckpoint,
		Description: "Registra un checkpoint, próxima acción o blocker del plan durable de la task actual.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":      map[string]any{"type": "string", "enum": []string{"draft", "active", "blocked", "completed", "failed", "escalated"}},
				"checkpoint":  map[string]any{"type": "object"},
				"next_action": map[string]any{"type": "string"},
				"blocker":     map[string]any{"type": "string"},
			},
		},
	}, toolPolicy{RequiresTenant: true, RequiresUser: true, RequiresTask: true}, func(ctx context.Context, args json.RawMessage) (string, error) {
		taskID, ok := taskIDFromContext(ctx)
		if !ok {
			return `{"error":"task context required"}`, nil
		}
		var input PlannerRecordTaskPlanCheckpointInput
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("parse planner checkpoint args: %w", err)
		}
		plan, err := planner.RecordTaskPlanCheckpoint(ctx, taskID, input)
		if err != nil {
			return "", fmt.Errorf("record task plan checkpoint: %w", err)
		}
		return taskPlanToolResult(plan), nil
	})

	tk.add(ToolSchema{
		Name:        toolExecuteTaskPlanStep,
		Description: "Ejecuta de forma durable un paso del plan de la task actual: marca running, invoca una tool permitida, verifica resultado estructural y actualiza evidencia/checkpoint.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"step_id":         map[string]any{"type": "string", "description": "ID o step_key del paso a ejecutar. Si se omite, se elige el próximo paso ejecutable."},
				"tool_name":       map[string]any{"type": "string", "description": "Tool objetivo. Debe coincidir con el tool_name del paso si el paso ya lo declara."},
				"tool_args":       map[string]any{"type": "object", "description": "Argumentos para la tool objetivo."},
				"retry":           map[string]any{"type": "boolean", "description": "Permite reintentar explícitamente un paso failed o blocked."},
				"retry_reason":    map[string]any{"type": "string", "description": "Motivo operacional del retry."},
				"replay_only":     map[string]any{"type": "boolean", "description": "No ejecuta tools; devuelve evidencia/checkpoint existente para replay auditado."},
				"idempotency_key": map[string]any{"type": "string", "description": "Override excepcional de idempotency key. Si se omite, Companion genera una key determinística por task/step/attempt."},
			},
		},
	}, toolPolicy{RequiresTenant: true, RequiresUser: true, RequiresTask: true}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return executeTaskPlanStepTool(ctx, tk, planner, args)
	})

	tk.add(ToolSchema{
		Name:        toolPrepareTaskPlanComp,
		Description: "Prepara una compensación/rollback gobernado para un paso ejecutado. No ejecuta rollback: registra intención, evidencia y requirement de aprobación.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"step_id": map[string]any{"type": "string", "description": "ID o step_key del paso a compensar."},
				"reason":  map[string]any{"type": "string", "description": "Motivo de negocio/operación para preparar compensación."},
			},
			"required": []string{"step_id", "reason"},
		},
	}, toolPolicy{RequiresTenant: true, RequiresUser: true, RequiresTask: true}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return prepareTaskPlanCompensationTool(ctx, planner, args)
	})
}

type plannerSetTaskPlanArgs struct {
	Objective   string                   `json:"objective"`
	Status      string                   `json:"status"`
	Strategy    string                   `json:"strategy"`
	Assumptions json.RawMessage          `json:"assumptions"`
	Constraints json.RawMessage          `json:"constraints"`
	Checkpoint  json.RawMessage          `json:"checkpoint"`
	NextAction  string                   `json:"next_action"`
	Blocker     string                   `json:"blocker"`
	Steps       []plannerSetTaskStepArgs `json:"steps"`
}

type plannerSetTaskStepArgs struct {
	ID              string          `json:"id"`
	StepKey         string          `json:"step_key"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	DependsOn       json.RawMessage `json:"depends_on"`
	ToolName        string          `json:"tool_name"`
	Capability      string          `json:"capability"`
	ExpectedOutcome string          `json:"expected_outcome"`
	Postcondition   string          `json:"postcondition"`
	Evidence        json.RawMessage `json:"evidence"`
	Observation     string          `json:"observation"`
	Blocker         string          `json:"blocker"`
	ErrorMessage    string          `json:"error_message"`
	AttemptCount    int             `json:"attempt_count"`
	SortOrder       int             `json:"sort_order"`
}

type plannerUpdateTaskPlanStepArgs struct {
	StepID       string          `json:"step_id"`
	Status       string          `json:"status"`
	Evidence     json.RawMessage `json:"evidence"`
	Observation  string          `json:"observation"`
	Blocker      string          `json:"blocker"`
	ErrorMessage string          `json:"error_message"`
	Checkpoint   json.RawMessage `json:"checkpoint"`
	NextAction   string          `json:"next_action"`
}

type plannerExecuteTaskPlanStepArgs struct {
	StepID         string          `json:"step_id"`
	ToolName       string          `json:"tool_name"`
	ToolArgs       json.RawMessage `json:"tool_args"`
	Retry          bool            `json:"retry"`
	RetryReason    string          `json:"retry_reason"`
	ReplayOnly     bool            `json:"replay_only"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type plannerPrepareCompensationArgs struct {
	StepID string `json:"step_id"`
	Reason string `json:"reason"`
}

func (a plannerSetTaskPlanArgs) toInput(createdBy string) PlannerSetTaskPlanInput {
	out := PlannerSetTaskPlanInput{
		Objective:       a.Objective,
		Status:          a.Status,
		Strategy:        a.Strategy,
		AssumptionsJSON: a.Assumptions,
		ConstraintsJSON: a.Constraints,
		CheckpointJSON:  a.Checkpoint,
		NextAction:      a.NextAction,
		Blocker:         a.Blocker,
		CreatedBy:       createdBy,
		Steps:           make([]PlannerSetTaskPlanStepInput, 0, len(a.Steps)),
	}
	for _, step := range a.Steps {
		var stepID uuid.UUID
		if strings.TrimSpace(step.ID) != "" {
			stepID, _ = uuid.Parse(strings.TrimSpace(step.ID))
		}
		out.Steps = append(out.Steps, PlannerSetTaskPlanStepInput{
			ID:              stepID,
			StepKey:         step.StepKey,
			Title:           step.Title,
			Status:          step.Status,
			DependsOnJSON:   step.DependsOn,
			ToolName:        step.ToolName,
			Capability:      step.Capability,
			ExpectedOutcome: step.ExpectedOutcome,
			Postcondition:   step.Postcondition,
			EvidenceJSON:    step.Evidence,
			Observation:     step.Observation,
			Blocker:         step.Blocker,
			ErrorMessage:    step.ErrorMessage,
			AttemptCount:    step.AttemptCount,
			SortOrder:       step.SortOrder,
		})
	}
	return out
}

func (a plannerUpdateTaskPlanStepArgs) toInput() PlannerUpdateTaskPlanStepInput {
	return PlannerUpdateTaskPlanStepInput{
		Status:         a.Status,
		EvidenceJSON:   a.Evidence,
		Observation:    a.Observation,
		Blocker:        a.Blocker,
		ErrorMessage:   a.ErrorMessage,
		CheckpointJSON: a.Checkpoint,
		NextAction:     a.NextAction,
	}
}

func executeTaskPlanStepTool(ctx context.Context, tk *ToolKit, planner TaskPlanner, args json.RawMessage) (string, error) {
	taskID, ok := taskIDFromContext(ctx)
	if !ok {
		return `{"error":"task context required"}`, nil
	}
	var input plannerExecuteTaskPlanStepArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parse planner execute args: %w", err)
	}
	plan, err := planner.GetTaskPlan(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("get task plan: %w", err)
	}
	if input.ReplayOnly {
		step, replayErr := pickReplayPlanStep(plan, input.StepID)
		if replayErr != "" {
			return taskPlanReplayResult(plan, taskdomain.TaskPlanStep{}, replayErr), nil
		}
		return taskPlanReplayResult(plan, step, ""), nil
	}
	step, pickErr := pickExecutablePlanStep(plan, input.StepID, input.Retry)
	if pickErr != "" {
		planStatus := taskdomain.TaskPlanStatusBlocked
		blocker := pickErr
		if planStepsAllTerminal(plan.Steps) {
			planStatus = taskdomain.TaskPlanStatusCompleted
			blocker = ""
		}
		checkpoint := taskPlanCheckpointJSON(map[string]any{
			"source": "execute_task_plan_step",
			"status": planStatus,
			"reason": pickErr,
		})
		updated, err := planner.RecordTaskPlanCheckpoint(ctx, taskID, PlannerRecordTaskPlanCheckpointInput{
			Status:         planStatus,
			CheckpointJSON: checkpoint,
			Blocker:        blocker,
		})
		if err != nil {
			return "", fmt.Errorf("record no executable step checkpoint: %w", err)
		}
		return taskPlanExecutionResult(updated, uuid.Nil, "", planStatus, pickErr), nil
	}

	execMeta := planStepExecutionMeta{
		AttemptNumber:  step.AttemptCount + 1,
		Retry:          input.Retry,
		RetryReason:    strings.TrimSpace(input.RetryReason),
		PreviousStatus: step.Status,
	}
	targetTool := strings.TrimSpace(input.ToolName)
	if targetTool == "" {
		targetTool = strings.TrimSpace(step.ToolName)
	}
	if step.ToolName != "" && targetTool != "" && targetTool != step.ToolName {
		reason := fmt.Sprintf("requested tool %q does not match step tool_name %q", targetTool, step.ToolName)
		updated, err := updatePlanStepBlocked(ctx, planner, taskID, step, reason)
		if err != nil {
			return "", err
		}
		return taskPlanExecutionResult(updated, step.ID, targetTool, taskdomain.TaskPlanStepStatusBlocked, reason), nil
	}
	if targetTool == "" {
		reason := "plan step has no tool_name and execute_task_plan_step did not receive tool_name"
		updated, err := updatePlanStepBlocked(ctx, planner, taskID, step, reason)
		if err != nil {
			return "", err
		}
		return taskPlanExecutionResult(updated, step.ID, "", taskdomain.TaskPlanStepStatusBlocked, reason), nil
	}
	if reason := validateNestedPlanTool(ctx, tk, targetTool); reason != "" {
		updated, err := updatePlanStepBlocked(ctx, planner, taskID, step, reason)
		if err != nil {
			return "", err
		}
		return taskPlanExecutionResult(updated, step.ID, targetTool, taskdomain.TaskPlanStepStatusBlocked, reason), nil
	}
	metadata, metadataOK := tk.ToolMetadata(targetTool)
	toolArgs, argsErr := planStepToolArgs(input.ToolArgs, step.EvidenceJSON)
	if argsErr != "" {
		updated, err := updatePlanStepFailed(ctx, planner, taskID, step, targetTool, argsErr, json.RawMessage(`{}`), 0)
		if err != nil {
			return "", err
		}
		return taskPlanExecutionResult(updated, step.ID, targetTool, taskdomain.TaskPlanStepStatusFailed, argsErr), nil
	}

	runningCheckpoint := taskPlanCheckpointJSON(map[string]any{
		"source":          "execute_task_plan_step",
		"status":          taskdomain.TaskPlanStepStatusRunning,
		"step_id":         step.ID.String(),
		"tool_name":       targetTool,
		"attempt_number":  execMeta.AttemptNumber,
		"retry":           execMeta.Retry,
		"retry_reason":    execMeta.RetryReason,
		"previous_status": execMeta.PreviousStatus,
	})
	if _, err := planner.UpdateTaskPlanStep(ctx, taskID, step.ID, PlannerUpdateTaskPlanStepInput{
		Status:         taskdomain.TaskPlanStepStatusRunning,
		Observation:    "Executing " + targetTool,
		CheckpointJSON: runningCheckpoint,
		NextAction:     step.Title,
	}); err != nil {
		return "", fmt.Errorf("mark plan step running: %w", err)
	}

	idempotencyKey := planStepAttemptIdempotencyKey(taskID, step.ID, targetTool, execMeta.AttemptNumber, input.Retry, input.IdempotencyKey)
	toolCtx := WithPlanStepExecution(ctx, step.ID, idempotencyKey)
	started := time.Now()
	result := tk.ExecuteTool(toolCtx, targetTool, toolArgs)
	durationMS := time.Since(started).Milliseconds()
	verification := verifyPlanStepToolResult(step, result, metadata, metadataOK)
	evidence := planStepEvidenceJSON(step, targetTool, toolArgs, result, idempotencyKey, durationMS, verification, execMeta)

	var updated taskdomain.TaskPlan
	switch verification.Status {
	case taskdomain.TaskPlanStepStatusDone:
		updated, err = planner.UpdateTaskPlanStep(ctx, taskID, step.ID, PlannerUpdateTaskPlanStepInput{
			Status:         taskdomain.TaskPlanStepStatusDone,
			EvidenceJSON:   evidence,
			Observation:    verification.Summary,
			CheckpointJSON: planStepCheckpointJSON(step, targetTool, verification, execMeta),
		})
	case taskdomain.TaskPlanStepStatusBlocked:
		updated, err = planner.UpdateTaskPlanStep(ctx, taskID, step.ID, PlannerUpdateTaskPlanStepInput{
			Status:         taskdomain.TaskPlanStepStatusBlocked,
			EvidenceJSON:   evidence,
			Observation:    verification.Summary,
			Blocker:        verification.Summary,
			CheckpointJSON: planStepCheckpointJSON(step, targetTool, verification, execMeta),
		})
	default:
		updated, err = planner.UpdateTaskPlanStep(ctx, taskID, step.ID, PlannerUpdateTaskPlanStepInput{
			Status:         taskdomain.TaskPlanStepStatusFailed,
			EvidenceJSON:   evidence,
			Observation:    verification.Summary,
			ErrorMessage:   verification.Summary,
			CheckpointJSON: planStepCheckpointJSON(step, targetTool, verification, execMeta),
		})
	}
	if err != nil {
		return "", fmt.Errorf("update executed plan step: %w", err)
	}
	return taskPlanExecutionResult(updated, step.ID, targetTool, verification.Status, verification.Summary), nil
}

func prepareTaskPlanCompensationTool(ctx context.Context, planner TaskPlanner, args json.RawMessage) (string, error) {
	taskID, ok := taskIDFromContext(ctx)
	if !ok {
		return `{"error":"task context required"}`, nil
	}
	var input plannerPrepareCompensationArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parse compensation args: %w", err)
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return `{"error":"reason is required"}`, nil
	}
	plan, err := planner.GetTaskPlan(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("get task plan: %w", err)
	}
	step, pickErr := pickReplayPlanStep(plan, input.StepID)
	if pickErr != "" {
		return taskPlanReplayResult(plan, taskdomain.TaskPlanStep{}, pickErr), nil
	}
	compensation, supported := compensationFromStepEvidence(step.EvidenceJSON)
	status := "compensation_unavailable"
	planStatus := taskdomain.TaskPlanStatusBlocked
	blocker := "compensation is not declared for this step"
	nextAction := "review step manually"
	if supported {
		status = "compensation_prepared"
		planStatus = taskdomain.TaskPlanStatusEscalated
		blocker = "compensation requires governance approval: " + reason
		nextAction = "request governance approval for compensation"
	}
	checkpoint := taskPlanCheckpointJSON(map[string]any{
		"source":              "prepare_task_plan_compensation",
		"status":              status,
		"step_id":             step.ID.String(),
		"step_key":            step.StepKey,
		"reason":              reason,
		"governance_required": true,
		"compensation":        compensation,
	})
	updated, err := planner.RecordTaskPlanCheckpoint(ctx, taskID, PlannerRecordTaskPlanCheckpointInput{
		Status:         planStatus,
		CheckpointJSON: checkpoint,
		NextAction:     nextAction,
		Blocker:        blocker,
	})
	if err != nil {
		return "", fmt.Errorf("record compensation checkpoint: %w", err)
	}
	return taskPlanCompensationResult(updated, step, status, reason, compensation), nil
}

type planStepVerification struct {
	Status           string
	Summary          string
	RequiredEvidence []string
	MissingEvidence  []string
	ToolMetadata     *ToolMetadata
}

type planStepExecutionMeta struct {
	AttemptNumber  int
	Retry          bool
	RetryReason    string
	PreviousStatus string
}

func pickExecutablePlanStep(plan taskdomain.TaskPlan, requested string, retry bool) (taskdomain.TaskPlanStep, string) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		for _, step := range plan.Steps {
			if step.ID.String() == requested || strings.EqualFold(step.StepKey, requested) {
				if isTerminalPlanStepStatus(step.Status) {
					if retry && step.Status == taskdomain.TaskPlanStepStatusFailed {
						if ok, missing := planStepDependenciesSatisfied(step, plan.Steps); !ok {
							return taskdomain.TaskPlanStep{}, "plan step dependencies not completed: " + strings.Join(missing, ", ")
						}
						return step, ""
					}
					if step.Status == taskdomain.TaskPlanStepStatusFailed {
						return taskdomain.TaskPlanStep{}, "requested plan step is failed; set retry=true to retry it"
					}
					return taskdomain.TaskPlanStep{}, "requested plan step is already terminal"
				}
				if ok, missing := planStepDependenciesSatisfied(step, plan.Steps); !ok {
					return taskdomain.TaskPlanStep{}, "plan step dependencies not completed: " + strings.Join(missing, ", ")
				}
				return step, ""
			}
		}
		return taskdomain.TaskPlanStep{}, "requested plan step not found"
	}
	statuses := []string{taskdomain.TaskPlanStepStatusReady, taskdomain.TaskPlanStepStatusPending, taskdomain.TaskPlanStepStatusRunning}
	if retry {
		statuses = append(statuses, taskdomain.TaskPlanStepStatusBlocked, taskdomain.TaskPlanStepStatusFailed)
	}
	for _, status := range statuses {
		for _, step := range plan.Steps {
			if step.Status != status {
				continue
			}
			if ok, _ := planStepDependenciesSatisfied(step, plan.Steps); !ok {
				continue
			}
			return step, ""
		}
	}
	if len(plan.Steps) == 0 {
		return taskdomain.TaskPlanStep{}, "task plan has no steps"
	}
	return taskdomain.TaskPlanStep{}, "no executable plan step is ready"
}

func pickReplayPlanStep(plan taskdomain.TaskPlan, requested string) (taskdomain.TaskPlanStep, string) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		for _, step := range plan.Steps {
			if step.ID.String() == requested || strings.EqualFold(step.StepKey, requested) {
				return step, ""
			}
		}
		return taskdomain.TaskPlanStep{}, "requested plan step not found"
	}
	for i := len(plan.Steps) - 1; i >= 0; i-- {
		if strings.TrimSpace(string(plan.Steps[i].EvidenceJSON)) != "" && strings.TrimSpace(string(plan.Steps[i].EvidenceJSON)) != "{}" {
			return plan.Steps[i], ""
		}
	}
	return taskdomain.TaskPlanStep{}, "no plan step evidence is available for replay"
}

func planStepDependenciesSatisfied(step taskdomain.TaskPlanStep, steps []taskdomain.TaskPlanStep) (bool, []string) {
	var refs []string
	if len(step.DependsOnJSON) > 0 {
		_ = json.Unmarshal(step.DependsOnJSON, &refs)
	}
	if len(refs) == 0 {
		return true, nil
	}
	byRef := make(map[string]taskdomain.TaskPlanStep, len(steps)*2)
	for _, candidate := range steps {
		byRef[candidate.ID.String()] = candidate
		if candidate.StepKey != "" {
			byRef[candidate.StepKey] = candidate
		}
	}
	var missing []string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		dep, ok := byRef[ref]
		if !ok || (dep.Status != taskdomain.TaskPlanStepStatusDone && dep.Status != taskdomain.TaskPlanStepStatusSkipped) {
			missing = append(missing, ref)
		}
	}
	return len(missing) == 0, missing
}

func validateNestedPlanTool(ctx context.Context, tk *ToolKit, targetTool string) string {
	if tk == nil {
		return "toolkit is not configured"
	}
	if _, ok := tk.Handlers[targetTool]; !ok {
		return "target tool is not registered"
	}
	if isPlannerControlTool(targetTool) {
		return "planner control tools cannot be executed as plan step targets"
	}
	id := IdentityFromContext(ctx)
	if !allowedToolName(id.AllowedTools, targetTool) {
		return "target tool is not allowed for the current agent route"
	}
	chain := IdentityChain{
		InitiatingUser:     id.UserID,
		Tenant:             id.OrgID,
		CustomerOrgID:      id.OrgID,
		HumanUserID:        id.UserID,
		ActorType:          id.ActorType,
		ProductSurface:     productSurfaceFromIdentity(id),
		TaskID:             id.TaskID,
		AuthScopes:         append([]string(nil), id.AuthScopes...),
		CompanionPrincipal: firstNonEmpty(id.CompanionPrincipal, CompanionPrincipal),
		OnBehalfOf:         id.OnBehalfOf,
		ServicePrincipal:   id.ServicePrincipal,
	}
	if !tk.CanUseTool(targetTool, chain) {
		return "target tool requires customer org, user, task, or scopes not present in this request"
	}
	return ""
}

func isPlannerControlTool(name string) bool {
	switch strings.TrimSpace(name) {
	case toolSetTaskPlan, toolUpdateTaskPlanStep, toolRecordTaskPlanCheckpoint, toolExecuteTaskPlanStep, toolPrepareTaskPlanComp:
		return true
	default:
		return false
	}
}

func allowedToolName(allowed []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range allowed {
		item = strings.TrimSpace(item)
		if item == target {
			return true
		}
		if strings.HasSuffix(item, "*") && strings.HasPrefix(target, strings.TrimSuffix(item, "*")) {
			return true
		}
	}
	return false
}

func planStepToolArgs(inputArgs, evidence json.RawMessage) (json.RawMessage, string) {
	if raw := normalizeToolArgs(inputArgs); len(raw) > 0 {
		return raw, ""
	}
	var payload map[string]json.RawMessage
	if len(evidence) > 0 && json.Unmarshal(evidence, &payload) == nil {
		if raw := normalizeToolArgs(payload["tool_args"]); len(raw) > 0 {
			return raw, ""
		}
	}
	return json.RawMessage(`{}`), ""
}

func normalizeToolArgs(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil
	}
	return json.RawMessage(trimmed)
}

func verifyPlanStepToolResult(step taskdomain.TaskPlanStep, result string, metadata ToolMetadata, hasMetadata bool) planStepVerification {
	verification := planStepVerification{}
	if hasMetadata {
		metadata.EvidenceRequired = cleanStringList(metadata.EvidenceRequired)
		verification.RequiredEvidence = append([]string(nil), metadata.EvidenceRequired...)
		metadataCopy := metadata
		verification.ToolMetadata = &metadataCopy
	}
	result = strings.TrimSpace(result)
	if result == "" {
		verification.Status = taskdomain.TaskPlanStepStatusFailed
		verification.Summary = "target tool returned an empty result"
		return verification
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		if strings.TrimSpace(step.Postcondition) == "" {
			verification.Status = taskdomain.TaskPlanStepStatusDone
			verification.Summary = "target tool completed with a non-json result"
			return verification
		}
		verification.Status = taskdomain.TaskPlanStepStatusBlocked
		verification.Summary = "postcondition could not be verified because target tool returned non-json output"
		return verification
	}
	if value, ok := payload["error"]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
		verification.Status = taskdomain.TaskPlanStepStatusFailed
		verification.Summary = "target tool returned error: " + fmt.Sprint(value)
		return verification
	}
	if value, ok := payload["status"]; ok {
		status := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
		switch status {
		case "pending_approval", "pending":
			verification.Status = taskdomain.TaskPlanStepStatusBlocked
			verification.Summary = "target tool is waiting for approval"
			return verification
		case "denied", "rejected", "execution_failed", "failed", "failure":
			verification.Status = taskdomain.TaskPlanStepStatusFailed
			verification.Summary = "target tool reported status " + status
			return verification
		}
	}
	if len(verification.RequiredEvidence) > 0 {
		verification.MissingEvidence = missingEvidenceFields(payload, verification.RequiredEvidence)
		if len(verification.MissingEvidence) > 0 {
			verification.Status = taskdomain.TaskPlanStepStatusBlocked
			verification.Summary = "evidence contract missing required fields: " + strings.Join(verification.MissingEvidence, ", ")
			return verification
		}
		verification.Status = taskdomain.TaskPlanStepStatusDone
		verification.Summary = "target tool completed and evidence contract verified"
		return verification
	}
	if strings.TrimSpace(step.Postcondition) == "" || resultHasVerificationEvidence(payload) {
		verification.Status = taskdomain.TaskPlanStepStatusDone
		verification.Summary = "target tool completed and structural verification passed"
		return verification
	}
	verification.Status = taskdomain.TaskPlanStepStatusBlocked
	verification.Summary = "postcondition could not be verified from target tool result"
	return verification
}

func resultHasVerificationEvidence(payload map[string]any) bool {
	for _, key := range []string{"evidence", "external_ref", "result", "results", "data", "items", "status"} {
		if value, ok := payload[key]; ok && fmt.Sprint(value) != "" && fmt.Sprint(value) != "<nil>" {
			return true
		}
	}
	return false
}

func missingEvidenceFields(payload map[string]any, required []string) []string {
	var missing []string
	for _, field := range cleanStringList(required) {
		if !evidenceFieldExists(payload, field) {
			missing = append(missing, field)
		}
	}
	return missing
}

func evidenceFieldExists(payload map[string]any, field string) bool {
	field = strings.TrimSpace(field)
	if field == "" {
		return true
	}
	if strings.Contains(field, ".") {
		if value, ok := valueAtPath(payload, strings.Split(field, ".")); ok && evidenceValuePresent(value) {
			return true
		}
	}
	return recursiveEvidenceKeyExists(payload, field)
}

func valueAtPath(value any, parts []string) (any, bool) {
	if len(parts) == 0 {
		return value, true
	}
	current, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	next, ok := current[parts[0]]
	if !ok {
		return nil, false
	}
	return valueAtPath(next, parts[1:])
}

func recursiveEvidenceKeyExists(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if found, ok := typed[key]; ok && evidenceValuePresent(found) {
			return true
		}
		for _, nested := range typed {
			if recursiveEvidenceKeyExists(nested, key) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if recursiveEvidenceKeyExists(nested, key) {
				return true
			}
		}
	}
	return false
}

func evidenceValuePresent(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return typed != nil
	case map[string]any:
		return typed != nil
	default:
		return true
	}
}

func compensationFromStepEvidence(raw json.RawMessage) (map[string]any, bool) {
	var evidence map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &evidence) != nil {
		return map[string]any{"supported": false}, false
	}
	if compensation, ok := evidence["compensation"].(map[string]any); ok {
		supported := boolFromAny(compensation["supported"])
		return compensation, supported
	}
	if metadata, ok := evidence["tool_metadata"].(map[string]any); ok && boolFromAny(metadata["rollback_supported"]) {
		compensation := map[string]any{
			"supported":      true,
			"capability_id":  strings.TrimSpace(fmt.Sprint(metadata["rollback_capability_id"])),
			"requires_nexus": true,
		}
		return compensation, true
	}
	return map[string]any{"supported": false}, false
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func updatePlanStepBlocked(ctx context.Context, planner TaskPlanner, taskID uuid.UUID, step taskdomain.TaskPlanStep, reason string) (taskdomain.TaskPlan, error) {
	checkpoint := taskPlanCheckpointJSON(map[string]any{
		"source":  "execute_task_plan_step",
		"status":  taskdomain.TaskPlanStepStatusBlocked,
		"step_id": step.ID.String(),
		"reason":  reason,
	})
	updated, err := planner.UpdateTaskPlanStep(ctx, taskID, step.ID, PlannerUpdateTaskPlanStepInput{
		Status:         taskdomain.TaskPlanStepStatusBlocked,
		Observation:    reason,
		Blocker:        reason,
		CheckpointJSON: checkpoint,
	})
	if err != nil {
		return taskdomain.TaskPlan{}, fmt.Errorf("block plan step: %w", err)
	}
	return updated, nil
}

func updatePlanStepFailed(ctx context.Context, planner TaskPlanner, taskID uuid.UUID, step taskdomain.TaskPlanStep, toolName, reason string, toolArgs json.RawMessage, durationMS int64) (taskdomain.TaskPlan, error) {
	verification := planStepVerification{Status: taskdomain.TaskPlanStepStatusFailed, Summary: reason}
	errResult, _ := json.Marshal(map[string]any{"error": reason})
	evidence := planStepEvidenceJSON(step, toolName, toolArgs, string(errResult), planStepIdempotencyKey(taskID, step.ID, toolName), durationMS, verification, planStepExecutionMeta{})
	updated, err := planner.UpdateTaskPlanStep(ctx, taskID, step.ID, PlannerUpdateTaskPlanStepInput{
		Status:         taskdomain.TaskPlanStepStatusFailed,
		EvidenceJSON:   evidence,
		Observation:    reason,
		ErrorMessage:   reason,
		CheckpointJSON: planStepCheckpointJSON(step, toolName, verification),
	})
	if err != nil {
		return taskdomain.TaskPlan{}, fmt.Errorf("fail plan step: %w", err)
	}
	return updated, nil
}

func planStepIdempotencyKey(taskID, stepID uuid.UUID, toolName string) string {
	return fmt.Sprintf("task-plan-step-%s-%s-%s", taskID.String(), stepID.String(), strings.TrimSpace(toolName))
}

func planStepAttemptIdempotencyKey(taskID, stepID uuid.UUID, toolName string, attemptNumber int, retry bool, override string) string {
	if value := strings.TrimSpace(override); value != "" {
		return value
	}
	base := planStepIdempotencyKey(taskID, stepID, toolName)
	if attemptNumber <= 1 && !retry {
		return base
	}
	if retry {
		return fmt.Sprintf("%s-retry-%d", base, maxInt(attemptNumber, 1))
	}
	return fmt.Sprintf("%s-attempt-%d", base, maxInt(attemptNumber, 1))
}

func planStepEvidenceJSON(step taskdomain.TaskPlanStep, toolName string, toolArgs json.RawMessage, toolResult string, idempotencyKey string, durationMS int64, verification planStepVerification, execMeta planStepExecutionMeta) json.RawMessage {
	payload := map[string]any{
		"source":          "execute_task_plan_step",
		"step_id":         step.ID.String(),
		"step_key":        step.StepKey,
		"tool_name":       toolName,
		"tool_args":       rawJSONOrString(toolArgs),
		"tool_result":     rawJSONOrString(json.RawMessage(toolResult)),
		"idempotency_key": idempotencyKey,
		"duration_ms":     durationMS,
		"attempt_number":  execMeta.AttemptNumber,
		"retry":           execMeta.Retry,
		"retry_reason":    execMeta.RetryReason,
		"previous_status": execMeta.PreviousStatus,
		"verification": map[string]any{
			"status":            verification.Status,
			"summary":           verification.Summary,
			"postcondition":     step.Postcondition,
			"required_evidence": verification.RequiredEvidence,
			"missing_evidence":  verification.MissingEvidence,
		},
	}
	if verification.ToolMetadata != nil {
		payload["tool_metadata"] = verification.ToolMetadata
		if verification.ToolMetadata.RollbackSupported {
			payload["compensation"] = map[string]any{
				"supported":      true,
				"capability_id":  verification.ToolMetadata.RollbackCapabilityID,
				"requires_nexus": true,
			}
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func planStepCheckpointJSON(step taskdomain.TaskPlanStep, toolName string, verification planStepVerification, meta ...planStepExecutionMeta) json.RawMessage {
	payload := map[string]any{
		"source":                "execute_task_plan_step",
		"last_executed_step_id": step.ID.String(),
		"last_step_key":         step.StepKey,
		"tool_name":             toolName,
		"status":                verification.Status,
		"summary":               verification.Summary,
		"missing_evidence":      verification.MissingEvidence,
		"recorded_at":           time.Now().UTC().Format(time.RFC3339),
	}
	if len(meta) > 0 {
		payload["attempt_number"] = meta[0].AttemptNumber
		payload["retry"] = meta[0].Retry
		payload["retry_reason"] = meta[0].RetryReason
		payload["previous_status"] = meta[0].PreviousStatus
	}
	return taskPlanCheckpointJSON(payload)
}

func taskPlanCheckpointJSON(payload map[string]any) json.RawMessage {
	if _, ok := payload["recorded_at"]; !ok {
		payload["recorded_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func rawJSONOrString(raw json.RawMessage) any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return trimmed
	}
	return json.RawMessage(trimmed)
}

func isTerminalPlanStepStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case taskdomain.TaskPlanStepStatusDone, taskdomain.TaskPlanStepStatusFailed, taskdomain.TaskPlanStepStatusSkipped:
		return true
	default:
		return false
	}
}

func planStepsAllTerminal(steps []taskdomain.TaskPlanStep) bool {
	if len(steps) == 0 {
		return false
	}
	for _, step := range steps {
		if !isTerminalPlanStepStatus(step.Status) {
			return false
		}
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func taskIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id := IdentityFromContext(ctx)
	taskID, err := uuid.Parse(strings.TrimSpace(id.TaskID))
	if err != nil || taskID == uuid.Nil {
		return uuid.Nil, false
	}
	return taskID, true
}

func taskPlanToolResult(plan taskdomain.TaskPlan) string {
	result := map[string]any{
		"task_id":     plan.TaskID.String(),
		"objective":   plan.Objective,
		"status":      plan.Status,
		"next_action": plan.NextAction,
		"blocker":     plan.Blocker,
		"steps":       len(plan.Steps),
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"result":"task plan updated"}`
	}
	return string(raw)
}

func taskPlanExecutionResult(plan taskdomain.TaskPlan, stepID uuid.UUID, toolName, status, summary string) string {
	result := map[string]any{
		"task_id":     plan.TaskID.String(),
		"plan_status": plan.Status,
		"next_action": plan.NextAction,
		"blocker":     plan.Blocker,
		"step_status": status,
		"summary":     summary,
	}
	if stepID != uuid.Nil {
		result["step_id"] = stepID.String()
	}
	if toolName != "" {
		result["tool_name"] = toolName
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"result":"task plan step execution recorded"}`
	}
	return string(raw)
}

func taskPlanReplayResult(plan taskdomain.TaskPlan, step taskdomain.TaskPlanStep, errMsg string) string {
	result := map[string]any{
		"task_id":     plan.TaskID.String(),
		"plan_status": plan.Status,
		"replay_only": true,
	}
	if errMsg != "" {
		result["error"] = errMsg
	} else {
		result["step_id"] = step.ID.String()
		result["step_key"] = step.StepKey
		result["step_status"] = step.Status
		result["attempt_count"] = step.AttemptCount
		result["observation"] = step.Observation
		result["blocker"] = step.Blocker
		result["error_message"] = step.ErrorMessage
		result["evidence"] = rawJSONOrString(step.EvidenceJSON)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"result":"task plan step replay unavailable"}`
	}
	return string(raw)
}

func taskPlanCompensationResult(plan taskdomain.TaskPlan, step taskdomain.TaskPlanStep, status, reason string, compensation map[string]any) string {
	result := map[string]any{
		"task_id":             plan.TaskID.String(),
		"plan_status":         plan.Status,
		"step_id":             step.ID.String(),
		"step_status":         step.Status,
		"status":              status,
		"reason":              reason,
		"governance_required": true,
		"compensation":        compensation,
		"next_action":         plan.NextAction,
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"result":"task plan compensation checkpoint recorded"}`
	}
	return string(raw)
}
