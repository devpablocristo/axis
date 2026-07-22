package jobroles

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

func TestUseCasesCreateAndListActive(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:             " Sales Assistant ",
		Responsibilities: []domain.Responsibility{{Title: " Qualify leads ", ExpectedOutcome: " Qualified pipeline ", Priority: 1}},
		SuccessCriteria:  []domain.SuccessCriterion{{Title: " Response time ", TargetValue: "under 5 minutes", Priority: 1}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TenantID != "tenant-1" || created.Name != "Sales Assistant" || created.Slug != "sales-assistant" {
		t.Fatalf("unexpected create output: %+v", created)
	}
	if len(created.Responsibilities) != 1 || created.Responsibilities[0].Title != "Qualify leads" || len(created.SuccessCriteria) != 1 {
		t.Fatalf("professional definition was not persisted: %+v", created)
	}

	active, err := uc.ListActive(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != created.ID {
		t.Fatalf("unexpected active list: %+v", active)
	}
}

func TestUseCasesLifecycle(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{Name: "Ops"})
	if err != nil {
		t.Fatal(err)
	}

	if err := uc.Archive(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	assertListLen(t, uc.ListActive, 0)
	assertListLen(t, uc.ListArchived, 1)

	if err := uc.Unarchive(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	assertListLen(t, uc.ListActive, 1)

	if err := uc.Trash(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	assertListLen(t, uc.ListActive, 0)
	assertListLen(t, uc.ListTrash, 1)

	if err := uc.Restore(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	assertListLen(t, uc.ListActive, 1)

	if err := uc.Trash(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Trash again: %v", err)
	}
	if err := uc.Purge(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if _, err := uc.Get(context.Background(), "tenant-1", created.ID); !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found after purge, got %v", err)
	}
}

func TestUseCasesEnsureActive(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{Name: "Ops"})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.EnsureActive(context.Background(), "tenant-1", created.ID); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := uc.Archive(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := uc.EnsureActive(context.Background(), "tenant-1", created.ID); !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict for archived job role, got %v", err)
	}
}

func assertListLen(t *testing.T, fn func(context.Context, string) ([]domain.JobRole, error), want int) {
	t.Helper()
	got, err := fn(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != want {
		t.Fatalf("expected %d rows, got %d: %+v", want, len(got), got)
	}
}

type fakeRepo struct {
	rows map[uuid.UUID]domain.JobRole
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: make(map[uuid.UUID]domain.JobRole)}
}

func (r *fakeRepo) Create(_ context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.JobRole, error) {
	now := time.Now().UTC()
	for _, row := range r.rows {
		if row.TenantID == tenantID && row.Slug == input.Slug {
			return domain.JobRole{}, domainerr.Conflict("job role slug already exists")
		}
	}
	row := domain.JobRole{
		ID:               uuid.New(),
		TenantID:         tenantID,
		Name:             input.Name,
		Slug:             input.Slug,
		Mission:          input.Mission,
		Responsibilities: append([]domain.Responsibility(nil), input.Responsibilities...),
		SuccessCriteria:  append([]domain.SuccessCriterion(nil), input.SuccessCriteria...),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	r.rows[row.ID] = row
	return row, nil
}

func (r *fakeRepo) List(_ context.Context, tenantID string, state domain.State) ([]domain.JobRole, error) {
	out := []domain.JobRole{}
	for _, row := range r.rows {
		if row.TenantID == tenantID && row.State() == state {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeRepo) Get(_ context.Context, tenantID string, id uuid.UUID) (domain.JobRole, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() == domain.StateTrashed {
		return domain.JobRole{}, domainerr.NotFoundf("job role", id.String())
	}
	return row, nil
}

func (r *fakeRepo) Update(_ context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.JobRole, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.JobRole{}, domainerr.NotFoundf("job role", id.String())
	}
	if row.State() != domain.StateActive {
		return domain.JobRole{}, domainerr.Conflict("job role is not active")
	}
	for existingID, existing := range r.rows {
		if existingID != id && existing.TenantID == tenantID && existing.Slug == input.Slug {
			return domain.JobRole{}, domainerr.Conflict("job role slug already exists")
		}
	}
	row.Name = input.Name
	row.Slug = input.Slug
	row.Mission = input.Mission
	row.Responsibilities = append([]domain.Responsibility(nil), input.Responsibilities...)
	row.SuccessCriteria = append([]domain.SuccessCriterion(nil), input.SuccessCriteria...)
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return row, nil
}

func (r *fakeRepo) Archive(_ context.Context, tenantID string, id uuid.UUID, at time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() != domain.StateActive {
		return domainerr.NotFoundf("job role", id.String())
	}
	row.ArchivedAt = &at
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Unarchive(_ context.Context, tenantID string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() != domain.StateArchived {
		return domainerr.NotFoundf("job role", id.String())
	}
	row.ArchivedAt = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Trash(_ context.Context, tenantID string, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() == domain.StateTrashed {
		return domainerr.NotFoundf("job role", id.String())
	}
	row.ArchivedAt = nil
	row.TrashedAt = &at
	row.PurgeAfter = purgeAfter
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Restore(_ context.Context, tenantID string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() != domain.StateTrashed {
		return domainerr.NotFoundf("job role", id.String())
	}
	row.TrashedAt = nil
	row.PurgeAfter = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Purge(_ context.Context, tenantID string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domainerr.NotFoundf("job role", id.String())
	}
	delete(r.rows, id)
	return nil
}

func (r *fakeRepo) IsArchived(_ context.Context, tenantID string, id uuid.UUID) (bool, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return false, domainerr.NotFoundf("job role", id.String())
	}
	return row.State() == domain.StateArchived, nil
}

func (r *fakeRepo) State(_ context.Context, tenantID string, id uuid.UUID) (lifecycle.LifecycleState, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return "", domainerr.NotFoundf("job role", id.String())
	}
	return lifecycleState(row.State()), nil
}

func lifecycleState(state domain.State) lifecycle.LifecycleState {
	switch state {
	case domain.StateArchived:
		return lifecycle.StateArchived
	case domain.StateTrashed:
		return lifecycle.StateTrashed
	default:
		return lifecycle.StateActive
	}
}
