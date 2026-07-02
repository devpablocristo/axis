package handoffs

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("handoff not found")
	ErrValidation = errors.New("handoff validation failed")
	ErrConflict   = errors.New("handoff conflict")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

type Handoff struct {
	HandoffID       uuid.UUID  `json:"handoff_id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	OrgID           string     `json:"-"`
	ProductSurface  string     `json:"-"`
	TaskID          *uuid.UUID `json:"task_id,omitempty"`
	FromVirployeeID *uuid.UUID `json:"from_virployee_id,omitempty"`
	ToVirployeeID   uuid.UUID  `json:"to_virployee_id"`
	Reason          string     `json:"reason"`
	Status          Status     `json:"status"`
	CreatedBy       string     `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
}

func normalize(input Handoff) Handoff {
	input.OrgID = strings.TrimSpace(input.OrgID)
	input.ProductSurface = strings.TrimSpace(strings.ToLower(input.ProductSurface))
	input.Reason = strings.TrimSpace(input.Reason)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if input.Status == "" {
		input.Status = StatusPending
	}
	return input
}

func validate(input Handoff) error {
	if input.TenantID == uuid.Nil || input.OrgID == "" || input.ProductSurface == "" || input.ToVirployeeID == uuid.Nil {
		return ErrValidation
	}
	switch input.Status {
	case StatusPending, StatusAccepted, StatusRejected, StatusCancelled:
	default:
		return ErrValidation
	}
	return nil
}
