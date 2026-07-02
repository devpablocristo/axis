package runtime

import (
	"context"
	"strings"
	"testing"
)

type fakeAgentProfileResolver struct {
	profile RuntimeAgentProfileConfig
	err     error
}

func (f fakeAgentProfileResolver) ResolveRuntimeAgentProfile(context.Context, string) (RuntimeAgentProfileConfig, error) {
	if f.err != nil {
		return RuntimeAgentProfileConfig{}, f.err
	}
	return f.profile, nil
}

func TestOrchestrator_LoadsAgentProfilePrompt(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetAgentResolver(fakeAgentResolver{agent: RuntimeAgentConfig{
		AgentID:     "billing_agent",
		ProfileID:   "axis.ops.billing.v1",
		Status:      "active",
		MaxAutonomy: AutonomyA2,
	}})
	orch.SetAgentProfileResolver(fakeAgentProfileResolver{profile: RuntimeAgentProfileConfig{
		ProfileID:    "axis.ops.billing.v1",
		VersionLabel: "v1",
		SystemPrompt: "Do not access clinical data. Explain billing only.",
		MaxAutonomy:  AutonomyA1,
		Enabled:      true,
		SnapshotID:   "profile-row-1",
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", AgentID: "billing_agent", Message: "explicá cuotas",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 1 {
		t.Fatalf("expected provider call, got %d", provider.callCount)
	}
	if !strings.Contains(provider.requests[0].SystemPrompt, "Explain billing only") {
		t.Fatalf("expected profile prompt in system prompt, got %s", provider.requests[0].SystemPrompt)
	}
	if result.Trace.AutonomyLevel != AutonomyA1 {
		t.Fatalf("expected profile to cap autonomy to A1, got %s", result.Trace.AutonomyLevel)
	}
	if result.Trace.PromptVersion != "companion.system.v3+axis.ops.billing.v1:v1" {
		t.Fatalf("unexpected prompt version %q", result.Trace.PromptVersion)
	}
	if result.Trace.IdentityChain.AgentID != "billing_agent" {
		t.Fatalf("expected agent identity, got %+v", result.Trace.IdentityChain)
	}
}

func TestOrchestrator_LoadsVirployeeProfilePrompt(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetVirployeeResolver(fakeVirployeeResolver{virployee: RuntimeVirployeeConfig{
		VirployeeID: "11111111-1111-4111-8111-111111111111",
		TenantID:    "22222222-2222-4222-8222-222222222222",
		Name:        "Billing Virployee",
		Status:      "active",
		ProfileID:   "axis.ops.billing.v1",
		Autonomy:    AutonomyA2,
	}})
	orch.SetAgentProfileResolver(fakeAgentProfileResolver{profile: RuntimeAgentProfileConfig{
		ProfileID:    "axis.ops.billing.v1",
		VersionLabel: "v1",
		SystemPrompt: "Explain billing only.",
		MaxAutonomy:  AutonomyA1,
		Enabled:      true,
		SnapshotID:   "profile-row-1",
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID:      "user-1",
		OrgID:       "org-1",
		TenantID:    "22222222-2222-4222-8222-222222222222",
		VirployeeID: "11111111-1111-4111-8111-111111111111",
		Message:     "explica cuotas",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 1 {
		t.Fatalf("expected provider call, got %d", provider.callCount)
	}
	if result.Trace.IdentityChain.VirployeeID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("expected virployee identity, got %+v", result.Trace.IdentityChain)
	}
	if result.Trace.IdentityChain.AgentID != "" {
		t.Fatalf("expected no agent identity for virployee run, got %+v", result.Trace.IdentityChain)
	}
	if result.Trace.AutonomyLevel != AutonomyA1 {
		t.Fatalf("expected profile to cap autonomy to A1, got %s", result.Trace.AutonomyLevel)
	}
}

func TestOrchestrator_RejectsDisabledVirployeeBeforeProvider(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "should not run"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetVirployeeResolver(fakeVirployeeResolver{virployee: RuntimeVirployeeConfig{
		VirployeeID: "11111111-1111-4111-8111-111111111111",
		TenantID:    "22222222-2222-4222-8222-222222222222",
		Status:      "disabled",
		ProfileID:   "axis.ops.billing.v1",
		Autonomy:    AutonomyA2,
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID:      "user-1",
		OrgID:       "org-1",
		TenantID:    "22222222-2222-4222-8222-222222222222",
		VirployeeID: "11111111-1111-4111-8111-111111111111",
		Message:     "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 0 {
		t.Fatalf("expected provider not called, got %d", provider.callCount)
	}
	if len(result.Trace.GuardrailEvents) != 1 || result.Trace.GuardrailEvents[0].Type != "virployee" {
		t.Fatalf("expected virployee guardrail, got %+v", result.Trace.GuardrailEvents)
	}
}

func TestOrchestrator_AppliesProfileLLMConfig(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetModel("global-default-model")
	orch.SetAgentResolver(fakeAgentResolver{agent: RuntimeAgentConfig{
		AgentID:     "billing_agent",
		ProfileID:   "axis.ops.billing.v1",
		Status:      "active",
		MaxAutonomy: AutonomyA2,
	}})
	orch.SetAgentProfileResolver(fakeAgentProfileResolver{profile: RuntimeAgentProfileConfig{
		ProfileID:    "axis.ops.billing.v1",
		VersionLabel: "v1",
		SystemPrompt: "Explain billing only.",
		MaxAutonomy:  AutonomyA1,
		Enabled:      true,
		SnapshotID:   "profile-row-1",
		LLM: RuntimeLLMConfig{
			Model:       "gemini-2.5-pro",
			MaxTokens:   4096,
			Temperature: 0.3,
		},
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", AgentID: "billing_agent", Message: "explicá cuotas",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 1 {
		t.Fatalf("expected provider call, got %d", provider.callCount)
	}
	if got := provider.requests[0].MaxTokens; got != 4096 {
		t.Fatalf("expected profile max_tokens 4096 in request, got %d", got)
	}
	if result.Trace.Model != "gemini-2.5-pro" {
		t.Fatalf("expected profile model in trace, got %q", result.Trace.Model)
	}
}

func TestOrchestrator_FallsBackToDefaultsWhenProfileLLMConfigEmpty(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetModel("global-default-model")
	orch.SetAgentResolver(fakeAgentResolver{agent: RuntimeAgentConfig{
		AgentID:     "billing_agent",
		ProfileID:   "axis.ops.billing.v1",
		Status:      "active",
		MaxAutonomy: AutonomyA2,
	}})
	orch.SetAgentProfileResolver(fakeAgentProfileResolver{profile: RuntimeAgentProfileConfig{
		ProfileID:    "axis.ops.billing.v1",
		VersionLabel: "v1",
		SystemPrompt: "Explain billing only.",
		MaxAutonomy:  AutonomyA1,
		Enabled:      true,
		SnapshotID:   "profile-row-1",
		// LLM left zero: runtime must use its own defaults.
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", AgentID: "billing_agent", Message: "explicá cuotas",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.requests[0].MaxTokens; got != defaultMaxTokens {
		t.Fatalf("expected default max_tokens %d in request, got %d", defaultMaxTokens, got)
	}
	if result.Trace.Model != "global-default-model" {
		t.Fatalf("expected global model fallback in trace, got %q", result.Trace.Model)
	}
}

func TestOrchestrator_RejectsMissingAgentProfileResolver(t *testing.T) {
	t.Parallel()

	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "should not run"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetAgentResolver(fakeAgentResolver{agent: RuntimeAgentConfig{
		AgentID:   "billing_agent",
		ProfileID: "axis.ops.billing.v1",
		Status:    "active",
	}})

	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", AgentID: "billing_agent", Message: "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount != 0 {
		t.Fatalf("expected provider not called, got %d", provider.callCount)
	}
	if len(result.Trace.GuardrailEvents) == 0 || result.Trace.GuardrailEvents[0].Type != "agent_fleet" {
		t.Fatalf("expected agent guardrail, got %+v", result.Trace.GuardrailEvents)
	}
}

func TestApplyRuntimeAgentProfileRejectsArchived(t *testing.T) {
	t.Parallel()

	_, event := applyRuntimeAgentProfile(AgentRoute{}, RuntimeAgentProfileConfig{
		ProfileID:    "axis.ops.billing.v1",
		SystemPrompt: "billing",
		Enabled:      true,
		Archived:     true,
	})
	if event == nil || event.Type != "agent_profile" {
		t.Fatalf("expected archived profile guardrail, got %+v", event)
	}
}
