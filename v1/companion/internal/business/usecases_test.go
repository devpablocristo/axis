package business

import (
	"context"
	"strings"
	"testing"
)

type fakeRepo struct {
	models []Model
}

func (f *fakeRepo) GetActive(_ context.Context, orgID, productSurface string) (Model, error) {
	for i := len(f.models) - 1; i >= 0; i-- {
		m := f.models[i]
		if m.OrgID == orgID && m.ProductSurface == productSurface && m.Status == "active" {
			return m, nil
		}
	}
	return Model{}, ErrNotFound
}

func (f *fakeRepo) Save(_ context.Context, model Model) (Model, error) {
	for i := range f.models {
		if f.models[i].OrgID == model.OrgID && f.models[i].ProductSurface == model.ProductSurface && f.models[i].Status == "active" {
			f.models[i].Status = "archived"
		}
	}
	model.Version = len(f.models) + 1
	model.Status = "active"
	f.models = append(f.models, model)
	return model, nil
}

func TestUsecases_SaveVersionsBusinessModel(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := NewUsecases(repo)
	first, err := uc.Save(context.Background(), Model{
		OrgID:          "org-1",
		ProductSurface: "companion",
		Organization:   Organization{Name: "Acme", Industry: "services"},
		Priorities:     []Priority{{ID: "p1", Name: "VIP response", Weight: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Save(context.Background(), Model{
		OrgID:          "org-1",
		ProductSurface: "companion",
		Organization:   Organization{Name: "Acme", Industry: "services"},
		SLAs:           []SLA{{ID: "sla-1", Name: "Premium", Target: "4h"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("expected versions 1/2, got %d/%d", first.Version, second.Version)
	}
	active, err := uc.Get(context.Background(), "org-1", "companion")
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != 2 || len(active.SLAs) != 1 {
		t.Fatalf("expected active v2 with SLA, got %+v", active)
	}
	if summary := active.Summary(); summary == "" || !strings.Contains(summary, "SLAs") {
		t.Fatalf("expected useful summary, got %q", summary)
	}
}
