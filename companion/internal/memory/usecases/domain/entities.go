package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MemoryKind tipo de memoria operativa.
type MemoryKind string

const (
	MemoryTaskSummary    MemoryKind = "task_summary"
	MemoryTaskFacts      MemoryKind = "task_facts"
	MemoryPlaybook       MemoryKind = "playbook_snippet"
	MemoryUserPreference MemoryKind = "user_preference"
	MemoryEpisodicEvent  MemoryKind = "episodic_event"
	MemorySemanticFact   MemoryKind = "semantic_fact"
	MemoryOperational    MemoryKind = "operational_state"
)

type MemoryType string

const (
	MemoryTypeEpisodic       MemoryType = "episodic"
	MemoryTypeSemantic       MemoryType = "semantic"
	MemoryTypeOperational    MemoryType = "operational"
	MemoryTypePreference     MemoryType = "preference"
	MemoryTypePlaybook       MemoryType = "playbook"
	MemoryTypeTaskProjection MemoryType = "task_projection"
)

type MemoryClass string

const (
	MemoryClassStable      MemoryClass = "stable"
	MemoryClassOperational MemoryClass = "operational"
	MemoryClassAudit       MemoryClass = "audit"
)

// ScopeType alcance de la entrada de memoria.
type ScopeType string

const (
	ScopeTask ScopeType = "task"
	ScopeOrg  ScopeType = "org"
	ScopeUser ScopeType = "user"
)

// MemoryEntry entrada de memoria operativa del compañero.
type MemoryEntry struct {
	ID              uuid.UUID
	OrgID           string
	UserID          string
	ProductSurface  string
	Kind            MemoryKind
	MemoryType      MemoryType
	Classification  MemoryClass
	ScopeType       ScopeType
	ScopeID         string
	Key             string
	PayloadJSON     json.RawMessage
	ContentText     string
	ProvenanceJSON  json.RawMessage
	Confidence      float64
	RetentionPolicy string
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ExpiresAt       *time.Time
}

// DefaultRetentionDays retención por tipo de memoria.
func DefaultRetentionDays(kind MemoryKind) int {
	switch kind {
	case MemoryTaskSummary, MemoryEpisodicEvent:
		return 90
	case MemoryTaskFacts, MemorySemanticFact, MemoryOperational:
		return 90
	case MemoryPlaybook:
		return 0 // sin expiración
	case MemoryUserPreference:
		return 0 // sin expiración
	default:
		return 90
	}
}

func ClassForKind(kind MemoryKind) MemoryClass {
	switch kind {
	case MemoryUserPreference, MemoryPlaybook:
		return MemoryClassStable
	case MemoryTaskSummary, MemoryTaskFacts:
		return MemoryClassOperational
	default:
		return MemoryClassOperational
	}
}

func TypeForKind(kind MemoryKind) MemoryType {
	switch kind {
	case MemoryEpisodicEvent:
		return MemoryTypeEpisodic
	case MemorySemanticFact:
		return MemoryTypeSemantic
	case MemoryOperational:
		return MemoryTypeOperational
	case MemoryUserPreference:
		return MemoryTypePreference
	case MemoryPlaybook:
		return MemoryTypePlaybook
	case MemoryTaskSummary, MemoryTaskFacts:
		return MemoryTypeTaskProjection
	default:
		return MemoryTypeOperational
	}
}
