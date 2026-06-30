package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	connectorsdomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

type fakeRuntimeControls struct {
	policy   TenantRuntimePolicy
	usage    TenantRuntimeUsage
	recorded []RunUsage
}

type fakeCostLedger struct {
	summary CostSummary
	events  []CostEvent
}

type fakeAgentResolver struct {
	resolve func(context.Context, string, string, string) (RuntimeAgentConfig, error)
	agent   RuntimeAgentConfig
	err     error
}

type fakeEmployeeResolver struct {
	resolve  func(context.Context, string, string, string, string) (RuntimeEmployeeConfig, error)
	employee RuntimeEmployeeConfig
	err      error
}

func (f fakeAgentResolver) ResolveRuntimeAgent(ctx context.Context, orgID, productSurface, agentID string) (RuntimeAgentConfig, error) {
	if f.resolve != nil {
		return f.resolve(ctx, orgID, productSurface, agentID)
	}
	if f.err != nil {
		return RuntimeAgentConfig{}, f.err
	}
	return f.agent, nil
}

func (f fakeEmployeeResolver) ResolveRuntimeEmployee(ctx context.Context, tenantID, orgID, productSurface, employeeID string) (RuntimeEmployeeConfig, error) {
	if f.resolve != nil {
		return f.resolve(ctx, tenantID, orgID, productSurface, employeeID)
	}
	if f.err != nil {
		return RuntimeEmployeeConfig{}, f.err
	}
	return f.employee, nil
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

func (f *fakeRuntimeControls) ListRuntimePolicyAudit(_ context.Context, _ string, _ int) ([]RuntimePolicyAuditEntry, error) {
	return nil, nil
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

func (f *fakeCostLedger) RecordCostEvent(_ context.Context, event CostEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeCostLedger) GetCostSummary(_ context.Context, orgID, productSurface, period string, _ int) (CostSummary, error) {
	summary := f.summary
	summary.OrgID = orgID
	summary.ProductSurface = productSurface
	summary.Period = period
	return summary, nil
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

func TestOrchestrator_ProductBudgetRecordsGuardrailObservability(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}
	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "should not run"}}}
	policy := defaultRuntimePolicy("org-1")
	policy.ControlPlane.ProductPolicies = map[string]ProductRuntimePolicy{
		"pymes": {MonthlyCostBudgetCents: 100},
	}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: make(map[string]ToolHandler)}, ContextPorts{})
	orch.SetRuntimeControls(&fakeRuntimeControls{policy: policy})
	orch.SetCostLedger(&fakeCostLedger{summary: CostSummary{EstimatedCostCents: 100}})
	orch.SetObservabilityRecorder(observer)

	result, err := orch.Run(context.Background(), RunInput{
		UserID:         "user-1",
		OrgID:          "org-1",
		ProductSurface: "pymes",
		Message:        "analizá stock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 0 {
		t.Fatalf("expected provider not called when product budget is exhausted, got %d", provider.callCount)
	}
	if len(result.Trace.GuardrailEvents) != 1 || result.Trace.GuardrailEvents[0].Type != "product_runtime_budget" {
		t.Fatalf("expected product budget guardrail, got %+v", result.Trace.GuardrailEvents)
	}
	event := findRuntimeEvent(observer.events, "guardrail", "product_runtime_budget")
	if event == nil {
		t.Fatalf("expected product budget observability event, got %+v", observer.events)
	}
	if event.OrgID != "org-1" || event.ProductSurface != "pymes" {
		t.Fatalf("expected org/product scoped event, got %+v", event)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["target"] != "cost:pymes" || payload["reason"] != "monthly product cost budget exhausted" {
		t.Fatalf("unexpected guardrail payload: %+v", payload)
	}
}

func TestApplyRuntimePolicyHonorsProductPolicies(t *testing.T) {
	t.Parallel()

	route := AgentRoute{Product: "pymes", Autonomy: AutonomyA3, Profile: AgentProfile{MaxAutonomy: AutonomyA3}}
	policy := defaultRuntimePolicy("org-1")
	policy.ControlPlane.ProductPolicies = map[string]ProductRuntimePolicy{
		"pymes": {Denied: true},
	}
	decision := applyRuntimePolicy(policy, TenantRuntimeUsage{OrgID: "org-1"}, route, DefaultGeminiModel)
	if decision.Event == nil || decision.Event.Type != "product_runtime_policy" || decision.Reply == "" {
		t.Fatalf("expected denied product policy decision, got %+v", decision)
	}

	policy.ControlPlane.ProductPolicies = map[string]ProductRuntimePolicy{
		"pymes": {MaxAutonomy: AutonomyA1},
	}
	decision = applyRuntimePolicy(policy, TenantRuntimeUsage{OrgID: "org-1"}, route, DefaultGeminiModel)
	if decision.Route.Autonomy != AutonomyA1 {
		t.Fatalf("expected product policy to cap autonomy to A1, got %+v", decision.Route)
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

func TestApplyRuntimePolicy_FiltersToolsWithOrgControlPlane(t *testing.T) {
	t.Parallel()

	policy := TenantRuntimePolicy{
		OrgID:       "org-1",
		Enabled:     true,
		MaxAutonomy: AutonomyA2,
		ControlPlane: OrgControlPlaneSettings{
			AllowedTools:     []string{"remember", "demo_*"},
			DeniedTools:      []string{"demo_delete"},
			ToolKillSwitches: map[string]bool{"demo_archive": true},
		},
	}
	route := AgentRoute{
		AllowedTools: []string{"remember", "check_approvals", "demo_search", "demo_delete", "demo_archive"},
		Profile: AgentProfile{
			ID:           "default",
			AllowedTools: []string{"remember", "check_approvals", "demo_search", "demo_delete", "demo_archive"},
		},
	}

	decision := applyRuntimePolicy(policy, TenantRuntimeUsage{}, route, "gemini-1")
	if decision.Reply != "" {
		t.Fatalf("expected policy to filter tools without rejecting route: %s", decision.Reply)
	}
	got := decision.Route.AllowedTools
	want := []string{"remember", "demo_search"}
	if len(got) != len(want) {
		t.Fatalf("expected filtered tools %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected filtered tools %v, got %v", want, got)
		}
	}
}

func TestOrchestrator_AgentFleetRestrictsToolsAndRecordsIdentity(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	toolkit := &ToolKit{
		Schemas: []ToolSchema{{Name: "remember"}, {Name: "check_approvals"}},
		Handlers: map[string]ToolHandler{
			"remember":        func(_ context.Context, _ json.RawMessage) (string, error) { return `{}`, nil },
			"check_approvals": func(_ context.Context, _ json.RawMessage) (string, error) { return `{}`, nil },
		},
		policies: map[string]toolPolicy{
			"remember":        {RequiresTenant: true, RequiresUser: true},
			"check_approvals": {RequiresTenant: true},
		},
	}
	orch := NewOrchestrator(provider, toolkit, ContextPorts{})
	called := false
	orch.SetAgentResolver(fakeAgentResolver{resolve: func(context.Context, string, string, string) (RuntimeAgentConfig, error) {
		called = true
		return RuntimeAgentConfig{
			AgentID:      "support-agent",
			ProfileID:    "support-profile",
			Role:         "support",
			Status:       "active",
			MaxAutonomy:  AutonomyA1,
			AllowedTools: []string{"remember"},
			Version:      3,
		}, nil
	}})
	orch.SetAgentProfileResolver(fakeAgentProfileResolver{profile: RuntimeAgentProfileConfig{
		ProfileID:    "support-profile",
		VersionLabel: "v1",
		SystemPrompt: "Support profile.",
		MaxAutonomy:  AutonomyA2,
		Enabled:      true,
		SnapshotID:   "profile-support",
	}})
	orch.SetDefaultAutonomy(AutonomyA3)

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", AgentID: "support-agent", Message: "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trace.IdentityChain.AgentID != "support-agent" {
		t.Fatalf("expected agent identity, got %+v", result.Trace.IdentityChain)
	}
	if !called {
		t.Fatal("expected agent resolver to be called")
	}
	if result.Trace.AutonomyLevel != AutonomyA1 {
		t.Fatalf("expected agent autonomy cap, got %s reply=%q guardrails=%+v provider_calls=%d", result.Trace.AutonomyLevel, result.Reply, result.Trace.GuardrailEvents, provider.callCount)
	}
	if len(provider.lastTools()) != 1 || provider.lastTools()[0].Name != "remember" {
		t.Fatalf("expected agent-restricted tools, got %+v", provider.lastTools())
	}
}

func TestApplyRuntimeAgentCapsAutonomy(t *testing.T) {
	t.Parallel()

	route, event := applyRuntimeAgent(AgentRoute{
		Autonomy:     AutonomyA2,
		AllowedTools: []string{"remember", "check_approvals"},
		Profile: AgentProfile{
			ID:           "base",
			MaxAutonomy:  AutonomyA2,
			AllowedTools: []string{"remember", "check_approvals"},
		},
	}, RuntimeAgentConfig{
		AgentID:      "support-agent",
		ProfileID:    "support-profile",
		Status:       "active",
		MaxAutonomy:  AutonomyA1,
		AllowedTools: []string{"remember"},
	})
	if event != nil {
		t.Fatalf("unexpected event %+v", event)
	}
	if route.Autonomy != AutonomyA1 {
		t.Fatalf("expected A1, got %s", route.Autonomy)
	}
	if len(route.AllowedTools) != 1 || route.AllowedTools[0] != "remember" {
		t.Fatalf("expected tool restriction, got %+v", route.AllowedTools)
	}
}

func TestApplyRuntimeAgentRejectsMissingProfile(t *testing.T) {
	t.Parallel()

	_, event := applyRuntimeAgent(AgentRoute{}, RuntimeAgentConfig{
		AgentID: "support-agent",
		Status:  "active",
	})
	if event == nil || event.Type != "agent_fleet" || !strings.Contains(event.Reason, "profile") {
		t.Fatalf("expected missing profile guardrail, got %+v", event)
	}
}

func TestApplyRuntimePolicyPreservesLowerAgentAutonomy(t *testing.T) {
	t.Parallel()

	decision := applyRuntimePolicy(defaultRuntimePolicy("org-1"), TenantRuntimeUsage{}, AgentRoute{
		Autonomy: AutonomyA1,
		Profile:  AgentProfile{ID: "support", AgentID: "agent-1", MaxAutonomy: AutonomyA1},
	}, "gemini-test")
	if decision.Route.Autonomy != AutonomyA1 {
		t.Fatalf("expected A1, got %s event=%+v", decision.Route.Autonomy, decision.Event)
	}
}

func TestOrchestrator_AgentFleetRejectsDisabledAgentBeforeProvider(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "should not run"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetAgentResolver(fakeAgentResolver{agent: RuntimeAgentConfig{
		AgentID: "disabled-agent",
		Status:  "disabled",
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", AgentID: "disabled-agent", Message: "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 0 {
		t.Fatalf("expected provider not called, got %d", provider.callCount)
	}
	if len(result.Trace.GuardrailEvents) != 1 || result.Trace.GuardrailEvents[0].Type != "agent_fleet" {
		t.Fatalf("expected agent fleet guardrail, got %+v", result.Trace.GuardrailEvents)
	}
}

func TestApplyRuntimePolicy_RejectsDisabledProfile(t *testing.T) {
	t.Parallel()

	policy := TenantRuntimePolicy{
		OrgID:       "org-1",
		Enabled:     true,
		MaxAutonomy: AutonomyA2,
		ControlPlane: OrgControlPlaneSettings{
			AllowedProfiles: []string{"finance"},
		},
	}
	decision := applyRuntimePolicy(policy, TenantRuntimeUsage{}, AgentRoute{
		Profile: AgentProfile{ID: "support"},
	}, "gemini-1")
	if decision.Event == nil || decision.Event.Type != "org_control_plane" {
		t.Fatalf("expected org control plane rejection, got %+v", decision.Event)
	}
}

func TestValidateCapabilityControlPlane_FailsClosedWithoutPolicy(t *testing.T) {
	t.Parallel()

	reader := &missingPolicyReader{}
	event := validateCapabilityControlPlane(context.Background(), reader, "org-1", "demo_update", "demo", connectorsCapability("demo.update", "medium", true))
	if event == nil || event.Target != "policy" {
		t.Fatalf("expected missing policy to fail closed, got %+v", event)
	}
}

func TestValidateCapabilityControlPlane_RejectsDeniedAndRiskyCapability(t *testing.T) {
	t.Parallel()

	reader := &fakePolicyReader{policy: TenantRuntimePolicy{
		OrgID:       "org-1",
		Enabled:     true,
		MaxAutonomy: AutonomyA2,
		ControlPlane: OrgControlPlaneSettings{
			AllowedConnectors:  []string{"demo"},
			DeniedCapabilities: []string{"demo.delete"},
			MaxRiskClass:       "medium",
			ApprovalThresholds: map[string]string{"medium": "require_approval"},
		},
	}}
	if event := validateCapabilityControlPlane(context.Background(), reader, "org-1", "demo_delete", "demo", connectorsCapability("demo.delete", "medium", true)); event == nil {
		t.Fatal("expected denied capability to be rejected")
	}
	if event := validateCapabilityControlPlane(context.Background(), reader, "org-1", "demo_admin", "demo", connectorsCapability("demo.admin", "critical", true)); event == nil || event.Target != "risk:critical" {
		t.Fatalf("expected critical risk to be rejected, got %+v", event)
	}
	if event := validateCapabilityControlPlane(context.Background(), reader, "org-1", "demo_read", "demo", connectorsCapability("demo.read", "medium", false)); event == nil || event.Target != "approval_threshold" {
		t.Fatalf("expected threshold to reject ungated medium capability, got %+v", event)
	}
}

type missingPolicyReader struct{}

func (missingPolicyReader) GetRuntimePolicy(context.Context, string) (TenantRuntimePolicy, error) {
	return TenantRuntimePolicy{}, ErrRuntimePolicyNotFound
}

type fakePolicyReader struct {
	policy TenantRuntimePolicy
}

func (f *fakePolicyReader) GetRuntimePolicy(context.Context, string) (TenantRuntimePolicy, error) {
	return f.policy, nil
}

func connectorsCapability(operation, risk string, approval bool) connectorsdomain.Capability {
	mode := connectorsdomain.CapabilityModeRead
	if approval {
		mode = connectorsdomain.CapabilityModeWrite
	}
	return connectorsdomain.Capability{
		ID:                    operation,
		Version:               "1.0.0",
		Operation:             operation,
		Mode:                  mode,
		ReadOnly:              mode == connectorsdomain.CapabilityModeRead,
		RiskClass:             risk,
		RequiresNexusApproval: approval,
	}
}

func findRuntimeEvent(events []ObservabilityEvent, eventType, eventName string) *ObservabilityEvent {
	for i := range events {
		if events[i].EventType == eventType && events[i].EventName == eventName {
			return &events[i]
		}
	}
	return nil
}
