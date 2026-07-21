package planner

// ProposeRequest is what Companion sends: the natural-language input plus the
// runtime context needed to classify it — the assigned capabilities, the
// system prompt/job role, and safe memory references (no content).
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
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
}

// ProposeResponse is the proposal: which capability the input maps to (if any).
// Go (Companion) decides on it; the planner never decides.
type ProposeResponse struct {
	Intent        ProposedIntent `json:"intent"`
	Model         string         `json:"model,omitempty"`
	PromptVersion string         `json:"prompt_version,omitempty"`
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
}
