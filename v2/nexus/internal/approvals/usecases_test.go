package approvals

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestUseCasesListDefaultsToPending(t *testing.T) {
	repo := newFakeRepo()
	pending := repo.add("tenant-1", domain.StatusPending)
	repo.add("tenant-1", domain.StatusApproved)
	uc := NewUseCases(repo)

	out, err := uc.List(context.Background(), "tenant-1", "", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 1 || out[0].ID != pending.ID {
		t.Fatalf("unexpected list: %+v", out)
	}
}

func TestUseCasesGet(t *testing.T) {
	repo := newFakeRepo()
	item := repo.add("tenant-1", domain.StatusPending)
	repo.add("tenant-2", domain.StatusPending)
	uc := NewUseCases(repo)

	out, err := uc.Get(context.Background(), "tenant-1", item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.ID != item.ID || out.TenantID != "tenant-1" {
		t.Fatalf("unexpected item: %+v", out)
	}

	_, err = uc.Get(context.Background(), "tenant-2", item.ID)
	if !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found for another tenant, got %v", err)
	}
}

func TestUseCasesApproveAndReject(t *testing.T) {
	repo := newFakeRepo()
	approve := repo.add("tenant-1", domain.StatusPending)
	reject := repo.add("tenant-1", domain.StatusPending)
	uc := NewUseCases(repo)

	approved, err := uc.Approve(context.Background(), "tenant-1", approve.ID, "approver-1", domain.DecisionInput{Note: "ok"})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != domain.StatusApproved || approved.DecidedBy != "approver-1" || approved.DecisionNote != "ok" || approved.DecidedAt == nil {
		t.Fatalf("unexpected approved item: %+v", approved)
	}

	rejected, err := uc.Reject(context.Background(), "tenant-1", reject.ID, "approver-2", domain.DecisionInput{Note: "no"})
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if rejected.Status != domain.StatusRejected || rejected.DecidedBy != "approver-2" || rejected.DecisionNote != "no" || rejected.DecidedAt == nil {
		t.Fatalf("unexpected rejected item: %+v", rejected)
	}
}

func TestUseCasesRejectsInvalidDecisions(t *testing.T) {
	repo := newFakeRepo()
	item := repo.add("tenant-1", domain.StatusApproved)
	uc := NewUseCases(repo)

	_, err := uc.Approve(context.Background(), "tenant-1", item.ID, "approver-1", domain.DecisionInput{})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict deciding already decided approval, got %v", err)
	}

	_, err = uc.Approve(context.Background(), "tenant-1", uuid.New(), "", domain.DecisionInput{})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing actor, got %v", err)
	}

	_, err = uc.List(context.Background(), "tenant-1", "bad", 0)
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid status, got %v", err)
	}
}

type fakeRepo struct {
	rows map[uuid.UUID]domain.Approval
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: make(map[uuid.UUID]domain.Approval)}
}

func (r *fakeRepo) add(tenantID string, status domain.Status) domain.Approval {
	now := time.Now().UTC()
	row := domain.Approval{
		ID:                uuid.New(),
		TenantID:          tenantID,
		GovernanceCheckID: uuid.New(),
		RequesterID:       "virployee-1",
		ActionType:        "calendar.events.delete",
		TargetSystem:      "calendar",
		TargetResource:    "events",
		RiskLevel:         "high",
		Reason:            "delete event",
		BindingHash:       "binding-hash",
		Status:            status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	r.rows[row.ID] = row
	return row
}

func (r *fakeRepo) List(_ context.Context, tenantID string, status domain.Status, limit int) ([]domain.Approval, error) {
	out := []domain.Approval{}
	for _, row := range r.rows {
		if row.TenantID != tenantID || row.Status != status {
			continue
		}
		out = append(out, row)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *fakeRepo) Get(_ context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.Approval{}, domainerr.NotFound("approval not found")
	}
	return row, nil
}

func (r *fakeRepo) Decide(_ context.Context, tenantID string, id uuid.UUID, status domain.Status, actorID string, note string) (domain.Approval, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.Approval{}, domainerr.NotFound("approval not found")
	}
	if row.Status != domain.StatusPending {
		return domain.Approval{}, domainerr.Conflict("approval is already decided")
	}
	now := time.Now().UTC()
	row.Status = status
	row.DecidedBy = actorID
	row.DecisionNote = note
	row.DecidedAt = &now
	row.UpdatedAt = now
	r.rows[id] = row
	return row, nil
}
