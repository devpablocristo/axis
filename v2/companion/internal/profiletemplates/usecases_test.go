package profiletemplates

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

func TestUseCasesCreateAndListActive(t *testing.T) {
	repo := newFakeProfileRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}

	created, err := uc.Create(context.Background(), "organization-1", domain.CreateInput{
		Name:         " Default assistant ",
		SystemPrompt: " Stay concise. ",
		MaxAutonomy:  "A2",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.OrgID != "organization-1" || created.Name != "Default assistant" || created.SystemPrompt != "Stay concise." || created.MaxAutonomy != virployeedomain.AutonomyA2 {
		t.Fatalf("unexpected create output: %+v", created)
	}

	active, err := uc.ListActive(context.Background(), "organization-1")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != created.ID {
		t.Fatalf("unexpected active list: %+v", active)
	}
}

func TestUseCasesCreateValidatesRequiredFields(t *testing.T) {
	uc, err := NewUseCases(newFakeProfileRepo())
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.Create(context.Background(), "organization-1", domain.CreateInput{
		Name:        "Default",
		MaxAutonomy: "A2",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing system_prompt, got %v", err)
	}

	_, err = uc.Create(context.Background(), "organization-1", domain.CreateInput{
		Name:         "Default",
		SystemPrompt: "Prompt",
		MaxAutonomy:  "A9",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid max_autonomy, got %v", err)
	}
}

func TestEnsureUsableByVirployeeUsesMaxAutonomy(t *testing.T) {
	repo := newFakeProfileRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := uc.Create(context.Background(), "organization-1", domain.CreateInput{
		Name:         "Safe assistant",
		SystemPrompt: "Do safe work.",
		MaxAutonomy:  "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := uc.EnsureUsableByVirployee(context.Background(), "organization-1", profile.ID, virployeedomain.AutonomyA2); err != nil {
		t.Fatalf("expected A2 profile template to accept A2 virployee, got %v", err)
	}
	if err := uc.EnsureUsableByVirployee(context.Background(), "organization-1", profile.ID, virployeedomain.AutonomyA3); !domainerr.IsValidation(err) {
		t.Fatalf("expected A2 profile template to reject A3 virployee, got %v", err)
	}
	if err := uc.EnsureUsableByVirployee(context.Background(), "other-organization", profile.ID, virployeedomain.AutonomyA2); !domainerr.IsValidation(err) {
		t.Fatalf("expected other organization to fail, got %v", err)
	}
}

func TestLifecycleBlocksAssignedProfiles(t *testing.T) {
	repo := newFakeProfileRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := uc.Create(context.Background(), "organization-1", domain.CreateInput{
		Name:         "Assigned",
		SystemPrompt: "Prompt",
		MaxAutonomy:  "A1",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.assigned[profile.ID] = true

	if err := uc.Archive(context.Background(), "organization-1", profile.ID, "", ""); !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict archiving assigned profile, got %v", err)
	}
	if err := uc.Trash(context.Background(), "organization-1", profile.ID, "", ""); !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict trashing assigned profile, got %v", err)
	}
	if err := uc.Purge(context.Background(), "organization-1", profile.ID, "", ""); !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict purging assigned profile, got %v", err)
	}
}

type fakeProfileRepo struct {
	rows     map[uuid.UUID]domain.ProfileTemplate
	assigned map[uuid.UUID]bool
}

func newFakeProfileRepo() *fakeProfileRepo {
	return &fakeProfileRepo{
		rows:     make(map[uuid.UUID]domain.ProfileTemplate),
		assigned: make(map[uuid.UUID]bool),
	}
}

func (r *fakeProfileRepo) Create(_ context.Context, orgID string, input domain.NormalizedCreateInput) (domain.ProfileTemplate, error) {
	now := time.Now().UTC()
	row := domain.ProfileTemplate{
		ID:           uuid.New(),
		OrgID:        orgID,
		Name:         input.Name,
		Description:  input.Description,
		SystemPrompt: input.SystemPrompt,
		MaxAutonomy:  input.MaxAutonomy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	r.rows[row.ID] = row
	return row, nil
}

func (r *fakeProfileRepo) List(_ context.Context, orgID string, state domain.State) ([]domain.ProfileTemplate, error) {
	out := []domain.ProfileTemplate{}
	for _, row := range r.rows {
		if row.OrgID == orgID && row.State() == state {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeProfileRepo) Get(_ context.Context, orgID string, id uuid.UUID) (domain.ProfileTemplate, error) {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID || row.State() == domain.StateTrashed {
		return domain.ProfileTemplate{}, domainerr.NotFoundf("profile", id.String())
	}
	return row, nil
}

func (r *fakeProfileRepo) Update(_ context.Context, orgID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ProfileTemplate, error) {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID {
		return domain.ProfileTemplate{}, domainerr.NotFoundf("profile", id.String())
	}
	if row.State() != domain.StateActive {
		return domain.ProfileTemplate{}, domainerr.Conflict("profile is not active")
	}
	row.Name = input.Name
	row.Description = input.Description
	row.SystemPrompt = input.SystemPrompt
	row.MaxAutonomy = input.MaxAutonomy
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return row, nil
}

func (r *fakeProfileRepo) HasActiveVirployeeAssignments(_ context.Context, _ string, id uuid.UUID) (bool, error) {
	return r.assigned[id], nil
}

func (r *fakeProfileRepo) Archive(_ context.Context, orgID string, id uuid.UUID, at time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID || row.State() != domain.StateActive {
		return domainerr.NotFoundf("profile", id.String())
	}
	row.ArchivedAt = &at
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeProfileRepo) Unarchive(_ context.Context, orgID string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID || row.State() != domain.StateArchived {
		return domainerr.NotFoundf("profile", id.String())
	}
	row.ArchivedAt = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeProfileRepo) Trash(_ context.Context, orgID string, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID || row.State() == domain.StateTrashed {
		return domainerr.NotFoundf("profile", id.String())
	}
	row.ArchivedAt = nil
	row.TrashedAt = &at
	row.PurgeAfter = purgeAfter
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeProfileRepo) Restore(_ context.Context, orgID string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID || row.State() != domain.StateTrashed {
		return domainerr.NotFoundf("profile", id.String())
	}
	row.TrashedAt = nil
	row.PurgeAfter = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeProfileRepo) Purge(_ context.Context, orgID string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID {
		return domainerr.NotFoundf("profile", id.String())
	}
	delete(r.rows, id)
	return nil
}

func (r *fakeProfileRepo) IsArchived(_ context.Context, orgID string, id uuid.UUID) (bool, error) {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID {
		return false, domainerr.NotFoundf("profile", id.String())
	}
	return row.State() == domain.StateArchived, nil
}

func (r *fakeProfileRepo) State(_ context.Context, orgID string, id uuid.UUID) (lifecycle.LifecycleState, error) {
	row, ok := r.rows[id]
	if !ok || row.OrgID != orgID {
		return "", domainerr.NotFoundf("profile", id.String())
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
