package dto

import (
	"encoding/json"
	"time"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

// UpsertMemoryRequest petición para crear o actualizar memoria.
type UpsertMemoryRequest struct {
	Kind            string          `json:"kind"`
	MemoryType      string          `json:"memory_type,omitempty"`
	Classification  string          `json:"classification,omitempty"`
	ScopeType       string          `json:"scope_type"`
	ScopeID         string          `json:"scope_id"`
	Key             string          `json:"key"`
	PayloadJSON     json.RawMessage `json:"payload_json,omitempty"`
	ContentText     string          `json:"content_text,omitempty"`
	ProvenanceJSON  json.RawMessage `json:"provenance,omitempty"`
	Confidence      float64         `json:"confidence,omitempty"`
	RetentionPolicy string          `json:"retention_policy,omitempty"`
	Source          string          `json:"source,omitempty"`
	Supersede       bool            `json:"supersede,omitempty"`
	Version         int             `json:"version,omitempty"`
	TTLDays         int             `json:"ttl_days,omitempty"`
}

// MemoryResponse respuesta de una entrada de memoria.
type MemoryResponse struct {
	ID                 string          `json:"id"`
	OrgID              string          `json:"org_id"`
	UserID             string          `json:"user_id,omitempty"`
	ProductSurface     string          `json:"product_surface"`
	Kind               string          `json:"kind"`
	MemoryType         string          `json:"memory_type"`
	Classification     string          `json:"classification"`
	ScopeType          string          `json:"scope_type"`
	ScopeID            string          `json:"scope_id"`
	Key                string          `json:"key"`
	PayloadJSON        json.RawMessage `json:"payload_json"`
	ContentText        string          `json:"content_text"`
	ProvenanceJSON     json.RawMessage `json:"provenance"`
	Confidence         float64         `json:"confidence"`
	TrustScore         float64         `json:"trust_score"`
	RetentionPolicy    string          `json:"retention_policy"`
	Status             string          `json:"status"`
	Source             string          `json:"source,omitempty"`
	EmbeddingNamespace string          `json:"embedding_namespace,omitempty"`
	EmbeddingModel     string          `json:"embedding_model,omitempty"`
	PoisoningFlags     []string        `json:"poisoning_flags,omitempty"`
	Version            int             `json:"version"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
	ExpiresAt          *string         `json:"expires_at,omitempty"`
}

// MemoryListResponse lista de entradas de memoria.
type MemoryListResponse struct {
	Entries []MemoryResponse `json:"entries"`
}

type MemorySearchResult struct {
	Entry   MemoryResponse `json:"entry"`
	Score   float64        `json:"score"`
	Reasons []string       `json:"reasons,omitempty"`
}

type MemorySearchResponse struct {
	Results []MemorySearchResult `json:"results"`
}

// EntryToResponse convierte entidad de dominio a DTO de respuesta.
func EntryToResponse(e domain.MemoryEntry) MemoryResponse {
	var expires *string
	if e.ExpiresAt != nil {
		s := e.ExpiresAt.UTC().Format(time.RFC3339)
		expires = &s
	}
	return MemoryResponse{
		ID:                 e.ID.String(),
		OrgID:              e.OrgID,
		UserID:             e.UserID,
		ProductSurface:     e.ProductSurface,
		Kind:               string(e.Kind),
		MemoryType:         string(e.MemoryType),
		Classification:     string(e.Classification),
		ScopeType:          string(e.ScopeType),
		ScopeID:            e.ScopeID,
		Key:                e.Key,
		PayloadJSON:        e.PayloadJSON,
		ContentText:        e.ContentText,
		ProvenanceJSON:     e.ProvenanceJSON,
		Confidence:         e.Confidence,
		TrustScore:         e.TrustScore,
		RetentionPolicy:    e.RetentionPolicy,
		Status:             e.Status,
		Source:             e.Source,
		EmbeddingNamespace: e.EmbeddingNamespace,
		EmbeddingModel:     e.EmbeddingModel,
		PoisoningFlags:     append([]string(nil), e.PoisoningFlags...),
		Version:            e.Version,
		CreatedAt:          e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          e.UpdatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:          expires,
	}
}

func SearchResultToResponse(e domain.MemoryEntry, score float64, reasons []string) MemorySearchResult {
	return MemorySearchResult{
		Entry:   EntryToResponse(e),
		Score:   score,
		Reasons: append([]string(nil), reasons...),
	}
}
