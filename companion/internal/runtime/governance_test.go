package runtime

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeRuntimeControls struct {
	policy   TenantRuntimePolicy
	usage    TenantRuntimeUsage
	recorded []RunUsage
}

func (f *fakeRuntimeControls) GetRuntimePolicy(_ context.Context, orgID string) (TenantRuntimePolicy, error) {
	if f.policy.OrgID == "" {
		return defaultRuntimePolicy(orgID), nil
	}
	return f.policy, nil
}

func (f *fakeRuntimeControls) UpsertRuntimePolicy(_ context.Context, policy TenantRuntimePolicy) (TenantRuntimePolicy, error) {
	f.policy = normalizeRuntimePolicy(policy)
	return f.policy, nil
}

func (f *fakeRuntimeControls) GetRuntimeUsage(_ context.Context, orgID, period string) (TenantRuntimeUsage, error) {
	if f.usage.OrgID == "" {
		return TenantRuntimeUsage{OrgID: orgID, Period: period}, nil
	}
	return f.usage, nil
}

func (f *fakeRuntimeControls) AddRuntimeUsage(_ context.Context, _ string, _ string, usage RunUsage) error {
	f.recorded = append(f.recorded, usage)
	return nil
}

func TestOrchestrator_RuntimeKillSwitchRejectsBeforeProvider(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "should not run"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: make(map[string]ToolHandler)}, ContextPorts{})
	orch.SetRuntimeControls(&fakeRuntimeControls{policy: TenantRuntimePolicy{
		OrgID:       "org-1",
		Enabled:     true,
		KillSwitch:  true,
		MaxAutonomy: AutonomyA2,
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", Message: "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 0 {
		t.Fatalf("expected provider not called when runtime kill switch is active, got %d", provider.callCount)
	}
	if len(result.Trace.GuardrailEvents) != 1 || result.Trace.GuardrailEvents[0].Type != "tenant_runtime_policy" {
		t.Fatalf("expected tenant policy guardrail event, got %+v", result.Trace.GuardrailEvents)
	}
}

func TestOrchestrator_RuntimePolicyCapsAutonomyAndRecordsUsage(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	gov := &fakeRuntimeControls{policy: TenantRuntimePolicy{
		OrgID:       "org-1",
		Enabled:     true,
		MaxAutonomy: AutonomyA1,
	}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: make(map[string]ToolHandler)}, ContextPorts{})
	orch.SetDefaultAutonomy(AutonomyA3)
	orch.SetRuntimeControls(gov)

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", Message: "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trace.AutonomyLevel != AutonomyA1 {
		t.Fatalf("expected autonomy capped to A1, got %s", result.Trace.AutonomyLevel)
	}
	if result.Trace.Usage.LLMCalls != 1 || result.Trace.Usage.EstimatedTotalTokens == 0 {
		t.Fatalf("expected usage accounting, got %+v", result.Trace.Usage)
	}
	if len(gov.recorded) != 1 || gov.recorded[0].LLMCalls != 1 {
		t.Fatalf("expected runtime usage recorded, got %+v", gov.recorded)
	}
}

func TestOrchestrator_FiltersLLMVisibleToolsByAgentRoute(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	toolkit := &ToolKit{
		Schemas: []ToolSchema{
			{Name: "remember"},
			{Name: "check_approvals"},
		},
		Handlers: map[string]ToolHandler{
			"remember":        func(_ context.Context, _ json.RawMessage) (string, error) { return `{}`, nil },
			"check_approvals": func(_ context.Context, _ json.RawMessage) (string, error) { return `{}`, nil },
		},
		policies: map[string]toolPolicy{
			"remember":        {RequiresTenant: true, RequiresUser: true},
			"check_approvals": {RequiresTenant: true, RequiredAnyScope: []string{scopeCompanionNexusAdmin}},
		},
	}
	orch := NewOrchestrator(provider, toolkit, ContextPorts{})

	_, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", Message: "recordar preferencia",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.lastTools()) != 1 || provider.lastTools()[0].Name != "remember" {
		t.Fatalf("expected only memory route tool to be visible to LLM, got %+v", provider.lastTools())
	}
}

func TestRouteAllowsWildcardProductTools(t *testing.T) {
	t.Parallel()

	route := AgentRoute{AllowedTools: []string{"ponti_*"}}
	if !routeAllowsTool(route, "ponti_customers_search") {
		t.Fatal("expected wildcard product tool to be allowed")
	}
	if routeAllowsTool(route, "pymes_customers_search") {
		t.Fatal("expected different product prefix to be rejected")
	}
}
