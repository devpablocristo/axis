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

	enrichToolName      = "rewrite_procedure"
	enrichPromptVersion = "enrich.v1"

	answerPromptVersion = "answer.v1"
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

// Enrich rewrites the wording of a distilled procedure to be clearer, without
// changing its meaning. It never decides anything and never invents steps the
// procedure does not imply — Companion re-validates the output and a human
// still accepts it. With Echo (no model) the structured output does not parse,
// so Enriched is false and the original text is returned unchanged (the safe
// default: Companion then keeps the deterministic distillation).
func (p *Planner) Enrich(ctx context.Context, req EnrichRequest) (EnrichResponse, error) {
	original := EnrichResponse{
		Title:         req.Title,
		Content:       req.Content,
		Enriched:      false,
		Model:         p.model,
		PromptVersion: enrichPromptVersion,
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" {
		return original, nil
	}
	// Defense in depth: the distilled text is our own structural output, but the
	// same injection guard as Propose keeps a poisoned distillation from steering
	// the rewrite.
	if looksAdversarial(req.Title) || looksAdversarial(req.Content) {
		return original, nil
	}

	resp, err := p.provider.Chat(ctx, ai.ChatRequest{
		SystemPrompt:   buildEnrichSystemPrompt(req),
		Messages:       []ai.Message{{Role: "user", Content: "Title: " + req.Title + "\n\nProcedure:\n" + req.Content}},
		Tools:          []ai.Tool{buildEnrichTool()},
		ResponseSchema: enrichSchema(),
		MaxTokens:      2048,
	})
	if err != nil {
		return EnrichResponse{}, err
	}

	title, content, ok := interpretEnrich(resp)
	if !ok {
		return original, nil
	}
	return EnrichResponse{
		Title:         title,
		Content:       content,
		Enriched:      true,
		Model:         p.model,
		PromptVersion: enrichPromptVersion,
	}, nil
}

// Answer processes the input data under the virployee's (system-prompt) role and
// returns an answer. It does not classify or decide governance — it is the
// "process and respond" path (read/explain, no external effects). When a
// ResponseSchema is provided the model is asked for a single JSON object and
// Answered is true only if the text parses as JSON; otherwise Answered reflects
// non-empty text. With Echo (no model) the canned text is not JSON, so a
// structured request returns Answered=false and Companion marks the run degraded.
func (p *Planner) Answer(ctx context.Context, req AnswerRequest) (AnswerResponse, error) {
	base := AnswerResponse{Answered: false, Model: p.model, PromptVersion: answerPromptVersion}
	input := strings.TrimSpace(string(req.InputJSON))
	if input == "" || input == "null" {
		return base, nil
	}
	// Defense in depth: do not feed obviously adversarial input to the model.
	if looksAdversarial(input) {
		return base, nil
	}

	chatReq := ai.ChatRequest{
		SystemPrompt: buildAnswerSystemPrompt(req),
		Messages:     []ai.Message{{Role: "user", Content: "Input JSON:\n" + input}},
		MaxTokens:    4096,
	}
	if len(req.ResponseSchema) > 0 {
		chatReq.ResponseSchema = req.ResponseSchema
	}
	resp, err := p.provider.Chat(ctx, chatReq)
	if err != nil {
		return AnswerResponse{}, err
	}

	text := stripCodeFences(resp.Text)
	base.OutputText = text
	if len(req.ResponseSchema) > 0 {
		if raw, ok := asJSONObject(text); ok {
			base.OutputJSON = raw
			base.Answered = true
		}
		return base, nil
	}
	base.Answered = text != ""
	return base, nil
}

// asJSONObject returns the text as a JSON object/array if it is valid structured
// JSON; a bare echo string (Echo provider) is not, so it degrades to (nil,false).
func asJSONObject(text string) (json.RawMessage, bool) {
	t := strings.TrimSpace(text)
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return nil, false
	}
	if !json.Valid([]byte(t)) {
		return nil, false
	}
	return json.RawMessage(t), true
}

func buildAnswerSystemPrompt(req AnswerRequest) string {
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
	b.WriteString("Read the input JSON and produce your answer based ONLY on what it contains; do not invent facts. ")
	if len(req.ResponseSchema) > 0 {
		b.WriteString("Respond with a SINGLE JSON object that conforms to the required schema, and nothing outside the JSON.")
	} else {
		b.WriteString("Respond concisely.")
	}
	return b.String()
}

func enrichSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "A concise, clear title for the procedure.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The rewritten procedure: clear, ordered steps. Same meaning, better wording.",
			},
		},
		"required": []string{"title", "content"},
	}
}

func buildEnrichTool() ai.Tool {
	return ai.Tool{
		Name:        enrichToolName,
		Description: "Return an improved title and step-by-step wording for a learned procedure. Do not change what it does.",
		Parameters:  enrichSchema(),
	}
}

func buildEnrichSystemPrompt(req EnrichRequest) string {
	var b strings.Builder
	b.WriteString("You improve the WORDING of a learned operating procedure so a supervisor can read it quickly. ")
	b.WriteString("Rewrite it as clear, ordered steps in the same language as the input. ")
	b.WriteString("Keep the exact meaning: do NOT add, remove, or reorder actions, and do NOT invent details. ")
	b.WriteString("The procedure is for the capability \"")
	b.WriteString(req.CapabilityKey)
	b.WriteString("\"; keep that capability_key referenced in the text. ")
	b.WriteString("Never include secrets, credentials, emails, or any personal data. ")
	b.WriteString("Call ")
	b.WriteString(enrichToolName)
	b.WriteString(" with the improved title and content.")
	return b.String()
}

func interpretEnrich(resp ai.ChatResponse) (title, content string, ok bool) {
	for _, call := range resp.ToolCalls {
		if call.Name != enrichToolName {
			continue
		}
		if t, c, valid := enrichFromArgs(call.Args); valid {
			return t, c, true
		}
	}
	if text := stripCodeFences(resp.Text); text != "" {
		if t, c, valid := enrichFromArgs([]byte(text)); valid {
			return t, c, true
		}
	}
	return "", "", false
}

func enrichFromArgs(raw []byte) (title, content string, ok bool) {
	var args struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", "", false
	}
	title = strings.TrimSpace(args.Title)
	content = strings.TrimSpace(args.Content)
	if title == "" || content == "" {
		return "", "", false
	}
	return title, content, true
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
