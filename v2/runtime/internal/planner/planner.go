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

const (
	proposeToolName = "propose_intent"
	promptVersion   = "propose.v2"
)

type Planner struct {
	provider ai.Provider
	model    string
}

func New(provider ai.Provider, model string) *Planner {
	return &Planner{provider: provider, model: model}
}

func (p *Planner) Propose(ctx context.Context, req ProposeRequest) (ProposeResponse, error) {
	noIntent := ProposeResponse{Intent: ProposedIntent{Matched: false}, Model: p.model, PromptVersion: promptVersion}
	if strings.TrimSpace(req.Input) == "" || len(req.Capabilities) == 0 {
		return noIntent, nil
	}
	// Prompt-injection defense: do not feed obviously adversarial input to the
	// model. Defense in depth — the model only has the constrained
	// propose_intent tool and Companion re-checks assignment and governance.
	if looksAdversarial(req.Input) {
		return noIntent, nil
	}

	// Provide both structured-output mechanisms so the same call works across
	// providers: Anthropic uses the tool (tool_use), Gemini/Vertex uses the
	// ResponseSchema (JSON in the text). Echo uses neither → no intent.
	resp, err := p.provider.Chat(ctx, ai.ChatRequest{
		SystemPrompt:   buildSystemPrompt(req),
		Messages:       []ai.Message{{Role: "user", Content: req.Input}},
		Tools:          []ai.Tool{buildProposeTool(req.Capabilities)},
		ResponseSchema: proposeSchema(req.Capabilities),
		MaxTokens:      512,
	})
	if err != nil {
		return ProposeResponse{}, err
	}

	return ProposeResponse{Intent: interpret(resp, req.Capabilities), Model: p.model, PromptVersion: promptVersion}, nil
}

// looksAdversarial flags obvious prompt-injection attempts in the user input.
func looksAdversarial(input string) bool {
	lower := strings.ToLower(input)
	markers := []string{
		"ignore previous", "ignore all previous", "disregard previous",
		"ignore the above", "system prompt", "you are now", "new instructions",
		"ignora las instrucciones", "ignora todas las instrucciones",
		"olvida las instrucciones", "olvida todo", "override your", "jailbreak",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// proposeSchema is the structured-output contract for the proposal, used both
// as the Anthropic tool input schema and as the Gemini/Vertex ResponseSchema.
// capability_key is constrained to the assigned keys (plus empty) so the model
// cannot propose a capability the virployee does not have.
func proposeSchema(capabilities []CapabilityInfo) map[string]any {
	keys := make([]string, 0, len(capabilities)+1)
	for _, capability := range capabilities {
		keys = append(keys, capability.CapabilityKey)
	}
	keys = append(keys, "") // allow "no capability applies"
	return map[string]any{
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
	}
}

func buildProposeTool(capabilities []CapabilityInfo) ai.Tool {
	return ai.Tool{
		Name:        proposeToolName,
		Description: "Classify the user's request into exactly one assigned capability, or empty string when none applies. Never invent a capability key.",
		Parameters:  proposeSchema(capabilities),
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
	b.WriteString(" with the capability_key that best matches. ")
	b.WriteString("When the user asks for an operational task (create, change, find or look something up), pick the capability that best covers it even if the phrasing is loose, indirect, or includes extra conditions to resolve later; lower the confidence instead of refusing. ")
	b.WriteString("Use an empty capability_key only for greetings, small talk, or requests unrelated to every assigned capability. Never invent a key.\n\nAssigned capabilities:\n")
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
	// Anthropic path: a tool_use call.
	for _, call := range resp.ToolCalls {
		if call.Name != proposeToolName {
			continue
		}
		if intent, ok := intentFromArgs(call.Args, assigned); ok {
			return intent
		}
	}
	// Gemini/Vertex path: structured JSON returned as text via ResponseSchema.
	if text := stripCodeFences(resp.Text); text != "" {
		if intent, ok := intentFromArgs([]byte(text), assigned); ok {
			return intent
		}
	}
	return ProposedIntent{Matched: false}
}

// intentFromArgs validates a structured proposal and maps it to an intent.
// An empty or unassigned capability_key yields (,false): the model cannot
// propose a capability the virployee does not have (Companion re-checks too).
func intentFromArgs(raw []byte, assigned map[string]CapabilityInfo) (ProposedIntent, bool) {
	var args struct {
		CapabilityKey string  `json:"capability_key"`
		Confidence    float64 `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ProposedIntent{}, false
	}
	key := strings.TrimSpace(args.CapabilityKey)
	capability, ok := assigned[key]
	if key == "" || !ok {
		return ProposedIntent{}, false
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
	}, true
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func splitKey(key string) (string, string, string) {
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}
