package domain

import (
	"time"

	"github.com/google/uuid"
)

// ProposalStatus representa el estado de una propuesta.
type ProposalStatus string

const (
	ProposalStatusPending   ProposalStatus = "pending"
	ProposalStatusAccepted  ProposalStatus = "accepted"
	ProposalStatusDismissed ProposalStatus = "dismissed"
	ProposalStatusExpired   ProposalStatus = "expired"
)

type PolicyProposal struct {
	ID                  uuid.UUID
	OrgID               *string
	ProposedName        string
	ProposedDescription string
	ProposedExpression  string
	ProposedEffect      string
	ProposedActionType  *string
	ProposedPriority    int
	PatternSummary      string
	Confidence          float64
	SampleSize          int
	TimeWindow          string
	Status              ProposalStatus
	DecidedBy           *string
	DecidedAt           *time.Time
	PolicyID            *uuid.UUID
	CreatedAt           time.Time
}
