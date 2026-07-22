package audit

import (
	"context"
	"strings"
	"time"

	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// RepositoryPort is the persistence contract the usecases depend on.
type RepositoryPort interface {
	Append(ctx context.Context, e auditdomain.AuditEvent) (auditdomain.AuditEvent, error)
	ListByScope(ctx context.Context, chainScope string) ([]auditdomain.AuditEvent, error)
	VerifySignatures(events []auditdomain.AuditEvent) error
}

type UseCases struct {
	repo RepositoryPort
}

type subjectChainRepository interface {
	ListVirployeeIDsBySubject(context.Context, string, string) ([]string, error)
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) Append(ctx context.Context, orgID string, in auditdomain.AppendInput) (auditdomain.AuditEvent, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return auditdomain.AuditEvent{}, domainerr.Validation("organization is required")
	}
	virployeeID := strings.TrimSpace(in.VirployeeID)
	if virployeeID == "" {
		return auditdomain.AuditEvent{}, domainerr.Validation("virployee_id is required")
	}
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" {
		return auditdomain.AuditEvent{}, domainerr.Validation("event_type is required")
	}
	actorType := strings.TrimSpace(in.ActorType)
	if actorType == "" {
		actorType = "service"
	}
	event := auditdomain.AuditEvent{
		OrgID:       orgID,
		ChainScope:  auditdomain.ChainScopeFor(orgID, virployeeID),
		VirployeeID: virployeeID,
		SubjectType: strings.TrimSpace(in.SubjectType),
		SubjectID:   strings.TrimSpace(in.SubjectID),
		EventType:   eventType,
		ActorType:   actorType,
		ActorID:     strings.TrimSpace(in.ActorID),
		Summary:     in.Summary,
		Data:        in.Data,
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		id, err := uuid.Parse(key)
		if err != nil {
			return auditdomain.AuditEvent{}, domainerr.Validation("Idempotency-Key must be a UUID")
		}
		event.ID = id
	}
	return u.repo.Append(ctx, event)
}

// ReplayOutput is the timeline of a virployee's ledger plus its integrity proof.
type ReplayOutput struct {
	Scope         string           `json:"scope"`
	VirployeeID   string           `json:"virployee_id"`
	EventCount    int              `json:"event_count"`
	DurationTotal string           `json:"duration_total,omitempty"`
	Timeline      []TimelineEntry  `json:"timeline"`
	Integrity     *IntegrityOutput `json:"integrity,omitempty"`
}

type TimelineEntry struct {
	Event     string         `json:"event"`
	Actor     string         `json:"actor"`
	Subject   string         `json:"subject,omitempty"`
	SubjectID string         `json:"subject_id,omitempty"`
	At        string         `json:"at"`
	Summary   string         `json:"summary"`
	Data      map[string]any `json:"data,omitempty"`
	EventHash string         `json:"event_hash"`
}

type IntegrityOutput struct {
	Status        string `json:"status"`
	CheckedEvents int    `json:"checked_events"`
	FirstHash     string `json:"first_hash,omitempty"`
	LastHash      string `json:"last_hash,omitempty"`
	Signed        bool   `json:"signed"`
	Error         string `json:"error,omitempty"`
}

func (u *UseCases) Replay(ctx context.Context, orgID, virployeeID string) (ReplayOutput, error) {
	scope, err := scopeFor(orgID, virployeeID)
	if err != nil {
		return ReplayOutput{}, err
	}
	events, err := u.repo.ListByScope(ctx, scope)
	if err != nil {
		return ReplayOutput{}, err
	}
	out := ReplayOutput{
		Scope:       scope,
		VirployeeID: strings.TrimSpace(virployeeID),
		EventCount:  len(events),
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

func (u *UseCases) Verify(ctx context.Context, orgID, virployeeID string) (IntegrityOutput, error) {
	scope, err := scopeFor(orgID, virployeeID)
	if err != nil {
		return IntegrityOutput{}, err
	}
	events, err := u.repo.ListByScope(ctx, scope)
	if err != nil {
		return IntegrityOutput{}, err
	}
	out := verifyEvents(events)
	if out.Status == "ok" {
		out = u.verifySignatures(events, out)
	}
	return out, nil
}

// ReplaySubject returns every independently verified virployee chain linked by
// one subject (for example, an entrypoint run plus specialist consultations).
func (u *UseCases) ReplaySubject(ctx context.Context, orgID, subjectID string) ([]ReplayOutput, error) {
	orgID = strings.TrimSpace(orgID)
	subjectID = strings.TrimSpace(subjectID)
	if orgID == "" || subjectID == "" {
		return nil, domainerr.Validation("organization and subject are required")
	}
	lister, ok := u.repo.(subjectChainRepository)
	if !ok {
		return []ReplayOutput{}, nil
	}
	virployeeIDs, err := lister.ListVirployeeIDsBySubject(ctx, orgID, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]ReplayOutput, 0, len(virployeeIDs))
	for _, virployeeID := range virployeeIDs {
		replay, replayErr := u.Replay(ctx, orgID, virployeeID)
		if replayErr != nil {
			return nil, replayErr
		}
		out = append(out, replay)
	}
	return out, nil
}

func (u *UseCases) verifySignatures(events []auditdomain.AuditEvent, out IntegrityOutput) IntegrityOutput {
	if err := u.repo.VerifySignatures(events); err != nil {
		out.Status = "failed"
		out.Error = err.Error()
	}
	return out
}

func scopeFor(orgID, virployeeID string) (string, error) {
	orgID = strings.TrimSpace(orgID)
	virployeeID = strings.TrimSpace(virployeeID)
	if orgID == "" {
		return "", domainerr.Validation("organization is required")
	}
	if virployeeID == "" {
		return "", domainerr.Validation("virployee_id is required")
	}
	return auditdomain.ChainScopeFor(orgID, virployeeID), nil
}

// verifyEvents recomputes every hash and validates the chain. Ported from v1
// (nexus/internal/audit/usecases.go verifyEvents). Any mismatch stops the walk
// and reports "failed" with the offending index in CheckedEvents.
func verifyEvents(events []auditdomain.AuditEvent) IntegrityOutput {
	out := IntegrityOutput{Status: "ok", CheckedEvents: len(events)}
	for _, event := range events {
		if strings.TrimSpace(event.SignatureKeyID) != "" {
			out.Signed = true
			break
		}
	}
	var previous string
	for i, event := range events {
		if event.EventHash == "" {
			return IntegrityOutput{Status: "failed", CheckedEvents: i, Error: "unsealed event encountered"}
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

func timelineEntryFromEvent(e auditdomain.AuditEvent) TimelineEntry {
	subject := e.SubjectType
	if e.SubjectID != "" {
		subject = strings.TrimSpace(e.SubjectType + " " + e.SubjectID)
	}
	return TimelineEntry{
		Event:     e.EventType,
		Actor:     e.ActorID,
		Subject:   subject,
		SubjectID: e.SubjectID,
		At:        e.CreatedAt.Format(time.RFC3339),
		Summary:   e.Summary,
		Data:      e.Data,
		EventHash: e.EventHash,
	}
}

var _ RepositoryPort = (*Repository)(nil)
