package domain

import (
	"time"

	"github.com/google/uuid"
)

type RuleMode string

const (
	RuleModeEnforced RuleMode = "enforced"
	RuleModeShadow   RuleMode = "shadow"
)

type FindingStatus string

const (
	FindingStatusOpen         FindingStatus = "open"
	FindingStatusAcknowledged FindingStatus = "acknowledged"
	FindingStatusResolved     FindingStatus = "resolved"
	FindingStatusDismissed    FindingStatus = "dismissed"
	FindingStatusShadow       FindingStatus = "shadow"
)

type FindingRule struct {
	ID             uuid.UUID  `json:"id"`
	OrgID          string     `json:"org_id"`
	OwnerSystem    string     `json:"owner_system"`
	SourceSystem   string     `json:"source_system"`
	FactType       string     `json:"fact_type"`
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Expression     string     `json:"expression"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Message        string     `json:"message"`
	Recommendation string     `json:"recommendation"`
	Mode           RuleMode   `json:"mode"`
	Enabled        bool       `json:"enabled"`
	Priority       int        `json:"priority"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type FactEvaluation struct {
	ID            uuid.UUID      `json:"id"`
	OrgID         string         `json:"org_id"`
	OwnerSystem   string         `json:"owner_system"`
	SourceSystem  string         `json:"source_system"`
	FactType      string         `json:"fact_type"`
	SourceEventID string         `json:"source_event_id"`
	SubjectType   string         `json:"subject_type"`
	SubjectID     string         `json:"subject_id"`
	Facts         map[string]any `json:"facts"`
	CreatedAt     time.Time      `json:"created_at"`
}

type Finding struct {
	ID             uuid.UUID      `json:"id"`
	OrgID          string         `json:"org_id"`
	EvaluationID   uuid.UUID      `json:"evaluation_id"`
	RuleID         uuid.UUID      `json:"rule_id"`
	OwnerSystem    string         `json:"owner_system"`
	SourceSystem   string         `json:"source_system"`
	FactType       string         `json:"fact_type"`
	SourceEventID  string         `json:"source_event_id"`
	SubjectType    string         `json:"subject_type"`
	SubjectID      string         `json:"subject_id"`
	Code           string         `json:"code"`
	Severity       string         `json:"severity"`
	Title          string         `json:"title"`
	Message        string         `json:"message"`
	Recommendation string         `json:"recommendation"`
	Evidence       map[string]any `json:"evidence"`
	Status         FindingStatus  `json:"status"`
	ResolutionNote string         `json:"resolution_note"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
