package tasks

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/security/go/tenant"
	"github.com/google/uuid"
)

type fakeRepo struct {
	mu        sync.Mutex
	tasks     map[uuid.UUID]domain.Task
	messages  map[uuid.UUID][]domain.TaskMessage
	actions   map[uuid.UUID][]domain.TaskAction
	artifacts map[uuid.UUID][]domain.TaskArtifact
	sync      map[uuid.UUID]domain.TaskNexusSyncState
	plans     map[uuid.UUID]domain.TaskPlan
	steps     map[uuid.UUID]domain.TaskPlanStep
}

func (r *fakeRepo) init() {
	if r.tasks == nil {
		r.tasks = make(map[uuid.UUID]domain.Task)
	}
	if r.messages == nil {
		r.messages = make(map[uuid.UUID][]domain.TaskMessage)
	}
	if r.actions == nil {
		r.actions = make(map[uuid.UUID][]domain.TaskAction)
	}
	if r.artifacts == nil {
		r.artifacts = make(map[uuid.UUID][]domain.TaskArtifact)
	}
	if r.sync == nil {
		r.sync = make(map[uuid.UUID]domain.TaskNexusSyncState)
	}
	if r.plans == nil {
		r.plans = make(map[uuid.UUID]domain.TaskPlan)
	}
	if r.steps == nil {
		r.steps = make(map[uuid.UUID]domain.TaskPlanStep)
	}
}

func (r *fakeRepo) CreateTask(_ context.Context, task domain.Task) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	now := time.Now().UTC()
	if task.ID == uuid.Nil {
		task.ID = uuid.New()
	}
	if task.Status == "" {
		task.Status = domain.TaskStatusNew
	}
	if task.Priority == "" {
		task.Priority = "normal"
	}
	if len(task.ContextJSON) == 0 {
		task.ContextJSON = json.RawMessage(`{}`)
	}
	task.CreatedAt = now
	task.UpdatedAt = now
	r.tasks[task.ID] = task
	return task, nil
}

func (r *fakeRepo) GetTaskByID(_ context.Context, id uuid.UUID) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	task, ok := r.tasks[id]
	if !ok {
		return domain.Task{}, ErrNotFound
	}
	return task, nil
}

func (r *fakeRepo) GetTaskByAgentConversationID(_ context.Context, conversationID uuid.UUID) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	for _, task := range r.tasks {
		var holder map[string]any
		if err := json.Unmarshal(task.ContextJSON, &holder); err != nil {
			continue
		}
		if holder[agentConversationContextKey] == conversationID.String() {
			return task, nil
		}
	}
	return domain.Task{}, ErrNotFound
}

func (r *fakeRepo) ListTasks(_ context.Context, orgID tenant.ID, limit int) ([]domain.Task, error) {
	if orgID.IsZero() {
		return nil, domainerr.TenantMissing()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	return limitTasks(filterTasks(r.tasks, func(task domain.Task) bool {
		return task.OrgID == orgID.String()
	}), limit), nil
}

func (r *fakeRepo) ListAllTasks(_ context.Context, limit int) ([]domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	return limitTasks(filterTasks(r.tasks, func(domain.Task) bool { return true }), limit), nil
}

func (r *fakeRepo) UpdateTask(_ context.Context, task domain.Task) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if _, ok := r.tasks[task.ID]; !ok {
		return domain.Task{}, ErrNotFound
	}
	task.UpdatedAt = time.Now().UTC()
	r.tasks[task.ID] = task
	return task, nil
}

func (r *fakeRepo) ListTasksByStatus(_ context.Context, status string, limit int) ([]domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	return limitTasks(filterTasks(r.tasks, func(task domain.Task) bool {
		return task.Status == status
	}), limit), nil
}

func (r *fakeRepo) ListTasksPendingNexusSync(_ context.Context, _ time.Time, limit int) ([]domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	var out []domain.Task
	for taskID := range r.sync {
		if task, ok := r.tasks[taskID]; ok {
			out = append(out, task)
		}
	}
	return limitTasks(out, limit), nil
}

func (r *fakeRepo) LatestProposeNexusRequestID(_ context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	for i := len(r.actions[taskID]) - 1; i >= 0; i-- {
		if r.actions[taskID][i].NexusRequestID != nil {
			return *r.actions[taskID][i].NexusRequestID, nil
		}
	}
	return uuid.Nil, ErrNotFound
}

func (r *fakeRepo) GetNexusSyncState(_ context.Context, taskID uuid.UUID) (domain.TaskNexusSyncState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	state, ok := r.sync[taskID]
	if !ok {
		return domain.TaskNexusSyncState{}, ErrNotFound
	}
	return state, nil
}

func (r *fakeRepo) UpsertNexusSyncState(_ context.Context, state domain.TaskNexusSyncState) (domain.TaskNexusSyncState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	now := time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	r.sync[state.TaskID] = state
	return state, nil
}

func (r *fakeRepo) GetTaskPlan(_ context.Context, taskID uuid.UUID) (domain.TaskPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	plan, ok := r.plans[taskID]
	if !ok {
		return domain.TaskPlan{}, ErrNotFound
	}
	return plan, nil
}

func (r *fakeRepo) UpsertTaskPlan(_ context.Context, plan domain.TaskPlan) (domain.TaskPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	now := time.Now().UTC()
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	plan.UpdatedAt = now
	r.plans[plan.TaskID] = plan
	return plan, nil
}

func (r *fakeRepo) UpdateTaskPlan(ctx context.Context, plan domain.TaskPlan) (domain.TaskPlan, error) {
	return r.UpsertTaskPlan(ctx, plan)
}

func (r *fakeRepo) UpdateTaskPlanStep(_ context.Context, step domain.TaskPlanStep) (domain.TaskPlanStep, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if step.ID == uuid.Nil {
		step.ID = uuid.New()
	}
	step.UpdatedAt = time.Now().UTC()
	r.steps[step.ID] = step
	return step, nil
}

func (r *fakeRepo) InsertMessage(_ context.Context, msg domain.TaskMessage) (domain.TaskMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}
	msg.CreatedAt = time.Now().UTC()
	r.messages[msg.TaskID] = append(r.messages[msg.TaskID], msg)
	return msg, nil
}

func (r *fakeRepo) ListMessagesByTaskID(_ context.Context, taskID uuid.UUID) ([]domain.TaskMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	return append([]domain.TaskMessage(nil), r.messages[taskID]...), nil
}

func (r *fakeRepo) InsertAction(_ context.Context, action domain.TaskAction) (domain.TaskAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if action.ID == uuid.Nil {
		action.ID = uuid.New()
	}
	action.CreatedAt = time.Now().UTC()
	r.actions[action.TaskID] = append(r.actions[action.TaskID], action)
	return action, nil
}

func (r *fakeRepo) UpdateActionNexusResult(_ context.Context, actionID uuid.UUID, nexusRequestID *uuid.UUID, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	for taskID, actions := range r.actions {
		for i := range actions {
			if actions[i].ID == actionID {
				actions[i].NexusRequestID = nexusRequestID
				actions[i].ErrorMessage = errMsg
				r.actions[taskID] = actions
				return nil
			}
		}
	}
	return ErrNotFound
}

func (r *fakeRepo) ListActionsByTaskID(_ context.Context, taskID uuid.UUID) ([]domain.TaskAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	return append([]domain.TaskAction(nil), r.actions[taskID]...), nil
}

func (r *fakeRepo) InsertArtifact(_ context.Context, artifact domain.TaskArtifact) (domain.TaskArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if artifact.ID == uuid.Nil {
		artifact.ID = uuid.New()
	}
	artifact.CreatedAt = time.Now().UTC()
	r.artifacts[artifact.TaskID] = append(r.artifacts[artifact.TaskID], artifact)
	return artifact, nil
}

func (r *fakeRepo) ListArtifactsByTaskID(_ context.Context, taskID uuid.UUID) ([]domain.TaskArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	return append([]domain.TaskArtifact(nil), r.artifacts[taskID]...), nil
}

type stubNexus struct{}

func (stubNexus) SubmitRequest(context.Context, string, nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
	return nexusclient.SubmitResponse{
		RequestID: uuid.NewString(),
		Decision:  nexusclient.DecisionAllow,
		Status:    nexusclient.StatusAllowed,
	}, nil
}

func (stubNexus) GetRequest(_ context.Context, id string) (nexusclient.RequestSummary, int, error) {
	return nexusclient.RequestSummary{ID: id, Status: nexusclient.StatusAllowed, Decision: nexusclient.DecisionAllow}, 200, nil
}

func (stubNexus) ReportResult(context.Context, string, bool, map[string]any, int64, string) (int, error) {
	return 200, nil
}

func filterTasks(tasks map[uuid.UUID]domain.Task, keep func(domain.Task) bool) []domain.Task {
	out := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if keep(task) {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func limitTasks(tasks []domain.Task, limit int) []domain.Task {
	if limit <= 0 || limit >= len(tasks) {
		return append([]domain.Task(nil), tasks...)
	}
	return append([]domain.Task(nil), tasks[:limit]...)
}
