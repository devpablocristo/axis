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
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/kernels/governance/go/governanceclient"
)

type fakeRepo struct {
	tasks          map[uuid.UUID]domain.Task
	lastPropose    map[uuid.UUID]uuid.UUID
	actions        []domain.TaskAction
	artifacts      []domain.TaskArtifact
	governanceSync map[uuid.UUID]domain.TaskGovernanceSyncState
	executionPlan  map[uuid.UUID]domain.TaskExecutionPlan
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
	if state, ok := f.governanceSync[id]; ok {
		t.GovernanceStatus = state.LastGovernanceStatus
		t.GovernanceLastCheckedAt = &state.LastCheckedAt
		t.GovernanceSyncError = state.LastError
	}
	return t, nil
}

func (f *fakeRepo) ListTasks(ctx context.Context, orgID string, limit int) ([]domain.Task, error) {
	var out []domain.Task
	for _, t := range f.tasks {
		if orgID != "" && t.OrgID != "" && t.OrgID != orgID {
			continue
		}
		if state, ok := f.governanceSync[t.ID]; ok {
			t.GovernanceStatus = state.LastGovernanceStatus
			t.GovernanceLastCheckedAt = &state.LastCheckedAt
			t.GovernanceSyncError = state.LastError
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

func (f *fakeRepo) ListTasksPendingGovernanceSync(ctx context.Context, now time.Time, limit int) ([]domain.Task, error) {
	var out []domain.Task
	for _, t := range f.tasks {
		if t.Status != domain.TaskStatusWaitingForApproval {
			continue
		}
		state, ok := f.governanceSync[t.ID]
		if ok && state.NextCheckAt.After(now) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeRepo) LatestProposeGovernanceRequestID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	if f.lastPropose == nil {
		return uuid.Nil, ErrNotFound
	}
	rid, ok := f.lastPropose[taskID]
	if !ok {
		return uuid.Nil, ErrNotFound
	}
	return rid, nil
}

func (f *fakeRepo) GetGovernanceSyncState(ctx context.Context, taskID uuid.UUID) (domain.TaskGovernanceSyncState, error) {
	if f.governanceSync == nil {
		return domain.TaskGovernanceSyncState{}, ErrNotFound
	}
	state, ok := f.governanceSync[taskID]
	if !ok {
		return domain.TaskGovernanceSyncState{}, ErrNotFound
	}
	return state, nil
}

func (f *fakeRepo) UpsertGovernanceSyncState(ctx context.Context, s domain.TaskGovernanceSyncState) (domain.TaskGovernanceSyncState, error) {
	if f.governanceSync == nil {
		f.governanceSync = make(map[uuid.UUID]domain.TaskGovernanceSyncState)
	}
	if existing, ok := f.governanceSync[s.TaskID]; ok {
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
	f.governanceSync[s.TaskID] = s
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

func (f *fakeRepo) UpdateActionGovernanceResult(ctx context.Context, actionID uuid.UUID, governanceRequestID *uuid.UUID, errMsg string) error {
	for i := range f.actions {
		if f.actions[i].ID != actionID {
			continue
		}
		f.actions[i].GovernanceRequestID = governanceRequestID
		f.actions[i].ErrorMessage = errMsg
		if governanceRequestID != nil && f.actions[i].ActionType == TaskActionPropose {
			if f.lastPropose == nil {
				f.lastPropose = make(map[uuid.UUID]uuid.UUID)
			}
			f.lastPropose[f.actions[i].TaskID] = *governanceRequestID
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

type stubGovernance struct {
	submitFn func(ctx context.Context, idempotencyKey string, body governanceclient.SubmitRequestBody) (governanceclient.SubmitResponse, error)
	getFn    func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error)
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
		"org_id":          spec.OrgID,
		"actor_id":        spec.ActorID,
		"actor_type":      "agent",
		"product_surface": "companion",
		"connector_id":    spec.ConnectorID.String(),
		"capability_id":   spec.Operation,
		"operation":       spec.Operation,
		"target_system":   "mock",
		"target_resource": spec.ConnectorID.String(),
		"payload_hash":    "payload-hash",
		"idempotency_key": spec.IdempotencyKey,
	}
	return binding, "binding-hash", nil
}

func (s *stubExecutor) Execute(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
	if s.executeFn != nil {
		return s.executeFn(ctx, spec)
	}
	return connectordomain.ExecutionResult{
		ID:                  uuid.New(),
		ConnectorID:         spec.ConnectorID,
		Operation:           spec.Operation,
		Status:              connectordomain.ExecSuccess,
		ExternalRef:         "exec-ref",
		Payload:             spec.Payload,
		ResultJSON:          json.RawMessage(`{"ok":true}`),
		TaskID:              spec.TaskID,
		GovernanceRequestID: spec.GovernanceRequestID,
		CreatedAt:           time.Now().UTC(),
	}, nil
}

func (s *stubGovernance) SubmitRequest(ctx context.Context, idempotencyKey string, body governanceclient.SubmitRequestBody) (governanceclient.SubmitResponse, error) {
	if s.submitFn != nil {
		return s.submitFn(ctx, idempotencyKey, body)
	}
	return governanceclient.SubmitResponse{}, nil
}

func (s *stubGovernance) GetRequest(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
	if s.getFn != nil {
		return s.getFn(ctx, id)
	}
	return governanceclient.RequestSummary{}, http.StatusNotFound, nil
}

func (s *stubGovernance) ReportResult(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error) {
	if s.reportFn != nil {
		return s.reportFn(ctx, id, success, result, durationMS, errorMessage)
	}
	return http.StatusOK, nil
}

func createWaitingTask(t *testing.T, repo *fakeRepo) domain.Task {
	t.Helper()
	uc := NewUsecases(repo, &stubGovernance{})
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
	uc := NewUsecases(&fakeRepo{}, &stubGovernance{})
	_, err := uc.Create(context.Background(), CreateTaskInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUsecases_Create_ok(t *testing.T) {
	t.Parallel()
	r := &fakeRepo{}
	uc := NewUsecases(r, &stubGovernance{})
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
	uc := NewUsecases(&fakeRepo{}, &stubGovernance{})

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
	uc := NewUsecases(repo, &stubGovernance{})
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

func TestUsecases_Propose_persistsInitialGovernanceSyncState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{}
	governanceRequestID := uuid.New()
	mem := &stubTaskMemory{}
	uc := NewUsecases(repo, &stubGovernance{
		submitFn: func(ctx context.Context, idempotencyKey string, body governanceclient.SubmitRequestBody) (governanceclient.SubmitResponse, error) {
			return governanceclient.SubmitResponse{
				RequestID: governanceRequestID.String(),
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
	if action.GovernanceRequestID == nil || *action.GovernanceRequestID != governanceRequestID {
		t.Fatalf("unexpected action governance request id %+v", action.GovernanceRequestID)
	}

	state, err := repo.GetGovernanceSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.GovernanceRequestID != governanceRequestID {
		t.Fatalf("unexpected state governance_request_id %s", state.GovernanceRequestID)
	}
	if state.LastGovernanceStatus != "pending_approval" {
		t.Fatalf("unexpected state status %q", state.LastGovernanceStatus)
	}
	if state.LastGovernanceHTTPStatus != http.StatusCreated {
		t.Fatalf("unexpected state http status %d", state.LastGovernanceHTTPStatus)
	}
	if state.LastError != "" || state.ConsecutiveFailures != 0 {
		t.Fatalf("unexpected state error/failures %+v", state)
	}
	if len(mem.writes) != 6 {
		t.Fatalf("expected create+plan+propose memory projection, got %d writes", len(mem.writes))
	}
	lastSummary := mem.writes[len(mem.writes)-2]
	if lastSummary.Kind != taskMemoryKindSummary || !strings.Contains(lastSummary.ContentText, "waiting for Governance") {
		t.Fatalf("unexpected summary write %+v", lastSummary)
	}
}

func TestUsecases_SyncTaskGovernance_pendingIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	lastChecked := time.Now().UTC().Add(-time.Minute)
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:                   task.ID,
		GovernanceRequestID:      rid,
		LastGovernanceStatus:     "pending_approval",
		LastGovernanceHTTPStatus: http.StatusOK,
		LastCheckedAt:            lastChecked,
		LastError:                "",
		ConsecutiveFailures:      0,
		NextCheckAt:              time.Now().UTC().Add(-time.Second),
		CreatedAt:                lastChecked,
		UpdatedAt:                lastChecked,
	}
	uc := NewUsecases(repo, &stubGovernance{
		getFn: func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
			return governanceclient.RequestSummary{ID: rid.String(), Status: "pending_approval"}, http.StatusOK, nil
		},
	})
	uc.SetGovernanceSyncInterval(5 * time.Second)

	out, err := uc.SyncTaskGovernance(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusWaitingForApproval {
		t.Fatalf("expected waiting_for_approval, got %q", out.Status)
	}
	if repo.countActions(TaskActionSyncGovernance) != 0 {
		t.Fatalf("expected no sync_governance action, got %d", repo.countActions(TaskActionSyncGovernance))
	}

	state, err := repo.GetGovernanceSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastGovernanceStatus != "pending_approval" || state.LastError != "" || state.ConsecutiveFailures != 0 {
		t.Fatalf("unexpected state %+v", state)
	}
	if !state.LastCheckedAt.After(lastChecked) {
		t.Fatalf("expected LastCheckedAt to move forward: %s <= %s", state.LastCheckedAt, lastChecked)
	}
}

func TestUsecases_SyncTaskGovernance_approvedToDone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:                   task.ID,
		GovernanceRequestID:      rid,
		LastGovernanceStatus:     "pending_approval",
		LastGovernanceHTTPStatus: http.StatusCreated,
		LastCheckedAt:            time.Now().UTC().Add(-time.Minute),
		NextCheckAt:              time.Now().UTC().Add(-time.Second),
		CreatedAt:                time.Now().UTC().Add(-time.Minute),
		UpdatedAt:                time.Now().UTC().Add(-time.Minute),
	}
	uc := NewUsecases(repo, &stubGovernance{
		getFn: func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
			if id != rid.String() {
				return governanceclient.RequestSummary{}, http.StatusNotFound, nil
			}
			return governanceclient.RequestSummary{ID: rid.String(), Status: "approved"}, http.StatusOK, nil
		},
	})

	out, err := uc.SyncTaskGovernance(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusDone {
		t.Fatalf("expected done, got %q", out.Status)
	}
	if out.ClosedAt == nil {
		t.Fatal("expected ClosedAt on terminal state")
	}
	if repo.countActions(TaskActionSyncGovernance) != 1 {
		t.Fatalf("expected one sync_governance action, got %d", repo.countActions(TaskActionSyncGovernance))
	}
	state, err := repo.GetGovernanceSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastGovernanceStatus != "approved" || state.LastError != "" || state.ConsecutiveFailures != 0 {
		t.Fatalf("unexpected state %+v", state)
	}
}

func TestUsecases_SyncTaskGovernance_approvedWithExecutionPlanToWaitingForInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{
		governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
	}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:                   task.ID,
		GovernanceRequestID:      rid,
		LastGovernanceStatus:     "pending_approval",
		LastGovernanceHTTPStatus: http.StatusCreated,
		LastCheckedAt:            time.Now().UTC().Add(-time.Minute),
		NextCheckAt:              time.Now().UTC().Add(-time.Second),
		CreatedAt:                time.Now().UTC().Add(-time.Minute),
		UpdatedAt:                time.Now().UTC().Add(-time.Minute),
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:      task.ID,
		ConnectorID: uuid.New(),
		Operation:   "mock.write",
		Payload:     json.RawMessage(`{"message":"run"}`),
		CreatedAt:   time.Now().UTC().Add(-time.Minute),
		UpdatedAt:   time.Now().UTC().Add(-time.Minute),
	}
	uc := NewUsecases(repo, &stubGovernance{
		getFn: func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
			return governanceclient.RequestSummary{ID: rid.String(), Status: "approved"}, http.StatusOK, nil
		},
	})

	out, err := uc.SyncTaskGovernance(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusWaitingForInput {
		t.Fatalf("expected waiting_for_input, got %q", out.Status)
	}
}

func TestUsecases_SyncTaskGovernance_rejectedToFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{lastPropose: make(map[uuid.UUID]uuid.UUID)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	repo.lastPropose[task.ID] = rid
	uc := NewUsecases(repo, &stubGovernance{
		getFn: func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
			return governanceclient.RequestSummary{ID: rid.String(), Status: "rejected"}, http.StatusOK, nil
		},
	})

	out, err := uc.SyncTaskGovernance(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusFailed {
		t.Fatalf("expected failed, got %q", out.Status)
	}
	if out.ClosedAt == nil {
		t.Fatal("expected ClosedAt on failed task")
	}
	state, err := repo.GetGovernanceSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastGovernanceStatus != "rejected" {
		t.Fatalf("unexpected state status %q", state.LastGovernanceStatus)
	}
}

func TestUsecases_SyncTaskGovernance_errorBackoffThenReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState)}
	task := createWaitingTask(t, repo)
	rid := uuid.New()
	originalNextCheck := time.Now().UTC().Add(-time.Second)
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:                   task.ID,
		GovernanceRequestID:      rid,
		LastGovernanceStatus:     "pending_approval",
		LastGovernanceHTTPStatus: http.StatusOK,
		LastCheckedAt:            time.Now().UTC().Add(-time.Minute),
		LastError:                "",
		ConsecutiveFailures:      0,
		NextCheckAt:              originalNextCheck,
		CreatedAt:                time.Now().UTC().Add(-time.Minute),
		UpdatedAt:                time.Now().UTC().Add(-time.Minute),
	}
	rev := &stubGovernance{
		getFn: func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
			return governanceclient.RequestSummary{}, http.StatusBadGateway, errors.New("governance unavailable")
		},
	}
	uc := NewUsecases(repo, rev)
	uc.SetGovernanceSyncInterval(2 * time.Second)

	if _, err := uc.SyncTaskGovernance(ctx, task.ID); err == nil {
		t.Fatal("expected sync error")
	}
	state, err := repo.GetGovernanceSyncState(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ConsecutiveFailures != 1 {
		t.Fatalf("expected 1 failure, got %d", state.ConsecutiveFailures)
	}
	if !strings.Contains(state.LastError, "governance unavailable") {
		t.Fatalf("unexpected error %q", state.LastError)
	}
	firstBackoff := state.NextCheckAt
	if !firstBackoff.After(originalNextCheck) {
		t.Fatalf("expected next_check_at to advance, got %s", firstBackoff)
	}

	rev.getFn = func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
		return governanceclient.RequestSummary{ID: rid.String(), Status: "pending_approval"}, http.StatusOK, nil
	}
	state.NextCheckAt = time.Now().UTC().Add(-time.Second)
	repo.governanceSync[task.ID] = state

	out, err := uc.SyncTaskGovernance(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.TaskStatusWaitingForApproval {
		t.Fatalf("expected waiting_for_approval, got %q", out.Status)
	}
	state, err = repo.GetGovernanceSyncState(ctx, task.ID)
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

func TestUsecases_SyncPendingGovernanceTasks_syncsOnlyEligibleTasks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := &fakeRepo{governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState)}
	eligible := createWaitingTask(t, repo)
	notDue := createWaitingTask(t, repo)
	doneTask, err := NewUsecases(repo, &stubGovernance{}).Create(ctx, CreateTaskInput{Title: "done"})
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
	repo.governanceSync[eligible.ID] = domain.TaskGovernanceSyncState{
		TaskID:                   eligible.ID,
		GovernanceRequestID:      eligibleRID,
		LastGovernanceStatus:     "pending_approval",
		LastGovernanceHTTPStatus: http.StatusOK,
		LastCheckedAt:            now.Add(-time.Minute),
		NextCheckAt:              now.Add(-time.Second),
		CreatedAt:                now.Add(-time.Minute),
		UpdatedAt:                now.Add(-time.Minute),
	}
	repo.governanceSync[notDue.ID] = domain.TaskGovernanceSyncState{
		TaskID:                   notDue.ID,
		GovernanceRequestID:      notDueRID,
		LastGovernanceStatus:     "pending_approval",
		LastGovernanceHTTPStatus: http.StatusOK,
		LastCheckedAt:            now.Add(-time.Minute),
		NextCheckAt:              now.Add(time.Minute),
		CreatedAt:                now.Add(-time.Minute),
		UpdatedAt:                now.Add(-time.Minute),
	}

	uc := NewUsecases(repo, &stubGovernance{
		getFn: func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
			switch id {
			case eligibleRID.String():
				return governanceclient.RequestSummary{ID: eligibleRID.String(), Status: "approved"}, http.StatusOK, nil
			case notDueRID.String():
				return governanceclient.RequestSummary{ID: notDueRID.String(), Status: "rejected"}, http.StatusOK, nil
			default:
				return governanceclient.RequestSummary{}, http.StatusNotFound, nil
			}
		},
	})

	uc.SyncPendingGovernanceTasks(ctx, 10)

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
		governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubGovernance{}).Create(ctx, CreateTaskInput{Title: "execute"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForInput
	task.GovernanceStatus = "approved"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	governanceRequestID := uuid.New()
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:               task.ID,
		GovernanceRequestID:  governanceRequestID,
		LastGovernanceStatus: "approved",
		LastCheckedAt:        time.Now().UTC(),
		NextCheckAt:          time.Now().UTC().Add(time.Minute),
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
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
	uc := NewUsecases(repo, &stubGovernance{})
	mem := &stubTaskMemory{}
	uc.SetTaskMemory(mem)
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			gotSpec = spec
			return connectordomain.ExecutionResult{
				ID:                  uuid.New(),
				ConnectorID:         spec.ConnectorID,
				Operation:           spec.Operation,
				Status:              connectordomain.ExecSuccess,
				ExternalRef:         "connector-ref",
				Payload:             spec.Payload,
				ResultJSON:          json.RawMessage(`{"sent":true}`),
				TaskID:              spec.TaskID,
				GovernanceRequestID: spec.GovernanceRequestID,
				CreatedAt:           time.Now().UTC(),
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
	if gotSpec.GovernanceRequestID == nil || *gotSpec.GovernanceRequestID != governanceRequestID {
		t.Fatalf("unexpected governance request id %+v", gotSpec.GovernanceRequestID)
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
		governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubGovernance{}).Create(ctx, CreateTaskInput{Title: "execute failure"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForInput
	task.GovernanceStatus = "approved"
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

	uc := NewUsecases(repo, &stubGovernance{})
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
		governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubGovernance{}).Create(ctx, CreateTaskInput{Title: "verification failure"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForInput
	task.GovernanceStatus = "approved"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	governanceRequestID := uuid.New()
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:               task.ID,
		GovernanceRequestID:  governanceRequestID,
		LastGovernanceStatus: "approved",
		LastCheckedAt:        time.Now().UTC(),
		NextCheckAt:          time.Now().UTC().Add(time.Minute),
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:      task.ID,
		ConnectorID: uuid.New(),
		Operation:   "mock.echo",
		Payload:     json.RawMessage(`{"message":"hello"}`),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	uc := NewUsecases(repo, &stubGovernance{})
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			return connectordomain.ExecutionResult{
				ID:                  uuid.New(),
				ConnectorID:         spec.ConnectorID,
				Operation:           spec.Operation,
				Status:              connectordomain.ExecSuccess,
				Payload:             spec.Payload,
				ResultJSON:          json.RawMessage(`{}`),
				TaskID:              spec.TaskID,
				GovernanceRequestID: spec.GovernanceRequestID,
				CreatedAt:           time.Now().UTC(),
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
		governanceSync: make(map[uuid.UUID]domain.TaskGovernanceSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
	task, err := NewUsecases(repo, &stubGovernance{}).Create(ctx, CreateTaskInput{Title: "retry execution"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task.Status = domain.TaskStatusFailed
	task.GovernanceStatus = "approved"
	task.ClosedAt = &now
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	governanceRequestID := uuid.New()
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:                   task.ID,
		GovernanceRequestID:      governanceRequestID,
		LastGovernanceStatus:     "approved",
		LastGovernanceHTTPStatus: http.StatusOK,
		LastCheckedAt:            now,
		NextCheckAt:              now.Add(time.Minute),
		CreatedAt:                now,
		UpdatedAt:                now,
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

	uc := NewUsecases(repo, &stubGovernance{
		getFn: func(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
			return governanceclient.RequestSummary{ID: governanceRequestID.String(), Status: "approved"}, http.StatusOK, nil
		},
	})
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			return connectordomain.ExecutionResult{
				ID:                  uuid.New(),
				ConnectorID:         spec.ConnectorID,
				Operation:           spec.Operation,
				Status:              connectordomain.ExecSuccess,
				ExternalRef:         "retry-ref",
				Payload:             spec.Payload,
				ResultJSON:          json.RawMessage(`{"ok":true}`),
				TaskID:              spec.TaskID,
				GovernanceRequestID: spec.GovernanceRequestID,
				CreatedAt:           time.Now().UTC(),
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

func TestNotifyAlert_PreservesOrgID(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubGovernance{})

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
