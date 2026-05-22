package domain

import (
	"time"

	"github.com/google/uuid"
)

// Delegation modela quién delega qué a quién:
// owner → agent → action_types → resources → propósito → expiración
type Delegation struct {
	ID                 uuid.UUID
	OrgID              *string  // nil = global
	OwnerID            string   // quién delega (ej: "team-finops", "user:pablo@empresa.com")
	OwnerType          string   // "user", "team", "service"
	AgentID            string   // a quién delega (ej: "ops-bot", "deploy-svc")
	AgentType          string   // "agent", "service"
	AllowedActionTypes []string // action_types permitidos (vacío = todos)
	AllowedResources   []string // resources permitidos (vacío = todos)
	Purpose            string   // para qué (ej: "maintenance automation")
	MaxRiskClass       string   // riesgo máximo permitido (low, medium, high, critical)
	ExpiresAt          *time.Time
	Enabled            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
