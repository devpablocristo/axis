package assist

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/assist/usecases/domain"
)

type fakeAssistRepo struct {
	packs map[uuid.UUID]domain.AssistPack
	runs  map[uuid.UUID]domain.AssistRun
}

func newFakeAssistRepo() *fakeAssistRepo {
	return &fakeAssistRepo{packs: map[uuid.UUID]domain.AssistPack{}, runs: map[uuid.UUID]domain.AssistRun{}}
}

func (f *fakeAssistRepo) findPack(orgID, owner, surface, atype string, includeArchived bool) (domain.AssistPack, bool) {
	for _, p := range f.packs {
		if p.OrgID == orgID && p.OwnerSystem == owner && p.ProductSurface == surface && p.AssistType == atype {
			if !includeArchived && p.ArchivedAt != nil {
				continue
			}
			return p, true
		}
	}
	return domain.AssistPack{}, false
}

func (f *fakeAssistRepo) UpsertPack(_ context.Context, pack domain.AssistPack) (domain.AssistPack, error) {
	f.packs[pack.ID] = pack
	return pack, nil
}

func (f *fakeAssistRepo) GetPack(_ context.Context, id uuid.UUID) (domain.AssistPack, error) {
	if p, ok := f.packs[id]; ok {
		return p, nil
	}
	return domain.AssistPack{}, ErrNotFound
}

func (f *fakeAssistRepo) GetPackByType(_ context.Context, orgID, owner, surface, atype string) (domain.AssistPack, error) {
	if p, ok := f.findPack(orgID, owner, surface, atype, false); ok {
		return p, nil
	}
	return domain.AssistPack{}, ErrNotFound
}

func (f *fakeAssistRepo) GetPackByTypeIncludingArchived(_ context.Context, orgID, owner, surface, atype string) (domain.AssistPack, error) {
	if p, ok := f.findPack(orgID, owner, surface, atype, true); ok {
		return p, nil
	}
	return domain.AssistPack{}, ErrNotFound
}

func (f *fakeAssistRepo) ListPacks(context.Context, PackFilter) ([]domain.AssistPack, error) {
	return nil, nil
}

func (f *fakeAssistRepo) UpdatePack(_ context.Context, pack domain.AssistPack) (domain.AssistPack, error) {
	f.packs[pack.ID] = pack
	return pack, nil
}

func (f *fakeAssistRepo) SoftDelete(context.Context, string, uuid.UUID, time.Time) error { return nil }
func (f *fakeAssistRepo) Restore(context.Context, string, uuid.UUID) error               { return nil }
func (f *fakeAssistRepo) HardDelete(context.Context, string, uuid.UUID) error            { return nil }
func (f *fakeAssistRepo) IsArchived(context.Context, string, uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeAssistRepo) CreateRun(_ context.Context, run domain.AssistRun) (domain.AssistRun, error) {
	if run.IdempotencyKey != "" && run.Status != "failed" {
		for _, r := range f.runs {
			if r.OrgID == run.OrgID && r.IdempotencyKey == run.IdempotencyKey && r.Status != "failed" {
				return domain.AssistRun{}, domainerr.Conflict("duplicate idempotency key")
			}
		}
	}
	f.runs[run.ID] = run
	return run, nil
}

func (f *fakeAssistRepo) GetRun(_ context.Context, id uuid.UUID) (domain.AssistRun, error) {
	if r, ok := f.runs[id]; ok {
		return r, nil
	}
	return domain.AssistRun{}, ErrNotFound
}

func (f *fakeAssistRepo) GetRunByIdempotencyKey(_ context.Context, orgID, key string) (domain.AssistRun, error) {
	if key == "" {
		return domain.AssistRun{}, ErrNotFound
	}
	for _, r := range f.runs {
		if r.OrgID == orgID && r.IdempotencyKey == key {
			return r, nil
		}
	}
	return domain.AssistRun{}, ErrNotFound
}

func (f *fakeAssistRepo) UpdateRunResult(_ context.Context, id uuid.UUID, status string, output map[string]any, errMsg string, completedAt time.Time) (domain.AssistRun, error) {
	r, ok := f.runs[id]
	if !ok {
		return domain.AssistRun{}, ErrNotFound
	}
	r.Status = status
	r.Output = output
	r.ErrorMessage = errMsg
	c := completedAt
	r.CompletedAt = &c
	f.runs[id] = r
	return r, nil
}

func (f *fakeAssistRepo) ListRuns(context.Context, RunFilter) ([]domain.AssistRun, error) {
	return nil, nil
}

type countingProvider struct{ calls int }

func (c *countingProvider) Chat(_ context.Context, _ runtime.ChatRequest) (runtime.ChatResponse, error) {
	c.calls++
	return runtime.ChatResponse{Text: `{"summary":"ok"}`}, nil
}

func newTestUsecases(t *testing.T, repo Repository, provider runtime.LLMProvider) *Usecases {
	t.Helper()
	uc, err := NewUsecases(repo, provider, nil)
	if err != nil {
		t.Fatalf("build usecases: %v", err)
	}
	return uc
}

func seedEnabledPack(repo *fakeAssistRepo) {
	pack := domain.AssistPack{
		ID: uuid.New(), OrgID: "org-1", OwnerSystem: "medmory", ProductSurface: "medmory",
		AssistType: "clinical_summary", Name: "Summary",
		PromptTemplate: "Summarize {{input_json}}", ModelPolicy: map[string]any{}, Enabled: true,
	}
	repo.packs[pack.ID] = pack
}

func TestUpsertPackRejectsArchived(t *testing.T) {
	repo := newFakeAssistRepo()
	now := time.Now().UTC()
	repo.packs[uuid.New()] = domain.AssistPack{
		OrgID: "org-1", OwnerSystem: "medmory", ProductSurface: "medmory",
		AssistType: "clinical_summary", ArchivedAt: &now,
	}
	uc := newTestUsecases(t, repo, &countingProvider{})
	_, err := uc.UpsertPack(context.Background(), UpsertPackInput{
		OrgID: "org-1", OwnerSystem: "medmory", ProductSurface: "medmory", AssistType: "clinical_summary",
		Name: "Summary", PromptTemplate: "Do {{input_json}}",
	})
	if !domainerr.IsKind(err, domainerr.KindConflict) {
		t.Fatalf("expected Conflict for archived pack, got %v", err)
	}
}

func TestUpsertPackRequiresInputPlaceholder(t *testing.T) {
	uc := newTestUsecases(t, newFakeAssistRepo(), &countingProvider{})
	_, err := uc.UpsertPack(context.Background(), UpsertPackInput{
		OrgID: "org-1", OwnerSystem: "medmory", ProductSurface: "medmory", AssistType: "clinical_summary",
		Name: "Summary", PromptTemplate: "no placeholder here",
	})
	if !domainerr.IsKind(err, domainerr.KindValidation) {
		t.Fatalf("expected Validation without {{input_json}}, got %v", err)
	}
}

func TestRunAssistIdempotentReplaySkipsLLM(t *testing.T) {
	repo := newFakeAssistRepo()
	seedEnabledPack(repo)
	provider := &countingProvider{}
	uc := newTestUsecases(t, repo, provider)
	in := RunAssistInput{
		OrgID: "org-1", OwnerSystem: "medmory", ProductSurface: "medmory",
		AssistType: "clinical_summary", IdempotencyKey: "k1",
	}

	first, err := uc.RunAssist(context.Background(), in)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if provider.calls != 1 || first.Status != "completed" {
		t.Fatalf("expected one llm call and completed run, calls=%d status=%s", provider.calls, first.Status)
	}

	second, err := uc.RunAssist(context.Background(), in)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected replay to skip the LLM, calls=%d", provider.calls)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same run id on replay, got %s vs %s", second.ID, first.ID)
	}
}
