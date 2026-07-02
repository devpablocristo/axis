package virployees

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestUseCasesCreateAndListActive(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	supervisorID := uuid.New()

	created, err := uc.Create(context.Background(), domain.CreateInput{
		Name:             " Sales Assistant ",
		Role:             " sales_assistant ",
		SupervisorUserID: " " + supervisorID.String() + " ",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Sales Assistant" || created.Role != "sales_assistant" || created.SupervisorUserID != supervisorID {
		t.Fatalf("unexpected create output: %+v", created)
	}

	active, err := uc.ListActive(context.Background())
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
	created, err := uc.Create(context.Background(), domain.CreateInput{Name: "Ops", Role: "ops", SupervisorUserID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}

	if err := uc.Archive(context.Background(), created.ID, "", ""); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	assertListLen(t, uc.ListActive, 0)
	assertListLen(t, uc.ListArchived, 1)

	if err := uc.Unarchive(context.Background(), created.ID, "", ""); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	assertListLen(t, uc.ListActive, 1)

	if err := uc.Trash(context.Background(), created.ID, "", ""); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	assertListLen(t, uc.ListActive, 0)
	assertListLen(t, uc.ListTrash, 1)

	if err := uc.Restore(context.Background(), created.ID, "", ""); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	assertListLen(t, uc.ListActive, 1)

	if err := uc.Trash(context.Background(), created.ID, "", ""); err != nil {
		t.Fatalf("Trash again: %v", err)
	}
	if err := uc.Purge(context.Background(), created.ID, "", ""); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if _, err := uc.Get(context.Background(), created.ID); !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found after purge, got %v", err)
	}
}

func TestUseCasesUpdateArchivedOrTrashedFails(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), domain.CreateInput{Name: "Ops", Role: "ops", SupervisorUserID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), created.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	_, err = uc.Update(context.Background(), created.ID, domain.UpdateInput{Name: "New", Role: "new", SupervisorUserID: uuid.NewString()})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict updating archived, got %v", err)
	}
}

func assertListLen(t *testing.T, fn func(context.Context) ([]domain.Virployee, error), want int) {
	t.Helper()
	got, err := fn(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != want {
		t.Fatalf("expected %d rows, got %d: %+v", want, len(got), got)
	}
}

type fakeRepo struct {
	rows map[uuid.UUID]domain.Virployee
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: make(map[uuid.UUID]domain.Virployee)}
}

func (r *fakeRepo) Create(_ context.Context, _ string, input domain.NormalizedCreateInput) (domain.Virployee, error) {
	now := time.Now().UTC()
	row := domain.Virployee{
		ID:               uuid.New(),
		Name:             input.Name,
		Role:             input.Role,
		Description:      input.Description,
		SupervisorUserID: input.SupervisorUserID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	r.rows[row.ID] = row
	return row, nil
}

func (r *fakeRepo) List(_ context.Context, _ string, state domain.State) ([]domain.Virployee, error) {
	out := []domain.Virployee{}
	for _, row := range r.rows {
		if row.State() == state {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeRepo) Get(_ context.Context, _ string, id uuid.UUID) (domain.Virployee, error) {
	row, ok := r.rows[id]
	if !ok || row.State() == domain.StateTrashed {
		return domain.Virployee{}, domainerr.NotFoundf("virployee", id.String())
	}
	return row, nil
}

func (r *fakeRepo) Update(_ context.Context, _ string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error) {
	row, ok := r.rows[id]
	if !ok {
		return domain.Virployee{}, domainerr.NotFoundf("virployee", id.String())
	}
	if row.State() != domain.StateActive {
		return domain.Virployee{}, domainerr.Conflict("virployee is not active")
	}
	row.Name = input.Name
	row.Role = input.Role
	row.Description = input.Description
	row.SupervisorUserID = input.SupervisorUserID
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return row, nil
}

func (r *fakeRepo) SoftDelete(_ context.Context, _ string, id uuid.UUID, at time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateActive {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.ArchivedAt = &at
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Restore(_ context.Context, _ string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateArchived {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.ArchivedAt = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Trash(_ context.Context, _ string, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.State() == domain.StateTrashed {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.ArchivedAt = nil
	row.TrashedAt = &at
	row.PurgeAfter = purgeAfter
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) RestoreTrashed(_ context.Context, _ string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateTrashed {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.TrashedAt = nil
	row.PurgeAfter = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) HardDelete(_ context.Context, _ string, id uuid.UUID) error {
	if _, ok := r.rows[id]; !ok {
		return domainerr.NotFoundf("virployee", id.String())
	}
	delete(r.rows, id)
	return nil
}

func (r *fakeRepo) IsArchived(_ context.Context, _ string, id uuid.UUID) (bool, error) {
	row, ok := r.rows[id]
	if !ok {
		return false, domainerr.NotFoundf("virployee", id.String())
	}
	return row.State() == domain.StateArchived, nil
}

func (r *fakeRepo) State(_ context.Context, _ string, id uuid.UUID) (domain.State, error) {
	row, ok := r.rows[id]
	if !ok {
		return "", domainerr.NotFoundf("virployee", id.String())
	}
	return row.State(), nil
}
