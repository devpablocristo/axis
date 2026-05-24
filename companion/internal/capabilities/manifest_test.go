package capabilities

import (
	"errors"
	"testing"
	"testing/fstest"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

func validManifest() Manifest {
	return Manifest{
		SchemaVersion:      SchemaVersion,
		CapabilityID:       "billing.invoice.create",
		Version:            "1.0.0",
		DisplayName:        "Create invoice",
		Description:        "Creates an invoice in an external billing system.",
		Owner:              "billing",
		ProductSurface:     "billing",
		Connector:          "billing",
		ActionType:         ActionTypeWrite,
		RiskLevel:          RiskHigh,
		SideEffectType:     SideEffectWrite,
		AuthMode:           "delegated_user",
		RequiredScopes:     []string{"companion:connectors:execute"},
		InputSchema:        objectSchema("org_id", "customer_id"),
		OutputSchema:       objectSchema("invoice_id"),
		EvidenceSchema:     objectSchema("invoice_id", "external_ref"),
		RequiredEvidence:   []string{"invoice_id", "external_ref"},
		IdempotencyMode:    IdempotencyRequired,
		NexusActionType:    DefaultInvokeActionType,
		ApprovalRequired:   true,
		TenantConfigurable: true,
		EnabledByDefault:   true,
		RateLimitClass:     "standard",
		CostClass:          "medium",
		Timeout:            "30s",
		Retries:            RetryPolicy{MaxAttempts: 2, Backoff: "exponential"},
		Preconditions:      []string{"customer_org_context"},
		Postconditions:     []string{"invoice_id"},
		ObservabilityTags:  []string{"connector:billing"},
	}
}

func TestRegistryAcceptsVersionedManifestsAndIndexesLatestOperation(t *testing.T) {
	t.Parallel()
	v1 := validManifest()
	v2 := validManifest()
	v2.Version = "1.1.0"
	v2.DisplayName = "Create invoice v2"

	reg, err := NewRegistry([]Manifest{v1, v2})
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.All(); len(got) != 2 {
		t.Fatalf("expected both versions, got %d", len(got))
	}
	latest, ok := reg.LookupOperation("billing", "billing.invoice.create")
	if !ok {
		t.Fatal("expected operation lookup")
	}
	if latest.Version != "1.1.0" {
		t.Fatalf("expected latest version, got %q", latest.Version)
	}
}

func TestRegistryRejectsDuplicateCapabilityVersion(t *testing.T) {
	t.Parallel()
	m := validManifest()
	_, err := NewRegistry([]Manifest{m, m})
	if !errors.Is(err, ErrDuplicateManifest) {
		t.Fatalf("expected duplicate manifest error, got %v", err)
	}
}

func TestManifestRejectsSchemaWithMissingRequiredProperty(t *testing.T) {
	t.Parallel()
	m := validManifest()
	m.InputSchema = map[string]any{
		"type":     "object",
		"required": []string{"org_id"},
		"properties": map[string]any{
			"customer_id": map[string]any{"type": "string"},
		},
	}
	if err := m.Validate(); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected invalid manifest, got %v", err)
	}
}

func TestLoadFSLoadsManifestSetsStrictly(t *testing.T) {
	t.Parallel()
	raw := `{
		"capabilities": [
			{
				"schema_version":"capability_manifest.v1",
				"capability_id":"crm.customer.lookup",
				"version":"1.0.0",
				"display_name":"Lookup customer",
				"description":"Reads one customer record.",
				"owner":"crm",
				"product_surface":"crm",
				"connector":"crm",
				"action_type":"read",
				"risk_level":"low",
				"side_effect_type":"read",
				"auth_mode":"delegated_user",
				"required_scopes":["companion:connectors:execute"],
				"input_schema":{"type":"object","required":["org_id"],"properties":{"org_id":{"type":"string"}}},
				"output_schema":{"type":"object","properties":{"customer":{"type":"object"}}},
				"evidence_schema":{"type":"object","properties":{"customer":{"type":"object"}}},
				"required_evidence":["customer"],
				"idempotency_mode":"none",
				"rollback_supported":false,
				"compensation_strategy":"none",
				"approval_required":false,
				"tenant_configurable":true,
				"enabled_by_default":true,
				"rate_limit_class":"standard",
				"cost_class":"low",
				"timeout":"15s",
				"retries":{"max_attempts":1,"backoff":"none"},
				"postconditions":["customer"],
				"preconditions":["customer_org_context"],
				"observability_tags":["connector:crm"]
			}
		]
	}`
	reg, err := LoadFS(fstest.MapFS{"manifests/crm.json": {Data: []byte(raw)}}, "manifests")
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.All(); len(got) != 1 || got[0].CapabilityID != "crm.customer.lookup" {
		t.Fatalf("unexpected loaded manifests %+v", got)
	}
}

func TestFromConnectorCapabilityRepairsHistoricalSchemas(t *testing.T) {
	t.Parallel()
	manifest, err := FromConnectorCapability("mock", "mock", connectordomain.Capability{
		Operation:             "mock.write",
		Mode:                  connectordomain.CapabilityModeWrite,
		SideEffect:            true,
		RiskClass:             connectordomain.RiskClassMedium,
		RequiresNexusApproval: true,
		RequiredScopes:        []string{"companion:connectors:execute"},
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"message"},
		},
		EvidenceFields: []string{"external_ref"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.NexusActionType != DefaultInvokeActionType || !manifest.ApprovalRequired {
		t.Fatalf("expected approved write defaults, got %+v", manifest)
	}
	props := manifest.InputSchema["properties"].(map[string]any)
	if _, ok := props["message"]; !ok {
		t.Fatalf("expected historical required field to be materialized, got %+v", manifest.InputSchema)
	}
	evidenceProps := manifest.EvidenceSchema["properties"].(map[string]any)
	if _, ok := evidenceProps["external_ref"]; !ok {
		t.Fatalf("expected evidence schema to include historical field, got %+v", manifest.EvidenceSchema)
	}
}

func objectSchema(required ...string) map[string]any {
	props := make(map[string]any, len(required))
	for _, key := range required {
		props[key] = map[string]any{"type": "string"}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
