package dto

// ApprovalDecisionRequest representa el body de approve/reject.
type ApprovalDecisionRequest struct {
	DecidedBy string `json:"decided_by"`
	Note      string `json:"note,omitempty"`
}

// ApprovalDecisionDTO representa una decisión individual en break-glass.
type ApprovalDecisionDTO struct {
	ApproverID string `json:"approver_id"`
	Action     string `json:"action"`
	Note       string `json:"note,omitempty"`
	DecidedAt  string `json:"decided_at"`
}

// ApprovalResponse representa una approval en la respuesta HTTP.
type ApprovalResponse struct {
	ID                string                `json:"id"`
	OrgID             string                `json:"org_id,omitempty"`
	RequestID         string                `json:"request_id"`
	Status            string                `json:"status"`
	DecidedBy         string                `json:"decided_by,omitempty"`
	DecisionNote      string                `json:"decision_note,omitempty"`
	DecidedAt         *string               `json:"decided_at,omitempty"`
	ExpiresAt         string                `json:"expires_at"`
	CreatedAt         string                `json:"created_at"`
	BreakGlass        bool                  `json:"break_glass"`
	RequiredApprovals int                   `json:"required_approvals"`
	CurrentApprovals  int                   `json:"current_approvals"`
	Decisions         []ApprovalDecisionDTO `json:"decisions,omitempty"`
}
