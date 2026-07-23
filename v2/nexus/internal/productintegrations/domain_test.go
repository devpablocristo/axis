package productintegrations

import (
	"strings"
	"testing"
)

func TestNormalizeSectionCanonicalizesAndRejectsUnsafeWebhooks(t *testing.T) {
	section, err := normalizeSection(Section{
		SchemaVersion: SchemaVersion,
		APIContracts: []APIContract{
			{Name: "Operations", Version: "V1"},
			{Name: "Authorization", Version: "v1"},
		},
		ActionTypes: []ActionTypeRef{{Key: "Calendar.Event.Create"}},
		AccessModes: []string{"via_companion", "direct"},
		Webhooks: []WebhookSubscription{{
			URL: "https://consumer.example.test/axis/events", EventTypes: []string{"Approval.Resolved"},
			SecretRef: "secret://products/reference/webhook-signing",
		}},
	})
	if err != nil {
		t.Fatalf("normalize section: %v", err)
	}
	if section.APIContracts[0].Name != "authorization" || section.ActionTypes[0].Key != "calendar.event.create" {
		t.Fatalf("expected canonical section, got %#v", section)
	}
	if section.AccessModes[0] != "direct" || section.AccessModes[1] != "via_companion" {
		t.Fatalf("expected deterministic access modes, got %#v", section.AccessModes)
	}

	_, err = normalizeSection(Section{
		SchemaVersion: SchemaVersion,
		APIContracts:  []APIContract{{Name: "operations", Version: "v1"}},
		AccessModes:   []string{"direct"},
		Webhooks: []WebhookSubscription{{
			URL: "http://127.0.0.1/admin", EventTypes: []string{"approval.resolved"},
			SecretRef: "plaintext-secret",
		}},
	})
	if err == nil {
		t.Fatal("expected unsafe webhook to be rejected")
	}
}

func TestSectionHashIsStableAcrossInputOrdering(t *testing.T) {
	first, err := normalizeSection(Section{
		SchemaVersion: SchemaVersion,
		APIContracts: []APIContract{
			{Name: "operations", Version: "v1"},
			{Name: "authorization", Version: "v1"},
		},
		ActionTypes: []ActionTypeRef{{Key: "z.action"}, {Key: "a.action"}},
		AccessModes: []string{"via_companion", "direct"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizeSection(Section{
		SchemaVersion: SchemaVersion,
		APIContracts: []APIContract{
			{Name: "authorization", Version: "v1"},
			{Name: "operations", Version: "v1"},
		},
		ActionTypes: []ActionTypeRef{{Key: "a.action"}, {Key: "z.action"}},
		AccessModes: []string{"direct", "via_companion"},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstHash, _ := sectionHash(first)
	secondHash, _ := sectionHash(second)
	if firstHash != secondHash || !validContentHash(firstHash) {
		t.Fatalf("expected stable valid hash: %q != %q", firstHash, secondHash)
	}
}

func TestNormalizeSectionNeverAcceptsInlineSecrets(t *testing.T) {
	for _, secretRef := range []string{"sk_live_123", "Bearer abc", strings.Repeat("a", 600)} {
		_, err := normalizeSection(Section{
			SchemaVersion: SchemaVersion,
			APIContracts:  []APIContract{{Name: "operations", Version: "v1"}},
			AccessModes:   []string{"direct"},
			Webhooks: []WebhookSubscription{{
				URL: "https://consumer.example.test/events", EventTypes: []string{"incident.opened"},
				SecretRef: secretRef,
			}},
		})
		if err == nil {
			t.Fatalf("expected secret reference %q to be rejected", secretRef)
		}
	}
}

func TestConfiguredAreasAreDerivedWithoutProductBranches(t *testing.T) {
	areas := configuredAreas(Section{
		APIContracts: []APIContract{
			{Name: "authorization", Version: "v1"},
			{Name: "approvals", Version: "v1"},
		},
		AccessModes: []string{"direct"},
	})
	if !containsConfigured(areas, "authorization", "direct") || !containsConfigured(areas, "approval", "direct") {
		t.Fatalf("unexpected configured areas: %#v", areas)
	}
}

func TestAccessModeCompatibilityUsesNeutralCanonicalValue(t *testing.T) {
	if got := canonicalAccessMode(AccessModeViaCompanion); got != AccessModeViaOrchestrator {
		t.Fatalf("legacy mode canonicalized to %q", got)
	}
	if !accessModeAllowed([]string{AccessModeViaCompanion}, AccessModeViaOrchestrator) {
		t.Fatal("active v2 snapshot must authorize the neutral orchestrator mode")
	}
	if accessModeAllowed([]string{AccessModeDirect}, "unknown") {
		t.Fatal("unknown access mode must fail closed")
	}
}
