package memory

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

type fakeRepo struct {
	entries map[string]domain.MemoryEntry
}

func memoryKey(orgID, productSurface string, scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) string {
	return orgID + "|" + productSurface + "|" + string(scopeType) + "|" + scopeID + "|" + string(kind) + "|" + key
}

func (f *fakeRepo) Upsert(ctx context.Context, e domain.MemoryEntry) (domain.MemoryEntry, error) {
	if f.entries == nil {
		f.entries = make(map[string]domain.MemoryEntry)
	}
	now := time.Now().UTC()
	k := memoryKey(e.OrgID, e.ProductSurface, e.ScopeType, e.ScopeID, e.Kind, e.Key)
	if e.Version == 0 {
		e.ID = uuid.New()
		e.Version = 1
		e.CreatedAt = now
		e.UpdatedAt = now
		f.entries[k] = e
		return e, nil
	}
	current := f.entries[k]
	e.CreatedAt = current.CreatedAt
	e.UpdatedAt = now
	e.Version = current.Version + 1
	f.entries[k] = e
	return e, nil
}

func (f *fakeRepo) Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error) {
	for _, entry := range f.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return domain.MemoryEntry{}, ErrNotFound
}

func (f *fakeRepo) GetByScopeKey(ctx context.Context, orgID, productSurface string, scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) (domain.MemoryEntry, error) {
	if f.entries == nil {
		return domain.MemoryEntry{}, ErrNotFound
	}
	entry, ok := f.entries[memoryKey(orgID, productSurface, scopeType, scopeID, kind, key)]
	if !ok {
		return domain.MemoryEntry{}, ErrNotFound
	}
	return entry, nil
}

func (f *fakeRepo) Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error) {
	return nil, nil
}

func (f *fakeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeRepo) PurgeExpired(ctx context.Context) (int64, error) {
	return 0, nil
}

func (f *fakeRepo) CountByScope(_ context.Context, scopeType domain.ScopeType, scopeID string) (int, error) {
	n := 0
	for _, e := range f.entries {
		if e.ScopeType == scopeType && e.ScopeID == scopeID {
			n++
		}
	}
	return n, nil
}

func TestUsecases_Upsert_updatesExistingEntryByScopeKey(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	uc := NewUsecases(repo)

	created, err := uc.Upsert(context.Background(), UpsertInput{
		OrgID:          "org-a",
		ProductSurface: "companion",
		Kind:           domain.MemoryTaskSummary,
		ScopeType:      domain.ScopeTask,
		ScopeID:        "task-1",
		Key:            "current",
		ContentText:    "initial",
		PayloadJSON:    json.RawMessage(`{"status":"new"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := uc.Upsert(context.Background(), UpsertInput{
		OrgID:          "org-a",
		ProductSurface: "companion",
		Kind:           domain.MemoryTaskSummary,
		ScopeType:      domain.ScopeTask,
		ScopeID:        "task-1",
		Key:            "current",
		Version:        created.Version,
		ContentText:    "updated",
		PayloadJSON:    json.RawMessage(`{"status":"done"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID {
		t.Fatalf("expected same ID, got %s != %s", updated.ID, created.ID)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("expected version %d, got %d", created.Version+1, updated.Version)
	}
	if updated.ContentText != "updated" {
		t.Fatalf("expected updated content, got %q", updated.ContentText)
	}
}

func TestUsecases_UpsertDefaultsMemoryTypeFromKind(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	uc := NewUsecases(repo)

	entry, err := uc.Upsert(context.Background(), UpsertInput{
		OrgID:          "org-a",
		UserID:         "user-a",
		ProductSurface: "pymes",
		Kind:           domain.MemoryEpisodicEvent,
		ScopeType:      domain.ScopeUser,
		ScopeID:        "org-a:user-a",
		Key:            "episode-1",
		ContentText:    "El usuario pidio una alerta de stock.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry.MemoryType != domain.MemoryTypeEpisodic {
		t.Fatalf("expected episodic memory type, got %q", entry.MemoryType)
	}
}

func TestUsecases_Upsert_rejectsInsertOverQuota(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	uc := NewUsecases(repo).WithPerScopeQuota(2)

	for _, k := range []string{"k1", "k2"} {
		if _, err := uc.Upsert(context.Background(), UpsertInput{
			OrgID:          "org-a",
			ProductSurface: "companion",
			Kind:           domain.MemoryTaskSummary,
			ScopeType:      domain.ScopeTask,
			ScopeID:        "task-q",
			Key:            k,
		}); err != nil {
			t.Fatalf("seed %s failed: %v", k, err)
		}
	}

	_, err := uc.Upsert(context.Background(), UpsertInput{
		OrgID:          "org-a",
		ProductSurface: "companion",
		Kind:           domain.MemoryTaskSummary,
		ScopeType:      domain.ScopeTask,
		ScopeID:        "task-q",
		Key:            "k3",
	})
	if !IsQuotaExceeded(err) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}

	// Update sobre key existente debe seguir funcionando aunque esté en límite.
	if _, err := uc.Upsert(context.Background(), UpsertInput{
		OrgID:          "org-a",
		ProductSurface: "companion",
		Kind:           domain.MemoryTaskSummary,
		ScopeType:      domain.ScopeTask,
		ScopeID:        "task-q",
		Key:            "k1",
		ContentText:    "updated",
	}); err != nil {
		t.Fatalf("update at quota should succeed, got: %v", err)
	}
}
