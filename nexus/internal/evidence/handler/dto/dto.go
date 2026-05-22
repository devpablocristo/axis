package dto

// EvidencePackResponse es el DTO HTTP del evidence pack completo.
type EvidencePackResponse struct {
	Version     string              `json:"version"`
	GeneratedAt string              `json:"generated_at"`
	Request     RequestSection      `json:"request"`
	PolicyEval  PolicySection       `json:"policy_evaluation"`
	Approval    *ApprovalSection    `json:"approval,omitempty"`
	Execution   *ExecutionSection   `json:"execution,omitempty"`
	Attestation *AttestationSection `json:"attestation,omitempty"`
	Timeline    []TimelineEntry     `json:"timeline"`
	Signature   SignatureSection    `json:"signature"`
}

// RequestSection identifica la request original.
type RequestSection struct {
	ID            string         `json:"id"`
	OrgID         string         `json:"org_id,omitempty"`
	Requester     RequesterInfo  `json:"requester"`
	Action        ActionInfo     `json:"action"`
	ActionBinding map[string]any `json:"action_binding,omitempty"`
	BindingHash   string         `json:"binding_hash,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Context       string         `json:"context,omitempty"`
	AISummary     string         `json:"ai_summary,omitempty"`
	CreatedAt     string         `json:"created_at"`
}

// RequesterInfo identifica al solicitante.
type RequesterInfo struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ActionInfo identifica la acción solicitada.
type ActionInfo struct {
	Type           string `json:"type"`
	TargetSystem   string `json:"target_system,omitempty"`
	TargetResource string `json:"target_resource,omitempty"`
}

// PolicySection contiene el resultado de la evaluación.
type PolicySection struct {
	RiskLevel      string  `json:"risk_level"`
	Decision       string  `json:"decision"`
	DecisionReason string  `json:"decision_reason"`
	PolicyID       *string `json:"policy_id,omitempty"`
	EvaluatedAt    string  `json:"evaluated_at,omitempty"`
}

// ApprovalSection contiene la cadena de aprobación completa.
type ApprovalSection struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	BreakGlass        bool            `json:"break_glass"`
	RequiredApprovals int             `json:"required_approvals"`
	Decisions         []DecisionEntry `json:"decisions"`
	FinalDecidedBy    string          `json:"final_decided_by,omitempty"`
	DecidedAt         string          `json:"decided_at,omitempty"`
}

// DecisionEntry registra una decisión individual.
type DecisionEntry struct {
	ApproverID string `json:"approver_id"`
	Action     string `json:"action"`
	Note       string `json:"note,omitempty"`
	DecidedAt  string `json:"decided_at"`
}

// ExecutionSection contiene el resultado de ejecución.
type ExecutionSection struct {
	Status     string         `json:"status"`
	Result     map[string]any `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	ExecutedAt string         `json:"executed_at,omitempty"`
}

// AttestationSection contiene la prueba verificable del executor.
type AttestationSection struct {
	ID                string         `json:"id"`
	Status            string         `json:"status"`
	ProviderRefs      map[string]any `json:"provider_refs,omitempty"`
	Signature         string         `json:"signature"`
	Attester          string         `json:"attester"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         string         `json:"created_at"`
	Verified          bool           `json:"verified"`
	VerificationError string         `json:"verification_error,omitempty"`
}

// TimelineEntry es un evento en la línea de tiempo.
type TimelineEntry struct {
	Event   string         `json:"event"`
	Actor   string         `json:"actor"`
	At      string         `json:"at"`
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data,omitempty"`
}

// SignatureSection contiene la firma criptográfica.
type SignatureSection struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	SignedAt  string `json:"signed_at"`
	Value     string `json:"value"`
}
