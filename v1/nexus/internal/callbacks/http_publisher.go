package callbacks

import (
	"context"

	webhook "github.com/devpablocristo/platform/webhook/go"
)

const (
	EventApprovalPending  = "approval_pending"
	EventApprovalResolved = "approval_resolved"
)

// ApprovalEvent es el payload de dominio que se envía por webhook.
type ApprovalEvent struct {
	Event          string  `json:"event"`
	ApprovalID     string  `json:"approval_id,omitempty"`
	OrgID          string  `json:"org_id,omitempty"`
	RequestID      string  `json:"request_id"`
	Decision       string  `json:"decision,omitempty"`
	DecidedBy      string  `json:"decided_by,omitempty"`
	DecisionNote   string  `json:"decision_note,omitempty"`
	ActionType     string  `json:"action_type,omitempty"`
	TargetResource string  `json:"target_resource,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	RiskLevel      string  `json:"risk_level,omitempty"`
	AISummary      *string `json:"ai_summary,omitempty"`
	CreatedAt      string  `json:"created_at,omitempty"`
	ExpiresAt      *string `json:"expires_at,omitempty"`
	DecidedAt      *string `json:"decided_at,omitempty"`
}

// ApprovalPublisher es el port que los usecases consumen.
type ApprovalPublisher interface {
	Publish(ctx context.Context, event ApprovalEvent) error
}

// HTTPApprovalPublisher delega en platform/webhook Publisher.
type HTTPApprovalPublisher struct {
	pub *webhook.Publisher
}

// NewHTTPApprovalPublisher crea un publisher de approval events.
func NewHTTPApprovalPublisher(token string, pendingURLs, resolvedURLs []string) *HTTPApprovalPublisher {
	return &HTTPApprovalPublisher{
		pub: webhook.NewPublisher(token, map[string][]string{
			EventApprovalPending:  pendingURLs,
			EventApprovalResolved: resolvedURLs,
		}),
	}
}

// Publish envía el evento al conjunto de URLs correspondiente.
func (h *HTTPApprovalPublisher) Publish(ctx context.Context, event ApprovalEvent) error {
	return h.pub.Publish(ctx, event.Event, event)
}
