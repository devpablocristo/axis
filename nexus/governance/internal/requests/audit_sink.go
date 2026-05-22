package requests

import (
	"context"
	"time"

	"github.com/google/uuid"
	auditdomain "github.com/devpablocristo/nexus/governance/internal/audit/usecases/domain"
	"github.com/devpablocristo/nexus/governance/internal/audit"
)

type AuditSink interface {
	AppendEvent(ctx context.Context, requestID uuid.UUID, eventType, actorType, actorID, summary string, data map[string]any) error
}

type auditSinkAdapter struct {
	repo audit.Repository
}

func NewAuditSinkAdapter(repo audit.Repository) AuditSink {
	return &auditSinkAdapter{repo: repo}
}

func (a *auditSinkAdapter) AppendEvent(ctx context.Context, requestID uuid.UUID, eventType, actorType, actorID, summary string, data map[string]any) error {
	if data == nil {
		data = make(map[string]any)
	}
	return a.repo.Append(ctx, auditdomain.RequestEvent{
		ID:        uuid.New(),
		RequestID: requestID,
		EventType: eventType,
		ActorType: actorType,
		ActorID:   actorID,
		Summary:   summary,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	})
}
