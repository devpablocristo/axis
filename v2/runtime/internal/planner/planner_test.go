package planner

import (
	"context"
	"encoding/json"
	"testing"

	ai "github.com/devpablocristo/platform/kernels/ai/go"
)

var testCapabilities = []CapabilityInfo{
	{CapabilityKey: "calendar.events.create", Name: "Create calendar events", RequiredAutonomy: "A2"},
}

func toolCallResponse(key string) ai.ChatResponse {
	args, _ := json.Marshal(map[string]any{"capability_key": key, "confidence": 0.9})
	return ai.ChatResponse{ToolCalls: []ai.ToolCall{{Name: proposeToolName, Args: args}}}
}

func TestInterpretMatchesAssignedCapability(t *testing.T) {
	intent := interpret(toolCallResponse("calendar.events.create"), testCapabilities)
	if !intent.Matched || intent.CapabilityKey != "calendar.events.create" {
		t.Fatalf("expected matched create, got %+v", intent)
	}
	if intent.Domain != "calendar" || intent.Resource != "events" || intent.Action != "create" {
		t.Fatalf("expected key segments split, got %+v", intent)
	}
	if intent.RequiredAutonomy != "A2" {
		t.Fatalf("expected required autonomy from assigned capability, got %q", intent.RequiredAutonomy)
	}
}

func TestInterpretRejectsUnassignedProposedKey(t *testing.T) {
	// Safety: the model may hallucinate or propose a capability the virployee
	// does not have assigned. The planner must not surface it as an intent.
	if interpret(toolCallResponse("calendar.events.delete"), testCapabilities).Matched {
		t.Fatal("expected not matched for an unassigned proposed key")
	}
}

func TestInterpretEmptyKeyIsNotMatched(t *testing.T) {
	if interpret(toolCallResponse(""), testCapabilities).Matched {
		t.Fatal("empty capability_key must not match")
	}
}

func TestInterpretNoToolCallIsNotMatched(t *testing.T) {
	if interpret(ai.ChatResponse{Text: "hello"}, testCapabilities).Matched {
		t.Fatal("a response without a tool call must not match")
	}
}

func TestProposeWithEchoReturnsNoIntent(t *testing.T) {
	// The Echo provider (no API key) never calls a tool, so the proposal is
	// "no intent" — the safe default until a real model is configured.
	p := New(ai.NewEcho(), "")
	resp, err := p.Propose(context.Background(), ProposeRequest{
		Input:        "agendá una reunión mañana",
		Capabilities: testCapabilities,
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if resp.Intent.Matched {
		t.Fatalf("echo must not match, got %+v", resp.Intent)
	}
}
