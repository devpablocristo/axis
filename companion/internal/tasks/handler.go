package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/devpablocristo/platform/security/go/tenant"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/nexusclient"
	tasksdto "github.com/devpablocristo/companion/internal/tasks/handler/dto"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

const scopeCompanionCrossOrg = "companion:cross_org"

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

type taskUsecase interface {
	Create(ctx context.Context, in CreateTaskInput) (domain.Task, error)
	List(ctx context.Context, orgID tenant.ID, limit int) ([]domain.Task, error)
	ListAll(ctx context.Context, limit int) ([]domain.Task, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Task, error)
	GetDetail(ctx context.Context, id uuid.UUID) (TaskDetail, error)
	AddMessage(ctx context.Context, taskID uuid.UUID, in AddMessageInput) (domain.TaskMessage, error)
	Investigate(ctx context.Context, taskID uuid.UUID, in InvestigateInput) (domain.Task, error)
	Propose(ctx context.Context, taskID uuid.UUID, in ProposeInput) (domain.Task, domain.TaskAction, nexusclient.SubmitResponse, error)
	SetTaskPlan(ctx context.Context, taskID uuid.UUID, in SetTaskPlanInput) (domain.TaskPlan, error)
	ListTaskExecutionGraph(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskExecutionGraphEvent, error)
	UpdateTaskPlanStep(ctx context.Context, taskID, stepID uuid.UUID, in UpdateTaskPlanStepInput) (domain.TaskPlan, error)
	RecordTaskPlanCheckpoint(ctx context.Context, taskID uuid.UUID, in RecordTaskPlanCheckpointInput) (domain.TaskPlan, error)
	SetExecutionPlan(ctx context.Context, taskID uuid.UUID, in SetExecutionPlanInput) (domain.TaskExecutionPlan, error)
	ExecuteTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error)
	RetryTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error)
	SyncTaskNexus(ctx context.Context, taskID uuid.UUID) (domain.Task, error)
	Chat(ctx context.Context, in ChatInput) (ChatResult, error)
}

type Handler struct {
	uc taskUsecase
}

func NewHandler(uc taskUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/tasks", h.create)
	mux.HandleFunc("GET /v1/tasks", h.list)
	mux.HandleFunc("GET /v1/tasks/{id}", h.getByID)
	mux.HandleFunc("POST /v1/tasks/{id}/message", h.addMessage)
	mux.HandleFunc("POST /v1/tasks/{id}/investigate", h.investigate)
	mux.HandleFunc("POST /v1/tasks/{id}/propose", h.propose)
	mux.HandleFunc("PUT /v1/tasks/{id}/plan", h.setTaskPlan)
	mux.HandleFunc("GET /v1/tasks/{id}/graph", h.executionGraph)
	mux.HandleFunc("PATCH /v1/tasks/{id}/plan/steps/{step_id}", h.updatePlanStep)
	mux.HandleFunc("POST /v1/tasks/{id}/plan/checkpoint", h.recordPlanCheckpoint)
	mux.HandleFunc("PUT /v1/tasks/{id}/execution-plan", h.setExecutionPlan)
	mux.HandleFunc("POST /v1/tasks/{id}/execute", h.execute)
	mux.HandleFunc("POST /v1/tasks/{id}/retry", h.retry)
	mux.HandleFunc("POST /v1/tasks/{id}/sync", h.syncNexus)
	mux.HandleFunc("POST /v1/chat", h.chat)
	mux.HandleFunc("POST /v1/customer-messaging/inbound", h.customerMessagingInbound)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body tasksdto.CreateTaskRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Title == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "title is required")
		return
	}
	identity, ok := workIdentity(r)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "customer org context required")
		return
	}
	createdBy, ok := resolveCreatedBy(r, body.CreatedBy)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "created_by is not allowed for this principal")
		return
	}
	t, err := h.uc.Create(r.Context(), CreateTaskInput{
		OrgID:       identity.CustomerOrgID,
		Title:       body.Title,
		Goal:        body.Goal,
		Priority:    body.Priority,
		CreatedBy:   createdBy,
		AssignedTo:  body.AssignedTo,
		Channel:     body.Channel,
		Summary:     body.Summary,
		ContextJSON: body.ContextJSON,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "create task failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, tasksdto.TaskToResponse(t))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksRead) {
		return
	}
	limit := defaultListLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= maxListLimit {
			limit = n
		}
	}
	// Routing por scope/auth:
	//   - sin auth context (dev/healthz)     → ListAll (compat dev)
	//   - con scope companion:cross_org      → ListAll (admin explícito)
	//   - con principal y org_id no-vacío    → List(orgID) strict
	//   - con principal y org_id vacío       → 403 (customer org context required)
	//
	// Cierra el leak histórico donde principalOrgID(r) == "" + auth presente
	// devolvía TODOS los tenants.
	var (
		list []domain.Task
		err  error
	)
	switch {
	case identityctx.HasNoAuthContext(r), identityctx.HasScope(r, scopeCompanionCrossOrg):
		list, err = h.uc.ListAll(r.Context(), limit)
	default:
		orgID := tenant.FromString(principalOrgID(r))
		if orgID.IsZero() {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "customer org context required")
			return
		}
		list, err = h.uc.List(r.Context(), orgID, limit)
	}
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list tasks failed")
		return
	}
	out := make([]tasksdto.TaskResponse, 0, len(list))
	for _, t := range list {
		// canAccessTaskOrg queda como defense-in-depth: el SQL ya filtra,
		// pero si el repo se cambia o un fake olvida el filtro, esto evita
		// fugas cross-org.
		if !canAccessTaskOrg(r, t) {
			continue
		}
		out = append(out, tasksdto.TaskToResponse(t))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	detail, err := h.uc.GetDetail(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get task failed")
		return
	}
	if !canAccessTaskOrg(r, detail.Task) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "task org is not allowed for this principal")
		return
	}
	resp := tasksdto.TaskDetailResponse{
		Task:                tasksdto.TaskToResponse(detail.Task),
		Messages:            make([]tasksdto.MessageResponse, 0, len(detail.Messages)),
		Actions:             make([]tasksdto.ActionResponse, 0, len(detail.Actions)),
		Artifacts:           make([]tasksdto.ArtifactResponse, 0, len(detail.Artifacts)),
		LinkedNexusRequests: make([]tasksdto.LinkedNexusRequestResponse, 0, len(detail.LinkedNexusRequests)),
	}
	for _, m := range detail.Messages {
		resp.Messages = append(resp.Messages, tasksdto.MessageToResponse(m))
	}
	for _, a := range detail.Actions {
		resp.Actions = append(resp.Actions, tasksdto.ActionToResponse(a))
	}
	for _, ar := range detail.Artifacts {
		resp.Artifacts = append(resp.Artifacts, tasksdto.ArtifactToResponse(ar))
	}
	for _, lr := range detail.LinkedNexusRequests {
		resp.LinkedNexusRequests = append(resp.LinkedNexusRequests, tasksdto.LinkedNexusRequestResponse{
			ActionID: lr.ActionID.String(),
			Request:  lr.Request,
		})
	}
	if detail.NexusSync != nil {
		resp.NexusSync = tasksdto.NexusSyncToResponse(*detail.NexusSync)
	}
	if detail.ExecutionPlan != nil {
		resp.ExecutionPlan = tasksdto.ExecutionPlanToResponse(*detail.ExecutionPlan)
	}
	if detail.DurablePlan != nil {
		resp.DurablePlan = tasksdto.TaskPlanToResponse(*detail.DurablePlan)
	}
	if detail.ExecutionState != nil {
		resp.ExecutionState = tasksdto.ExecutionStateToResponse(*detail.ExecutionState)
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) executionGraph(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || id == uuid.Nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	task, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get task failed")
		return
	}
	if !canAccessTaskOrg(r, task) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "task org is not allowed for this principal")
		return
	}
	events, err := h.uc.ListTaskExecutionGraph(r.Context(), id, queryLimit(r, 200, 1000))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list task execution graph failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *Handler) addMessage(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.AddMessageRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	m, err := h.uc.AddMessage(r.Context(), id, AddMessageInput{
		AuthorType: body.AuthorType,
		AuthorID:   body.AuthorID,
		Body:       body.Body,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "add message failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, tasksdto.MessageToResponse(m))
}

func (h *Handler) investigate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body tasksdto.InvestigateRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
			return
		}
	}
	t, err := h.uc.Investigate(r.Context(), id, InvestigateInput{Note: body.Note})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "investigate failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskToResponse(t))
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.ProposeRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	t, action, sub, err := h.uc.Propose(r.Context(), id, ProposeInput{
		Note:           body.Note,
		TargetSystem:   body.TargetSystem,
		TargetResource: body.TargetResource,
		SessionID:      body.SessionID,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		if errors.Is(err, ErrNexusSubmit) && t.ID != uuid.Nil {
			httpjson.WriteJSON(w, http.StatusBadGateway, map[string]any{
				"code":    "NEXUS_SUBMIT_FAILED",
				"message": "nexus request failed",
				"task":    tasksdto.TaskToResponse(t),
				"action":  tasksdto.ActionToResponse(action),
			})
			return
		}
		httpjson.WriteFlatInternalError(w, err, "propose failed")
		return
	}
	var pr tasksdto.ProposeResponse
	pr.Task = tasksdto.TaskToResponse(t)
	pr.Action = tasksdto.ActionToResponse(action)
	pr.NexusSubmit.RequestID = sub.RequestID
	pr.NexusSubmit.Decision = sub.Decision
	pr.NexusSubmit.Status = sub.Status
	pr.NexusSubmit.RiskLevel = sub.RiskLevel
	pr.NexusSubmit.DecisionReason = sub.DecisionReason
	httpjson.WriteJSON(w, http.StatusOK, pr)
}

func (h *Handler) syncNexus(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	t, err := h.uc.SyncTaskNexus(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "sync failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskToResponse(t))
}

func (h *Handler) setTaskPlan(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.SetTaskPlanRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	steps := make([]SetTaskPlanStepInput, 0, len(body.Steps))
	for _, step := range body.Steps {
		var stepID uuid.UUID
		if strings.TrimSpace(step.ID) != "" {
			parsed, err := uuid.Parse(step.ID)
			if err != nil {
				httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid step id")
				return
			}
			stepID = parsed
		}
		steps = append(steps, SetTaskPlanStepInput{
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
	plan, err := h.uc.SetTaskPlan(r.Context(), id, SetTaskPlanInput{
		Objective:       body.Objective,
		Status:          body.Status,
		Strategy:        body.Strategy,
		AssumptionsJSON: body.Assumptions,
		ConstraintsJSON: body.Constraints,
		CheckpointJSON:  body.Checkpoint,
		NextAction:      body.NextAction,
		Blocker:         body.Blocker,
		CreatedBy:       principalUserID(r),
		Steps:           steps,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskPlanToResponse(plan))
}

func (h *Handler) updatePlanStep(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	stepID, err := uuid.Parse(r.PathValue("step_id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid step_id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.UpdateTaskPlanStepRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	plan, err := h.uc.UpdateTaskPlanStep(r.Context(), id, stepID, UpdateTaskPlanStepInput{
		Status:         body.Status,
		EvidenceJSON:   body.Evidence,
		Observation:    body.Observation,
		Blocker:        body.Blocker,
		ErrorMessage:   body.ErrorMessage,
		CheckpointJSON: body.Checkpoint,
		NextAction:     body.NextAction,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task plan or step not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskPlanToResponse(plan))
}

func (h *Handler) recordPlanCheckpoint(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.RecordTaskPlanCheckpointRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	plan, err := h.uc.RecordTaskPlanCheckpoint(r.Context(), id, RecordTaskPlanCheckpointInput{
		Status:         body.Status,
		CheckpointJSON: body.Checkpoint,
		NextAction:     body.NextAction,
		Blocker:        body.Blocker,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task plan not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskPlanToResponse(plan))
}

func (h *Handler) setExecutionPlan(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.SetExecutionPlanRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	connectorID, err := uuid.Parse(body.ConnectorID)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid connector_id")
		return
	}
	plan, err := h.uc.SetExecutionPlan(r.Context(), id, SetExecutionPlanInput{
		ConnectorID:    connectorID,
		Operation:      body.Operation,
		Payload:        body.Payload,
		IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "set execution plan failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecutionPlanToResponse(plan))
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	out, err := h.uc.ExecuteTask(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if writeNexusBlocked(w, err) {
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "execute task failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecuteTaskResponse{
		Task:           tasksdto.TaskToResponse(out.Task),
		Plan:           *tasksdto.ExecutionPlanToResponse(out.Plan),
		Execution:      tasksdto.ExecutionResultToResponse(out.Execution),
		ExecutionState: tasksdto.ExecutionStateToResponse(out.ExecutionState),
	})
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	out, err := h.uc.RetryTask(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if writeNexusBlocked(w, err) {
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "retry task failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecuteTaskResponse{
		Task:           tasksdto.TaskToResponse(out.Task),
		Plan:           *tasksdto.ExecutionPlanToResponse(out.Plan),
		Execution:      tasksdto.ExecutionResultToResponse(out.Execution),
		ExecutionState: tasksdto.ExecutionStateToResponse(out.ExecutionState),
	})
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body tasksdto.ChatRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Message == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "message is required")
		return
	}

	var taskID *uuid.UUID
	if body.TaskID != "" {
		parsed, err := uuid.Parse(body.TaskID)
		if err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid task_id")
			return
		}
		if !h.authorizeTaskOrg(w, r, parsed) {
			return
		}
		taskID = &parsed
	}
	var chatID *uuid.UUID
	if body.ChatID != "" {
		parsed, err := uuid.Parse(body.ChatID)
		if err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid chat_id")
			return
		}
		chatID = &parsed
	}

	identity, ok := identityctx.WorkIdentityForOrg(r, r.URL.Query().Get("org_id"), scopeCompanionCrossOrg)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "customer org context required")
		return
	}
	identity = identity.WithProductSurface(body.ProductSurface)
	userID := identity.EffectiveActorID()
	orgID := identity.CustomerOrgID

	result, err := h.uc.Chat(r.Context(), ChatInput{
		TaskID:         taskID,
		ChatID:         chatID,
		UserID:         userID,
		OrgID:          orgID,
		AuthScopes:     identity.Scopes,
		Message:        body.Message,
		RouteHint:      body.RouteHint,
		Handoff:        body.Handoff,
		Workspace:      body.Workspace,
		Channel:        body.Channel,
		ProductSurface: identity.ProductSurface,
		AgentID:        body.AgentID,
		Identity:       identity,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "chat failed")
		return
	}

	msgs := make([]tasksdto.MessageResponse, 0, len(result.Messages))
	for _, m := range result.Messages {
		msgs = append(msgs, tasksdto.MessageToResponse(m))
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ChatResponseFromRuntimeResult(result.Task, result.Messages, result.RunID, result.AgentID, chatToolCallsToResponse(result.ToolCalls)))
}

func chatToolCallsToResponse(calls []OrchestratorToolCall) []tasksdto.ChatToolCallResponse {
	out := make([]tasksdto.ChatToolCallResponse, 0, len(calls))
	for _, call := range calls {
		status := "executed"
		if !call.Allowed {
			status = "blocked"
		}
		if call.Error != "" {
			status = "error"
		}
		out = append(out, tasksdto.ChatToolCallResponse{
			Name:           call.Name,
			ToolCallID:     call.ToolCallID,
			Allowed:        call.Allowed,
			DecisionReason: call.DecisionReason,
			DurationMS:     call.DurationMS,
			Error:          call.Error,
			Status:         status,
			Result:         call.Result,
		})
	}
	return out
}

type customerMessagingInboundRequest struct {
	OrgID         string `json:"org_id"`
	PhoneNumberID string `json:"phone_number_id"`
	FromPhone     string `json:"from_phone"`
	Message       string `json:"message"`
	MessageID     string `json:"message_id,omitempty"`
	ProfileName   string `json:"profile_name,omitempty"`
}

type customerMessagingInboundResponse struct {
	ConversationID string   `json:"conversation_id"`
	Reply          string   `json:"reply"`
	TokensUsed     int      `json:"tokens_used"`
	ToolCalls      []string `json:"tool_calls"`
}

func (h *Handler) customerMessagingInbound(w http.ResponseWriter, r *http.Request) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusUnauthorized, "UNAUTHORIZED", "customer messaging inbound requires authenticated Axis service identity")
		return
	}
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body customerMessagingInboundRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	body.OrgID = strings.TrimSpace(body.OrgID)
	body.PhoneNumberID = strings.TrimSpace(body.PhoneNumberID)
	body.FromPhone = strings.TrimSpace(body.FromPhone)
	body.Message = strings.TrimSpace(body.Message)
	body.MessageID = strings.TrimSpace(body.MessageID)
	body.ProfileName = strings.TrimSpace(body.ProfileName)
	if body.OrgID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	if body.PhoneNumberID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "phone_number_id is required")
		return
	}
	if body.FromPhone == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "from_phone is required")
		return
	}
	if body.Message == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "message is required")
		return
	}
	identity, ok := identityctx.WorkIdentityForOrg(r, body.OrgID, scopeCompanionCrossOrg)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "customer messaging org is not allowed for this principal")
		return
	}
	if surface := strings.TrimSpace(identity.ProductSurface); surface != "" && surface != "pymes" {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "customer messaging product surface is not allowed")
		return
	}
	orgID := identity.CustomerOrgID
	if orgID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}

	userID := body.FromPhone
	if userID == "" {
		userID = "whatsapp"
	} else {
		userID = "whatsapp:" + userID
	}
	if body.ProfileName != "" {
		userID = userID + ":" + body.ProfileName
	}

	identity = identity.WithProductSurface("pymes")
	result, err := h.uc.Chat(r.Context(), ChatInput{
		UserID:         userID,
		OrgID:          orgID,
		AuthScopes:     identity.Scopes,
		Message:        body.Message,
		Channel:        "whatsapp",
		ProductSurface: identity.ProductSurface,
		Identity:       identity,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "customer messaging inbound failed")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, customerMessagingInboundResponse{
		ConversationID: chatConversationID(result.Task.ContextJSON),
		Reply:          lastAssistantReply(result.Messages),
		ToolCalls:      []string{},
	})
}

func chatConversationID(raw json.RawMessage) string {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if id, ok := payload[agentConversationContextKey].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

func lastAssistantReply(messages []domain.TaskMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		role := strings.TrimSpace(strings.ToLower(messages[i].AuthorType))
		if role == "assistant" || role == "system" {
			return messages[i].Body
		}
	}
	return ""
}

func queryLimit(r *http.Request, fallback, max int) int {
	limit := fallback
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if max > 0 && limit > max {
		return max
	}
	return limit
}

func (h *Handler) authorizeTaskOrg(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	task, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return false
		}
		httpjson.WriteFlatInternalError(w, err, "get task failed")
		return false
	}
	if !canAccessTaskOrg(r, task) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "task org is not allowed for this principal")
		return false
	}
	return true
}

// writeNexusBlocked detecta el typed error ErrNexusNotApproved y escribe
// HTTP 412 (precondition_failed) con nexus_request_id y nexus_status en el
// body para que el caller pueda actuar (esperar, refrescar, escalar).
// Devuelve true si manejó el error.
func writeNexusBlocked(w http.ResponseWriter, err error) bool {
	blocked, ok := AsNexusBlocked(err)
	if !ok {
		return false
	}
	httpjson.WriteJSON(w, http.StatusPreconditionFailed, map[string]any{
		"code":             "NEXUS_NOT_APPROVED",
		"message":          "execution requires the linked nexus to be approved",
		"nexus_request_id": blocked.NexusRequestID,
		"nexus_status":     blocked.NexusStatus,
		"reason":           blocked.Reason,
	})
	return true
}
