package planner

import "encoding/json"

// ProposeRequest is what Companion sends: the natural-language input plus the
// runtime context needed to classify it — the assigned capabilities, the
// system prompt/job role, and content that Companion has curated, approved,
// scoped to the virployee, and selected for this request.
type ProposeRequest struct {
	Input        string           `json:"input"`
	SystemPrompt string           `json:"system_prompt,omitempty"`
	JobRole      string           `json:"job_role,omitempty"`
	Capabilities []CapabilityInfo `json:"capabilities"`
	Memory       []MemoryRef      `json:"memory,omitempty"`
}

type CapabilityInfo struct {
	CapabilityKey    string `json:"capability_key"`
	Name             string `json:"name,omitempty"`
	Description      string `json:"description,omitempty"`
	RequiredAutonomy string `json:"required_autonomy,omitempty"`
	RiskClass        string `json:"risk_class,omitempty"`
	SideEffectClass  string `json:"side_effect_class,omitempty"`
}

type MemoryRef struct {
	Title   string `json:"title,omitempty"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content,omitempty"`
}

// ProposeResponse is the proposal: which capability the input maps to (if any).
// Go (Companion) decides on it; the planner never decides.
type ProposeResponse struct {
	Intent        ProposedIntent `json:"intent"`
	Model         string         `json:"model,omitempty"`
	PromptVersion string         `json:"prompt_version,omitempty"`
	Usage         Usage          `json:"usage"`
}

type ProposedIntent struct {
	Matched          bool    `json:"matched"`
	CapabilityKey    string  `json:"capability_key,omitempty"`
	Domain           string  `json:"domain,omitempty"`
	Resource         string  `json:"resource,omitempty"`
	Action           string  `json:"action,omitempty"`
	RequiredAutonomy string  `json:"required_autonomy,omitempty"`
	Confidence       float64 `json:"confidence,omitempty"`
}

// AnswerRequest is what Companion sends when a virployee must PROCESS input data
// and RESPOND (e.g. a product like medmory sends structured facts and expects a
// governed answer). Unlike Propose, this does not classify into a capability: the
// system prompt bounds the role and the model answers directly. InputJSON is the
// product's opaque payload; ResponseSchema, when set, forces a structured JSON
// answer that must conform to it.
type AnswerRequest struct {
	SystemPrompt   string          `json:"system_prompt,omitempty"`
	JobRole        string          `json:"job_role,omitempty"`
	InputJSON      json.RawMessage `json:"input_json"`
	ResponseSchema map[string]any  `json:"response_schema,omitempty"`
	ContentParts   []ContentPart   `json:"content_parts,omitempty"`
}

type ContentPart struct {
	Kind       string          `json:"kind"`
	Text       string          `json:"text,omitempty"`
	Data       []byte          `json:"data,omitempty"`
	URI        string          `json:"uri,omitempty"`
	MIMEType   string          `json:"mime_type,omitempty"`
	Name       string          `json:"name,omitempty"`
	SHA256     string          `json:"sha256,omitempty"`
	DocumentID string          `json:"document_id,omitempty"`
	Locator    json.RawMessage `json:"locator,omitempty"`
}

// AnswerResponse carries the model's answer. Answered is true only when the model
// produced a usable answer: a parseable JSON object when a ResponseSchema was
// requested, or non-empty text otherwise. With Echo (no credentials) the canned
// text is not valid JSON, so a structured request degrades cleanly to
// Answered=false and Companion can flag the run as degraded.
type AnswerResponse struct {
	OutputText    string          `json:"output_text,omitempty"`
	OutputJSON    json.RawMessage `json:"output_json,omitempty"`
	Answered      bool            `json:"answered"`
	Model         string          `json:"model,omitempty"`
	PromptVersion string          `json:"prompt_version,omitempty"`
	Usage         Usage           `json:"usage"`
}

// EnrichRequest is what Companion sends to improve the WORDING of a distilled
// procedure before it is filed as a proposal. It carries only the structural,
// already-PII-free distilled text plus the capability_key and job role — never
// draft values or memory content.
type EnrichRequest struct {
	CapabilityKey string `json:"capability_key"`
	Title         string `json:"title"`
	Content       string `json:"content"`
}

// EnrichResponse returns the rewritten text. Enriched is true only when the
// model actually produced a usable rewrite; with Echo (no credentials) it is
// false and Companion keeps the deterministic distillation.
type EnrichResponse struct {
	Title         string `json:"title"`
	Content       string `json:"content"`
	Enriched      bool   `json:"enriched"`
	Model         string `json:"model,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
	Usage         Usage  `json:"usage"`
}

// Usage is deliberately marked estimated until each provider adapter exposes
// authoritative billing metadata. Runtime still reports a consistent token and
// cost envelope so Companion can enforce budgets before and account after calls.
type Usage struct {
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
	EstimatedCostMicroUSD int64 `json:"estimated_cost_microusd"`
	Estimated             bool  `json:"estimated"`
}
