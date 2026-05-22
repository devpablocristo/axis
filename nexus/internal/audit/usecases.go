package audit

import (
	"context"
	"time"

	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	"github.com/google/uuid"
)

type ReplayRequestInfo struct {
	OrgID          string
	RequesterType  string
	RequesterID    string
	ActionType     string
	TargetSystem   string
	TargetResource string
	Status         string
}

type RequestGetter interface {
	GetReplayInfo(ctx context.Context, id uuid.UUID) (ReplayRequestInfo, error)
}

type Usecases struct {
	repo        Repository
	requestRepo RequestGetter
}

func NewUsecases(repo Repository, requestRepo RequestGetter) *Usecases {
	return &Usecases{repo: repo, requestRepo: requestRepo}
}

type ReplayOutput struct {
	RequestID     string                    `json:"request_id"`
	OrgID         string                    `json:"org_id,omitempty"`
	Requester     struct{ Type, ID string } `json:"requester"`
	ActionType    string                    `json:"action_type"`
	Target        string                    `json:"target"`
	FinalStatus   string                    `json:"final_status"`
	DurationTotal string                    `json:"duration_total,omitempty"`
	Timeline      []TimelineEntry           `json:"timeline"`
}

type TimelineEntry struct {
	Event   string `json:"event"`
	Actor   string `json:"actor"`
	At      string `json:"at"`
	Summary string `json:"summary"`
}

func (u *Usecases) Replay(ctx context.Context, requestID uuid.UUID) (ReplayOutput, error) {
	// Pedir primero la info de la request: el OrgID se necesita para que el
	// handler pueda autorizar antes de exponer la timeline. Si la request no
	// existe (o pertenece a otra org y el repo lo filtra), salimos sin tocar
	// audit events.
	req, err := u.requestRepo.GetReplayInfo(ctx, requestID)
	if err != nil {
		return ReplayOutput{}, err
	}
	events, err := u.repo.ListByRequestID(ctx, requestID)
	if err != nil {
		return ReplayOutput{}, err
	}
	out := ReplayOutput{
		RequestID:   requestID.String(),
		OrgID:       req.OrgID,
		Requester:   struct{ Type, ID string }{req.RequesterType, req.RequesterID},
		ActionType:  req.ActionType,
		Target:      req.TargetSystem + " / " + req.TargetResource,
		FinalStatus: req.Status,
	}
	var first, last time.Time
	for _, e := range events {
		out.Timeline = append(out.Timeline, timelineEntryFromEvent(e))
		if first.IsZero() || e.CreatedAt.Before(first) {
			first = e.CreatedAt
		}
		if e.CreatedAt.After(last) {
			last = e.CreatedAt
		}
	}
	if !first.IsZero() && !last.IsZero() {
		out.DurationTotal = last.Sub(first).Round(time.Second).String()
	}
	return out, nil
}

func timelineEntryFromEvent(e auditdomain.RequestEvent) TimelineEntry {
	return TimelineEntry{
		Event:   e.EventType,
		Actor:   e.ActorID,
		At:      e.CreatedAt.Format(time.RFC3339),
		Summary: e.Summary,
	}
}
