package invocation

import (
	"strings"
	"testing"
)

func TestNormalizeCanonicalizesTrustedInvocation(t *testing.T) {
	hash := strings.Repeat("a", 64)
	got, err := Normalize(Context{
		OrgID: " org-1 ", ProductID: " product-1 ", ProductSurface: " Medmory ",
		IntegrationID: " integration-1 ", IntegrationRevision: 7, IntegrationHash: hash,
		PrincipalType: " USER ", PrincipalID: "person-1", Scopes: []string{"Assist.Write", "assist.write"},
		AccessMode: " VIA_ORCHESTRATOR ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SchemaVersion || got.OrgID != "org-1" || got.ProductSurface != "medmory" ||
		got.AccessMode != AccessModeViaOrchestrator || len(got.Scopes) != 1 || got.Scopes[0] != "assist.write" {
		t.Fatalf("unexpected normalized invocation: %+v", got)
	}
}

func TestNormalizeRejectsPartialIntegrationBinding(t *testing.T) {
	_, err := Normalize(Context{OrgID: "org-1", ProductID: "product-1", IntegrationID: "integration-1"})
	if err == nil {
		t.Fatal("partial integration binding must fail closed")
	}
}

func TestNormalizeAllowsLegacyDirectInvocation(t *testing.T) {
	got, err := Normalize(Context{OrgID: "org-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessMode != AccessModeDirect {
		t.Fatalf("unexpected access mode: %s", got.AccessMode)
	}
}
