// Package planner turns a natural-language input into an intent proposal using
// an LLM provider. It only proposes which assigned capability the input maps
// to; the governance decision stays in Companion (Go). With the Echo provider
// (no API key) there are no tool calls, so the proposal is "no intent" — safe
// by default until a real model is configured.
package planner

import (
	"context"
	"encoding/json"
	"strings"

	ai "github.com/devpablocristo/platform/kernels/ai/go"
)

const proposeToolName = "propose_intent"

type Planner struct {
	provider ai.Provider
	model    string
}

func New(provider ai.Provider, model string) *Planner {
	return &Planner{provider: provider, model: model}
}

func (p *Planner) Propose(ctx context.Context, req ProposeRequest) (ProposeResponse, error) {
	if strings.TrimSpace(req.Input) == "" || len(req.Capabilities) == 0 {
		return ProposeResponse{Intent: ProposedIntent{Matched: false}, Model: p.model}, nil
	}

	resp, err := p.provider.Chat(ctx, ai.ChatRequest{
		SystemPrompt: buildSystemPrompt(req),
		Messages:     []ai.Message{{Role: "user", Content: req.Input}},
		Tools:        []ai.Tool{buildProposeTool(req.Capabilities)},
		MaxTokens:    512,
	})
	if err != nil {
		return ProposeResponse{}, err
	}

	return ProposeResponse{Intent: interpret(resp, req.Capabilities), Model: p.model}, nil
}

func buildProposeTool(capabilities []CapabilityInfo) ai.Tool {
	keys := make([]string, 0, len(capabilities)+1)
	for _, capability := range capabilities {
		keys = append(keys, capability.CapabilityKey)
	}
	keys = append(keys, "") // allow "no capability applies"
	return ai.Tool{
		Name:        proposeToolName,
		Description: "Classify the user's request into exactly one assigned capability, or empty string when none applies. Never invent a capability key.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"capability_key": map[string]any{
					"type":        "string",
					"enum":        keys,
					"description": "The assigned capability_key that best matches the request, or empty.",
				},
				"confidence": map[string]any{
					"type":        "number",
					"description": "Confidence between 0 and 1.",
				},
			},
			"required": []string{"capability_key"},
		},
	}
}

func buildSystemPrompt(req ProposeRequest) string {
	var b strings.Builder
	if s := strings.TrimSpace(req.SystemPrompt); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	if r := strings.TrimSpace(req.JobRole); r != "" {
		b.WriteString("Job role: ")
		b.WriteString(r)
		b.WriteString("\n\n")
	}
	b.WriteString("Classify the user's request into one of your assigned capabilities. ")
	b.WriteString("Call ")
	b.WriteString(proposeToolName)
	b.WriteString(" with the capability_key that best matches, or empty when none applies. Do not invent keys.\n\nAssigned capabilities:\n")
	for _, capability := range req.Capabilities {
		b.WriteString("- ")
		b.WriteString(capability.CapabilityKey)
		if capability.Name != "" {
			b.WriteString(" (")
			b.WriteString(capability.Name)
			b.WriteString(")")
		}
		if capability.Description != "" {
			b.WriteString(": ")
			b.WriteString(capability.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func interpret(resp ai.ChatResponse, capabilities []CapabilityInfo) ProposedIntent {
	assigned := make(map[string]CapabilityInfo, len(capabilities))
	for _, capability := range capabilities {
		assigned[capability.CapabilityKey] = capability
	}
	for _, call := range resp.ToolCalls {
		if call.Name != proposeToolName {
			continue
		}
		var args struct {
			CapabilityKey string  `json:"capability_key"`
			Confidence    float64 `json:"confidence"`
		}
		if err := json.Unmarshal(call.Args, &args); err != nil {
			continue
		}
		key := strings.TrimSpace(args.CapabilityKey)
		capability, ok := assigned[key]
		if key == "" || !ok {
			// Empty or a key the virployee does not have assigned: not matched.
			// Companion re-checks assignment too; this is defense in depth.
			continue
		}
		domain, resource, action := splitKey(key)
		confidence := args.Confidence
		if confidence <= 0 || confidence > 1 {
			confidence = 0.8
		}
		return ProposedIntent{
			Matched:          true,
			CapabilityKey:    key,
			Domain:           domain,
			Resource:         resource,
			Action:           action,
			RequiredAutonomy: capability.RequiredAutonomy,
			Confidence:       confidence,
		}
	}
	return ProposedIntent{Matched: false}
}

func splitKey(key string) (string, string, string) {
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}
