package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentConversation es una conversación durable del agente IA (chat),
// independiente de Tasks. La heredamos de la migración pymes-ai donde el
// historial vivía en `ai_conversations`. Companion la usa para persistir
// chats que no terminan en una Task (ej: consultas, exploración).
type AgentConversation struct {
	ID             uuid.UUID       `json:"id"`
	OrgID          string          `json:"org_id"`
	UserID         string          `json:"user_id,omitempty"`
	ProductSurface string          `json:"product_surface"`
	Title          string          `json:"title,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	Source         string          `json:"source"` // companion_native | pymes_ai_migrated
	MetadataJSON   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// AgentConversationMessage es un mensaje individual dentro de una conversación.
type AgentConversationMessage struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	OrgID          string          `json:"org_id"`
	Role           string          `json:"role"` // user | assistant | system | tool
	Content        string          `json:"content"`
	MetadataJSON   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// AgentMemoryFact es un fact aprendido por el agente sobre un tenant. Está
// scoped por (org_id, fact_key) y se actualiza idempotentemente. Es la
// contraparte de `ai_dossiers.memory.business_facts`.
type AgentMemoryFact struct {
	ID             uuid.UUID       `json:"id"`
	OrgID          string          `json:"org_id"`
	ConversationID *uuid.UUID      `json:"conversation_id,omitempty"`
	FactKey        string          `json:"fact_key"`
	FactValue      json.RawMessage `json:"fact_value"`
	Confidence     float64         `json:"confidence"`
	Source         string          `json:"source"`
	MetadataJSON   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// AgentUserProfile es el blob de preferencias/perfil por (org, usuario).
// Equivalente a `ai_dossiers.memory.user_profiles`.
type AgentUserProfile struct {
	OrgID       string          `json:"org_id"`
	UserID      string          `json:"user_id"`
	ProfileJSON json.RawMessage `json:"profile"`
	Source      string          `json:"source"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// Source values exposed for callers.
const (
	AgentSourceCompanionNative = "companion_native"
	AgentSourcePymesAIMigrated = "pymes_ai_migrated"
)
