package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/security/go/tenant"
)

type fakeRepo struct {
	tasks          map[uuid.UUID]domain.Task
	lastPropose    map[uuid.UUID]uuid.UUID
	actions        []domain.TaskAction
	artifacts      []domain.TaskArtifact
	nexusSync      map[uuid.UUID]domain.TaskNexusSyncState
	executionPlan  map[uuid.UUID]domain.TaskExecutionPlan
	taskPlan       map[uuid.UUID]domain.TaskPlan
	executionState map[uuid.UUID]domain.TaskExecutionState
}

func (f *fakeRepo) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if t.CreatedAt.IsZero() {
		now := time.Now().UTC()
		t.CreatedAt = now
		t.UpdatedAt = now
	}
	if f.tasks == nil {
		f.tasks = make(map[uuid.UUID]domain.Task)
	}
	f.tasks[t.ID] = t
	return t, nil
}

func (f *fakeRepo) GetTaskByID(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return domain.Task{}, ErrNotFound
	}
	if state, ok := f.nexusSync[id]; ok {
		t.NexusStatus = state.LastNexusStatus
		t.NexusLastCheckedAt = &state.LastCheckedAt
		t.NexusSyncError = state.LastError
	}
	return t, nil
}

func (f *fakeRepo) ListTasks(ctx context.Context, orgID tenant.ID, limit int) ([]domain.Task, error) {
	if orgID.IsZero() {
		return nil, domainerr.TenantMissing()
	}
	scope := orgID.String()
	var out []domain.Task
	for _, t := range f.tasks {
		if t.OrgID != "" && t.OrgID != scope {
			continue
		}
		if state, ok := f.nexusSync[t.ID]; ok {
			t.NexusStatus = state.LastNexusStatus
			t.NexusLastCheckedAt = &state.LastCheckedAt
			t.NexusSyncError = state.LastError
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeRepo) ListAllTasks(ctx context.Context, limit int) ([]domain.Task, error) {
	var out []domain.Task
	for _, t := range f.tasks {
		if state, ok := f.nexusSync[t.ID]; ok {
			t.NexusStatus = state.LastNexusStatus
			t.NexusLastCheckedAt = &state.LastCheckedAt
			t.NexusSyncError = state.LastError
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeRepo) UpdateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	if _, ok := f.tasks[t.ID]; !ok {
		return domain.Task{}, ErrNotFound
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now().UTC()
	}
	f.tasks[t.ID] = t
	return t, nil
}

func (f *fakeRepo) ListTasksByStatus(ctx context.Context, status string, limit int) ([]domain.Task, error) {
	var out []domain.Task
	for _, t := range f.tasks {
		if t.Status == status {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListTasksPendingNexusSync(ctx context.Context, now time.Time, limit int) ([]domain.Task, error) {
	var out []domain.Task
	for _, t := range f.tasks {
		if t.Status != domain.TaskStatusWaitingForApproval {
			continue
		}
		state, ok := f.nexusSync[t.ID]
		if ok && state.NextCheckAt.After(now) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeRepo) LatestProposeNexusRequestID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	if f.lastPropose == nil {
		return uuid.Nil, ErrNotFound
	}
	rid, ok := f.lastPropose[taskID]
	if !ok {
		return uuid.Nil, ErrNotFound
	}
	return rid, nil
}

func (f *fakeRepo) GetNexusSyncState(ctx context.Context, taskID uuid.UUID) (domain.TaskNexusSyncState, error) {
	if f.nexusSync == nil {
		return domain.TaskNexusSyncState{}, ErrNotFound
	}
	state, ok := f.nexusSync[taskID]
	if !ok {
		return domain.TaskNexusSyncState{}, ErrNotFound
	}
	return state, nil
}

func (f *fakeRepo) UpsertNexusSyncState(ctx context.Context, s domain.TaskNexusSyncState) (domain.TaskNexusSyncState, error) {
	if f.nexusSync == nil {
		f.nexusSync = make(map[uuid.UUID]domain.TaskNexusSyncState)
	}
	if existing, ok := f.nexusSync[s.TaskID]; ok {
		if s.CreatedAt.IsZero() {
			s.CreatedAt = existing.CreatedAt
		}
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = time.Now().UTC()
	}
	f.nexusSync[s.TaskID] = s
	return s, nil
}

func (f *fakeRepo) GetExecutionPlan(ctx context.Context, taskID uuid.UUID) (domain.TaskExecutionPlan, error) {
	if f.executionPlan == nil {
		return domain.TaskExecutionPlan{}, ErrNotFound
	}
	plan, ok := f.executionPlan[taskID]
	if !ok {
		return domain.TaskExecutionPlan{}, ErrNotFound
	}
	return plan, nil
}

func (f *fakeRepo) UpsertExecutionPlan(ctx context.Context, plan domain.TaskExecutionPlan) (domain.TaskExecutionPlan, error) {
	if f.executionPlan == nil {
		f.executionPlan = make(map[uuid.UUID]domain.TaskExecutionPlan)
	}
	if existing, ok := f.executionPlan[plan.TaskID]; ok && plan.CreatedAt.IsZero() {
		plan.CreatedAt = existing.CreatedAt
	}
	if len(plan.Payload) == 0 {
		plan.Payload = json.RawMessage(`{}`)
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now().UTC()
	}
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = time.Now().UTC()
	}
	f.executionPlan[plan.TaskID] = plan
	return plan, nil
}

func (f *fakeRepo) GetTaskPlan(ctx context.Context, taskID uuid.UUID) (domain.TaskPlan, error) {
	if f.taskPlan == nil {
		return domain.TaskPlan{}, ErrNotFound
	}
	plan, ok := f.taskPlan[taskID]
	if !ok {
		return domain.TaskPlan{}, ErrNotFound
	}
	return plan, nil
}

func (f *fakeRepo) UpsertTaskPlan(ctx context.Context, plan domain.TaskPlan) (domain.TaskPlan, error) {
	if f.taskPlan == nil {
		f.taskPlan = make(map[uuid.UUID]domain.TaskPlan)
	}
	if existing, ok := f.taskPlan[plan.TaskID]; ok && plan.CreatedAt.IsZero() {
		plan.CreatedAt = existing.CreatedAt
	}
	now := time.Now().UTC()
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = now
	}
	for i := range plan.Steps {
		if plan.Steps[i].ID == uuid.Nil {
			plan.Steps[i].ID = uuid.New()
		}
		if plan.Steps[i].CreatedAt.IsZero() {
			plan.Steps[i].CreatedAt = now
		}
		if plan.Steps[i].UpdatedAt.IsZero() {
			plan.Steps[i].UpdatedAt = now
		}
	}
	f.taskPlan[plan.TaskID] = plan
	return plan, nil
}

func (f *fakeRepo) UpdateTaskPlan(ctx context.Context, plan domain.TaskPlan) (domain.TaskPlan, error) {
	if f.taskPlan == nil {
		return domain.TaskPlan{}, ErrNotFound
	}
	existing, ok := f.taskPlan[plan.TaskID]
	if !ok {
		return domain.TaskPlan{}, ErrNotFound
	}
	plan.Steps = existing.Steps
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = existing.CreatedAt
	}
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = time.Now().UTC()
	}
	f.taskPlan[plan.TaskID] = plan
	return plan, nil
}

func (f *fakeRepo) UpdateTaskPlanStep(ctx context.Context, step domain.TaskPlanStep) (domain.TaskPlanStep, error) {
	if f.taskPlan == nil {
		return domain.TaskPlanStep{}, ErrNotFound
	}
	plan, ok := f.taskPlan[step.TaskID]
	if !ok {
		return domain.TaskPlanStep{}, ErrNotFound
	}
	for i := range plan.Steps {
		if plan.Steps[i].ID == step.ID {
			if step.UpdatedAt.IsZero() {
				step.UpdatedAt = time.Now().UTC()
			}
			plan.Steps[i] = step
			f.taskPlan[step.TaskID] = plan
			return step, nil
		}
	}
	return domain.TaskPlanStep{}, ErrNotFound
}

func (f *fakeRepo) GetExecutionState(ctx context.Context, taskID uuid.UUID) (domain.TaskExecutionState, error) {
	if f.executionState == nil {
		return domain.TaskExecutionState{}, ErrNotFound
	}
	state, ok := f.executionState[taskID]
	if !ok {
		return domain.TaskExecutionState{}, ErrNotFound
	}
	return state, nil
}

func (f *fakeRepo) UpsertExecutionState(ctx context.Context, state domain.TaskExecutionState) (domain.TaskExecutionState, error) {
	if f.executionState == nil {
		f.executionState = make(map[uuid.UUID]domain.TaskExecutionState)
	}
	if existing, ok := f.executionState[state.TaskID]; ok && state.CreatedAt.IsZero() {
		state.CreatedAt = existing.CreatedAt
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now().UTC()
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	if len(state.VerificationResult.Details) == 0 {
		state.VerificationResult.Details = json.RawMessage(`{}`)
	}
	f.executionState[state.TaskID] = state
	return state, nil
}

func (f *fakeRepo) InsertMessage(ctx context.Context, m domain.TaskMessage) (domain.TaskMessage, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return m, nil
}

func (f *fakeRepo) ListMessagesByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskMessage, error) {
	return nil, nil
}

func (f *fakeRepo) InsertAction(ctx context.Context, a domain.TaskAction) (domain.TaskAction, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	f.actions = append(f.actions, a)
	return a, nil
}

func (f *fakeRepo) UpdateActionNexusResult(ctx context.Context, actionID uuid.UUID, nexusRequestID *uuid.UUID, errMsg string) error {
	for i := range f.actions {
		if f.actions[i].ID != actionID {
			continue
		}
		f.actions[i].NexusRequestID = nexusRequestID
		f.actions[i].ErrorMessage = errMsg
		if nexusRequestID != nil && f.actions[i].ActionType == TaskActionPropose {
			if f.lastPropose == nil {
				f.lastPropose = make(map[uuid.UUID]uuid.UUID)
			}
			f.lastPropose[f.actions[i].TaskID] = *nexusRequestID
		}
		return nil
	}
	return nil
}

func (f *fakeRepo) ListActionsByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskAction, error) {
	var out []domain.TaskAction
	for _, action := range f.actions {
		if action.TaskID == taskID {
			out = append(out, action)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListArtifactsByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskArtifact, error) {
	var out []domain.TaskArtifact
	for _, artifact := range f.artifacts {
		if artifact.TaskID == taskID {
			out = append(out, artifact)
		}
	}
	return out, nil
}

func (f *fakeRepo) InsertArtifact(ctx context.Context, ar domain.TaskArtifact) (domain.TaskArtifact, error) {
	if ar.ID == uuid.Nil {
		ar.ID = uuid.New()
	}
	if ar.CreatedAt.IsZero() {
		ar.CreatedAt = time.Now().UTC()
	}
	f.artifacts = append(f.artifacts, ar)
	return ar, nil
}

func (f *fakeRepo) countActions(actionType string) int {
	count := 0
	for _, action := range f.actions {
		if action.ActionType == actionType {
			count++
		}
	}
	return count
}

type stubNexus struct {
	submitFn func(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error)
	getFn    func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error)
	reportFn func(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error)
}

type stubExecutor struct {
	getConnectorFn func(ctx context.Context, id uuid.UUID) (connectordomain.Connector, error)
	bindingFn      func(ctx context.Context, spec connectordomain.ExecutionSpec) (map[string]any, string, error)
	executeFn      func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error)
}

type taskMemoryWrite struct {
	TaskID      uuid.UUID
	Kind        string
	Key         string
	ContentText string
	Payload     json.RawMessage
}

type stubTaskMemory struct {
	writes []taskMemoryWrite
}

func (s *stubTaskMemory) UpsertTaskMemory(ctx context.Context, taskID uuid.UUID, kind, key string, contentText string, payload json.RawMessage) error {
	s.writes = append(s.writes, taskMemoryWrite{
		TaskID:      taskID,
		Kind:        kind,
		Key:         key,
		ContentText: contentText,
		Payload:     append(json.RawMessage(nil), payload...),
	})
	return nil
}

func (s *stubTaskMemory) kinds() []string {
	out := make([]string, 0, len(s.writes))
	for _, write := range s.writes {
		out = append(out, write.Kind)
	}
	return out
}

func (s *stubExecutor) GetConnector(ctx context.Context, id uuid.UUID) (connectordomain.Connector, error) {
	if s.getConnectorFn != nil {
		return s.getConnectorFn(ctx, id)
	}
	return connectordomain.Connector{ID: id, Name: "mock", Kind: "mock", Enabled: true}, nil
}

func (s *stubExecutor) BuildActionBinding(ctx context.Context, spec connectordomain.ExecutionSpec) (map[string]any, string, error) {
	if s.bindingFn != nil {
		return s.bindingFn(ctx, spec)
	}
	binding := map[string]any{
		"schema_version":     nexusclient.ToolIntentSchemaVersion,
		"org_id":             spec.OrgID,
		"actor_id":           spec.ActorID,
		"actor_type":         "agent",
		"product_surface":    "companion",
		"run_id":             "test-run",
		"tool_invocation_id": "test-tool",
		"connector_id":       spec.ConnectorID.String(),
		"capability_id":      spec.Operation,
		"operation":          spec.Operation,
		"target_system":      "mock",
		"target_resource":    spec.ConnectorID.String(),
		"payload_hash":       "payload-hash",
		"idempotency_key":    spec.IdempotencyKey,
	}
	return binding, "binding-hash", nil
}

func (s *stubExecutor) Execute(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
	if s.executeFn != nil {
		return s.executeFn(ctx, spec)
	}
	return connectordomain.ExecutionResult{
		ID:             uuid.New(),
		ConnectorID:    spec.ConnectorID,
		Operation:      spec.Operation,
		Status:         connectordomain.ExecSuccess,
		ExternalRef:    "exec-ref",
		Payload:        spec.Payload,
		ResultJSON:     json.RawMessage(`{"ok":true}`),
		TaskID:         spec.TaskID,
		NexusRequestID: spec.NexusRequestID,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func (s *stubNexus) SubmitRequest(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
	if s.submitFn != nil {
		return s.submitFn(ctx, idempotencyKey, body)
	}
	return nexusclient.SubmitResponse{}, nil
}

func (s *stubNexus) GetRequest(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
	if s.getFn != nil {
		return s.getFn(ctx, id)
	}
	return nexusclient.RequestSummary{}, http.StatusNotFound, nil
}

func (s *stubNexus) ReportResult(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error) {
	if s.reportFn != nil {
		return s.reportFn(ctx, id, success, result, durationMS, errorMessage)
	}
	return http.StatusOK, nil
}

func createWaitingTask(t *testing.T, repo *fakeRepo) domain.Task {
	t.Helper()
	uc := NewUsecases(repo, &stubNexus{})
	created, err := uc.Create(context.Background(), CreateTaskInput{Title: "sync-test"})
	if err != nil {
		t.Fatal(err)
	}
	created.Status = domain.TaskStatusWaitingForApproval
	created.UpdatedAt = time.Now().UTC()
	updated, err := repo.UpdateTask(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestUsecases_Create_requiresTitle(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(&fakeRepo{}, &stubNexus{})
	_, err := uc.Create(context.Background(), CreateTaskInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUsecases_Create_ok(t *testing.T) {
	t.Parallel()
	r := &fakeRepo{}
	uc := NewUsecases(r, &stubNexus{})
	mem := &stubTaskMemory{}
	uc.SetTaskMemory(mem)
	out, err := uc.Create(context.Background(), CreateTaskInput{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "x" || out.Status != domain.TaskStatusNew {
		t.Fatalf("task %+v", out)
	}
	if !slices.Equal(mem.kinds(), []string{taskMemoryKindSummary, taskMemoryKindFacts}) {
		t.Fatalf("unexpected memory writes %+v", mem.kinds())
	}
}

func TestUsecases_ChatDefaultsChannelToAPI(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(&fakeRepo{}, &stubNexus{})

	result, err := uc.Chat(context.Background(), ChatInput{
		UserID:  "user-1",
		OrgID:   "org-1",
		Message: "Hola",
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if result.Task.Channel != "api" {
		t.Fatalf("expected default channel api, got %q", result.Task.Channel)
	}
}

func TestUsecases_SetExecutionPlan_persistsAndAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	connectorID := uuid.New()
	uc.SetExecutor(&stubExecutor{
		getConnectorFn: func(ctx context.Context, id uuid.UUID) (connectordomain.Connector, error) {
			return connectordomain.Connector{ID: id, Kind: "mock", Enabled: true}, nil
		},
	})

	task, err := uc.Create(ctx, CreateTaskInput{Title: "planned task"})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := uc.SetExecutionPlan(ctx, task.ID, SetExecutionPlanInput{
		ConnectorID:    connectorID,
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hi"}`),
		IdempotencyKey: "task-plan-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ConnectorID != connectorID || plan.Operation != "mock.write" {
		t.Fatalf("unexpected plan %+v", plan)
	}
	if repo.countActions(TaskActionSetExecutionPlan) != 1 {
		t.Fatalf("expected one set_execution_plan action, got %d", repo.countActions(TaskActionSetExecutionPlan))
	}
}

func TestUsecases_SetTaskPlan_persistsStepsAndAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	mem := &stubTaskMemory{}
	uc.SetTaskMemory(mem)

	task, err := uc.Create(ctx, CreateTaskInput{OrgID: "org-a", CreatedBy: "user-a", Title: "durable work", Goal: "finish safely"})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := uc.SetTaskPlan(ctx, task.ID, SetTaskPlanInput{
		Objective:  "finish safely",
		Strategy:   "plan then verify",
		NextAction: "inspect context",
		Steps: []SetTaskPlanStepInput{
			{StepKey: "inspect", Title: "Inspect context", Status: domain.TaskPlanStepStatusReady, ExpectedOutcome: "facts collected"},
			{StepKey: "verify", Title: "Verify result", Postcondition: "evidence exists"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != domain.TaskPlanStatusActive || len(plan.Steps) != 2 {
		t.Fatalf("unexpected durable plan %+v", plan)
	}
	if plan.Steps[0].OrgID != "org-a" || plan.Steps[0].StepKey != "inspect" {
		t.Fatalf("unexpected step %+v", plan.Steps[0])
	}
	if repo.countActions(TaskActionSetDurablePlan) != 1 {
		t.Fatalf("expected one set durable plan action, got %d", repo.countActions(TaskActionSetDurablePlan))
	}
	if len(mem.writes) < 4 {
		t.Fatalf("expected task memory projection for durable plan, got %d writes", len(mem.writes))
	}
}

func TestUsecases_UpdateTaskPlanStepCompletesPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	task, err := uc.Create(ctx, CreateTaskInput{OrgID: "org-a", Title: "complete plan"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := uc.SetTaskPlan(ctx, task.ID, SetTaskPlanInput{
		Objective: "complete plan",
		Steps: []SetTaskPlanStepInput{
			{StepKey: "only", Title: "Only step", Status: domain.TaskPlanStepStatusReady},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := uc.UpdateTaskPlanStep(ctx, task.ID, plan.Steps[0].ID, UpdateTaskPlanStepInput{
		Status:       domain.TaskPlanStepStatusDone,
		Observation:  "done",
		EvidenceJSON: json.RawMessage(`{"checked":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != domain.TaskPlanStatusCompleted || updated.CompletedAt == nil {
		t.Fatalf("expected completed durable plan, got %+v", updated)
	}
	if updated.Steps[0].CompletedAt == nil || updated.Steps[0].Observation != "done" {
		t.Fatalf("expected completed step, got %+v", updated.Steps[0])
	}
	if repo.countActions(TaskActionUpdatePlanStep) != 1 {
		t.Fatalf("expected one update plan step action, got %d", repo.countActions(TaskActionUpdatePlanStep))
	}
}

func TestUsecases_RecordTaskPlanCheckpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	task, err := uc.Create(ctx, CreateTaskInput{OrgID: "org-a", Title: "checkpoint"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = uc.SetTaskPlan(ctx, task.ID, SetTaskPlanInput{
		Objective: "checkpoint",
		Steps:     []SetTaskPlanStepInput{{Title: "Step"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := uc.RecordTaskPlanCheckpoint(ctx, task.ID, RecordTaskPlanCheckpointInput{
		CheckpointJSON: json.RawMessage(`{"phase":"observed"}`),
		NextAction:     "continue",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NextAction != "continue" || !strings.Contains(string(updated.CheckpointJSON), "observed") {
		t.Fatalf("unexpected checkpoint %+v", updated)
	}
	if repo.countActions(TaskActionPlanCheckpoint) != 1 {
		t.Fatalf("expected one plan checkpoint action, got %d", repo.countActions(TaskActionPlanCheckpoint))
	}
}

func TestUsecases_PrepareTaskPlanCompensationSubmitsGenericNexusRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	nexusRequestID := uuid.New()
	connectorID := uuid.New()
	var capturedKey string
	var capturedBody nexusclient.SubmitRequestBody
	uc := NewUsecases(repo, &stubNexus{
		submitFn: func(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
			capturedKey = idempotencyKey
			capturedBody = body
			return nexusclient.SubmitResponse{
				RequestID:   nexusRequestID.String(),
				Status:      nexusclient.StatusPendingApproval,
				Decision:    nexusclient.DecisionRequireApproval,
				RiskLevel:   "high",
				BindingHash: "binding-hash",
			}, nil
		},
	})
	uc.SetExecutor(&stubExecutor{
		bindingFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (map[string]any, string, error) {
			if spec.ConnectorID != connectorID {
				t.Fatalf("unexpected compensation connector id %s", spec.ConnectorID)
			}
			if spec.Operation != "invoice.cancel" {
				t.Fatalf("unexpected compensation operation %q", spec.Operation)
			}
			binding := map[string]any{
				"schema_version":     nexusclient.ToolIntentSchemaVersion,
				"org_id":             spec.OrgID,
				"actor_id":           spec.ActorID,
				"actor_type":         "agent",
				"product_surface":    spec.ProductSurface,
				"run_id":             spec.RunID,
				"tool_invocation_id": spec.ToolInvocationID,
				"connector_id":       spec.ConnectorID.String(),
				"capability_id":      spec.Operation,
				"operation":          spec.Operation,
				"target_system":      "billing",
				"target_resource":    spec.ConnectorID.String(),
				"payload_hash":       "comp-payload-hash",
				"idempotency_key":    spec.IdempotencyKey,
			}
			return binding, "binding-hash", nil
		},
	})
	task, err := uc.Create(ctx, CreateTaskInput{OrgID: "org-a", CreatedBy: "user-a", Title: "compensate invoice"})
	if err != nil {
		t.Fatal(err)
	}
	originalBinding, err := json.Marshal(map[string]any{
		"schema_version":     nexusclient.ToolIntentSchemaVersion,
		"org_id":             "org-a",
		"actor_id":           "user-a",
		"actor_type":         "human",
		"product_surface":    "companion",
		"run_id":             "run-1",
		"tool_invocation_id": "invoke-1",
		"connector_id":       connectorID.String(),
		"capability_id":      "invoice.create",
		"operation":          "invoice.create",
		"target_system":      "billing",
		"target_resource":    connectorID.String(),
		"payload_hash":       "payload-hash",
		"idempotency_key":    "original-idem",
	})
	if err != nil {
		t.Fatal(err)
	}
	originalBindingJSON := string(originalBinding)
	plan, err := uc.SetTaskPlan(ctx, task.ID, SetTaskPlanInput{
		Objective: "compensate invoice",
		Steps: []SetTaskPlanStepInput{{
			StepKey:       "invoice",
			Title:         "Create invoice",
			Status:        domain.TaskPlanStepStatusDone,
			ToolName:      "billing_invoice_create",
			Capability:    "invoice.create",
			EvidenceJSON:  json.RawMessage(`{"compensation":{"supported":true,"capability_id":"invoice.cancel","requires_nexus":true},"tool_metadata":{"connector_kind":"billing","rollback_supported":true,"rollback_capability_id":"invoice.cancel"},"tool_result":{"evidence":{"action_binding":` + originalBindingJSON + `}}}`),
			Postcondition: "invoice exists",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := uc.PrepareTaskPlanCompensation(ctx, task.ID, plan.Steps[0].ID, PrepareTaskPlanCompensationInput{Reason: "customer cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "compensation_approval_requested" || out.NexusRequestID != nexusRequestID.String() {
		t.Fatalf("unexpected compensation output: %+v", out)
	}
	if capturedBody.ActionType != nexusclient.ActionTypeAgentCapabilityCompensate || capturedBody.RequesterType != "agent" {
		t.Fatalf("expected generic agent compensation request, got %+v", capturedBody)
	}
	if capturedBody.TargetSystem != "billing" || capturedBody.TargetResource != connectorID.String() {
		t.Fatalf("expected target from original binding, got %+v", capturedBody)
	}
	if capturedBody.ActionBinding["org_id"] != "org-a" || capturedBody.ActionBinding["capability_id"] != "invoice.cancel" {
		t.Fatalf("unexpected compensation action binding: %+v", capturedBody.ActionBinding)
	}
	if !strings.HasPrefix(capturedKey, "task-plan-compensation-"+task.ID.String()) {
		t.Fatalf("expected deterministic compensation idempotency key, got %q", capturedKey)
	}
	if repo.countActions(TaskActionPrepareComp) != 1 {
		t.Fatalf("expected one compensation action, got %d", repo.countActions(TaskActionPrepareComp))
	}
}

func TestUsecases_PrepareTaskPlanCompensationRejectsAmbiguousRollbackOperation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	connectorID := uuid.New()
	submitted := false
	uc := NewUsecases(repo, &stubNexus{
		submitFn: func(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
			submitted = true
			return nexusclient.SubmitResponse{}, nil
		},
	})
	uc.SetExecutor(&stubExecutor{})
	task, err := uc.Create(ctx, CreateTaskInput{OrgID: "org-a", CreatedBy: "user-a", Title: "ambiguous compensation"})
	if err != nil {
		t.Fatal(err)
	}
	originalBinding, err := json.Marshal(map[string]any{
		"schema_version":     nexusclient.ToolIntentSchemaVersion,
		"org_id":             "org-a",
		"actor_id":           "user-a",
		"actor_type":         "human",
		"product_surface":    "companion",
		"run_id":             "run-1",
		"tool_invocation_id": "invoke-1",
		"connector_id":       connectorID.String(),
		"capability_id":      "invoice.create",
		"operation":          "invoice.create",
		"target_system":      "billing",
		"target_resource":    connectorID.String(),
		"payload_hash":       "payload-hash",
		"idempotency_key":    "original-idem",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := uc.SetTaskPlan(ctx, task.ID, SetTaskPlanInput{
		Objective: "ambiguous compensation",
		Steps: []SetTaskPlanStepInput{{
			StepKey:      "invoice",
			Title:        "Create invoice",
			Status:       domain.TaskPlanStepStatusDone,
			ToolName:     "billing_invoice_create",
			Capability:   "invoice.create",
			EvidenceJSON: json.RawMessage(`{"compensation":{"supported":true,"requires_nexus":true},"tool_result":{"evidence":{"action_binding":` + string(originalBinding) + `}}}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := uc.PrepareTaskPlanCompensation(ctx, task.ID, plan.Steps[0].ID, PrepareTaskPlanCompensationInput{Reason: "customer cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "compensation_contract_invalid" {
		t.Fatalf("expected invalid compensation contract, got %+v", out)
	}
	if submitted {
		t.Fatal("ambiguous compensation should not be submitted to Nexus")
	}
}

func TestUsecases_ExecuteTaskPlanCompensationRequiresApprovedNexusAndReports(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	nexusRequestID := uuid.New()
	connectorID := uuid.New()
	var reported bool
	var executedSpec connectordomain.ExecutionSpec
	uc := NewUsecases(repo, &stubNexus{
		submitFn: func(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
			return nexusclient.SubmitResponse{
				RequestID:   nexusRequestID.String(),
				Status:      nexusclient.StatusPendingApproval,
				Decision:    nexusclient.DecisionRequireApproval,
				RiskLevel:   "high",
				BindingHash: "binding-hash",
			}, nil
		},
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			if id != nexusRequestID.String() {
				t.Fatalf("unexpected nexus request lookup %q", id)
			}
			return nexusclient.RequestSummary{
				ID:     nexusRequestID.String(),
				Status: nexusclient.StatusApproved,
			}, http.StatusOK, nil
		},
		reportFn: func(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error) {
			reported = true
			if id != nexusRequestID.String() || !success {
				t.Fatalf("unexpected nexus report id=%q success=%v", id, success)
			}
			if result["external_ref"] != "rollback-ref" || result["operation"] != "invoice.cancel" {
				t.Fatalf("unexpected nexus report payload %+v", result)
			}
			return http.StatusOK, nil
		},
	})
	uc.SetExecutor(&stubExecutor{
		bindingFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (map[string]any, string, error) {
			binding := map[string]any{
				"schema_version":     nexusclient.ToolIntentSchemaVersion,
				"org_id":             spec.OrgID,
				"actor_id":           spec.ActorID,
				"actor_type":         "agent",
				"product_surface":    spec.ProductSurface,
				"run_id":             spec.RunID,
				"tool_invocation_id": spec.ToolInvocationID,
				"connector_id":       spec.ConnectorID.String(),
				"capability_id":      spec.Operation,
				"operation":          spec.Operation,
				"target_system":      "billing",
				"target_resource":    spec.ConnectorID.String(),
				"payload_hash":       "comp-payload-hash",
				"idempotency_key":    spec.IdempotencyKey,
			}
			return binding, "binding-hash", nil
		},
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			executedSpec = spec
			return connectordomain.ExecutionResult{
				ID:             uuid.New(),
				ConnectorID:    spec.ConnectorID,
				OrgID:          spec.OrgID,
				ActorID:        spec.ActorID,
				Operation:      spec.Operation,
				Status:         connectordomain.ExecSuccess,
				ExternalRef:    "rollback-ref",
				Payload:        spec.Payload,
				ResultJSON:     json.RawMessage(`{"ok":true}`),
				EvidenceJSON:   json.RawMessage(`{"external_ref":"rollback-ref"}`),
				IdempotencyKey: spec.IdempotencyKey,
				TaskID:         spec.TaskID,
				NexusRequestID: spec.NexusRequestID,
				DurationMS:     42,
				CreatedAt:      time.Now().UTC(),
			}, nil
		},
	})

	task, err := uc.Create(ctx, CreateTaskInput{OrgID: "org-a", CreatedBy: "user-a", Title: "execute compensation"})
	if err != nil {
		t.Fatal(err)
	}
	originalBinding, err := json.Marshal(map[string]any{
		"schema_version":     nexusclient.ToolIntentSchemaVersion,
		"org_id":             "org-a",
		"actor_id":           "user-a",
		"actor_type":         "human",
		"product_surface":    "companion",
		"run_id":             "run-1",
		"tool_invocation_id": "invoke-1",
		"connector_id":       connectorID.String(),
		"capability_id":      "invoice.create",
		"operation":          "invoice.create",
		"target_system":      "billing",
		"target_resource":    connectorID.String(),
		"payload_hash":       "payload-hash",
		"idempotency_key":    "original-idem",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := uc.SetTaskPlan(ctx, task.ID, SetTaskPlanInput{
		Objective: "execute compensation",
		Steps: []SetTaskPlanStepInput{{
			StepKey:       "invoice",
			Title:         "Create invoice",
			Status:        domain.TaskPlanStepStatusDone,
			ToolName:      "billing_invoice_create",
			Capability:    "invoice.create",
			EvidenceJSON:  json.RawMessage(`{"compensation":{"supported":true,"capability_id":"invoice.cancel","requires_nexus":true},"tool_result":{"evidence":{"action_binding":` + string(originalBinding) + `}}}`),
			Postcondition: "invoice exists",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.PrepareTaskPlanCompensation(ctx, task.ID, plan.Steps[0].ID, PrepareTaskPlanCompensationInput{Reason: "customer cancelled"}); err != nil {
		t.Fatal(err)
	}

	out, err := uc.ExecuteTaskPlanCompensation(ctx, task.ID, plan.Steps[0].ID, ExecuteTaskPlanCompensationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "compensation_executed" || out.NexusRequestID != nexusRequestID.String() {
		t.Fatalf("unexpected compensation execution output: %+v", out)
	}
	if executedSpec.Operation != "invoice.cancel" || executedSpec.ConnectorID != connectorID {
		t.Fatalf("unexpected executed compensation spec: %+v", executedSpec)
	}
	if executedSpec.NexusRequestID == nil || *executedSpec.NexusRequestID != nexusRequestID {
		t.Fatalf("expected approved nexus request in execution spec, got %+v", executedSpec.NexusRequestID)
	}
	if !reported {
		t.Fatal("expected compensation execution result to be reported to Nexus")
	}
	if repo.countActions(TaskActionExecuteComp) != 1 {
		t.Fatalf("expected one execute compensation action, got %d", repo.countActions(TaskActionExecuteComp))
	}
}

func TestUsecases_Propose_persistsInitialNexusSyncState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	nexusRequestID := uuid.New()
	mem := &stubTaskMemory{}
	uc := NewUsecases(repo, &stubNexus{
		submitFn: func(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
			return nexusclient.SubmitResponse{
				RequestID: nexusRequestID.String(),
				Status:    "pending_approval",
				Decision:  "require_approval",
				RiskLevel: "high",
			}, nil
		},
	})
	uc.SetTaskMemory(mem)
	uc.SetExecutor(&stubExecutor{})

	task, err := uc.Create(ctx, CreateTaskInput{OrgID: "org-a", CreatedBy: "user-a", Title: "proposal"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = uc.SetExecutionPlan(ctx, task.ID, SetExecutionPlanInput{
		ConnectorID:    uuid.New(),
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "proposal-idem",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, action, submit, err := uc.Propose(ctx, task.ID, ProposeInput{Note: "needs approval"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != domain.TaskStatusWaitingForApproval {
		t.Fatalf("expected waiting_for_approval, got %q", updated.Status)
	}
	if submit.Status != "pending_approval" {
		t.Fatalf("unexpected submit status %q", submit.Status)
	}
	if action.NexusRequestID == nil || *action.NexusRequestID != nexusRequestID {
		t.Fatalf("unexpected action nexus request id %+v", action.NexusRequestID)
	}

	state, err := repo.GetNexusSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.NexusRequestID != nexusRequestID {
		t.Fatalf("unexpected state nexus_request_id %s", state.NexusRequestID)
	}
	if state.LastNexusStatus != "pending_approval" {
		t.Fatalf("unexpected state status %q", state.LastNexusStatus)
	}
	if state.LastNexusHTTPStatus != http.StatusCreated {
		t.Fatalf("unexpected state http status %d", state.LastNexusHTTPStatus)
	}
	if state.LastError != "" || state.ConsecutiveFailures != 0 {
		t.Fatalf("unexpected state error/failures %+v", state)
	}
	if len(mem.writes) != 6 {
		t.Fatalf("expected create+plan+propose memory projection, got %d writes", len(mem.writes))
	}
	lastSummary := mem.writes[len(mem.writes)-2]
	if lastSummary.Kind != taskMemoryKindSummary || !strings.Contains(lastSummary.ContentText, "waiting for Nexus") {
		t.Fatalf("unexpected summary write %+v", lastSummary)
	}
}

func TestUsecases_SyncTaskNexus_pendingIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{nexusSync: make(map[uuid.UUID]domain.TaskNexusSyncState)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	lastChecked := time.Now().UTC().Add(-time.Minute)
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:              task.ID,
		NexusRequestID:      rid,
		LastNexusStatus:     "pending_approval",
		LastNexusHTTPStatus: http.StatusOK,
		LastCheckedAt:       lastChecked,
		LastError:           "",
		ConsecutiveFailures: 0,
		NextCheckAt:         time.Now().UTC().Add(-time.Second),
		CreatedAt:           lastChecked,
		UpdatedAt:           lastChecked,
	}
	uc := NewUsecases(repo, &stubNexus{
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			return nexusclient.RequestSummary{ID: rid.String(), Status: "pending_approval"}, http.StatusOK, nil
		},
	})
	uc.SetNexusSyncInterval(5 * time.Second)

	out, err := uc.SyncTaskNexus(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusWaitingForApproval {
		t.Fatalf("expected waiting_for_approval, got %q", out.Status)
	}
	if repo.countActions(TaskActionSyncNexus) != 0 {
		t.Fatalf("expected no sync_nexus action, got %d", repo.countActions(TaskActionSyncNexus))
	}

	state, err := repo.GetNexusSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastNexusStatus != "pending_approval" || state.LastError != "" || state.ConsecutiveFailures != 0 {
		t.Fatalf("unexpected state %+v", state)
	}
	if !state.LastCheckedAt.After(lastChecked) {
		t.Fatalf("expected LastCheckedAt to move forward: %s <= %s", state.LastCheckedAt, lastChecked)
	}
}

func TestUsecases_SyncTaskNexus_approvedToDone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{nexusSync: make(map[uuid.UUID]domain.TaskNexusSyncState)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:              task.ID,
		NexusRequestID:      rid,
		LastNexusStatus:     "pending_approval",
		LastNexusHTTPStatus: http.StatusCreated,
		LastCheckedAt:       time.Now().UTC().Add(-time.Minute),
		NextCheckAt:         time.Now().UTC().Add(-time.Second),
		CreatedAt:           time.Now().UTC().Add(-time.Minute),
		UpdatedAt:           time.Now().UTC().Add(-time.Minute),
	}
	uc := NewUsecases(repo, &stubNexus{
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			if id != rid.String() {
				return nexusclient.RequestSummary{}, http.StatusNotFound, nil
			}
			return nexusclient.RequestSummary{ID: rid.String(), Status: "approved"}, http.StatusOK, nil
		},
	})

	out, err := uc.SyncTaskNexus(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusDone {
		t.Fatalf("expected done, got %q", out.Status)
	}
	if out.ClosedAt == nil {
		t.Fatal("expected ClosedAt on terminal state")
	}
	if repo.countActions(TaskActionSyncNexus) != 1 {
		t.Fatalf("expected one sync_nexus action, got %d", repo.countActions(TaskActionSyncNexus))
	}
	state, err := repo.GetNexusSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastNexusStatus != "approved" || state.LastError != "" || state.ConsecutiveFailures != 0 {
		t.Fatalf("unexpected state %+v", state)
	}
}

func TestUsecases_SyncTaskNexus_approvedWithExecutionPlanToWaitingForInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{
		nexusSync:     make(map[uuid.UUID]domain.TaskNexusSyncState),
		executionPlan: make(map[uuid.UUID]domain.TaskExecutionPlan),
	}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:              task.ID,
		NexusRequestID:      rid,
		LastNexusStatus:     "pending_approval",
		LastNexusHTTPStatus: http.StatusCreated,
		LastCheckedAt:       time.Now().UTC().Add(-time.Minute),
		NextCheckAt:         time.Now().UTC().Add(-time.Second),
		CreatedAt:           time.Now().UTC().Add(-time.Minute),
		UpdatedAt:           time.Now().UTC().Add(-time.Minute),
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:      task.ID,
		ConnectorID: uuid.New(),
		Operation:   "mock.write",
		Payload:     json.RawMessage(`{"message":"run"}`),
		CreatedAt:   time.Now().UTC().Add(-time.Minute),
		UpdatedAt:   time.Now().UTC().Add(-time.Minute),
	}
	uc := NewUsecases(repo, &stubNexus{
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			return nexusclient.RequestSummary{ID: rid.String(), Status: "approved"}, http.StatusOK, nil
		},
	})

	out, err := uc.SyncTaskNexus(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusWaitingForInput {
		t.Fatalf("expected waiting_for_input, got %q", out.Status)
	}
}

func TestUsecases_SyncTaskNexus_rejectedToFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{lastPropose: make(map[uuid.UUID]uuid.UUID)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	repo.lastPropose[task.ID] = rid
	uc := NewUsecases(repo, &stubNexus{
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			return nexusclient.RequestSummary{ID: rid.String(), Status: "rejected"}, http.StatusOK, nil
		},
	})

	out, err := uc.SyncTaskNexus(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusFailed {
		t.Fatalf("expected failed, got %q", out.Status)
	}
	if out.ClosedAt == nil {
		t.Fatal("expected ClosedAt on failed task")
	}
	state, err := repo.GetNexusSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastNexusStatus != "rejected" {
		t.Fatalf("unexpected state status %q", state.LastNexusStatus)
	}
}

func TestUsecases_SyncTaskNexus_errorBackoffThenReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{nexusSync: make(map[uuid.UUID]domain.TaskNexusSyncState)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	originalNextCheck := time.Now().UTC().Add(-time.Second)
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:              task.ID,
		NexusRequestID:      rid,
		LastNexusStatus:     "pending_approval",
		LastNexusHTTPStatus: http.StatusOK,
		LastCheckedAt:       time.Now().UTC().Add(-time.Minute),
		LastError:           "",
		ConsecutiveFailures: 0,
		NextCheckAt:         originalNextCheck,
		CreatedAt:           time.Now().UTC().Add(-time.Minute),
		UpdatedAt:           time.Now().UTC().Add(-time.Minute),
	}
	rev := &stubNexus{
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			return nexusclient.RequestSummary{}, http.StatusBadGateway, errors.New("nexus unavailable")
		},
	}
	uc := NewUsecases(repo, rev)
	uc.SetNexusSyncInterval(2 * time.Second)

	if _, err := uc.SyncTaskNexus(ctx, task.ID); err == nil {
		t.Fatal("expected sync error")
	}
	state, err := repo.GetNexusSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ConsecutiveFailures != 1 {
		t.Fatalf("expected 1 failure, got %d", state.ConsecutiveFailures)
	}
	if !strings.Contains(state.LastError, "nexus unavailable") {
		t.Fatalf("unexpected error %q", state.LastError)
	}
	firstBackoff := state.NextCheckAt
	if !firstBackoff.After(originalNextCheck) {
		t.Fatalf("expected next_check_at to advance, got %s", firstBackoff)
	}

	rev.getFn = func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
		return nexusclient.RequestSummary{ID: rid.String(), Status: "pending_approval"}, http.StatusOK, nil
	}
	state.NextCheckAt = time.Now().UTC().Add(-time.Second)
	repo.nexusSync[task.ID] = state

	out, err := uc.SyncTaskNexus(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusWaitingForApproval {
		t.Fatalf("expected waiting_for_approval, got %q", out.Status)
	}
	state, err = repo.GetNexusSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ConsecutiveFailures != 0 || state.LastError != "" {
		t.Fatalf("expected reset failures/error, got %+v", state)
	}
	if state.NextCheckAt.Before(time.Now().UTC()) {
		t.Fatalf("expected future next_check_at, got %s", state.NextCheckAt)
	}
}

func TestUsecases_SyncPendingNexusTasks_syncsOnlyEligibleTasks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{nexusSync: make(map[uuid.UUID]domain.TaskNexusSyncState)}
	eligible := createWaitingTask(t, repo)
	notDue := createWaitingTask(t, repo)
	doneTask, err := NewUsecases(repo, &stubNexus{}).Create(ctx, CreateTaskInput{Title: "done"})
	if err != nil {
		t.Fatal(err)
	}
	doneTask.Status = domain.TaskStatusDone
	doneTask, err = repo.UpdateTask(ctx, doneTask)
	if err != nil {
		t.Fatal(err)
	}

	eligibleRID := uuid.New()
	notDueRID := uuid.New()
	now := time.Now().UTC()
	repo.nexusSync[eligible.ID] = domain.TaskNexusSyncState{
		TaskID:              eligible.ID,
		NexusRequestID:      eligibleRID,
		LastNexusStatus:     "pending_approval",
		LastNexusHTTPStatus: http.StatusOK,
		LastCheckedAt:       now.Add(-time.Minute),
		NextCheckAt:         now.Add(-time.Second),
		CreatedAt:           now.Add(-time.Minute),
		UpdatedAt:           now.Add(-time.Minute),
	}
	repo.nexusSync[notDue.ID] = domain.TaskNexusSyncState{
		TaskID:              notDue.ID,
		NexusRequestID:      notDueRID,
		LastNexusStatus:     "pending_approval",
		LastNexusHTTPStatus: http.StatusOK,
		LastCheckedAt:       now.Add(-time.Minute),
		NextCheckAt:         now.Add(time.Minute),
		CreatedAt:           now.Add(-time.Minute),
		UpdatedAt:           now.Add(-time.Minute),
	}

	uc := NewUsecases(repo, &stubNexus{
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			switch id {
			case eligibleRID.String():
				return nexusclient.RequestSummary{ID: eligibleRID.String(), Status: "approved"}, http.StatusOK, nil
			case notDueRID.String():
				return nexusclient.RequestSummary{ID: notDueRID.String(), Status: "rejected"}, http.StatusOK, nil
			default:
				return nexusclient.RequestSummary{}, http.StatusNotFound, nil
			}
		},
	})

	uc.SyncPendingNexusTasks(ctx, 10)

	eligibleOut, err := repo.GetTaskByID(ctx, eligible.ID)
	if err != nil {
		t.Fatal(err)
	}
	if eligibleOut.Status != domain.TaskStatusDone {
		t.Fatalf("expected eligible task done, got %q", eligibleOut.Status)
	}

	notDueOut, err := repo.GetTaskByID(ctx, notDue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if notDueOut.Status != domain.TaskStatusWaitingForApproval {
		t.Fatalf("expected not-due task unchanged, got %q", notDueOut.Status)
	}

	doneOut, err := repo.GetTaskByID(ctx, doneTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doneOut.Status != domain.TaskStatusDone {
		t.Fatalf("expected done task unchanged, got %q", doneOut.Status)
	}
}

func TestUsecases_ExecuteTask_success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{
		nexusSync:      make(map[uuid.UUID]domain.TaskNexusSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubNexus{}).Create(ctx, CreateTaskInput{Title: "execute"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForInput
	task.NexusStatus = "approved"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	nexusRequestID := uuid.New()
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:          task.ID,
		NexusRequestID:  nexusRequestID,
		LastNexusStatus: "approved",
		LastCheckedAt:   time.Now().UTC(),
		NextCheckAt:     time.Now().UTC().Add(time.Minute),
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	plan := domain.TaskExecutionPlan{
		TaskID:         task.ID,
		ConnectorID:    uuid.New(),
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "exec-1",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	repo.executionPlan[task.ID] = plan

	var gotSpec connectordomain.ExecutionSpec
	uc := NewUsecases(repo, &stubNexus{})
	mem := &stubTaskMemory{}
	uc.SetTaskMemory(mem)
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			gotSpec = spec
			return connectordomain.ExecutionResult{
				ID:             uuid.New(),
				ConnectorID:    spec.ConnectorID,
				Operation:      spec.Operation,
				Status:         connectordomain.ExecSuccess,
				ExternalRef:    "connector-ref",
				Payload:        spec.Payload,
				ResultJSON:     json.RawMessage(`{"sent":true}`),
				TaskID:         spec.TaskID,
				NexusRequestID: spec.NexusRequestID,
				CreatedAt:      time.Now().UTC(),
			}, nil
		},
	})

	out, err := uc.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Task.Status != domain.TaskStatusDone {
		t.Fatalf("expected done, got %q", out.Task.Status)
	}
	if out.Task.ClosedAt == nil {
		t.Fatal("expected task to be closed")
	}
	if repo.countActions(TaskActionExecuteConnector) != 1 {
		t.Fatalf("expected one execute_connector action, got %d", repo.countActions(TaskActionExecuteConnector))
	}
	if repo.countActions(TaskActionVerifyExecution) != 1 {
		t.Fatalf("expected one verify_execution action, got %d", repo.countActions(TaskActionVerifyExecution))
	}
	if len(repo.artifacts) != 2 || repo.artifacts[0].Kind != TaskArtifactConnectorExecution || repo.artifacts[1].Kind != TaskArtifactExecutionVerification {
		t.Fatalf("unexpected artifacts %+v", repo.artifacts)
	}
	if gotSpec.IdempotencyKey != "exec-1" {
		t.Fatalf("expected stored idempotency key, got %q", gotSpec.IdempotencyKey)
	}
	if gotSpec.NexusRequestID == nil || *gotSpec.NexusRequestID != nexusRequestID {
		t.Fatalf("unexpected nexus request id %+v", gotSpec.NexusRequestID)
	}
	if out.ExecutionState.Retryable {
		t.Fatal("expected non-retryable execution state after verified success")
	}
	if out.ExecutionState.RetryCount != 0 {
		t.Fatalf("expected retry_count 0, got %d", out.ExecutionState.RetryCount)
	}
	if out.ExecutionState.VerificationResult.Status != domain.VerificationStatusVerified {
		t.Fatalf("expected verified result, got %q", out.ExecutionState.VerificationResult.Status)
	}
	lastSummary := mem.writes[len(mem.writes)-2]
	if lastSummary.Kind != taskMemoryKindSummary || !strings.Contains(lastSummary.ContentText, "completed successfully") {
		t.Fatalf("unexpected summary write %+v", lastSummary)
	}
}

func TestUsecases_ExecuteTask_failureMarksTaskFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{
		nexusSync:      make(map[uuid.UUID]domain.TaskNexusSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubNexus{}).Create(ctx, CreateTaskInput{Title: "execute failure"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForInput
	task.NexusStatus = "approved"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:      task.ID,
		ConnectorID: uuid.New(),
		Operation:   "mock.write",
		Payload:     json.RawMessage(`{"message":"hello"}`),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	uc := NewUsecases(repo, &stubNexus{})
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			return connectordomain.ExecutionResult{}, errors.New("connector unavailable")
		},
	})

	out, err := uc.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Task.Status != domain.TaskStatusFailed {
		t.Fatalf("expected failed, got %q", out.Task.Status)
	}
	if len(repo.artifacts) != 2 || repo.artifacts[0].Kind != TaskArtifactExecutionError || repo.artifacts[1].Kind != TaskArtifactExecutionVerification {
		t.Fatalf("unexpected artifacts %+v", repo.artifacts)
	}
	if repo.countActions(TaskActionExecuteConnector) != 1 {
		t.Fatalf("expected one execute action, got %d", repo.countActions(TaskActionExecuteConnector))
	}
	if repo.countActions(TaskActionVerifyExecution) != 1 {
		t.Fatalf("expected one verify action, got %d", repo.countActions(TaskActionVerifyExecution))
	}
	if !out.ExecutionState.Retryable {
		t.Fatal("expected retryable state after failure")
	}
	if out.ExecutionState.VerificationResult.Status != domain.VerificationStatusFailed {
		t.Fatalf("expected failed verification status, got %q", out.ExecutionState.VerificationResult.Status)
	}
}

func TestUsecases_ExecuteTask_verificationFailureMarksTaskFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{
		nexusSync:      make(map[uuid.UUID]domain.TaskNexusSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubNexus{}).Create(ctx, CreateTaskInput{Title: "verification failure"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForInput
	task.NexusStatus = "approved"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	nexusRequestID := uuid.New()
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:          task.ID,
		NexusRequestID:  nexusRequestID,
		LastNexusStatus: "approved",
		LastCheckedAt:   time.Now().UTC(),
		NextCheckAt:     time.Now().UTC().Add(time.Minute),
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:      task.ID,
		ConnectorID: uuid.New(),
		Operation:   "mock.echo",
		Payload:     json.RawMessage(`{"message":"hello"}`),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	uc := NewUsecases(repo, &stubNexus{})
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			return connectordomain.ExecutionResult{
				ID:             uuid.New(),
				ConnectorID:    spec.ConnectorID,
				Operation:      spec.Operation,
				Status:         connectordomain.ExecSuccess,
				Payload:        spec.Payload,
				ResultJSON:     json.RawMessage(`{}`),
				TaskID:         spec.TaskID,
				NexusRequestID: spec.NexusRequestID,
				CreatedAt:      time.Now().UTC(),
			}, nil
		},
	})

	out, err := uc.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Task.Status != domain.TaskStatusFailed {
		t.Fatalf("expected failed after verification failure, got %q", out.Task.Status)
	}
	if out.ExecutionState.VerificationResult.Status != domain.VerificationStatusFailed {
		t.Fatalf("expected failed verification result, got %q", out.ExecutionState.VerificationResult.Status)
	}
	if !out.ExecutionState.Retryable {
		t.Fatal("expected retryable state after verification failure")
	}
}

func TestUsecases_RetryTask_reexecutesRetryableFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{
		nexusSync:      make(map[uuid.UUID]domain.TaskNexusSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubNexus{}).Create(ctx, CreateTaskInput{Title: "retry execution"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task.Status = domain.TaskStatusFailed
	task.NexusStatus = "approved"
	task.ClosedAt = &now
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	nexusRequestID := uuid.New()
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:              task.ID,
		NexusRequestID:      nexusRequestID,
		LastNexusStatus:     "approved",
		LastNexusHTTPStatus: http.StatusOK,
		LastCheckedAt:       now,
		NextCheckAt:         now.Add(time.Minute),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:         task.ID,
		ConnectorID:    uuid.New(),
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"retry"}`),
		IdempotencyKey: "retry-me",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	repo.executionState[task.ID] = domain.TaskExecutionState{
		TaskID:              task.ID,
		LastExecutionID:     uuid.New(),
		LastExecutionStatus: connectordomain.ExecFailure,
		Retryable:           true,
		RetryCount:          0,
		LastError:           "connector unavailable",
		LastAttemptedAt:     now,
		VerificationResult: domain.TaskVerificationResult{
			Status:    domain.VerificationStatusFailed,
			Summary:   "connector unavailable",
			CheckedAt: now,
			Details:   json.RawMessage(`{"execution_status":"failure"}`),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	uc := NewUsecases(repo, &stubNexus{
		getFn: func(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
			return nexusclient.RequestSummary{ID: nexusRequestID.String(), Status: "approved"}, http.StatusOK, nil
		},
	})
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			return connectordomain.ExecutionResult{
				ID:             uuid.New(),
				ConnectorID:    spec.ConnectorID,
				Operation:      spec.Operation,
				Status:         connectordomain.ExecSuccess,
				ExternalRef:    "retry-ref",
				Payload:        spec.Payload,
				ResultJSON:     json.RawMessage(`{"ok":true}`),
				TaskID:         spec.TaskID,
				NexusRequestID: spec.NexusRequestID,
				CreatedAt:      time.Now().UTC(),
			}, nil
		},
	})

	out, err := uc.RetryTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Task.Status != domain.TaskStatusDone {
		t.Fatalf("expected done after retry, got %q", out.Task.Status)
	}
	if out.Task.ClosedAt == nil {
		t.Fatal("expected retried task to be closed again")
	}
	if out.ExecutionState.RetryCount != 1 {
		t.Fatalf("expected retry_count 1, got %d", out.ExecutionState.RetryCount)
	}
	if out.ExecutionState.Retryable {
		t.Fatal("expected non-retryable state after successful retry")
	}
	if repo.countActions(TaskActionRetryExecution) != 1 {
		t.Fatalf("expected one retry action, got %d", repo.countActions(TaskActionRetryExecution))
	}
}

// TestList_RejectsEmptyOrgID asegura que la firma strict de Usecases.List
// cierre el leak histórico: orgID vacío debe fallar con TenantMissing y
// nunca devolver tasks (Fase 5 del plan multitenancy).
func TestList_RejectsEmptyOrgID(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{tasks: map[uuid.UUID]domain.Task{
		uuid.New(): {ID: uuid.New(), Title: "leak-bait", OrgID: "tenant-A"},
		uuid.New(): {ID: uuid.New(), Title: "leak-bait-2", OrgID: "tenant-B"},
	}}
	uc := NewUsecases(repo, &stubNexus{})

	got, err := uc.List(context.Background(), tenant.ID(""), 100)
	if err == nil {
		t.Fatal("expected error for empty orgID")
	}
	if !errors.Is(err, domainerr.TenantMissing()) {
		t.Errorf("expected TenantMissing, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero rows on error, got %d (LEAK)", len(got))
	}
}

// TestList_FiltersByOrgID confirma que orgID válido SÍ aplica el filtro
// y no incluye tasks de otros tenants.
func TestList_FiltersByOrgID(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{tasks: map[uuid.UUID]domain.Task{
		uuid.New(): {ID: uuid.New(), Title: "A1", OrgID: "tenant-A"},
		uuid.New(): {ID: uuid.New(), Title: "A2", OrgID: "tenant-A"},
		uuid.New(): {ID: uuid.New(), Title: "B1", OrgID: "tenant-B"},
	}}
	uc := NewUsecases(repo, &stubNexus{})

	got, err := uc.List(context.Background(), tenant.FromString("tenant-A"), 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows for tenant-A, got %d", len(got))
	}
	for _, task := range got {
		if task.OrgID != "tenant-A" {
			t.Errorf("leaked row: %+v", task)
		}
	}
}

// TestListAll_BypassesTenantScope confirma que ListAll devuelve TODAS las
// rows — pensado para flows admin con scope companion:cross_org. El handler
// es quien valida el caller; el usecase confía en lo que recibe.
func TestListAll_BypassesTenantScope(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{tasks: map[uuid.UUID]domain.Task{
		uuid.New(): {ID: uuid.New(), Title: "A", OrgID: "tenant-A"},
		uuid.New(): {ID: uuid.New(), Title: "B", OrgID: "tenant-B"},
	}}
	uc := NewUsecases(repo, &stubNexus{})

	got, err := uc.ListAll(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows across orgs, got %d", len(got))
	}
}

func TestNotifyAlert_PreservesOrgID(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})

	if err := uc.NotifyAlert(context.Background(), "org-alerts", "stock bajo"); err != nil {
		t.Fatal(err)
	}
	if len(repo.tasks) != 1 {
		t.Fatalf("expected one alert task, got %d", len(repo.tasks))
	}
	for _, task := range repo.tasks {
		if task.OrgID != "org-alerts" {
			t.Fatalf("expected alert task org_id to be preserved, got %q", task.OrgID)
		}
	}
}
