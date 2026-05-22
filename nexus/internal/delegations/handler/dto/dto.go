package dto

type CreateDelegationRequest struct {
	OwnerID            string   `json:"owner_id"`
	OwnerType          string   `json:"owner_type,omitempty"`
	AgentID            string   `json:"agent_id"`
	AgentType          string   `json:"agent_type,omitempty"`
	AllowedActionTypes []string `json:"allowed_action_types,omitempty"`
	AllowedResources   []string `json:"allowed_resources,omitempty"`
	Purpose            string   `json:"purpose,omitempty"`
	MaxRiskClass       string   `json:"max_risk_class,omitempty"`
	ExpiresAt          *string  `json:"expires_at,omitempty"`
}

type UpdateDelegationRequest struct {
	AllowedActionTypes *[]string `json:"allowed_action_types,omitempty"`
	AllowedResources   *[]string `json:"allowed_resources,omitempty"`
	Purpose            *string   `json:"purpose,omitempty"`
	MaxRiskClass       *string   `json:"max_risk_class,omitempty"`
	ExpiresAt          *string   `json:"expires_at,omitempty"`
	Enabled            *bool     `json:"enabled,omitempty"`
}

type DelegationResponse struct {
	ID                 string   `json:"id"`
	OrgID              string   `json:"org_id,omitempty"`
	OwnerID            string   `json:"owner_id"`
	OwnerType          string   `json:"owner_type"`
	AgentID            string   `json:"agent_id"`
	AgentType          string   `json:"agent_type"`
	AllowedActionTypes []string `json:"allowed_action_types"`
	AllowedResources   []string `json:"allowed_resources"`
	Purpose            string   `json:"purpose,omitempty"`
	MaxRiskClass       string   `json:"max_risk_class"`
	ExpiresAt          *string  `json:"expires_at,omitempty"`
	Enabled            bool     `json:"enabled"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}
