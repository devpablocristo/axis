package products

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type fakeRepo struct {
	products      map[string]Product
	installations map[string]Installation
}

type fakeGuardRecorder struct {
	events []GuardrailEvent
}

func (f *fakeGuardRecorder) RecordProductInstallationGuardrail(_ context.Context, event GuardrailEvent) error {
	f.events = append(f.events, event)
	return nil
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		products:      map[string]Product{},
		installations: map[string]Installation{},
	}
}

func (f *fakeRepo) UpsertProduct(_ context.Context, product Product) (Product, error) {
	f.products[product.ProductSurface] = product
	return product, nil
}

func (f *fakeRepo) GetProduct(_ context.Context, productSurface string) (Product, error) {
	product, ok := f.products[productSurface]
	if !ok {
		return Product{}, ErrProductNotFound
	}
	return product, nil
}

func (f *fakeRepo) ListProducts(context.Context) ([]Product, error) {
	out := make([]Product, 0, len(f.products))
	for _, product := range f.products {
		out = append(out, product)
	}
	return out, nil
}

func (f *fakeRepo) UpsertInstallation(_ context.Context, installation Installation) (Installation, error) {
	installation.ID = "installation-1"
	f.installations[installationKey(installation.OrgID, installation.ProductSurface)] = installation
	return installation, nil
}

func (f *fakeRepo) GetInstallation(_ context.Context, orgID, productSurface string) (Installation, error) {
	installation, ok := f.installations[installationKey(orgID, productSurface)]
	if !ok {
		return Installation{}, ErrInstallationNotFound
	}
	return installation, nil
}

func (f *fakeRepo) ListInstallations(_ context.Context, orgID string) ([]Installation, error) {
	out := make([]Installation, 0)
	for _, installation := range f.installations {
		if installation.OrgID == orgID {
			out = append(out, installation)
		}
	}
	return out, nil
}

func TestSaveProductNormalizesSurfaceAndDefaults(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	product, err := uc.SaveProduct(context.Background(), Product{ProductSurface: " Ponti "})
	if err != nil {
		t.Fatal(err)
	}
	if product.ProductSurface != "ponti" || product.DisplayName != "ponti" || product.Status != ProductStatusActive {
		t.Fatalf("unexpected normalized product: %+v", product)
	}
}

func TestSaveInstallationRejectsInlineSecrets(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, _ = uc.SaveProduct(context.Background(), Product{ProductSurface: "ponti"})

	_, err := uc.SaveInstallation(context.Background(), Installation{
		OrgID:          "org-a",
		ProductSurface: "ponti",
		BaseURL:        "https://ponti.example",
		AuthMode:       AuthModeNone,
		Enabled:        true,
		Config:         map[string]any{"nested": map[string]any{"api_key": "plain"}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestResolveInstallationFailsClosedWhenDisabled(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, _ = uc.SaveProduct(context.Background(), Product{ProductSurface: "ponti"})
	_, err := uc.SaveInstallation(context.Background(), Installation{
		OrgID:          "org-a",
		ProductSurface: "ponti",
		BaseURL:        "https://ponti.example",
		AuthMode:       AuthModeNone,
		Enabled:        false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = uc.ResolveInstallation(context.Background(), "org-a", "ponti")
	if !errors.Is(err, ErrInstallationDisabled) {
		t.Fatalf("expected disabled installation error, got %v", err)
	}
}

func TestResolveInstallationRequiresOrgAndProduct(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	_, err := uc.ResolveInstallation(context.Background(), "", "ponti")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected org validation error, got %v", err)
	}
	_, err = uc.ResolveInstallation(context.Background(), "org-a", "Ponti With Spaces")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected product validation error, got %v", err)
	}
}

func TestInstallationGuardAllowsInternalCompanionSurface(t *testing.T) {
	t.Parallel()

	recorder := &fakeGuardRecorder{}
	guard := NewInstallationGuard(nil).WithRecorder(recorder)
	if err := guard.RequireActiveInstallation(context.Background(), "", InternalProductSurface, "runtime_run"); err != nil {
		t.Fatalf("companion surface should bypass installation guard: %v", err)
	}
	if len(recorder.events) != 0 {
		t.Fatalf("expected no guardrail event for companion, got %+v", recorder.events)
	}
}

func TestInstallationGuardBlocksExternalWithoutActiveInstallation(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, _ = uc.SaveProduct(context.Background(), Product{ProductSurface: "ponti"})
	recorder := &fakeGuardRecorder{}
	guard := NewInstallationGuard(uc).WithRecorder(recorder)

	err := guard.RequireActiveInstallation(context.Background(), "org-a", "ponti", "connector_execution")
	if !errors.Is(err, ErrInstallationRequired) || !errors.Is(err, ErrInstallationNotFound) {
		t.Fatalf("expected installation-required/not-found error, got %v", err)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected one guardrail event, got %+v", recorder.events)
	}
	if recorder.events[0].OrgID != "org-a" || recorder.events[0].ProductSurface != "ponti" || recorder.events[0].Reason != "connector_execution" {
		t.Fatalf("unexpected guardrail event: %+v", recorder.events[0])
	}
}

func TestInstallationGuardAllowsActiveExternalInstallation(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, _ = uc.SaveProduct(context.Background(), Product{ProductSurface: "ponti"})
	_, err := uc.SaveInstallation(context.Background(), Installation{
		OrgID:          "org-a",
		ProductSurface: "ponti",
		BaseURL:        "https://ponti.example",
		AuthMode:       AuthModeNone,
		Enabled:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewInstallationGuard(uc).RequireActiveInstallation(context.Background(), "org-a", "ponti", "runtime_run"); err != nil {
		t.Fatalf("expected active installation to pass guard, got %v", err)
	}
}

func TestSaveInstallationRequiresExistingActiveProduct(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, err := uc.SaveInstallation(context.Background(), Installation{
		OrgID:          "org-a",
		ProductSurface: "ponti",
		BaseURL:        "https://ponti.example",
		AuthMode:       AuthModeNone,
		Enabled:        true,
	})
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected missing product error, got %v", err)
	}
	_, _ = uc.SaveProduct(context.Background(), Product{ProductSurface: "ponti", Status: ProductStatusDisabled})
	_, err = uc.SaveInstallation(context.Background(), Installation{
		OrgID:          "org-a",
		ProductSurface: "ponti",
		BaseURL:        "https://ponti.example",
		AuthMode:       AuthModeNone,
		Enabled:        true,
	})
	if !errors.Is(err, ErrProductDisabled) {
		t.Fatalf("expected disabled product error, got %v", err)
	}
}

func TestHandler_RegisterPatterns(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
}

func installationKey(orgID, productSurface string) string {
	return orgID + "/" + productSurface
}
