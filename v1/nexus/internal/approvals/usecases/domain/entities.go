package domain

import (
	"time"

	"github.com/google/uuid"
)

// ApprovalStatus representa el estado de una approval.
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
	ApprovalStatusExpired  ApprovalStatus = "expired"
)

type Approval struct {
	ID                uuid.UUID
	OrgID             *string // hereda del request
	RequestID         uuid.UUID
	Status            ApprovalStatus
	DecidedBy         string
	DecisionNote      string
	DecidedAt         *time.Time
	ExpiresAt         time.Time
	CreatedAt         time.Time
	BreakGlass        bool               // requiere múltiples aprobadores
	RequiredApprovals int                // cuántos aprobadores se necesitan (default 1)
	Decisions         []ApprovalDecision // historial de decisiones parciales
}

// ApprovalDecision registra una decisión individual en break-glass
type ApprovalDecision struct {
	ApproverID string    `json:"approver_id"`
	Action     string    `json:"action"` // "approve" o "reject"
	Note       string    `json:"note"`
	DecidedAt  time.Time `json:"decided_at"`
}
