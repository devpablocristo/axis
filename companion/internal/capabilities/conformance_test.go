package capabilities

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devpablocristo/companion/internal/products"
	"github.com/google/uuid"
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

type fakeManifestSourceFetcher struct {
	manifests []Manifest
	err       error
	sourceURL string
}

func (f *fakeManifestSourceFetcher) FetchManifests(_ context.Context, req ManifestSourceRequest) ([]Manifest, error) {
	f.sourceURL = req.SourceURL
	if f.err != nil {
		return nil, f.err
	}
	return append([]Manifest(nil), f.manifests...), nil
}

type fakeCapabilityRepo struct {
	records map[string]ManifestRecord
	runs    []ConformanceRun
}

func newFakeCapabilityRepo() *fakeCapabilityRepo {
	return &fakeCapabilityRepo{records: make(map[string]ManifestRecord)}
}

func (f *fakeCapabilityRepo) UpsertManifest(_ context.Context, record ManifestRecord) (ManifestRecord, error) {
	record.Manifest = record.Manifest.Normalize()
	if err := record.Manifest.Validate(); err != nil {
		return ManifestRecord{}, err
	}
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	if record.Status == "" {
		record.Status = ManifestStatusDraft
	}
	if record.Source == "" {
		record.Source = ManifestSourceImported
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	f.records[record.Manifest.Key()] = record
	return record, nil
}

func (f *fakeCapabilityRepo) GetManifest(_ context.Context, capabilityID, version string) (ManifestRecord, error) {
	record, ok := f.records[keyFor(capabilityID, version)]
	if !ok {
		return ManifestRecord{}, ErrManifestNotFound
	}
	return record, nil
}

func (f *fakeCapabilityRepo) ListManifests(_ context.Context, filter ManifestFilter) ([]ManifestRecord, error) {
	var out []ManifestRecord
	for _, record := range f.records {
		if filter.CapabilityID != "" && record.Manifest.CapabilityID != filter.CapabilityID {
			continue
		}
		if filter.Status != "" && record.Status != filter.Status {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (f *fakeCapabilityRepo) UpdateManifestStatus(_ context.Context, capabilityID, version, status string) (ManifestRecord, error) {
	key := keyFor(capabilityID, version)
	record, ok := f.records[key]
	if !ok {
		return ManifestRecord{}, ErrManifestNotFound
	}
	record.Status = normalizeManifestStatus(status)
	record.UpdatedAt = time.Now().UTC()
	f.records[key] = record
	return record, nil
}

func (f *fakeCapabilityRepo) SaveConformanceRun(_ context.Context, run ConformanceRun) (ConformanceRun, error) {
	run.ID = uuid.New()
	run.CreatedAt = time.Now().UTC()
	f.runs = append(f.runs, run)
	return run, nil
}

func (f *fakeCapabilityRepo) ListConformanceRuns(_ context.Context, _ string, _ string, _ int) ([]ConformanceRun, error) {
	return append([]ConformanceRun(nil), f.runs...), nil
}

func validReadManifest() Manifest {
	return Manifest{
		SchemaVersion:      SchemaVersion,
		CapabilityID:       "demo.customer.lookup",
		Version:            "1.0.0",
		DisplayName:        "Lookup customer",
		Description:        "Reads a customer record.",
		Owner:              "demo",
		ProductSurface:     "demo",
		Connector:          "demo",
		ActionType:         ActionTypeRead,
		RiskLevel:          RiskLow,
		SideEffectType:     SideEffectRead,
		AuthMode:           "delegated_user",
		RequiredScopes:     []string{"companion:connectors:execute"},
		InputSchema:        objectSchema("org_id", "customer_id"),
		OutputSchema:       objectSchema("customer_id"),
		EvidenceSchema:     objectSchema("customer_id"),
		RequiredEvidence:   []string{"customer_id"},
		IdempotencyMode:    IdempotencyNone,
		ApprovalRequired:   false,
		TenantConfigurable: true,
		EnabledByDefault:   true,
		RateLimitClass:     "standard",
		CostClass:          "low",
		Timeout:            "15s",
		Retries:            RetryPolicy{MaxAttempts: 1, Backoff: "none"},
		Preconditions:      []string{"customer_org_context"},
		ObservabilityTags:  []string{"connector:demo"},
	}
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
		RequiredScopes:   []string{"companion:connectors:execute"},
		InputSchema:      map[string]any{"type": "object", "properties": map[string]any{}},
		OutputSchema:     map[string]any{"type": "object", "properties": map[string]any{}},
		EvidenceSchema:   objectSchema("external_ref"),
		RequiredEvidence: []string{"external_ref"},
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
		RequiredScopes:   []string{"companion:connectors:execute"},
		InputSchema:      objectSchema("org_id"),
		OutputSchema:     objectSchema("result_id"),
		EvidenceSchema:   objectSchema("result_id"),
		RequiredEvidence: []string{"result_id"},
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
		RequiredScopes:   []string{"companion:connectors:execute"},
		InputSchema:      objectSchema("org_id"),
		OutputSchema:     objectSchema("result_id"),
		EvidenceSchema:   objectSchema("result_id"),
		RequiredEvidence: []string{"result_id"},
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

func TestUsecases_PromoteValidReadManifestActivatesIt(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo).WithProductRegistry(fakeProductRegistry{product: products.Product{
		ProductSurface: "demo",
		Status:         products.ProductStatusActive,
	}})
	manifest := validReadManifest()
	if _, err := uc.ImportManifest(context.Background(), manifest, "tester"); err != nil {
		t.Fatal(err)
	}

	record, err := uc.PromoteManifest(context.Background(), manifest.CapabilityID, manifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != ManifestStatusActive {
		t.Fatalf("expected active manifest, got %+v", record)
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != ConformanceStatusPassed {
		t.Fatalf("expected passing conformance run, got %+v", repo.runs)
	}
}

func TestUsecases_ImportManifestPersistsManualProvenance(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo)
	manifest := validReadManifest()

	record, err := uc.ImportManifest(context.Background(), manifest, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != ManifestStatusDraft || record.Source != ManifestSourceImported || record.ImportedBy != "tester" {
		t.Fatalf("unexpected manual import provenance: %+v", record)
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		t.Fatalf("expected import timestamps, got %+v", record)
	}
}

func TestUsecases_ImportRejectsWriteWithoutNexusMetadata(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo)
	manifest := validManifest()
	manifest.ApprovalRequired = false
	manifest.NexusActionType = ""

	if _, err := uc.ImportManifest(context.Background(), manifest, "tester"); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected invalid write manifest, got %v", err)
	}
}

func TestUsecases_ImportRejectsNonSemverVersionWithClearError(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo)
	manifest := validReadManifest()
	manifest.Version = "v1"

	_, err := uc.ImportManifest(context.Background(), manifest, "tester")
	if !errors.Is(err, ErrInvalidManifest) || !strings.Contains(err.Error(), "version must be semver") {
		t.Fatalf("expected clear semver validation error, got %v", err)
	}
}

func TestUsecases_ImportRejectsInvalidSchema(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo)
	manifest := validReadManifest()
	manifest.InputSchema = map[string]any{
		"type":     "object",
		"required": []string{"missing"},
		"properties": map[string]any{
			"org_id": map[string]any{"type": "string"},
		},
	}

	if _, err := uc.ImportManifest(context.Background(), manifest, "tester"); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected invalid schema manifest, got %v", err)
	}
}

func TestUsecases_DeprecatedManifestCanBeReactivatedWithAudit(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo).WithProductRegistry(fakeProductRegistry{product: products.Product{
		ProductSurface: "demo",
		Status:         products.ProductStatusActive,
	}})
	manifest := validReadManifest()
	if _, err := uc.ImportManifest(context.Background(), manifest, "tester"); err != nil {
		t.Fatal(err)
	}
	deprecated, err := uc.DeprecateManifestWithAudit(context.Background(), ManifestStatusChangeInput{
		CapabilityID: manifest.CapabilityID,
		Version:      manifest.Version,
		ActorID:      "admin-a",
		Reason:       "newer version available",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deprecated.Status != ManifestStatusDeprecated {
		t.Fatalf("expected deprecated manifest, got %+v", deprecated)
	}
	if len(repo.runs) != 1 || repo.runs[0].CreatedBy != "admin-a" {
		t.Fatalf("expected deprecate audit run, got %+v", repo.runs)
	}
	assertTransitionEvidence(t, repo.runs[0].Evidence, "deprecate", ManifestStatusDraft, ManifestStatusDeprecated)
	if repo.runs[0].Evidence["impact"] != "unknown" {
		t.Fatalf("expected impact unknown, got %+v", repo.runs[0].Evidence)
	}

	record, err := uc.PromoteManifestWithAudit(context.Background(), ManifestStatusChangeInput{
		CapabilityID: manifest.CapabilityID,
		Version:      manifest.Version,
		ActorID:      "admin-a",
		Reason:       "reactivate after review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != ManifestStatusActive {
		t.Fatalf("expected reactivated manifest, got %+v", record)
	}
	if len(repo.runs) != 2 || repo.runs[1].Status != ConformanceStatusPassed || repo.runs[1].CreatedBy != "admin-a" {
		t.Fatalf("expected promotion audit run, got %+v", repo.runs)
	}
	assertTransitionEvidence(t, repo.runs[1].Evidence, "promote", ManifestStatusDeprecated, ManifestStatusActive)
	if repo.runs[1].Evidence["promotion_gate"] != true {
		t.Fatalf("expected promotion gate evidence, got %+v", repo.runs[1].Evidence)
	}
}

func TestUsecases_PromoteRejectsDisabledProduct(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo).WithProductRegistry(fakeProductRegistry{product: products.Product{
		ProductSurface: "demo",
		Status:         products.ProductStatusDisabled,
	}})
	manifest := validReadManifest()
	if _, err := uc.ImportManifest(context.Background(), manifest, "tester"); err != nil {
		t.Fatal(err)
	}

	_, err := uc.PromoteManifest(context.Background(), manifest.CapabilityID, manifest.Version)
	if !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected disabled product conformance failure, got %v", err)
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != ConformanceStatusFailed {
		t.Fatalf("expected failed conformance run, got %+v", repo.runs)
	}
	assertTransitionEvidence(t, repo.runs[0].Evidence, "promote", ManifestStatusDraft, ManifestStatusActive)
}

func TestUsecases_BlockManifestPreventsPromotion(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	uc := NewUsecases(repo).WithProductRegistry(fakeProductRegistry{product: products.Product{
		ProductSurface: "demo",
		Status:         products.ProductStatusActive,
	}})
	manifest := validReadManifest()
	if _, err := uc.ImportManifest(context.Background(), manifest, "tester"); err != nil {
		t.Fatal(err)
	}
	blocked, err := uc.BlockManifest(context.Background(), manifest.CapabilityID, manifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != ManifestStatusBlocked {
		t.Fatalf("expected blocked manifest, got %+v", blocked)
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != ConformanceStatusPassed {
		t.Fatalf("expected block audit run, got %+v", repo.runs)
	}
	assertTransitionEvidence(t, repo.runs[0].Evidence, "block", ManifestStatusDraft, ManifestStatusBlocked)
	if _, err := uc.PromoteManifest(context.Background(), manifest.CapabilityID, manifest.Version); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected blocked manifest promotion rejection, got %v", err)
	}
	if len(repo.runs) != 2 || repo.runs[1].Status != ConformanceStatusFailed {
		t.Fatalf("expected rejected promotion audit run, got %+v", repo.runs)
	}
	assertTransitionEvidence(t, repo.runs[1].Evidence, "promote", ManifestStatusBlocked, ManifestStatusActive)
	if repo.runs[1].Evidence["blocked_promotion_rejected"] != true {
		t.Fatalf("expected blocked promotion evidence, got %+v", repo.runs[1].Evidence)
	}
}

func TestUsecases_ImportManifestSourcePersistsDraftsFromFetcher(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	fetcher := &fakeManifestSourceFetcher{manifests: []Manifest{validReadManifest()}}
	uc := NewUsecases(repo).WithManifestSourceFetcher(fetcher)

	records, err := uc.ImportManifestSource(context.Background(), ImportManifestSourceInput{
		SourceURL:              "https://example.test/capabilities.json",
		ExpectedProductSurface: "demo",
		ImportedBy:             "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.sourceURL != "https://example.test/capabilities.json" {
		t.Fatalf("expected source URL passed to fetcher, got %q", fetcher.sourceURL)
	}
	if len(records) != 1 {
		t.Fatalf("expected one imported record, got %+v", records)
	}
	if records[0].Status != ManifestStatusDraft || records[0].Source != ManifestSourceURL || records[0].SourceURI != "https://example.test/capabilities.json" {
		t.Fatalf("unexpected imported source record: %+v", records[0])
	}
	if records[0].ImportedBy != "tester" {
		t.Fatalf("expected imported_by provenance, got %+v", records[0])
	}
}

func TestUsecases_ImportManifestSourceRejectsProductMismatchWithClearError(t *testing.T) {
	t.Parallel()

	repo := newFakeCapabilityRepo()
	fetcher := &fakeManifestSourceFetcher{manifests: []Manifest{validReadManifest()}}
	uc := NewUsecases(repo).WithManifestSourceFetcher(fetcher)

	_, err := uc.ImportManifestSource(context.Background(), ImportManifestSourceInput{
		SourceURL:              "https://example.test/capabilities.json",
		ExpectedProductSurface: "other",
		ImportedBy:             "tester",
	})
	if !errors.Is(err, ErrInvalidManifest) || !strings.Contains(err.Error(), `does not match expected "other"`) {
		t.Fatalf("expected clear product mismatch error, got %v", err)
	}
	if len(repo.records) != 0 {
		t.Fatalf("product mismatch must not persist records, got %+v", repo.records)
	}
}

func assertTransitionEvidence(t *testing.T, evidence map[string]any, operation, from, to string) {
	t.Helper()
	if evidence["operation"] != operation {
		t.Fatalf("expected operation %q, got %+v", operation, evidence)
	}
	transition, ok := evidence["status_transition"].(map[string]any)
	if !ok {
		t.Fatalf("expected status_transition evidence, got %+v", evidence)
	}
	if transition["from"] != from || transition["to"] != to {
		t.Fatalf("expected transition %s -> %s, got %+v", from, to, transition)
	}
	if evidence["impact"] != "unknown" || evidence["impact_reason"] == "" {
		t.Fatalf("expected impact unknown evidence, got %+v", evidence)
	}
}
