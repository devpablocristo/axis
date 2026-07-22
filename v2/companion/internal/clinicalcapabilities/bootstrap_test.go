package clinicalcapabilities

import (
	"testing"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
)

func TestBootstrapDefinitionsMatchP1GovernanceAndAssignments(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 2 {
		t.Fatalf("expected two canonical definitions, got %d", len(definitions))
	}
	for _, definition := range definitions {
		if definition.RiskClass != "medium" || definition.SideEffectClass != "read" || definition.RequiresNexusApproval || !definition.EvidenceRequired {
			t.Fatalf("unsafe clinical definition: %+v", definition)
		}
		if definition.Manifest.RollbackMode != "none" || definition.Manifest.Retry.MaxAttempts != 1 || definition.Manifest.ProductSurface != "medmory" {
			t.Fatalf("unexpected manifest controls: %+v", definition.Manifest)
		}
		if _, err := domain.HashManifest(definition.Manifest); err != nil {
			t.Fatalf("manifest is not hashable: %v", err)
		}
		for _, role := range definition.JobRoleNames {
			if role == "Billing" || role == "Administrator" {
				t.Fatalf("clinical capability leaked into administrative role %q", role)
			}
		}
	}
	if definitions[0].CapabilityKey != RecordsSearchKey || definitions[0].RequiredAutonomy != "A0" || definitions[0].Manifest.TimeoutMS != 30000 {
		t.Fatalf("search definition drifted: %+v", definitions[0])
	}
	if definitions[1].CapabilityKey != TimelineBuildKey || definitions[1].RequiredAutonomy != "A1" || definitions[1].Manifest.TimeoutMS != 120000 || len(definitions[1].JobRoleNames) != 3 {
		t.Fatalf("timeline definition drifted: %+v", definitions[1])
	}
}
