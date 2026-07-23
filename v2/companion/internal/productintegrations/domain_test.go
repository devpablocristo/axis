package productintegrations

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeSectionV3UsesCapabilityUUIDAndOptionalLegacyKey(t *testing.T) {
	capabilityID := uuid.New()
	section, err := normalizeSection(Section{
		SchemaVersion: FunctionalSchemaVersion,
		APIContracts:  []APIContract{{Name: "axis.product-edge", Version: "v1"}},
		Capabilities: []CapabilityRef{{
			ID: capabilityID.String(), Version: "v1", ManifestHash: strings.Repeat("a", 64),
		}},
	})
	if err != nil {
		t.Fatalf("normalizeSection: %v", err)
	}
	if len(section.Capabilities) != 1 || section.Capabilities[0].ID != capabilityID.String() ||
		section.Capabilities[0].Key != "" {
		t.Fatalf("unexpected normalized capability: %+v", section.Capabilities)
	}
}

func TestNormalizeSectionV2StillRequiresCapabilityKey(t *testing.T) {
	_, err := normalizeSection(Section{
		SchemaVersion: SchemaVersion,
		APIContracts:  []APIContract{{Name: "assist-runs", Version: "v1"}},
		Capabilities: []CapabilityRef{{
			Version: "v1", ManifestHash: strings.Repeat("a", 64),
		}},
	})
	if err == nil {
		t.Fatal("expected a v2 capability without key to be rejected")
	}
}
