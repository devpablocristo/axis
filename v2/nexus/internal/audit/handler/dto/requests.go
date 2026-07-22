package dto

import auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"

type AppendRequest struct {
	VirployeeID string         `json:"virployee_id" binding:"required"`
	SubjectType string         `json:"subject_type"`
	SubjectID   string         `json:"subject_id"`
	EventType   string         `json:"event_type" binding:"required"`
	ActorType   string         `json:"actor_type"`
	ActorID     string         `json:"actor_id"`
	Summary     string         `json:"summary"`
	Data        map[string]any `json:"data"`
}

func (r AppendRequest) ToDomain() auditdomain.AppendInput {
	return auditdomain.AppendInput{
		VirployeeID: r.VirployeeID,
		SubjectType: r.SubjectType,
		SubjectID:   r.SubjectID,
		EventType:   r.EventType,
		ActorType:   r.ActorType,
		ActorID:     r.ActorID,
		Summary:     r.Summary,
		Data:        r.Data,
	}
}
