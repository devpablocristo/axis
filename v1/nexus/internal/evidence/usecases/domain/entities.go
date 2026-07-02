package domain

import "time"

// EvidencePack es el documento completo de evidencia verificable para una request.
type EvidencePack struct {
	Version     string               `json:"version"`
	GeneratedAt time.Time            `json:"generated_at"`
	Request     RequestEvidence      `json:"request"`
	PolicyEval  PolicyEvidence       `json:"policy_evaluation"`
	Approval    *ApprovalEvidence    `json:"approval,omitempty"`
	Execution   *ExecutionEvidence   `json:"execution,omitempty"`
	Attestation *AttestationEvidence `json:"attestation,omitempty"`
	Timeline    []TimelineEvent      `json:"timeline"`
	Signature   Signature            `json:"signature"`
}

// RequestEvidence contiene la información del request original.
type RequestEvidence struct {
	ID            string         `json:"id"`
	OrgID         string         `json:"org_id,omitempty"`
	Requester     Requester      `json:"requester"`
	Action        Action         `json:"action"`
	ActionBinding map[string]any `json:"action_binding,omitempty"`
	BindingHash   string         `json:"binding_hash,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Context       string         `json:"context,omitempty"`
	AISummary     string         `json:"ai_summary,omitempty"`
	CreatedAt     string         `json:"created_at"`
}

// Requester identifica al solicitante.
type Requester struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Action identifica la acción solicitada.
type Action struct {
	Type           string `json:"type"`
	TargetSystem   string `json:"target_system,omitempty"`
	TargetResource string `json:"target_resource,omitempty"`
}

// PolicyEvidence contiene el resultado de la evaluación de policies.
type PolicyEvidence struct {
	RiskLevel      string  `json:"risk_level"`
	Decision       string  `json:"decision"`
	DecisionReason string  `json:"decision_reason"`
	PolicyID       *string `json:"policy_id,omitempty"`
	EvaluatedAt    string  `json:"evaluated_at,omitempty"`
}

// ApprovalEvidence contiene la cadena de aprobación completa.
type ApprovalEvidence struct {
	ID                string             `json:"id"`
	Status            string             `json:"status"`
	BreakGlass        bool               `json:"break_glass"`
	RequiredApprovals int                `json:"required_approvals"`
	Decisions         []ApprovalDecision `json:"decisions"`
	FinalDecidedBy    string             `json:"final_decided_by,omitempty"`
	DecidedAt         string             `json:"decided_at,omitempty"`
}

// ApprovalDecision registra una decisión individual en la cadena.
type ApprovalDecision struct {
	ApproverID string `json:"approver_id"`
	Action     string `json:"action"`
	Note       string `json:"note,omitempty"`
	DecidedAt  string `json:"decided_at"`
}

// ExecutionEvidence contiene el resultado de la ejecución.
type ExecutionEvidence struct {
	Status     string         `json:"status"`
	Result     map[string]any `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	ExecutedAt string         `json:"executed_at,omitempty"`
}

// AttestationEvidence contiene la prueba verificable del executor.
type AttestationEvidence struct {
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

// TimelineEvent es un evento en la línea de tiempo de la request.
type TimelineEvent struct {
	Event   string         `json:"event"`
	Actor   string         `json:"actor"`
	At      string         `json:"at"`
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data,omitempty"`
}

// Signature contiene la firma criptográfica del evidence pack.
type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	SignedAt  string `json:"signed_at"`
	Value     string `json:"value"`
}
