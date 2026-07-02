package domain

import (
	"time"

	"github.com/google/uuid"
)

// PolicyMode determina si la policy actúa o solo observa
type PolicyMode string

const (
	PolicyModeEnforced PolicyMode = "enforced" // actúa normalmente
	PolicyModeShadow   PolicyMode = "shadow"   // evalúa pero no actúa, solo loguea
)

type Policy struct {
	ID           uuid.UUID
	OrgID        *string // nil = global (aplica a todas las orgs)
	Name         string
	Description  string
	ActionType   *string
	TargetSystem *string
	Expression   string
	Effect       string
	RiskOverride *string
	Priority     int
	Origin       string
	Mode         PolicyMode
	ProposalID   *uuid.UUID
	Enabled      bool
	ShadowHits   int // cuántas requests habría afectado en modo shadow
	ArchivedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
