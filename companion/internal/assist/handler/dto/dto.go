package dto

type AssistPackRequest struct {
	OwnerSystem    string         `json:"owner_system"`
	ProductSurface string         `json:"product_surface"`
	AssistType     string         `json:"assist_type"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	InputContract  string         `json:"input_contract"`
	OutputContract string         `json:"output_contract"`
	PromptTemplate string         `json:"prompt_template"`
	ModelPolicy    map[string]any `json:"model_policy"`
	Enabled        *bool          `json:"enabled,omitempty"`
}

type AssistRunRequest struct {
	OwnerSystem    string         `json:"owner_system"`
	ProductSurface string         `json:"product_surface"`
	AssistType     string         `json:"assist_type"`
	SubjectType    string         `json:"subject_type"`
	SubjectID      string         `json:"subject_id"`
	Input          map[string]any `json:"input"`
}

type AssistPackResponse struct {
	ID             string         `json:"id"`
	OrgID          string         `json:"org_id"`
	OwnerSystem    string         `json:"owner_system"`
	ProductSurface string         `json:"product_surface"`
	AssistType     string         `json:"assist_type"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	InputContract  string         `json:"input_contract"`
	OutputContract string         `json:"output_contract"`
	PromptTemplate string         `json:"prompt_template"`
	ModelPolicy    map[string]any `json:"model_policy"`
	Enabled        bool           `json:"enabled"`
	ArchivedAt     string         `json:"archived_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type AssistRunResponse struct {
	ID             string         `json:"id"`
	OrgID          string         `json:"org_id"`
	PackID         string         `json:"pack_id"`
	OwnerSystem    string         `json:"owner_system"`
	ProductSurface string         `json:"product_surface"`
	AssistType     string         `json:"assist_type"`
	SubjectType    string         `json:"subject_type"`
	SubjectID      string         `json:"subject_id"`
	Input          map[string]any `json:"input"`
	Output         map[string]any `json:"output"`
	Status         string         `json:"status"`
	ErrorMessage   string         `json:"error_message"`
	CreatedAt      string         `json:"created_at"`
	CompletedAt    string         `json:"completed_at,omitempty"`
}
