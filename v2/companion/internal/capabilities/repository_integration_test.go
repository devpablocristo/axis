package capabilities

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryCapabilityPromotionAndInvalidation(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_CAPABILITY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_CAPABILITY_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool)
	orgID := "capability-test-" + uuid.NewString()
	approval := true
	created, err := repository.Create(ctx, orgID, domain.NormalizedCreateInput{
		CapabilityKey: "diagnosis.reports.create", Name: "Create diagnosis", RequiredAutonomy: "A3",
		Governance: domain.NormalizedGovernance{
			RiskClass: "high", SideEffectClass: "write", RequiresGovernanceApproval: approval, EvidenceRequired: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.PromotionState != domain.PromotionDraft {
		t.Fatalf("new capability must be draft, got %+v", created)
	}
	manifest, hash, err := domain.NormalizeManifest(domain.ManifestInput{
		Version: "1.0.0", ProductSurface: "producta",
		InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpdateManifest(ctx, created.OrgID, created.ID, manifest, hash)
	if err != nil || updated.ManifestHash != hash || updated.PromotionState != domain.PromotionDraft {
		t.Fatalf("UpdateManifest: %+v err=%v", updated, err)
	}
	report := domain.ConformanceReport{Conformant: true, ManifestHash: hash, Checks: []domain.ConformanceCheck{{Key: "test", Passed: true}}}
	conformed, err := repository.SaveConformance(ctx, created.OrgID, created.ID, updated, report)
	if err != nil || conformed.PromotionState != domain.PromotionConformant || conformed.ConformedHash != hash {
		t.Fatalf("SaveConformance: %+v err=%v", conformed, err)
	}
	active, err := repository.Activate(ctx, created.OrgID, created.ID, hash)
	if err != nil || active.PromotionState != domain.PromotionActive || active.ActivatedAt == nil {
		t.Fatalf("Activate: %+v err=%v", active, err)
	}
	changed, err := repository.Update(ctx, created.OrgID, created.ID, domain.NormalizedUpdateInput{
		Name: created.Name, RequiredAutonomy: created.RequiredAutonomy,
		Governance: domain.NormalizedGovernance{
			RiskClass: "critical", SideEffectClass: "write", RequiresGovernanceApproval: true, EvidenceRequired: true,
		},
	})
	if err != nil || changed.PromotionState != domain.PromotionDraft || changed.ConformedHash != "" || changed.ActivatedAt != nil {
		t.Fatalf("governance change must invalidate conformance: %+v err=%v", changed, err)
	}
	if _, err := repository.SaveConformance(ctx, created.OrgID, created.ID, active, report); err == nil {
		t.Fatal("stale conformance must not overwrite a concurrent governance change")
	}
	if _, err := repository.Get(ctx, "other-organization", created.ID); err == nil {
		t.Fatal("capability repository must enforce organization isolation")
	}
}
