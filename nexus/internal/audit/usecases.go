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
	Integrity     *IntegrityOutput          `json:"integrity,omitempty"`
}

type TimelineEntry struct {
	Event   string `json:"event"`
	Actor   string `json:"actor"`
	At      string `json:"at"`
	Summary string `json:"summary"`
}

type IntegrityOutput struct {
	Status        string `json:"status"`
	CheckedEvents int    `json:"checked_events"`
	FirstHash     string `json:"first_hash,omitempty"`
	LastHash      string `json:"last_hash,omitempty"`
	Error         string `json:"error,omitempty"`
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
	integrity := verifyEvents(events)
	if integrity.Status == "ok" {
		integrity = u.verifySignatures(events, integrity)
	}
	out.Integrity = &integrity
	return out, nil
}

func (u *Usecases) Verify(ctx context.Context, requestID uuid.UUID) (IntegrityOutput, error) {
	if _, err := u.requestRepo.GetReplayInfo(ctx, requestID); err != nil {
		return IntegrityOutput{}, err
	}
	events, err := u.repo.ListByRequestID(ctx, requestID)
	if err != nil {
		return IntegrityOutput{}, err
	}
	out := verifyEvents(events)
	if out.Status == "ok" {
		out = u.verifySignatures(events, out)
	}
	if recorder, ok := u.repo.(interface {
		RecordIntegrityCheck(context.Context, IntegrityCheck) error
	}); ok {
		_ = recorder.RecordIntegrityCheck(ctx, IntegrityCheck{
			Scope:          "request",
			ScopeID:        requestID.String(),
			Status:         out.Status,
			CheckedEvents:  out.CheckedEvents,
			FirstEventHash: out.FirstHash,
			LastEventHash:  out.LastHash,
			ErrorMessage:   out.Error,
		})
	}
	return out, nil
}

func (u *Usecases) verifySignatures(events []auditdomain.RequestEvent, out IntegrityOutput) IntegrityOutput {
	verifier, ok := u.repo.(interface {
		VerifySignatures([]auditdomain.RequestEvent) error
	})
	if !ok {
		return out
	}
	if err := verifier.VerifySignatures(events); err != nil {
		out.Status = "failed"
		out.Error = err.Error()
	}
	return out
}

func verifyEvents(events []auditdomain.RequestEvent) IntegrityOutput {
	out := IntegrityOutput{Status: "ok", CheckedEvents: len(events)}
	var previous string
	for i, event := range events {
		if event.EventHash == "" {
			return IntegrityOutput{Status: "failed", CheckedEvents: i, Error: "legacy or unsealed event encountered"}
		}
		if event.ChainScope == "" {
			event.ChainScope = event.RequestID.String()
		}
		expectedPayloadHash, err := ComputePayloadHash(event)
		if err != nil {
			return IntegrityOutput{Status: "failed", CheckedEvents: i, Error: err.Error()}
		}
		if event.PayloadHash != expectedPayloadHash {
			return IntegrityOutput{Status: "failed", CheckedEvents: i, Error: "payload hash mismatch"}
		}
		if event.PreviousHash != previous {
			return IntegrityOutput{Status: "failed", CheckedEvents: i, Error: "previous hash mismatch"}
		}
		expectedEventHash, err := ComputeEventHash(event, event.PayloadHash)
		if err != nil {
			return IntegrityOutput{Status: "failed", CheckedEvents: i, Error: err.Error()}
		}
		if event.EventHash != expectedEventHash {
			return IntegrityOutput{Status: "failed", CheckedEvents: i, Error: "event hash mismatch"}
		}
		if i == 0 {
			out.FirstHash = event.EventHash
		}
		previous = event.EventHash
		out.LastHash = event.EventHash
	}
	return out
}

func timelineEntryFromEvent(e auditdomain.RequestEvent) TimelineEntry {
	return TimelineEntry{
		Event:   e.EventType,
		Actor:   e.ActorID,
		At:      e.CreatedAt.Format(time.RFC3339),
		Summary: e.Summary,
	}
}
