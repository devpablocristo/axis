package productcontracts

import (
	"testing"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/productevals"
	"github.com/devpablocristo/companion/internal/products"
)

func TestValidateSpecPassesReusableProductContract(t *testing.T) {
	t.Parallel()

	report := ValidateSpec(validSpec())
	if report.Status != StatusPassed {
		t.Fatalf("expected passing contract report, got %+v", report)
	}
	if len(report.Steps) != 8 {
		t.Fatalf("expected onboarding checklist steps, got %+v", report.Steps)
	}
}

func TestReferenceProductContractFixturePasses(t *testing.T) {
	t.Parallel()

	contractPath, err := productevals.FindRepoFile("companion/scripts/onboarding/reference-product-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	evalPackPath, err := productevals.FindRepoFile("companion/scripts/evals/reference-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := LoadSpec(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := productevals.LoadPack(evalPackPath)
	if err != nil {
		t.Fatal(err)
	}
	spec.EvalPack = &pack

	report := ValidateSpec(spec)
	if report.Status != StatusPassed {
		t.Fatalf("expected reference fixture to pass, got %+v", report)
	}
}

func TestValidateSpecRejectsWriteWithoutNexusMetadata(t *testing.T) {
	t.Parallel()

	spec := validSpec()
	write := validManifest()
	write.CapabilityID = "demo.write"
	write.ActionType = capabilities.ActionTypeWrite
	write.SideEffectType = capabilities.SideEffectWrite
	write.ApprovalRequired = false
	write.NexusActionType = ""
	write.IdempotencyMode = capabilities.IdempotencyOptional
	spec.Capabilities = []capabilities.Manifest{write}

	report := ValidateSpec(spec)
	if report.Status != StatusFailed {
		t.Fatalf("expected failed contract report, got %+v", report)
	}
	if !hasFailure(report, "capability_contracts") {
		t.Fatalf("expected capability_contracts failure, got %+v", report.BlockingFailures)
	}
}

func TestValidateSpecRequiresExpectedErrorContract(t *testing.T) {
	t.Parallel()

	spec := validSpec()
	spec.ExpectedErrors = nil
	report := ValidateSpec(spec)
	if report.Status != StatusFailed || !hasFailure(report, "expected_errors") {
		t.Fatalf("expected expected_errors failure, got %+v", report)
	}
}

func TestValidateSpecRejectsMalformedSecretRef(t *testing.T) {
	t.Parallel()

	spec := validSpec()
	spec.Installation.AuthMode = products.AuthModeAPIKeyRef
	spec.Installation.SecretRef = "plain-secret"

	report := ValidateSpec(spec)
	if report.Status != StatusFailed || !hasFailure(report, "installation_active") {
		t.Fatalf("expected installation_active failure, got %+v", report)
	}
}

func validSpec() Spec {
	return Spec{
		Version: 1,
		OrgID:   "org-a",
		Product: products.Product{
			ProductSurface: "demo",
			DisplayName:    "Demo",
			Status:         products.ProductStatusActive,
		},
		Installation: products.Installation{
			OrgID:          "org-a",
			ProductSurface: "demo",
			BaseURL:        "https://demo.example.com",
			AuthMode:       products.AuthModeInternalJWT,
			Enabled:        true,
		},
		Identity: IdentitySpec{
			OrgID:          "org-a",
			ProductSurface: "demo",
			ActorID:        "user-a",
			ActorType:      "human",
			Scopes:         []string{"demo:read"},
		},
		Capabilities: []capabilities.Manifest{validManifest()},
		EvalPack: &productevals.Pack{
			Version:        1,
			SuiteID:        "demo-golden",
			ProductSurface: "demo",
			Thresholds:     map[string]float64{"routing_accuracy_min": 0.8},
			Tenants:        productevals.Tenants{Primary: "org-a", Shadow: "org-b"},
			Cases:          []productevals.Case{{ID: "read", Query: "show dashboard"}},
		},
		ExpectedErrors: []ExpectedError{
			{Scenario: "installation_missing", ExpectedCode: "FORBIDDEN"},
			{Scenario: "product_disabled", ExpectedCode: "FORBIDDEN"},
			{Scenario: "scope_missing", ExpectedCode: "FORBIDDEN"},
			{Scenario: "write_without_nexus", ExpectedCode: "VALIDATION"},
		},
		Runtime: RuntimeReadiness{Enabled: true},
	}
}

func validManifest() capabilities.Manifest {
	return capabilities.Manifest{
		SchemaVersion:      capabilities.SchemaVersion,
		CapabilityID:       "demo.summary",
		Version:            "1.0.0",
		DisplayName:        "Demo summary",
		Description:        "Read demo summary",
		Owner:              "axis",
		ProductSurface:     "demo",
		Connector:          "demo",
		ActionType:         capabilities.ActionTypeRead,
		RiskLevel:          capabilities.RiskLow,
		SideEffectType:     capabilities.SideEffectRead,
		AuthMode:           "internal_jwt",
		RequiredScopes:     []string{"demo:read"},
		InputSchema:        objectSchema(map[string]any{}),
		OutputSchema:       objectSchema(map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}),
		EvidenceSchema:     objectSchema(map[string]any{"source": map[string]any{"type": "string"}}),
		RequiredEvidence:   []string{"source"},
		TenantConfigurable: true,
		EnabledByDefault:   false,
		RateLimitClass:     "standard",
		CostClass:          "low",
		Timeout:            "30s",
		Retries:            capabilities.RetryPolicy{MaxAttempts: 1},
		ObservabilityTags:  []string{"product:demo"},
	}
}

func objectSchema(properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
}

func hasFailure(report Report, id string) bool {
	for _, failure := range report.BlockingFailures {
		if failure == id {
			return true
		}
	}
	return false
}
