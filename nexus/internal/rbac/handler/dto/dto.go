package dto

type GrantAssignmentRequest struct {
	OrgID  string `json:"org_id"`
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type AssignmentResponse struct {
	ID        string  `json:"id"`
	OrgID     string  `json:"org_id"`
	UserID    string  `json:"user_id"`
	Role      string  `json:"role"`
	GrantedBy string  `json:"granted_by,omitempty"`
	GrantedAt string  `json:"granted_at"`
	RevokedAt *string `json:"revoked_at,omitempty"`
}

type CheckResponse struct {
	OrgID   string `json:"org_id"`
	UserID  string `json:"user_id"`
	Role    string `json:"role"`
	Granted bool   `json:"granted"`
}
