package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/security/go/tenant"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

type CreateTaskInput struct {
	OrgID       string
	Title       string
	Goal        string
	Priority    string
	CreatedBy   string
	AssignedTo  string
	AssigneeEmployeeID string
	Channel     string
	Summary     string
	ContextJSON json.RawMessage
}

func (u *Usecases) Create(ctx context.Context, in CreateTaskInput) (domain.Task, error) {
	if in.Title == "" {
		return domain.Task{}, fmt.Errorf("title is required")
	}
	t := domain.Task{
		Title:       in.Title,
		OrgID:       in.OrgID,
		Goal:        in.Goal,
		Status:      domain.TaskStatusNew,
		Priority:    in.Priority,
		CreatedBy:   in.CreatedBy,
		AssignedTo:  in.AssignedTo,
		Channel:     in.Channel,
		Summary:     in.Summary,
		ContextJSON: in.ContextJSON,
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if len(t.ContextJSON) == 0 {
		t.ContextJSON = json.RawMessage(`{}`)
	}
	if updated, ok := mergeTaskEmployeeID(t.ContextJSON, in.AssigneeEmployeeID); ok {
		t.ContextJSON = updated
	}
	out, err := u.repo.CreateTask(ctx, t)
	if err != nil {
		return domain.Task{}, err
	}
	u.syncTaskMemory(ctx, out.ID, "create")
	slog.Info("companion task created", "task_id", out.ID.String(), "title", out.Title, "created_by", out.CreatedBy)
	return out, nil
}

// List devuelve tareas para un tenant. `orgID` obligatorio; vacío retorna
// `domainerr.TenantMissing` (el repo enforce esta semántica también).
func (u *Usecases) List(ctx context.Context, orgID tenant.ID, limit int) ([]domain.Task, error) {
	return u.repo.ListTasks(ctx, orgID, limit)
}

// ListAll devuelve tareas SIN filtro de tenant. SOLO callable después de
// validar `companion:cross_org` (o dev mode sin auth). El usecase no valida
// scopes; eso vive en el handler.
func (u *Usecases) ListAll(ctx context.Context, limit int) ([]domain.Task, error) {
	return u.repo.ListAllTasks(ctx, limit)
}

func (u *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	return u.repo.GetTaskByID(ctx, id)
}

type LinkedNexusRequest struct {
	ActionID uuid.UUID                   `json:"action_id"`
	Request  *nexusclient.RequestSummary `json:"request,omitempty"`
}

type TaskDetail struct {
	Task                domain.Task                `json:"task"`
	Messages            []domain.TaskMessage       `json:"messages"`
	Actions             []domain.TaskAction        `json:"actions"`
	Artifacts           []domain.TaskArtifact      `json:"artifacts"`
	LinkedNexusRequests []LinkedNexusRequest       `json:"linked_nexus_requests"`
	NexusSync           *domain.TaskNexusSyncState `json:"nexus_sync,omitempty"`
	ExecutionPlan       *domain.TaskExecutionPlan  `json:"execution_plan,omitempty"`
	DurablePlan         *domain.TaskPlan           `json:"durable_plan,omitempty"`
	ExecutionState      *domain.TaskExecutionState `json:"execution_state,omitempty"`
}

func (u *Usecases) GetDetail(ctx context.Context, id uuid.UUID) (TaskDetail, error) {
	var out TaskDetail
	t, err := u.repo.GetTaskByID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Task = t
	out.Messages, err = u.repo.ListMessagesByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Actions, err = u.repo.ListActionsByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Artifacts, err = u.repo.ListArtifactsByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	state, stateErr := u.repo.GetNexusSyncState(ctx, id)
	if stateErr == nil {
		out.NexusSync = &state
	} else if !domainerr.IsNotFound(stateErr) {
		return out, stateErr
	}
	plan, planErr := u.repo.GetExecutionPlan(ctx, id)
	if planErr == nil {
		out.ExecutionPlan = &plan
	} else if !domainerr.IsNotFound(planErr) {
		return out, planErr
	}
	durablePlan, durablePlanErr := u.repo.GetTaskPlan(ctx, id)
	if durablePlanErr == nil {
		out.DurablePlan = &durablePlan
	} else if !domainerr.IsNotFound(durablePlanErr) {
		return out, durablePlanErr
	}
	executionState, executionStateErr := u.repo.GetExecutionState(ctx, id)
	if executionStateErr == nil {
		out.ExecutionState = &executionState
	} else if !domainerr.IsNotFound(executionStateErr) {
		return out, executionStateErr
	}
	seen := make(map[uuid.UUID]struct{})
	for _, a := range out.Actions {
		if a.NexusRequestID == nil {
			continue
		}
		rid := *a.NexusRequestID
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		sum, st, gErr := u.nexus.GetRequest(ctx, rid.String())
		lr := LinkedNexusRequest{ActionID: a.ID}
		if gErr != nil {
			slog.Error("nexus get request failed", "error", gErr, "request_id", rid)
			out.LinkedNexusRequests = append(out.LinkedNexusRequests, lr)
			continue
		}
		if st == 404 {
			out.LinkedNexusRequests = append(out.LinkedNexusRequests, lr)
			continue
		}
		lr.Request = &sum
		out.LinkedNexusRequests = append(out.LinkedNexusRequests, lr)
	}
	return out, nil
}

type AddMessageInput struct {
	AuthorType string
	AuthorID   string
	Body       string
}

func (u *Usecases) AddMessage(ctx context.Context, taskID uuid.UUID, in AddMessageInput) (domain.TaskMessage, error) {
	if in.Body == "" {
		return domain.TaskMessage{}, fmt.Errorf("body is required")
	}
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return domain.TaskMessage{}, err
	}
	at := in.AuthorType
	if at == "" {
		at = "user"
	}
	return u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     taskID,
		AuthorType: at,
		AuthorID:   in.AuthorID,
		Body:       in.Body,
	})
}
