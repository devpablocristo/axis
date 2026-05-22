package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// makeWaitingForApprovalTask prepara una task en estado WaitingForApproval con
// nexus_request_id y un sync state inicial para cubrir el path donde
// ExecuteTask sincroniza con nexus antes de evaluar el gate.
func makeWaitingForApprovalTask(t *testing.T, repo *fakeRepo, nexusRequestID uuid.UUID) domain.Task {
	t.Helper()
	ctx := context.Background()
	uc := NewUsecases(repo, &stubNexus{})
	task, err := uc.Create(ctx, CreateTaskInput{Title: "nexus-gate"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForApproval
	task.NexusStatus = "pending"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	repo.nexusSync[task.ID] = domain.TaskNexusSyncState{
		TaskID:          task.ID,
		NexusRequestID:  nexusRequestID,
		LastNexusStatus: "pending",
		LastCheckedAt:   now,
		NextCheckAt:     now.Add(time.Minute),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:         task.ID,
		ConnectorID:    uuid.New(),
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"x":1}`),
		IdempotencyKey: "k",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return task
}

func newGateTestRepo() *fakeRepo {
	return &fakeRepo{
		nexusSync:      make(map[uuid.UUID]domain.TaskNexusSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
}

// nexusGetter devuelve un nexus summary con un status configurable, simulando
// la respuesta de Nexus al sincronizar.
func nexusGetter(status string) func(_ context.Context, id string) (nexusclient.RequestSummary, int, error) {
	return func(_ context.Context, id string) (nexusclient.RequestSummary, int, error) {
		return nexusclient.RequestSummary{
			ID:     id,
			Status: status,
		}, http.StatusOK, nil
	}
}

func TestExecuteTask_BlocksWhenNexusPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	nexusRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, nexusRequestID)

	uc := NewUsecases(repo, &stubNexus{getFn: nexusGetter("pending")})
	uc.SetExecutor(&stubExecutor{})

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error when nexus is pending")
	}
	if !IsNexusNotApproved(err) {
		t.Fatalf("expected typed ErrNexusNotApproved, got %v", err)
	}
	if IsInvalidTaskState(err) {
		t.Fatal("nexus block should not be reported as ErrInvalidTaskState")
	}
	blocked, ok := AsNexusBlocked(err)
	if !ok {
		t.Fatal("expected AsNexusBlocked to extract detail")
	}
	if blocked.NexusStatus != "pending" {
		t.Fatalf("expected status=pending, got %q", blocked.NexusStatus)
	}
	if blocked.NexusRequestID != nexusRequestID.String() {
		t.Fatalf("expected nexus_request_id %s, got %q", nexusRequestID, blocked.NexusRequestID)
	}
	if blocked.Reason != "execute" {
		t.Fatalf("expected reason=execute, got %q", blocked.Reason)
	}
}

func TestExecuteTask_BlocksWhenNexusDenied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	nexusRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, nexusRequestID)

	uc := NewUsecases(repo, &stubNexus{getFn: nexusGetter("denied")})
	uc.SetExecutor(&stubExecutor{})

	_, err := uc.ExecuteTask(ctx, task.ID)
	if !IsNexusNotApproved(err) {
		t.Fatalf("expected ErrNexusNotApproved for denied nexus, got %v", err)
	}
	blocked, _ := AsNexusBlocked(err)
	if blocked.NexusStatus != "denied" {
		t.Fatalf("expected status=denied, got %q", blocked.NexusStatus)
	}
}

func TestExecuteTask_AllowsWhenNexusApproved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	nexusRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, nexusRequestID)

	executed := false
	uc := NewUsecases(repo, &stubNexus{getFn: nexusGetter("approved")})
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			executed = true
			return connectordomain.ExecutionResult{
				ID:             uuid.New(),
				ConnectorID:    spec.ConnectorID,
				Operation:      spec.Operation,
				Status:         connectordomain.ExecSuccess,
				ExternalRef:    "ref",
				Payload:        spec.Payload,
				ResultJSON:     json.RawMessage(`{"ok":true}`),
				TaskID:         spec.TaskID,
				NexusRequestID: spec.NexusRequestID,
				CreatedAt:      time.Now().UTC(),
			}, nil
		},
	})

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("expected approved execution to succeed, got %v", err)
	}
	if !executed {
		t.Fatal("expected executor to be called when nexus is approved")
	}
}

func TestErrNexusNotApproved_IsStructured(t *testing.T) {
	t.Parallel()
	if errors.Is(ErrNexusNotApproved, ErrInvalidTaskState) {
		t.Fatal("ErrNexusNotApproved should not wrap ErrInvalidTaskState")
	}
	blocked := &NexusBlockedError{NexusRequestID: "rid", NexusStatus: "pending", Reason: "execute"}
	if !errors.Is(blocked, ErrNexusNotApproved) {
		t.Fatal("NexusBlockedError must satisfy errors.Is(_, ErrNexusNotApproved)")
	}
	if errors.Is(blocked, ErrInvalidTaskState) {
		t.Fatal("NexusBlockedError should not satisfy errors.Is(_, ErrInvalidTaskState)")
	}
}
