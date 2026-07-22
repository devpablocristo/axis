package capabilities

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

func TestEnsureAssignableUsesRequiredAutonomy(t *testing.T) {
	id := uuid.New()
	repo := &fakeCapabilityRepo{
		rows: map[uuid.UUID]domain.Capability{
			id: {
				ID:               id,
				TenantID:         "tenant-1",
				CapabilityKey:    "messages.replies.draft",
				Name:             "Draft replies",
				RequiredAutonomy: virployeedomain.AutonomyA2,
				PromotionState:   domain.PromotionActive,
			},
		},
	}
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}

	if err := uc.EnsureAssignable(context.Background(), "tenant-1", []uuid.UUID{id}, virployeedomain.AutonomyA1); !domainerr.IsValidation(err) {
		t.Fatalf("expected A1 to fail for A2 capability, got %v", err)
	}
	if err := uc.EnsureAssignable(context.Background(), "tenant-1", []uuid.UUID{id}, virployeedomain.AutonomyA2); err != nil {
		t.Fatalf("expected A2 to pass for A2 capability, got %v", err)
	}
	if err := uc.EnsureAssignable(context.Background(), "tenant-1", []uuid.UUID{id}, virployeedomain.AutonomyA3); err != nil {
		t.Fatalf("expected A3 to pass for A2 capability, got %v", err)
	}
}

type fakeCapabilityRepo struct {
	rows map[uuid.UUID]domain.Capability
}

func (r *fakeCapabilityRepo) Create(_ context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.Capability, error) {
	id := uuid.New()
	now := time.Now().UTC()
	row := domain.Capability{
		ID:               id,
		TenantID:         tenantID,
		CapabilityKey:    input.CapabilityKey,
		Name:             input.Name,
		Description:      input.Description,
		RequiredAutonomy: input.RequiredAutonomy,
		PromotionState:   domain.PromotionDraft,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	r.rows[id] = row
	return row, nil
}

func (r *fakeCapabilityRepo) UpdateManifest(_ context.Context, tenantID string, id uuid.UUID, manifest domain.Manifest, manifestHash string) (domain.Capability, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() != domain.StateActive {
		return domain.Capability{}, domainerr.NotFoundf("capability", id.String())
	}
	row.Manifest = manifest
	row.ManifestHash = manifestHash
	row.PromotionState = domain.PromotionDraft
	row.ConformedHash = ""
	row.ConformanceReport = domain.ConformanceReport{}
	row.ConformedAt = nil
	row.ActivatedAt = nil
	r.rows[id] = row
	return row, nil
}

func (r *fakeCapabilityRepo) SaveConformance(_ context.Context, tenantID string, id uuid.UUID, expected domain.Capability, report domain.ConformanceReport) (domain.Capability, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.ManifestHash != expected.ManifestHash || row.RiskClass != expected.RiskClass {
		return domain.Capability{}, domainerr.NotFoundf("capability", id.String())
	}
	row.ConformanceReport = report
	if report.Conformant {
		row.PromotionState = domain.PromotionConformant
		row.ConformedHash = expected.ManifestHash
		now := time.Now().UTC()
		row.ConformedAt = &now
	} else {
		row.PromotionState = domain.PromotionDraft
		row.ConformedHash = ""
	}
	r.rows[id] = row
	return row, nil
}

func (r *fakeCapabilityRepo) Activate(_ context.Context, tenantID string, id uuid.UUID, manifestHash string) (domain.Capability, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.PromotionState != domain.PromotionConformant || row.ManifestHash != manifestHash || row.ConformedHash != manifestHash {
		return domain.Capability{}, domainerr.Conflict("capability is not conformant")
	}
	row.PromotionState = domain.PromotionActive
	now := time.Now().UTC()
	row.ActivatedAt = &now
	r.rows[id] = row
	return row, nil
}

func (r *fakeCapabilityRepo) List(_ context.Context, _ string, state domain.State) ([]domain.Capability, error) {
	out := []domain.Capability{}
	for _, row := range r.rows {
		if row.State() == state {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeCapabilityRepo) Get(_ context.Context, tenantID string, id uuid.UUID) (domain.Capability, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() == domain.StateTrashed {
		return domain.Capability{}, domainerr.NotFoundf("capability", id.String())
	}
	return row, nil
}

func (r *fakeCapabilityRepo) Update(_ context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Capability, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.Capability{}, domainerr.NotFoundf("capability", id.String())
	}
	if row.State() != domain.StateActive {
		return domain.Capability{}, domainerr.Conflict("capability is not active")
	}
	row.Name = input.Name
	row.Description = input.Description
	row.RequiredAutonomy = input.RequiredAutonomy
	row.RiskClass = input.Governance.RiskClass
	row.SideEffectClass = input.Governance.SideEffectClass
	row.RequiresNexusApproval = input.Governance.RequiresNexusApproval
	row.EvidenceRequired = input.Governance.EvidenceRequired
	row.RollbackCapabilityKey = input.Governance.RollbackCapabilityKey
	row.PromotionState = domain.PromotionDraft
	row.ConformedHash = ""
	row.ConformedAt = nil
	row.ActivatedAt = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return row, nil
}

func (r *fakeCapabilityRepo) HasActiveVirployeeAssignments(context.Context, string, uuid.UUID) (bool, error) {
	return false, nil
}

func (r *fakeCapabilityRepo) Archive(_ context.Context, _ string, id uuid.UUID, at time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateActive {
		return domainerr.NotFoundf("capability", id.String())
	}
	row.ArchivedAt = &at
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeCapabilityRepo) Unarchive(_ context.Context, _ string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateArchived {
		return domainerr.NotFoundf("capability", id.String())
	}
	row.ArchivedAt = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeCapabilityRepo) Trash(_ context.Context, _ string, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.State() == domain.StateTrashed {
		return domainerr.NotFoundf("capability", id.String())
	}
	row.ArchivedAt = nil
	row.TrashedAt = &at
	row.PurgeAfter = purgeAfter
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeCapabilityRepo) Restore(_ context.Context, _ string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateTrashed {
		return domainerr.NotFoundf("capability", id.String())
	}
	row.TrashedAt = nil
	row.PurgeAfter = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeCapabilityRepo) Purge(_ context.Context, _ string, id uuid.UUID) error {
	if _, ok := r.rows[id]; !ok {
		return domainerr.NotFoundf("capability", id.String())
	}
	delete(r.rows, id)
	return nil
}

func (r *fakeCapabilityRepo) IsArchived(_ context.Context, _ string, id uuid.UUID) (bool, error) {
	row, ok := r.rows[id]
	if !ok {
		return false, domainerr.NotFoundf("capability", id.String())
	}
	return row.State() == domain.StateArchived, nil
}

func (r *fakeCapabilityRepo) State(_ context.Context, _ string, id uuid.UUID) (lifecycle.LifecycleState, error) {
	row, ok := r.rows[id]
	if !ok {
		return "", domainerr.NotFoundf("capability", id.String())
	}
	switch row.State() {
	case domain.StateArchived:
		return lifecycle.StateArchived, nil
	case domain.StateTrashed:
		return lifecycle.StateTrashed, nil
	default:
		return lifecycle.StateActive, nil
	}
}
