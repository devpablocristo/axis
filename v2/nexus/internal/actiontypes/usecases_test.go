package actiontypes

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestUseCasesCreateNormalizesOrg(t *testing.T) {
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
	if out.OrgID != DefaultOrgID {
		t.Fatalf("organization id = %q, want %q", out.OrgID, DefaultOrgID)
	}
	if out.RiskClass != domain.RiskClassMedium {
		t.Fatalf("risk class = %q, want medium", out.RiskClass)
	}
}

func TestUseCasesOrgIsolation(t *testing.T) {
	repo := newFakeActionTypeRepo()
	uc := NewUseCases(repo)
	_, err := uc.Create(context.Background(), "organization-a", domain.CreateInput{
		ActionTypeKey: "calendar.events.create",
		Name:          "Create event",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = uc.GetByKey(context.Background(), "organization-b", "calendar.events.create")
	if !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found for other organization, got %v", err)
	}
}

func TestUseCasesGetByKeyFallsBackToDefaultOrg(t *testing.T) {
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

	out, err := uc.GetByKey(context.Background(), "organization-a", "calendar.events.delete")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if out.OrgID != DefaultOrgID {
		t.Fatalf("fallback organization id = %q, want %q", out.OrgID, DefaultOrgID)
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

func (r *fakeActionTypeRepo) Create(_ context.Context, orgID string, input domain.NormalizedCreateInput) (domain.ActionType, error) {
	id := uuid.New()
	now := time.Now().UTC()
	row := domain.ActionType{
		ID:            id,
		OrgID:         orgID,
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

func (r *fakeActionTypeRepo) List(_ context.Context, orgID string) ([]domain.ActionType, error) {
	out := []domain.ActionType{}
	for _, row := range r.rows {
		if row.OrgID == orgID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeActionTypeRepo) Get(_ context.Context, orgID string, id uuid.UUID) (domain.ActionType, error) {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID {
		return domain.ActionType{}, domainerr.NotFound("action type not found")
	}
	return row, nil
}

func (r *fakeActionTypeRepo) GetByKey(_ context.Context, orgID string, key string) (domain.ActionType, error) {
	for _, row := range r.rows {
		if row.OrgID == orgID && row.ActionTypeKey == key {
			return row, nil
		}
	}
	return domain.ActionType{}, domainerr.NotFound("action type not found")
}

func (r *fakeActionTypeRepo) Update(_ context.Context, orgID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ActionType, error) {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID {
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
