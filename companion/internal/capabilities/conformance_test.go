package capabilities

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/devpablocristo/companion/internal/products"
)

type fakeProductRegistry struct {
	product products.Product
	err     error
}

func (f fakeProductRegistry) GetProduct(context.Context, string) (products.Product, error) {
	if f.err != nil {
		return products.Product{}, f.err
	}
	return f.product, nil
}

func TestCheckManifestConformanceRequiresGovernedSideEffects(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SchemaVersion:    SchemaVersion,
		CapabilityID:     "demo.write",
		Version:          "1.0.0",
		DisplayName:      "Demo Write",
		Description:      "Writes demo data.",
		Owner:            "demo",
		ProductSurface:   "demo",
		Connector:        "demo",
		ActionType:       ActionTypeWrite,
		RiskLevel:        RiskHigh,
		SideEffectType:   SideEffectWrite,
		AuthMode:         "hybrid",
		InputSchema:      map[string]any{"type": "object", "properties": map[string]any{}},
		OutputSchema:     map[string]any{"type": "object", "properties": map[string]any{}},
		EvidenceSchema:   map[string]any{"type": "object", "properties": map[string]any{}},
		IdempotencyMode:  IdempotencyOptional,
		ApprovalRequired: false,
		RateLimitClass:   "standard",
		CostClass:        "low",
		Timeout:          "30s",
		Retries:          RetryPolicy{MaxAttempts: 1, Backoff: "none"},
	}

	checks, errs := CheckManifestConformance(manifest)
	if len(errs) == 0 {
		t.Fatal("expected conformance errors for ungated write")
	}
	if checks["manifest_valid"] {
		t.Fatalf("manifest_valid should fail for write without approval")
	}
	if checks["idempotency"] {
		t.Fatalf("idempotency should fail for side effect without required idempotency")
	}
}

func TestCheckManifestConformanceAcceptsGovernedRead(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SchemaVersion:    SchemaVersion,
		CapabilityID:     "demo.read",
		Version:          "1.0.0",
		DisplayName:      "Demo Read",
		Description:      "Reads demo data.",
		Owner:            "demo",
		ProductSurface:   "demo",
		Connector:        "demo",
		ActionType:       ActionTypeRead,
		RiskLevel:        RiskLow,
		SideEffectType:   SideEffectRead,
		AuthMode:         "hybrid",
		InputSchema:      map[string]any{"type": "object", "properties": map[string]any{}},
		OutputSchema:     map[string]any{"type": "object", "properties": map[string]any{}},
		EvidenceSchema:   map[string]any{"type": "object", "properties": map[string]any{}},
		IdempotencyMode:  IdempotencyNone,
		ApprovalRequired: false,
		RateLimitClass:   "standard",
		CostClass:        "low",
		Timeout:          "30s",
		Retries:          RetryPolicy{MaxAttempts: 1, Backoff: "none"},
	}

	checks, errs := CheckManifestConformance(manifest)
	if len(errs) != 0 {
		t.Fatalf("expected no conformance errors, got %v", errs)
	}
	if !checks["manifest_valid"] || !checks["schema_contracts"] || !checks["nexus_binding"] {
		t.Fatalf("expected core checks to pass, got %+v", checks)
	}
}

func TestUsecases_CheckConformanceRequiresActiveProduct(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SchemaVersion:    SchemaVersion,
		CapabilityID:     "demo.read",
		Version:          "1.0.0",
		DisplayName:      "Demo Read",
		Description:      "Reads demo data.",
		Owner:            "demo",
		ProductSurface:   "demo",
		Connector:        "demo",
		ActionType:       ActionTypeRead,
		RiskLevel:        RiskLow,
		SideEffectType:   SideEffectRead,
		AuthMode:         "hybrid",
		InputSchema:      map[string]any{"type": "object", "properties": map[string]any{}},
		OutputSchema:     map[string]any{"type": "object", "properties": map[string]any{}},
		EvidenceSchema:   map[string]any{"type": "object", "properties": map[string]any{}},
		IdempotencyMode:  IdempotencyNone,
		ApprovalRequired: false,
		RateLimitClass:   "standard",
		CostClass:        "low",
		Timeout:          "30s",
		Retries:          RetryPolicy{MaxAttempts: 1, Backoff: "none"},
	}
	uc := NewUsecases(nil).WithProductRegistry(fakeProductRegistry{product: products.Product{
		ProductSurface: "demo",
		Status:         products.ProductStatusDisabled,
	}})

	checks, errs := uc.CheckConformance(context.Background(), manifest)
	if checks["product_active"] {
		t.Fatalf("expected disabled product to fail product_active check: %+v", checks)
	}
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, "; "), "disabled") {
		t.Fatalf("expected disabled product conformance error, got %v", errs)
	}

	uc = NewUsecases(nil).WithProductRegistry(fakeProductRegistry{product: products.Product{
		ProductSurface: "demo",
		Status:         products.ProductStatusActive,
	}})
	checks, errs = uc.CheckConformance(context.Background(), manifest)
	if len(errs) != 0 {
		t.Fatalf("expected active product to pass conformance, got %v", errs)
	}
	if !checks["product_active"] {
		t.Fatalf("expected active product check, got %+v", checks)
	}

	uc = NewUsecases(nil).WithProductRegistry(fakeProductRegistry{err: errors.New("not found")})
	checks, errs = uc.CheckConformance(context.Background(), manifest)
	if checks["product_active"] || len(errs) == 0 {
		t.Fatalf("expected missing product to fail conformance, checks=%+v errs=%v", checks, errs)
	}
}
