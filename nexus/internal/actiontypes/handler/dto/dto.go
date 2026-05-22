package dto

type CreateActionTypeRequest struct {
	OrgID              *string        `json:"org_id,omitempty"`
	Name               string         `json:"name"`
	Description        string         `json:"description,omitempty"`
	Category           string         `json:"category,omitempty"`
	RiskClass          string         `json:"risk_class,omitempty"`
	Schema             map[string]any `json:"schema,omitempty"`
	Reversible         bool           `json:"reversible"`
	RequiresBreakGlass bool           `json:"requires_break_glass"`
}

type UpdateActionTypeRequest struct {
	OrgID              *string         `json:"org_id,omitempty"`
	Name               *string         `json:"name,omitempty"`
	Description        *string         `json:"description,omitempty"`
	Category           *string         `json:"category,omitempty"`
	RiskClass          *string         `json:"risk_class,omitempty"`
	Schema             *map[string]any `json:"schema,omitempty"`
	Reversible         *bool           `json:"reversible,omitempty"`
	RequiresBreakGlass *bool           `json:"requires_break_glass,omitempty"`
	Enabled            *bool           `json:"enabled,omitempty"`
}

type ActionTypeResponse struct {
	ID                 string         `json:"id"`
	OrgID              string         `json:"org_id,omitempty"`
	Name               string         `json:"name"`
	Description        string         `json:"description,omitempty"`
	Category           string         `json:"category,omitempty"`
	RiskClass          string         `json:"risk_class"`
	Schema             map[string]any `json:"schema,omitempty"`
	Reversible         bool           `json:"reversible"`
	RequiresBreakGlass bool           `json:"requires_break_glass"`
	Enabled            bool           `json:"enabled"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
}
