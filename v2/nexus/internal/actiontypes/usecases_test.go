package actiontypes

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestUseCasesCreateNormalizesTenant(t *testing.T) {
	repo := newFakeActionTypeRepo()
	uc := NewUseCases(repo)

	out, err := uc.Create(context.Background(), "", domain.CreateInput{
		ActionTypeKey: "calendar.events.create",
		Name:          "Create event",
		RiskClass:     "medium",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if out.TenantID != DefaultTenantID {
		t.Fatalf("tenant id = %q, want %q", out.TenantID, DefaultTenantID)
	}
	if out.RiskClass != domain.RiskClassMedium {
		t.Fatalf("risk class = %q, want medium", out.RiskClass)
	}
}

func TestUseCasesTenantIsolation(t *testing.T) {
	repo := newFakeActionTypeRepo()
	uc := NewUseCases(repo)
	_, err := uc.Create(context.Background(), "tenant-a", domain.CreateInput{
		ActionTypeKey: "calendar.events.create",
		Name:          "Create event",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = uc.GetByKey(context.Background(), "tenant-b", "calendar.events.create")
	if !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found for other tenant, got %v", err)
	}
}

func TestUseCasesGetByKeyFallsBackToDefaultTenant(t *testing.T) {
	repo := newFakeActionTypeRepo()
	uc := NewUseCases(repo)
	_, err := uc.Create(context.Background(), "", domain.CreateInput{
		ActionTypeKey: "calendar.events.delete",
		Name:          "Delete event",
		RiskClass:     "high",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	out, err := uc.GetByKey(context.Background(), "tenant-a", "calendar.events.delete")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if out.TenantID != DefaultTenantID {
		t.Fatalf("fallback tenant id = %q, want %q", out.TenantID, DefaultTenantID)
	}
	if out.RiskClass != domain.RiskClassHigh {
		t.Fatalf("risk class = %q, want high", out.RiskClass)
	}
}

type fakeActionTypeRepo struct {
	rows map[uuid.UUID]domain.ActionType
}

func newFakeActionTypeRepo() *fakeActionTypeRepo {
	return &fakeActionTypeRepo{rows: map[uuid.UUID]domain.ActionType{}}
}

func (r *fakeActionTypeRepo) Create(_ context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.ActionType, error) {
	id := uuid.New()
	now := time.Now().UTC()
	row := domain.ActionType{
		ID:            id,
		TenantID:      tenantID,
		ActionTypeKey: input.ActionTypeKey,
		Name:          input.Name,
		Description:   input.Description,
		Category:      input.Category,
		RiskClass:     input.RiskClass,
		Enabled:       input.Enabled,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	r.rows[id] = row
	return row, nil
}

func (r *fakeActionTypeRepo) List(_ context.Context, tenantID string) ([]domain.ActionType, error) {
	out := []domain.ActionType{}
	for _, row := range r.rows {
		if row.TenantID == tenantID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeActionTypeRepo) Get(_ context.Context, tenantID string, id uuid.UUID) (domain.ActionType, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.ActionType{}, domainerr.NotFound("action type not found")
	}
	return row, nil
}

func (r *fakeActionTypeRepo) GetByKey(_ context.Context, tenantID string, key string) (domain.ActionType, error) {
	for _, row := range r.rows {
		if row.TenantID == tenantID && row.ActionTypeKey == key {
			return row, nil
		}
	}
	return domain.ActionType{}, domainerr.NotFound("action type not found")
}

func (r *fakeActionTypeRepo) Update(_ context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ActionType, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.ActionType{}, domainerr.NotFound("action type not found")
	}
	row.Name = input.Name
	row.Description = input.Description
	row.Category = input.Category
	row.RiskClass = input.RiskClass
	row.Enabled = input.Enabled
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return row, nil
}
