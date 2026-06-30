package jobroles

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepo struct {
	roles    map[string]JobRole
	versions map[string][]Version
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{roles: map[string]JobRole{}, versions: map[string][]Version{}}
}

func fakeKey(orgID, productSurface, jobRoleID string) string {
	return orgID + "/" + productSurface + "/" + jobRoleID
}

func (f *fakeRepo) ListJobRoles(_ context.Context, orgID, productSurface string, lifecycle LifecycleView) ([]JobRole, error) {
	out := make([]JobRole, 0, len(f.roles))
	for _, role := range f.roles {
		if role.OrgID != orgID || role.ProductSurface != productSurface {
			continue
		}
		switch lifecycle {
		case LifecycleArchived:
			if role.Status != "archived" {
				continue
			}
		case LifecycleAll:
		default:
			if role.Status != "active" {
				continue
			}
		}
		out = append(out, role)
	}
	return out, nil
}

func (f *fakeRepo) GetJobRole(_ context.Context, orgID, productSurface, jobRoleID string) (JobRole, error) {
	if role, ok := f.roles[fakeKey(orgID, productSurface, jobRoleID)]; ok {
		return role, nil
	}
	for _, role := range f.roles {
		if role.OrgID == orgID && role.ProductSurface == productSurface && role.JobRoleID == jobRoleID {
			return role, nil
		}
	}
	return JobRole{}, ErrNotFound
}

func (f *fakeRepo) UpsertJobRole(_ context.Context, role JobRole) (JobRole, error) {
	role = normalizeJobRole(role)
	storageKey := role.JobRoleKey
	if storageKey == "" {
		storageKey = role.JobRoleID
		if _, err := uuid.Parse(storageKey); err == nil {
			storageKey = ""
		}
	}
	if storageKey == "" {
		storageKey = role.Slug
	}
	for _, existing := range f.roles {
		if existing.OrgID == role.OrgID && existing.ProductSurface == role.ProductSurface && existing.JobRoleKey != storageKey && existing.Slug == role.Slug {
			return JobRole{}, ErrConflict
		}
	}
	if existing, ok := f.roles[fakeKey(role.OrgID, role.ProductSurface, storageKey)]; ok {
		role.Version = existing.Version + 1
		role.ID = existing.ID
		role.JobRoleID = existing.JobRoleID
	} else {
		role.Version = 1
		role.ID = uuid.New()
		role.JobRoleID = role.ID.String()
	}
	role.JobRoleKey = storageKey
	f.roles[fakeKey(role.OrgID, role.ProductSurface, storageKey)] = role
	f.versions[fakeKey(role.OrgID, role.ProductSurface, storageKey)] = append(f.versions[fakeKey(role.OrgID, role.ProductSurface, storageKey)], Version{
		JobRoleID:      role.JobRoleID,
		OrgID:          role.OrgID,
		ProductSurface: role.ProductSurface,
		Version:        role.Version,
		Action:         "upsert",
		Role:           role,
	})
	return role, nil
}

func (f *fakeRepo) ArchiveJobRole(_ context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error) {
	role, key, ok := f.getRoleWithKey(orgID, productSurface, jobRoleID)
	if !ok {
		return JobRole{}, ErrNotFound
	}
	if role.Status == "archived" {
		return role, nil
	}
	now := time.Now().UTC()
	role.Status = "archived"
	role.ArchivedAt = &now
	role.Version++
	f.roles[key] = role
	f.versions[key] = append(f.versions[key], Version{
		JobRoleID: role.JobRoleID, OrgID: orgID, ProductSurface: productSurface, Version: role.Version, Action: "archive", ChangedBy: actorID, Role: role,
	})
	return role, nil
}

func (f *fakeRepo) RestoreJobRole(_ context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error) {
	role, key, ok := f.getRoleWithKey(orgID, productSurface, jobRoleID)
	if !ok {
		return JobRole{}, ErrNotFound
	}
	if role.Status == "active" {
		return role, nil
	}
	role.Status = "active"
	role.ArchivedAt = nil
	role.Version++
	f.roles[key] = role
	f.versions[key] = append(f.versions[key], Version{
		JobRoleID: role.JobRoleID, OrgID: orgID, ProductSurface: productSurface, Version: role.Version, Action: "restore", ChangedBy: actorID, Role: role,
	})
	return role, nil
}

func (f *fakeRepo) ListVersions(_ context.Context, orgID, productSurface, jobRoleID string, _ int) ([]Version, error) {
	_, key, ok := f.getRoleWithKey(orgID, productSurface, jobRoleID)
	if !ok {
		return f.versions[fakeKey(orgID, productSurface, jobRoleID)], nil
	}
	return f.versions[key], nil
}

func (f *fakeRepo) getRoleWithKey(orgID, productSurface, jobRoleID string) (JobRole, string, bool) {
	if role, ok := f.roles[fakeKey(orgID, productSurface, jobRoleID)]; ok {
		return role, fakeKey(orgID, productSurface, jobRoleID), true
	}
	for key, role := range f.roles {
		if role.OrgID == orgID && role.ProductSurface == productSurface && role.JobRoleID == jobRoleID {
			return role, key, true
		}
	}
	return JobRole{}, "", false
}

func TestUpsertJobRoleNormalizesAndDefaults(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(newFakeRepo())

	role, err := uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:               "billing-specialist",
		OrgID:                   "org-a",
		ProductSurface:          "axis",
		Name:                    "Billing Specialist",
		RecommendedCapabilities: []string{" billing.read ", "billing.read", "billing.write"},
		Responsibilities: []Responsibility{{
			Title: " Review invoices ",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if role.Slug != "billing-specialist" || role.DefaultAutonomyLevel != "A2" || role.Status != "active" {
		t.Fatalf("unexpected normalized role: %+v", role)
	}
	if len(role.RecommendedCapabilities) != 2 {
		t.Fatalf("expected deduped capabilities, got %+v", role.RecommendedCapabilities)
	}
}

func TestUpsertJobRoleValidatesRequiredFields(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(newFakeRepo())

	_, err := uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:            "billing-specialist",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		DefaultAutonomyLevel: "A2",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestUpsertJobRoleRejectsArchivedStatus(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(newFakeRepo())

	_, err := uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:            "billing-specialist",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist",
		Slug:                 "billing-specialist",
		DefaultAutonomyLevel: "A2",
		Status:               "archived",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestUpsertJobRoleDoesNotRestoreArchived(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	now := time.Now().UTC()
	repo.roles[fakeKey("org-a", "axis", "billing-specialist")] = JobRole{
		JobRoleID:            "billing-specialist",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist",
		Slug:                 "billing-specialist",
		DefaultAutonomyLevel: "A2",
		Status:               "archived",
		ArchivedAt:           &now,
		Version:              2,
	}
	uc := NewUsecases(repo)

	_, err := uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:            "billing-specialist",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist Updated",
		Slug:                 "billing-specialist",
		DefaultAutonomyLevel: "A2",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict updating archived job role, got %v", err)
	}
	role := repo.roles[fakeKey("org-a", "axis", "billing-specialist")]
	if role.Status != "archived" || role.Version != 2 {
		t.Fatalf("expected archived role to remain unchanged, got %+v", role)
	}
}

func TestUpsertJobRoleRejectsDuplicateSlug(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, err := uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:            "billing-specialist",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist",
		Slug:                 "billing",
		DefaultAutonomyLevel: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:            "billing-manager",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Manager",
		Slug:                 "billing",
		DefaultAutonomyLevel: "A2",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate slug conflict, got %v", err)
	}
}

func TestCreateJobRoleWithoutIDRejectsExistingSlug(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, err := uc.UpsertJobRole(context.Background(), JobRole{
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist",
		Slug:                 "billing",
		DefaultAutonomyLevel: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.UpsertJobRole(context.Background(), JobRole{
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist Copy",
		Slug:                 "billing",
		DefaultAutonomyLevel: "A2",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate slug conflict for create, got %v", err)
	}
}

func TestJobRoleLifecycleAndVersions(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(newFakeRepo())
	_, err := uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:            "billing-specialist",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist",
		Slug:                 "billing-specialist",
		DefaultAutonomyLevel: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := uc.ArchiveJobRole(context.Background(), "org-a", "axis", "billing-specialist", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != "archived" || archived.ArchivedAt == nil {
		t.Fatalf("expected archived role, got %+v", archived)
	}
	restored, err := uc.RestoreJobRole(context.Background(), "org-a", "axis", "billing-specialist", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "active" || restored.ArchivedAt != nil {
		t.Fatalf("expected restored role, got %+v", restored)
	}
	versions, err := uc.ListVersions(context.Background(), "org-a", "axis", "billing-specialist", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
}

func TestJobRoleArchiveRestoreNoopDoesNotBumpVersion(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(newFakeRepo())
	created, err := uc.UpsertJobRole(context.Background(), JobRole{
		JobRoleID:            "billing-specialist",
		OrgID:                "org-a",
		ProductSurface:       "axis",
		Name:                 "Billing Specialist",
		Slug:                 "billing-specialist",
		DefaultAutonomyLevel: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := uc.ArchiveJobRole(context.Background(), "org-a", "axis", "billing-specialist", "admin")
	if err != nil {
		t.Fatal(err)
	}
	archivedAgain, err := uc.ArchiveJobRole(context.Background(), "org-a", "axis", "billing-specialist", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if archivedAgain.Version != archived.Version {
		t.Fatalf("expected archive no-op to keep version %d, got %d", archived.Version, archivedAgain.Version)
	}
	restored, err := uc.RestoreJobRole(context.Background(), "org-a", "axis", "billing-specialist", "admin")
	if err != nil {
		t.Fatal(err)
	}
	restoredAgain, err := uc.RestoreJobRole(context.Background(), "org-a", "axis", "billing-specialist", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if restoredAgain.Version != restored.Version {
		t.Fatalf("expected restore no-op to keep version %d, got %d", restored.Version, restoredAgain.Version)
	}
	versions, err := uc.ListVersions(context.Background(), "org-a", "axis", "billing-specialist", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 || created.Version != 1 {
		t.Fatalf("expected only create/archive/restore audit versions, got created=%d versions=%d", created.Version, len(versions))
	}
}
