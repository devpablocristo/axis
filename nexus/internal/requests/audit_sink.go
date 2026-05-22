package requests

import (
	"context"
	"time"

	"github.com/devpablocristo/nexus/internal/audit"
	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	"github.com/google/uuid"
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
