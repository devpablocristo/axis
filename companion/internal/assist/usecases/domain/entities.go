package domain

import (
	"time"

	"github.com/google/uuid"
)

type AssistPack struct {
	ID             uuid.UUID      `json:"id"`
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
	ArchivedAt     *time.Time     `json:"archived_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type AssistRun struct {
	ID             uuid.UUID      `json:"id"`
	OrgID          string         `json:"org_id"`
	PackID         uuid.UUID      `json:"pack_id"`
	OwnerSystem    string         `json:"owner_system"`
	ProductSurface string         `json:"product_surface"`
	AssistType     string         `json:"assist_type"`
	SubjectType    string         `json:"subject_type"`
	SubjectID      string         `json:"subject_id"`
	Input          map[string]any `json:"input"`
	Output         map[string]any `json:"output"`
	Status         string         `json:"status"`
	ErrorMessage   string         `json:"error_message"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
}
