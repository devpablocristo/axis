package mcpgovernance

import (
	"context"
	"errors"
	"testing"

	"github.com/devpablocristo/companion/internal/nexusclient"
)

type fakeNexus struct {
	calls          int
	idempotencyKey string
	body           nexusclient.SubmitRequestBody
	response       nexusclient.SubmitResponse
	err            error
}

func (f *fakeNexus) SubmitRequest(_ context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
	f.calls++
	f.idempotencyKey = idempotencyKey
	f.body = body
	return f.response, f.err
}

func TestGatewaySubmitsReadToolToNexusAndAllowsExecution(t *testing.T) {
	reg, err := NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	nexus := &fakeNexus{response: nexusclient.SubmitResponse{
		RequestID:      "req-1",
		Decision:       nexusclient.DecisionAllow,
		Status:         nexusclient.StatusAllowed,
		RiskLevel:      "low",
		DecisionReason: "policy",
	}}
	gateway := NewGateway(reg, nexus)

	decision, err := gateway.Authorize(context.Background(), DecisionInput{
		ToolName:       "axis.products.list",
		IdempotencyKey: "idem-1",
		Context: InvocationContext{
			OrgID:          "org-a",
			ProductSurface: "companion",
			ActorID:        "agent-a",
			Scopes:         []string{ScopeMCPExecute, "companion:products:read"},
		},
		Arguments: map[string]any{"api_key": "secret", "limit": 20},
	})
	if err != nil {
		t.Fatalf("Authorize() err = %v", err)
	}
	if !decision.CanExecute || decision.PendingApproval || decision.Denied {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if nexus.calls != 1 {
		t.Fatalf("expected one nexus call, got %d", nexus.calls)
	}
	if nexus.idempotencyKey != "idem-1" {
		t.Fatalf("idempotency key = %q", nexus.idempotencyKey)
	}
	if nexus.body.ActionType != nexusclient.ActionTypeAgentCapabilityInvoke {
		t.Fatalf("action type = %q", nexus.body.ActionType)
	}
	if nexus.body.TargetSystem != TargetSystemAxisMCP || nexus.body.TargetResource != "axis.products.list" {
		t.Fatalf("target = %s/%s", nexus.body.TargetSystem, nexus.body.TargetResource)
	}
	if got := nexus.body.Params["org_id"]; got != "org-a" {
		t.Fatalf("org_id param = %v", got)
	}
	for _, key := range []string{
		"schema_version",
		"org_id",
		"actor_id",
		"actor_type",
		"product_surface",
		"run_id",
		"tool_invocation_id",
		"capability_id",
		"operation",
		"target_system",
		"target_resource",
		"payload_hash",
		"idempotency_key",
	} {
		if got := nexus.body.ActionBinding[key]; got == nil || got == "" {
			t.Fatalf("expected action_binding.%s", key)
		}
	}
	if got := nexus.body.ActionBinding["idempotency_key"]; got != "idem-1" {
		t.Fatalf("action_binding.idempotency_key = %v", got)
	}
	payload := nexus.body.Params["payload"].(map[string]any)
	if payload["api_key"] != "[redacted]" {
		t.Fatalf("expected redacted api_key, got %v", payload["api_key"])
	}
}

func TestGatewayRequiresScopesBeforeNexus(t *testing.T) {
	reg, err := NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	nexus := &fakeNexus{}
	gateway := NewGateway(reg, nexus)

	_, err = gateway.Authorize(context.Background(), DecisionInput{
		ToolName: "axis.products.list",
		Context: InvocationContext{
			OrgID:          "org-a",
			ProductSurface: "companion",
			ActorID:        "agent-a",
			Scopes:         []string{ScopeMCPExecute},
		},
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if nexus.calls != 0 {
		t.Fatalf("expected no nexus call, got %d", nexus.calls)
	}
}

func TestGatewayReturnsPendingApprovalForApprovalRequiredTool(t *testing.T) {
	reg, err := NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	nexus := &fakeNexus{response: nexusclient.SubmitResponse{
		RequestID: "req-2",
		Decision:  nexusclient.DecisionRequireApproval,
		Status:    nexusclient.StatusPendingApproval,
		RiskLevel: "high",
	}}
	gateway := NewGateway(reg, nexus)

	decision, err := gateway.Authorize(context.Background(), DecisionInput{
		ToolName: "axis.tasks.create",
		Context: InvocationContext{
			OrgID:          "org-a",
			ProductSurface: "companion",
			ActorID:        "agent-a",
			Scopes:         []string{ScopeMCPExecute, "companion:tasks:write"},
		},
	})
	if err != nil {
		t.Fatalf("Authorize() err = %v", err)
	}
	if !decision.PendingApproval || decision.CanExecute || decision.Denied {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestGatewayFailsClosedWhenApprovalRequiredToolIsAllowedByNexus(t *testing.T) {
	reg, err := NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	nexus := &fakeNexus{response: nexusclient.SubmitResponse{
		RequestID: "req-3",
		Decision:  nexusclient.DecisionAllow,
		Status:    nexusclient.StatusAllowed,
		RiskLevel: "low",
	}}
	gateway := NewGateway(reg, nexus)

	_, err = gateway.Authorize(context.Background(), DecisionInput{
		ToolName: "axis.tasks.create",
		Context: InvocationContext{
			OrgID:          "org-a",
			ProductSurface: "companion",
			ActorID:        "agent-a",
			Scopes:         []string{ScopeMCPExecute, "companion:tasks:write"},
		},
	})
	if !errors.Is(err, ErrApprovalPolicyRequired) {
		t.Fatalf("expected ErrApprovalPolicyRequired, got %v", err)
	}
}

func TestGatewayRejectsUnsupportedNexusStatus(t *testing.T) {
	reg, err := NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	nexus := &fakeNexus{response: nexusclient.SubmitResponse{
		RequestID: "req-4",
		Status:    "weird",
	}}
	gateway := NewGateway(reg, nexus)

	_, err = gateway.Authorize(context.Background(), DecisionInput{
		ToolName: "axis.products.list",
		Context: InvocationContext{
			OrgID:          "org-a",
			ProductSurface: "companion",
			ActorID:        "agent-a",
			Scopes:         []string{ScopeMCPExecute, "companion:products:read"},
		},
	})
	if !errors.Is(err, ErrNexusDecision) {
		t.Fatalf("expected ErrNexusDecision, got %v", err)
	}
}
