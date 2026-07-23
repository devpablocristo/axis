package capabilities

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type fakeQuotaPolicyChecker struct {
	allowed               bool
	organization, product string
	areas                 []string
}

func (c *fakeQuotaPolicyChecker) HasActivePolicies(_ context.Context, organization, product string, areas []string) (bool, error) {
	c.organization, c.product, c.areas = organization, product, append([]string(nil), areas...)
	return c.allowed, nil
}

func validManifestInput() domain.ManifestInput {
	return domain.ManifestInput{
		Version: "1.0.0", ProductSurface: "producta",
		InputSchema:    json.RawMessage(`{"type":"object","required":["subject_id"]}`),
		OutputSchema:   json.RawMessage(`{"type":"object","required":["summary"]}`),
		RequiredScopes: []string{"documents:read", "diagnosis:write"},
		Idempotency:    domain.IdempotencyContract{Mode: "required", KeyFields: []string{"subject_id", "repository_generation"}},
		RollbackMode:   "manual", TimeoutMS: 30_000,
		Retry:               domain.RetryContract{MaxAttempts: 3, BackoffMS: 1_000},
		Postconditions:      []string{"diagnosis persisted", "evidence linked"},
		QuotaAreas:          []string{quotas.AreaInbound, quotas.AreaLLM, quotas.AreaExecutors},
		SecretRefs:          []string{"secretmanager://projects/project/secrets/executor/versions/latest"},
		AttestationRequired: true, CostClass: "medium",
	}
}

func TestCapabilityMustConformBeforeAssignmentAndActivation(t *testing.T) {
	id := uuid.New()
	repo := &fakeCapabilityRepo{rows: map[uuid.UUID]domain.Capability{id: {
		ID: id, OrgID: "organization-1", CapabilityKey: "diagnosis.reports.create", Name: "Create diagnosis",
		RequiredAutonomy: virployeedomain.AutonomyA3, RiskClass: "high", SideEffectClass: "write",
		RequiresGovernanceApproval: true, EvidenceRequired: true, PromotionState: domain.PromotionDraft,
	}}}
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	checker := &fakeQuotaPolicyChecker{allowed: true}
	uc.SetQuotaPolicyChecker(checker)

	updated, err := uc.UpdateManifest(context.Background(), "organization-1", id, validManifestInput())
	if err != nil || updated.ManifestHash == "" || updated.PromotionState != domain.PromotionDraft {
		t.Fatalf("UpdateManifest: %+v err=%v", updated, err)
	}
	if err := uc.EnsureAssignable(context.Background(), "organization-1", []uuid.UUID{id}, virployeedomain.AutonomyA3); err == nil {
		t.Fatal("draft capability must not be assignable")
	}

	conformed, report, err := uc.Conform(context.Background(), "organization-1", id)
	if err != nil || !report.Conformant || conformed.PromotionState != domain.PromotionConformant || conformed.ConformedHash != conformed.ManifestHash {
		t.Fatalf("Conform: capability=%+v report=%+v err=%v", conformed, report, err)
	}
	if err := uc.EnsureAssignable(context.Background(), "organization-1", []uuid.UUID{id}, virployeedomain.AutonomyA3); err == nil {
		t.Fatal("conformant but inactive capability must not be assignable")
	}
	checker.allowed = false
	demoted, report, err := uc.Activate(context.Background(), "organization-1", id)
	if err != nil || report.Conformant || demoted.PromotionState != domain.PromotionDraft {
		t.Fatalf("activation must recheck policies and demote on failure: capability=%+v report=%+v err=%v", demoted, report, err)
	}
	checker.allowed = true
	conformed, report, err = uc.Conform(context.Background(), "organization-1", id)
	if err != nil || !report.Conformant || conformed.PromotionState != domain.PromotionConformant {
		t.Fatalf("re-conform: capability=%+v report=%+v err=%v", conformed, report, err)
	}
	active, report, err := uc.Activate(context.Background(), "organization-1", id)
	if err != nil || !report.Conformant || active.PromotionState != domain.PromotionActive {
		t.Fatalf("Activate: capability=%+v report=%+v err=%v", active, report, err)
	}
	if err := uc.EnsureAssignable(context.Background(), "organization-1", []uuid.UUID{id}, virployeedomain.AutonomyA3); err != nil {
		t.Fatalf("active conformant capability must be assignable: %v", err)
	}
	if checker.organization != "organization-1" || checker.product != "producta" || len(checker.areas) != 3 {
		t.Fatalf("quota check was not correctly scoped: %+v", checker)
	}
}

func TestConformanceFailsClosedForMissingPoliciesAndPlainSecret(t *testing.T) {
	manifest, hash, err := domain.NormalizeManifest(validManifestInput())
	if err != nil {
		t.Fatal(err)
	}
	manifest.SecretRefs = []string{"executor-password-in-plain-text"}
	capability := domain.Capability{
		OrgID: "organization-1", SideEffectClass: "write", RequiresGovernanceApproval: true, EvidenceRequired: true,
		Manifest: manifest, ManifestHash: hash,
	}
	report, err := validateConformance(context.Background(), capability, &fakeQuotaPolicyChecker{allowed: false})
	if err != nil {
		t.Fatal(err)
	}
	if report.Conformant || checkPassed(report, "secrets") || checkPassed(report, "quotas") {
		t.Fatalf("plain secret and missing policies must fail conformance: %+v", report)
	}
}

func TestUpdateManifestRejectsPlainSecretBeforePersistence(t *testing.T) {
	id := uuid.New()
	repo := &fakeCapabilityRepo{rows: map[uuid.UUID]domain.Capability{id: {
		ID: id, OrgID: "organization-1", CapabilityKey: "calendar.events.create", PromotionState: domain.PromotionDraft,
	}}}
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	input := validManifestInput()
	input.SecretRefs = []string{"actual-client-secret"}
	if _, err := uc.UpdateManifest(context.Background(), "organization-1", id, input); err == nil {
		t.Fatal("plain credential must be rejected before storing the manifest")
	}
	if repo.rows[id].ManifestHash != "" {
		t.Fatal("rejected secret must not mutate the persisted capability")
	}
}

func checkPassed(report domain.ConformanceReport, key string) bool {
	for _, check := range report.Checks {
		if check.Key == key {
			return check.Passed
		}
	}
	return false
}
