package capabilities

import (
	"context"
	"testing"
)

func TestUsecasesExposeCapabilityAndToolDomainViews(t *testing.T) {
	t.Parallel()
	repo := newFakeCapabilityRepo()
	manifest := validReadManifest()
	manifest.CapabilityID = "billing.invoice.read"
	manifest.DisplayName = "Read invoice"
	record, err := repo.UpsertManifest(context.Background(), ManifestRecord{
		Manifest: manifest,
		Status:   ManifestStatusActive,
		Source:   ManifestSourceGenerated,
	})
	if err != nil {
		t.Fatal(err)
	}
	uc := NewUsecases(repo)

	capability, err := uc.GetCapability(context.Background(), record.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if capability.CapabilityID != record.ID.String() || capability.CapabilityKey != "billing.invoice.read" {
		t.Fatalf("expected UUID id plus semantic key, got %+v", capability)
	}
	if capability.ToolID != record.ID.String() || capability.Mode != "read" || capability.RiskClass != RiskLow {
		t.Fatalf("unexpected capability view: %+v", capability)
	}

	tool, err := uc.GetTool(context.Background(), record.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if tool.ToolID != record.ID.String() || tool.ToolKey != "billing.invoice.read" {
		t.Fatalf("expected derived tool view, got %+v", tool)
	}
	if tool.CapabilityID != capability.CapabilityID || tool.CapabilityKey != capability.CapabilityKey || tool.Status != "active" {
		t.Fatalf("unexpected tool relation/status: %+v", tool)
	}
}

func TestUsecasesSetToolStatusMapsDisabledToBlockedCapability(t *testing.T) {
	t.Parallel()
	repo := newFakeCapabilityRepo()
	record, err := repo.UpsertManifest(context.Background(), ManifestRecord{
		Manifest: validReadManifest(),
		Status:   ManifestStatusActive,
		Source:   ManifestSourceGenerated,
	})
	if err != nil {
		t.Fatal(err)
	}
	uc := NewUsecases(repo)

	tool, err := uc.SetToolStatus(context.Background(), record.ID.String(), "disabled")
	if err != nil {
		t.Fatal(err)
	}
	if tool.Status != "disabled" {
		t.Fatalf("expected disabled tool status, got %+v", tool)
	}
	updated, err := repo.GetManifestByID(context.Background(), record.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ManifestStatusBlocked {
		t.Fatalf("expected blocked manifest status, got %+v", updated)
	}
}
