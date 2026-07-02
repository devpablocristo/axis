package domain

import (
	"time"

	"github.com/google/uuid"
)

type ContractStatus string

const (
	ContractStatusDraft      ContractStatus = "draft"
	ContractStatusActive     ContractStatus = "active"
	ContractStatusDeprecated ContractStatus = "deprecated"
	ContractStatusArchived   ContractStatus = "archived"
)

type ValidationMode string

const (
	ValidationModeReportOnly ValidationMode = "report_only"
	ValidationModeEnforce    ValidationMode = "enforce"
)

type Contract struct {
	ID             uuid.UUID
	OrgID          *string
	Name           string
	Version        string
	SubjectType    string
	Schema         map[string]any
	Status         ContractStatus
	ValidationMode ValidationMode
	Compatibility  string
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	PromotedAt     *time.Time
	DeprecatedAt   *time.Time
}

type ValidationReport struct {
	ID              uuid.UUID
	OrgID           *string
	ContractName    string
	ContractVersion string
	SubjectType     string
	SubjectID       string
	Mode            ValidationMode
	Valid           bool
	Errors          []string
	PayloadHash     string
	CreatedAt       time.Time
}
