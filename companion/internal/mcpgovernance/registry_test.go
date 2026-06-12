package mcpgovernance

import (
	"errors"
	"testing"

	"github.com/devpablocristo/companion/internal/capabilities"
)

func TestDefaultRegistryValidatesGovernedTools(t *testing.T) {
	reg, err := NewDefaultRegistry()
	if err != nil {
		t.Fatalf("NewDefaultRegistry() err = %v", err)
	}
	for _, name := range []string{
		"axis.products.list",
		"axis.capabilities.import",
		"axis.ops.console",
		"axis.evals.run",
		"axis.tasks.create",
	} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("expected default tool %s", name)
		}
		if !contains(tool.RequiredScopes, ScopeMCPExecute) {
			t.Fatalf("%s does not require %s", name, ScopeMCPExecute)
		}
		if tool.NexusActionType == "" {
			t.Fatalf("%s does not declare nexus action type", name)
		}
	}
}

func TestRegistryRejectsSideEffectToolWithoutApproval(t *testing.T) {
	_, err := NewRegistry([]ToolDefinition{
		{
			Name:            "axis.tasks.create",
			RequiredScopes:  []string{ScopeMCPExecute, "companion:tasks:write"},
			RiskLevel:       capabilities.RiskHigh,
			SideEffectType:  capabilities.SideEffectWrite,
			NexusActionType: "agent.capability.invoke",
		},
	})
	if !errors.Is(err, ErrInvalidToolDefinition) {
		t.Fatalf("expected ErrInvalidToolDefinition, got %v", err)
	}
}

func TestRegistryRejectsToolWithoutMCPExecuteScope(t *testing.T) {
	_, err := NewRegistry([]ToolDefinition{
		{
			Name:            "axis.products.list",
			RequiredScopes:  []string{"companion:products:read"},
			RiskLevel:       capabilities.RiskLow,
			SideEffectType:  capabilities.SideEffectRead,
			NexusActionType: "agent.capability.invoke",
		},
	})
	if !errors.Is(err, ErrInvalidToolDefinition) {
		t.Fatalf("expected ErrInvalidToolDefinition, got %v", err)
	}
}
