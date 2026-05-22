package domain

import (
	"time"

	"github.com/google/uuid"
)

type RequestEvent struct {
	ID        uuid.UUID
	RequestID uuid.UUID
	EventType string
	ActorType string
	ActorID   string
	Summary   string
	Data      map[string]any
	CreatedAt time.Time
}

const (
	EventReceived        = "received"
	EventEvaluated       = "evaluated"
	EventAllowed         = "allowed"
	EventDenied          = "denied"
	EventSentToApproval  = "sent_to_approval"
	EventApproved        = "approved"
	EventRejected        = "rejected"
	EventExpired         = "expired"
	EventExecuted        = "executed"
	EventExecutionFailed = "execution_failed"
	EventCancelled       = "cancelled"
	EventAttested        = "attested"
)
