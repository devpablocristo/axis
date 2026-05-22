package domain

import (
	"time"

	"github.com/google/uuid"
)

// RiskClass define la clase de riesgo inherente de un tipo de acción
type RiskClass string

const (
	RiskClassLow      RiskClass = "low"
	RiskClassMedium   RiskClass = "medium"
	RiskClassHigh     RiskClass = "high"
	RiskClassCritical RiskClass = "critical"
)

// ActionType define un tipo de acción tipado con schema de validación
type ActionType struct {
	ID                 uuid.UUID
	OrgID              *string        // nil = global
	Name               string         // ej: "treasury.transfer", "iam.grant_role"
	Description        string
	Category           string         // ej: "treasury", "iam", "infra", "incident"
	RiskClass          RiskClass      // riesgo inherente de este tipo de acción
	Schema             map[string]any // JSON Schema para validar params de la request
	Reversible         bool           // si la acción es reversible
	RequiresBreakGlass bool       // si siempre requiere break-glass
	Enabled            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
