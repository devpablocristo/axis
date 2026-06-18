package productcontracts

import (
	"encoding/json"
	"os"
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

	assertContractPasses(t, "reference")
}

func TestShadowProductContractFixturePasses(t *testing.T) {
	t.Parallel()

	assertContractPasses(t, "shadow")
}

func TestRealProductContractsPass(t *testing.T) {
	t.Parallel()

	for _, product := range []string{"ponti", "medmory"} {
		product := product
		t.Run(product, func(t *testing.T) {
			t.Parallel()
			assertContractPasses(t, product)
		})
	}
}

func TestMedmoryContractUsesGenericBillingAgent(t *testing.T) {
	t.Parallel()

	contractPath, err := productevals.FindRepoFile("companion/scripts/onboarding/medmory-product-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Agents []struct {
			AgentID             string   `json:"agent_id"`
			Role                string   `json:"role"`
			ProfileID           string   `json:"profile_id"`
			MaxAutonomy         string   `json:"max_autonomy"`
			AllowedCapabilities []string `json:"allowed_capabilities"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	var billingAgent *struct {
		AgentID             string   `json:"agent_id"`
		Role                string   `json:"role"`
		ProfileID           string   `json:"profile_id"`
		MaxAutonomy         string   `json:"max_autonomy"`
		AllowedCapabilities []string `json:"allowed_capabilities"`
	}
	for i := range contract.Agents {
		agent := &contract.Agents[i]
		if agent.AgentID == "billing_ops_agent" {
			t.Fatal("medmory contract must not expose deprecated billing_ops_agent")
		}
		if agent.AgentID == "billing_agent" {
			billingAgent = agent
		}
	}
	if billingAgent == nil {
		t.Fatal("missing generic billing_agent in medmory product contract")
	}
	if billingAgent.Role != "billing" || billingAgent.ProfileID != "axis.ops.billing.v1" || billingAgent.MaxAutonomy != "A1" {
		t.Fatalf("unexpected billing_agent metadata: %+v", billingAgent)
	}
	for _, forbidden := range []string{"medmory.summary.read", "medmory.timeline.read", "medmory.search.query", "medmory.asset.read"} {
		if contains(forbidden, billingAgent.AllowedCapabilities) {
			t.Fatalf("billing_agent must not include clinical capability %q: %+v", forbidden, billingAgent.AllowedCapabilities)
		}
	}
	for _, required := range []string{"medmory.ops.billing_status.read", "medmory.ops.billing_metrics.read", "medmory.ops.plan_requests.read", "medmory.ops.subscription_status.read", "medmory.ops.billing_adjustment.propose"} {
		if !contains(required, billingAgent.AllowedCapabilities) {
			t.Fatalf("billing_agent missing required capability %q: %+v", required, billingAgent.AllowedCapabilities)
		}
	}
}

func TestReadinessFixturesUseDistinctProductsAndOrgs(t *testing.T) {
	t.Parallel()

	reference := loadFixtureSpec(t, "reference")
	shadow := loadFixtureSpec(t, "shadow")
	if reference.Product.ProductSurface == shadow.Product.ProductSurface {
		t.Fatalf("expected distinct product surfaces, got %q", reference.Product.ProductSurface)
	}
	if reference.OrgID == shadow.OrgID {
		t.Fatalf("expected distinct org ids, got %q", reference.OrgID)
	}
	for _, surface := range []string{reference.Product.ProductSurface, shadow.Product.ProductSurface} {
		if surface == "ponti" || surface == "pymes" {
			t.Fatalf("readiness fixture must not use real product surface %q", surface)
		}
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

func contains(value string, values []string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func assertContractPasses(t *testing.T, product string) {
	t.Helper()
	spec := loadFixtureSpec(t, product)
	report := ValidateSpec(spec)
	if report.Status != StatusPassed {
		t.Fatalf("expected %s contract to pass, got %+v", product, report)
	}
}

func loadFixtureSpec(t *testing.T, product string) Spec {
	t.Helper()
	contractPath, err := productevals.FindRepoFile("companion/scripts/onboarding/" + product + "-product-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	evalPackPath, err := productevals.FindRepoFile("companion/scripts/evals/" + product + "-golden.json")
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
	return spec
}
