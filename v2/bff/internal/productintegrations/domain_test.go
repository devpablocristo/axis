package productintegrations

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func validContract() Contract {
	return Contract{
		SchemaVersion:    SchemaVersion,
		RequiredServices: []string{"companion", "bff"},
		Authentication: AuthenticationRequirements{
			Mode:   "api_key",
			Scopes: []string{"assist.write", "assist.read"},
		},
		Limits: Limits{
			MaxRequestBytes: 1 << 20,
			MaxResultBytes:  1 << 20,
			RatePerMinute:   60,
		},
		Services: map[string]ServiceSection{
			"bff": {
				SchemaVersion: SchemaVersion,
				APIContracts:  []APIContract{{Name: "gateway", Version: "v2"}},
			},
			"companion": {
				SchemaVersion: SchemaVersion,
				APIContracts:  []APIContract{{Name: "assist", Version: "v2"}},
			},
		},
	}
}

func TestNormalizeContractProducesStableCanonicalHash(t *testing.T) {
	first, err := normalizeContract(validContract())
	if err != nil {
		t.Fatalf("normalize first contract: %v", err)
	}
	secondInput := validContract()
	secondInput.RequiredServices = []string{"bff", "companion"}
	secondInput.Authentication.Scopes = []string{"assist.read", "assist.write"}
	second, err := normalizeContract(secondInput)
	if err != nil {
		t.Fatalf("normalize second contract: %v", err)
	}
	firstHash, err := contractHash(first)
	if err != nil {
		t.Fatalf("hash first contract: %v", err)
	}
	secondHash, err := contractHash(second)
	if err != nil {
		t.Fatalf("hash second contract: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("equivalent contracts must have the same hash: %s != %s", firstHash, secondHash)
	}
}

func TestNormalizeContractRejectsUnsafeWebhook(t *testing.T) {
	input := validContract()
	section := input.Services["companion"]
	section.Webhooks = []WebhookSubscription{{
		URL:        "http://consumer.example.test/events",
		EventTypes: []string{"assist.completed"},
		SecretRef:  "inline-secret",
	}}
	input.Services["companion"] = section

	_, err := normalizeContract(input)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected unsafe webhook rejection, got %v", err)
	}
}

func TestNormalizeContractRequiresExactServiceSections(t *testing.T) {
	input := validContract()
	delete(input.Services, "companion")
	if _, err := normalizeContract(input); err == nil {
		t.Fatal("missing required service section must be rejected")
	}
}

func TestNormalizeFunctionalContractHasNoServiceTopology(t *testing.T) {
	capabilityID := uuid.New()
	input := Contract{
		SchemaVersion: FunctionalSchemaVersion,
		Authentication: AuthenticationRequirements{
			Mode: "api_key", Scopes: []string{"assist.write", "assist.read"},
		},
		Limits:      Limits{MaxRequestBytes: 1 << 20, MaxResultBytes: 1 << 20, RatePerMinute: 60},
		Entrypoints: []Entrypoint{{Kind: "virployee", ID: uuid.New()}},
		Capabilities: []FunctionalCapability{{
			ID: capabilityID, Name: "Analizar estudios médicos", Version: "1",
			ManifestHash: strings.Repeat("a", 64), ExecutorBindingID: "medical.analyzer",
			Operation: "analyze", InputSchemaHash: strings.Repeat("b", 64),
			OutputSchemaHash: strings.Repeat("c", 64),
		}},
		GovernedOperations: []GovernedOperation{{
			CapabilityID: capabilityID, Operation: "analyze", RequiredScopes: []string{"assist.write"},
		}},
		ConnectorBindings: []ConnectorBinding{{
			ID: "medical.analyzer", ConnectorID: "tenant.connector", Operation: "analyze",
			SecretRef: "secret://tenant/connector",
		}},
	}
	normalized, err := normalizeContract(input)
	if err != nil {
		t.Fatalf("normalize v3 contract: %v", err)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "required_services") || strings.Contains(string(raw), `"services"`) {
		t.Fatalf("v3 contract leaked service topology: %s", raw)
	}
	versionRaw, err := json.Marshal(Version{
		SchemaVersion: FunctionalSchemaVersion, RequiredServices: []string{"bff", "companion"},
		Contract: normalized,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(versionRaw), "required_services") {
		t.Fatalf("v3 version response leaked service topology: %s", versionRaw)
	}

	registry := NewParticipantRegistry(
		NewHTTPParticipantWithProjection("companion", "http://companion", nil, ProductInvocationProjection),
		NewHTTPParticipantWithProjection("nexus", "http://nexus", nil, GovernanceProjection),
	).WithInvocationProjection(ProductInvocationProjection)
	required, err := registry.Required(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(required, []string{"bff", "companion", "nexus"}) {
		t.Fatalf("unexpected participant projection: %v", required)
	}
	section, ok, err := registry.InvocationSection(normalized)
	if err != nil || !ok || len(section.Capabilities) != 1 || section.Capabilities[0].ID != capabilityID.String() {
		t.Fatalf("functional capability was not projected by UUID: ok=%v err=%v section=%+v", ok, err, section)
	}
}

func TestTranslateV2ToV3UsesCanonicalCapabilityUUID(t *testing.T) {
	input := validContract()
	section := input.Services["companion"]
	capabilityID := uuid.New()
	section.Capabilities = []CapabilityRef{{
		ID: capabilityID.String(), Key: "studies.analyze", Version: "1",
		ManifestHash: strings.Repeat("a", 64),
	}}
	input.Services["companion"] = section
	translated, err := TranslateV2ToV3(input)
	if err != nil {
		t.Fatalf("translate v2 contract: %v", err)
	}
	if translated.SchemaVersion != FunctionalSchemaVersion || len(translated.Capabilities) != 1 ||
		translated.Capabilities[0].ID != capabilityID ||
		translated.Capabilities[0].LegacyKey != "studies.analyze" {
		t.Fatalf("unexpected v3 translation: %+v", translated)
	}
}
