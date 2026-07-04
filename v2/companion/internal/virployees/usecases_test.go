package virployees

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
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
	supervisorID := "dev-user"
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              " Sales Assistant ",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		SupervisorUserID:  " " + supervisorID + " ",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Sales Assistant" || created.JobRoleID != jobRoleID || created.ProfileTemplateID != profileTemplateID || created.SupervisorUserID != supervisorID || created.Autonomy != domain.AutonomyA2 {
		t.Fatalf("unexpected create output: %+v", created)
	}

	active, err := uc.ListActive(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != created.ID {
		t.Fatalf("unexpected active list: %+v", active)
	}
}

func TestUseCasesCreateDefaultsAutonomyToA1AndValidatesJobRole(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	reader := &fakeJobRoleReader{}
	uc, err := NewUseCases(repo, reader)
	if err != nil {
		t.Fatal(err)
	}

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Autonomy != domain.AutonomyA1 {
		t.Fatalf("expected default autonomy A1, got %s", created.Autonomy)
	}
	if reader.lastTenant != "tenant-1" || reader.lastID != jobRoleID {
		t.Fatalf("expected job role validation, got tenant=%q id=%s", reader.lastTenant, reader.lastID)
	}
}

func TestUseCasesCreateRequiresProfileTemplateID(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:             "Ops",
		JobRoleID:        uuid.NewString(),
		SupervisorUserID: "dev-user",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing profile_template_id, got %v", err)
	}
}

func TestUseCasesCreateFailsWhenJobRoleIsNotActive(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	uc, err := NewUseCases(repo, &fakeJobRoleReader{err: domainerr.Conflict("job role is not active")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
	})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict for inactive job role, got %v", err)
	}
}

func TestUseCasesCreateValidatesProfileTemplate(t *testing.T) {
	repo := newFakeRepo()
	profileTemplateID := uuid.New()
	reader := &fakeProfileTemplateReader{}
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(reader)

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: profileTemplateID.String(),
		SupervisorUserID:  "dev-user",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ProfileTemplateID != profileTemplateID {
		t.Fatalf("expected profile_template_id to be persisted, got %+v", created.ProfileTemplateID)
	}
	if reader.lastTenant != "tenant-1" || reader.lastID != profileTemplateID || reader.lastAutonomy != domain.AutonomyA2 {
		t.Fatalf("expected profile template validation, got tenant=%q id=%s autonomy=%s", reader.lastTenant, reader.lastID, reader.lastAutonomy)
	}
}

func TestUseCasesCreateFailsWhenProfileTemplateRejectsAutonomy(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(&fakeProfileTemplateReader{err: domainerr.Validation("profile template max autonomy exceeded")})

	_, err = uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
		Autonomy:          "A3",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for incompatible profile template, got %v", err)
	}
}

func TestUseCasesUpdateRequiresProfileTemplateID(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.Update(context.Background(), "tenant-1", created.ID, domain.UpdateInput{
		Name:             "Ops",
		JobRoleID:        uuid.NewString(),
		SupervisorUserID: "dev-user",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing profile_template_id, got %v", err)
	}
}

func TestUseCasesUpdateChangesProfileTemplateReference(t *testing.T) {
	repo := newFakeRepo()
	reader := &fakeProfileTemplateReader{}
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(reader)
	firstTemplateID := uuid.New()
	secondTemplateID := uuid.New()
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: firstTemplateID.String(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := uc.Update(context.Background(), "tenant-1", created.ID, domain.UpdateInput{
		Name:              "Ops updated",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: secondTemplateID.String(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ProfileTemplateID != secondTemplateID {
		t.Fatalf("expected second profile template, got %s", updated.ProfileTemplateID)
	}
	if reader.lastID != secondTemplateID {
		t.Fatalf("expected reader to be called with second template, got %s", reader.lastID)
	}
}

func TestUseCasesLifecycle(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{Name: "Ops", JobRoleID: uuid.NewString(), ProfileTemplateID: uuid.NewString(), SupervisorUserID: "dev-user"})
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

func TestUseCasesUpdateArchivedOrTrashedFails(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{Name: "Ops", JobRoleID: uuid.NewString(), ProfileTemplateID: uuid.NewString(), SupervisorUserID: "dev-user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	_, err = uc.Update(context.Background(), "tenant-1", created.ID, domain.UpdateInput{Name: "New", JobRoleID: uuid.NewString(), ProfileTemplateID: uuid.NewString(), SupervisorUserID: "dev-user"})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict updating archived, got %v", err)
	}
}

func assertListLen(t *testing.T, fn func(context.Context, string) ([]domain.Virployee, error), want int) {
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
	rows map[uuid.UUID]domain.Virployee
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: make(map[uuid.UUID]domain.Virployee)}
}

func (r *fakeRepo) Create(_ context.Context, _ string, input domain.NormalizedCreateInput) (domain.Virployee, error) {
	now := time.Now().UTC()
	row := domain.Virployee{
		ID:                uuid.New(),
		Name:              input.Name,
		JobRoleID:         input.JobRoleID,
		ProfileTemplateID: input.ProfileTemplateID,
		CapabilityIDs:     input.CapabilityIDs,
		Description:       input.Description,
		SupervisorUserID:  input.SupervisorUserID,
		Autonomy:          input.Autonomy,
		CreatedAt:         now,
		UpdatedAt:         now,
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
	row.JobRoleID = input.JobRoleID
	row.ProfileTemplateID = input.ProfileTemplateID
	row.CapabilityIDs = input.CapabilityIDs
	row.Description = input.Description
	row.SupervisorUserID = input.SupervisorUserID
	row.Autonomy = input.Autonomy
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return row, nil
}

func (r *fakeRepo) Archive(_ context.Context, _ string, id uuid.UUID, at time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateActive {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.ArchivedAt = &at
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Unarchive(_ context.Context, _ string, id uuid.UUID) error {
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

func (r *fakeRepo) Restore(_ context.Context, _ string, id uuid.UUID) error {
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

func (r *fakeRepo) Purge(_ context.Context, _ string, id uuid.UUID) error {
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

func (r *fakeRepo) State(_ context.Context, _ string, id uuid.UUID) (lifecycle.LifecycleState, error) {
	row, ok := r.rows[id]
	if !ok {
		return "", domainerr.NotFoundf("virployee", id.String())
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

type fakeJobRoleReader struct {
	lastTenant string
	lastID     uuid.UUID
	err        error
}

func (r *fakeJobRoleReader) EnsureActive(_ context.Context, tenantID string, id uuid.UUID) error {
	r.lastTenant = tenantID
	r.lastID = id
	if r.err != nil {
		return r.err
	}
	return nil
}

type fakeProfileTemplateReader struct {
	lastTenant   string
	lastID       uuid.UUID
	lastAutonomy domain.AutonomyLevel
	err          error
}

func (v *fakeProfileTemplateReader) EnsureUsableByVirployee(_ context.Context, tenantID string, id uuid.UUID, autonomy domain.AutonomyLevel) error {
	v.lastTenant = tenantID
	v.lastID = id
	v.lastAutonomy = autonomy
	if v.err != nil {
		return v.err
	}
	return nil
}
