package audit

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrValidation = errors.New("audit validation failed")
	ErrNotFound   = errors.New("audit event not found")
)

const (
	ActionCreate        = "created"
	ActionUpdate        = "updated"
	ActionStatusChanged = "status.changed"
	ActionArchive       = "archived"
	ActionRestore       = "restored"
	ActionHardDelete    = "hard_deleted"
)

type Event struct {
	AuditEventID    uuid.UUID  `json:"audit_event_id"`
	TenantID        string     `json:"tenant_id"`
	ResourceType    string     `json:"resource_type"`
	ResourceID      uuid.UUID  `json:"resource_id"`
	Action          string     `json:"action"`
	OccurredAt      time.Time  `json:"occurred_at"`
	ActorUserID     string     `json:"actor_user_id,omitempty"`
	Reason          *string    `json:"reason,omitempty"`
	BatchID         *uuid.UUID `json:"batch_id,omitempty"`
	RetentionExpiry *time.Time `json:"retention_expires,omitempty"`
}

type Filter struct {
	TenantID     string
	ResourceType string
	ResourceID   uuid.UUID
	Limit        int
}

func normalizeEvent(event Event) (Event, error) {
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.ResourceType = strings.TrimSpace(event.ResourceType)
	event.Action = strings.TrimSpace(event.Action)
	event.ActorUserID = strings.TrimSpace(event.ActorUserID)
	if event.AuditEventID == uuid.Nil {
		event.AuditEventID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	if event.TenantID == "" || event.ResourceType == "" || event.ResourceID == uuid.Nil || event.Action == "" {
		return Event{}, ErrValidation
	}
	return event, nil
}
