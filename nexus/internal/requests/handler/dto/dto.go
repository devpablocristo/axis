package dto

type SubmitRequest struct {
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	RequesterType  string         `json:"requester_type"`
	RequesterID    string         `json:"requester_id"`
	RequesterName  string         `json:"requester_name,omitempty"`
	ActionType     string         `json:"action_type"`
	TargetSystem   string         `json:"target_system,omitempty"`
	TargetResource string         `json:"target_resource,omitempty"`
	ActionBinding  map[string]any `json:"action_binding,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Context        string         `json:"context,omitempty"`
}

type SubmitResponse struct {
	RequestID      string           `json:"request_id"`
	Decision       string           `json:"decision"`
	RiskLevel      string           `json:"risk_level"`
	DecisionReason string           `json:"decision_reason"`
	Status         string           `json:"status"`
	BindingHash    string           `json:"binding_hash,omitempty"`
	Approval       *ApprovalPayload `json:"approval,omitempty"`
	AISummary      string           `json:"ai_summary,omitempty"`
	AIDegraded     bool             `json:"ai_degraded,omitempty"`
}

type ApprovalPayload struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

// SimulateRequest es idéntico a SubmitRequest — mismos campos
type SimulateRequest = SubmitRequest

// SimulateResponse muestra qué habría hecho Nexus sin ejecutar nada
type SimulateResponse struct {
	Decision             string  `json:"decision"`
	RiskLevel            string  `json:"risk_level"`
	DecisionReason       string  `json:"decision_reason"`
	Status               string  `json:"status"`
	PolicyMatched        *string `json:"policy_matched,omitempty"`
	RiskAssessment       any     `json:"risk_assessment"`
	WouldRequireApproval bool    `json:"would_require_approval"`
	AISummary            string  `json:"ai_summary,omitempty"`
}

// ReplaySimulateRequest es el body para replay simulation
type ReplaySimulateRequest struct {
	Expression string `json:"expression"`
	Effect     string `json:"effect"`
	Limit      int    `json:"limit,omitempty"`
}

type ReportResultRequest struct {
	ResultID     string         `json:"result_id,omitempty"`
	Success      bool           `json:"success"`
	Result       map[string]any `json:"result,omitempty"`
	DurationMs   int64          `json:"duration_ms,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

// AttestRequest es el body para registrar una attestation.
type AttestRequest struct {
	Status       string         `json:"status"`
	ProviderRefs map[string]any `json:"provider_refs,omitempty"`
	Signature    string         `json:"signature"`
	Attester     string         `json:"attester"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// AttestResponse es la respuesta al registrar una attestation.
type AttestResponse struct {
	ID           string         `json:"id"`
	RequestID    string         `json:"request_id"`
	Status       string         `json:"status"`
	ProviderRefs map[string]any `json:"provider_refs,omitempty"`
	Signature    string         `json:"signature"`
	Attester     string         `json:"attester"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    string         `json:"created_at"`
	// Verified indica si la firma fue criptográficamente verificada.
	// Cuando es false, VerificationError explica por qué (e.g.
	// "verifier_not_configured"). Los callers (Companion u otros) deben
	// inspeccionar este flag antes de tratar la attestation como prueba.
	Verified          bool   `json:"verified"`
	VerificationError string `json:"verification_error,omitempty"`
}

// BatchSimulateRequest es el body para simulación batch.
type BatchSimulateRequest struct {
	Requests []SubmitRequest `json:"requests"`
}

// BatchSimulateResponse contiene resultados agregados del batch.
type BatchSimulateResponse struct {
	Total           int                 `json:"total"`
	Allowed         int                 `json:"allowed"`
	Denied          int                 `json:"denied"`
	RequireApproval int                 `json:"require_approval"`
	ByRisk          map[string]int      `json:"by_risk"`
	Results         []BatchSimulateItem `json:"results"`
}

// BatchSimulateItem es un resultado individual del batch.
type BatchSimulateItem struct {
	ActionType     string  `json:"action_type"`
	RequesterID    string  `json:"requester_id"`
	TargetSystem   string  `json:"target_system"`
	Decision       string  `json:"decision"`
	RiskLevel      string  `json:"risk_level"`
	DecisionReason string  `json:"decision_reason"`
	PolicyMatched  *string `json:"policy_matched,omitempty"`
}

// ApprovalSimulateRequest es el body para simular aprobación/rechazo.
type ApprovalSimulateRequest struct {
	RequestID  string `json:"request_id"`
	Action     string `json:"action"`
	ApproverID string `json:"approver_id"`
}

// ApprovalSimulateResponse muestra el estado resultante simulado.
type ApprovalSimulateResponse struct {
	CurrentStatus     string `json:"current_status"`
	SimulatedStatus   string `json:"simulated_status"`
	BreakGlass        bool   `json:"break_glass"`
	RequiredApprovals int    `json:"required_approvals"`
	CurrentApprovals  int    `json:"current_approvals"`
	AfterApprovals    int    `json:"after_approvals"`
	WouldFinalize     bool   `json:"would_finalize"`
	AlreadyDecided    bool   `json:"already_decided"`
	Reason            string `json:"reason"`
}

type RequestResponse struct {
	ID             string         `json:"id"`
	OrgID          string         `json:"org_id,omitempty"`
	ProductSurface string         `json:"product_surface,omitempty"`
	RequesterType  string         `json:"requester_type"`
	RequesterID    string         `json:"requester_id"`
	RequesterName  string         `json:"requester_name,omitempty"`
	ActionType     string         `json:"action_type"`
	TargetSystem   string         `json:"target_system,omitempty"`
	TargetResource string         `json:"target_resource,omitempty"`
	ActionBinding  map[string]any `json:"action_binding,omitempty"`
	BindingHash    string         `json:"binding_hash,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	RiskLevel      string         `json:"risk_level"`
	Decision       string         `json:"decision"`
	DecisionReason string         `json:"decision_reason,omitempty"`
	Status         string         `json:"status"`
	AISummary      string         `json:"ai_summary,omitempty"`
	AIDegraded     bool           `json:"ai_degraded,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}
