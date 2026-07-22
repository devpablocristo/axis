package domain

import (
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
	StatusExpired  Status = "expired"
)

type Approval struct {
	ID                 uuid.UUID
	TenantID           string
	GovernanceCheckID  uuid.UUID
	RequesterID        string
	ActionType         string
	TargetSystem       string
	TargetResource     string
	RiskLevel          string
	Reason             string
	BindingHash        string
	Status             Status
	ApprovalKind       string
	SupervisorUserID   string
	QuorumRequired     int
	ApprovalCount      int
	Decisions          []Decision
	PostReviewRequired bool
	ReviewedBy         string
	ReviewNote         string
	ReviewedAt         *time.Time
	DecidedBy          string
	DecisionNote       string
	DecidedAt          *time.Time
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Decision struct {
	ID        uuid.UUID
	ActorID   string
	ActorRole string
	Decision  string
	Note      string
	DecidedAt time.Time
}

type DecisionActor struct {
	ID   string
	Role string
}

type ListInput struct {
	StatusRaw string
	Limit     int
	Cursor    string
}

type ListCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type ListPage struct {
	Items      []Approval
	HasMore    bool
	NextCursor string
}

type DecisionInput struct {
	Note string
}

func NormalizeListStatus(value string) (Status, error) {
	status := Status(strings.TrimSpace(value))
	if status == "" {
		return StatusPending, nil
	}
	if status != StatusPending && status != StatusApproved && status != StatusRejected && status != StatusExpired {
		return "", domainerr.Validation("invalid approval status")
	}
	return status, nil
}

func NormalizeDecisionNote(input DecisionInput) string {
	return strings.TrimSpace(input.Note)
}
