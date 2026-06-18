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
