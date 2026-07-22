package evidence

import (
	"context"
	"strings"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/audit"
	evidencedomain "github.com/devpablocristo/nexus-v2/internal/evidence/usecases/domain"
)

// EvidenceVersion is the pack format version.
const EvidenceVersion = "1.1"

// AuditReader is the read side of the audit ledger the evidence pack is built
// from. The audit UseCases satisfy it.
type AuditReader interface {
	Replay(ctx context.Context, tenantID, virployeeID string) (audit.ReplayOutput, error)
}

type subjectAuditReader interface {
	ReplaySubject(context.Context, string, string) ([]audit.ReplayOutput, error)
}

type UseCases struct {
	audit  AuditReader
	signer *Signer
}

// NewUseCases wires the evidence generator. signer may be nil (no signing key):
// packs are then emitted with algorithm "none".
func NewUseCases(reader AuditReader, signer *Signer) *UseCases {
	return &UseCases{audit: reader, signer: signer}
}

// Generate assembles and signs an evidence pack for a virployee's ledger. When
// subject is non-empty the timeline is focused on that subject (e.g. one
// diagnosis run) while the integrity block still proves the whole chain intact.
func (u *UseCases) Generate(ctx context.Context, tenantID, virployeeID, subject string) (evidencedomain.EvidencePack, error) {
	replay, err := u.audit.Replay(ctx, tenantID, virployeeID)
	if err != nil {
		return evidencedomain.EvidencePack{}, err
	}

	pack := evidencedomain.EvidencePack{
		Version:     EvidenceVersion,
		GeneratedAt: time.Now().UTC(),
		Scope:       replay.Scope,
		Virployee: evidencedomain.VirployeeRef{
			TenantID: strings.TrimSpace(tenantID),
			ID:       replay.VirployeeID,
		},
	}
	if replay.Integrity != nil {
		pack.Integrity = evidencedomain.Integrity{
			Status:        replay.Integrity.Status,
			CheckedEvents: replay.Integrity.CheckedEvents,
			FirstHash:     replay.Integrity.FirstHash,
			LastHash:      replay.Integrity.LastHash,
			Signed:        replay.Integrity.Signed,
			Error:         replay.Integrity.Error,
		}
	}

	subject = strings.TrimSpace(subject)
	timeline := make([]evidencedomain.TimelineEvent, 0, len(replay.Timeline))
	for _, e := range replay.Timeline {
		if subject != "" && e.SubjectID != subject {
			continue
		}
		timeline = append(timeline, evidencedomain.TimelineEvent{
			Event:     e.Event,
			Actor:     e.Actor,
			Subject:   e.Subject,
			At:        e.At,
			Summary:   e.Summary,
			Data:      e.Data,
			EventHash: e.EventHash,
		})
	}
	pack.Timeline = timeline
	pack.EventCount = len(timeline)
	if subject != "" {
		pack.Subject = &evidencedomain.SubjectRef{ID: subject, ChainEventCount: len(replay.Timeline)}
		if reader, ok := u.audit.(subjectAuditReader); ok {
			chains, chainErr := reader.ReplaySubject(ctx, tenantID, subject)
			if chainErr != nil {
				return evidencedomain.EvidencePack{}, chainErr
			}
			for _, chain := range chains {
				if chain.VirployeeID == replay.VirployeeID {
					continue
				}
				linkedTimeline := evidenceTimelineForSubject(chain.Timeline, subject)
				if len(linkedTimeline) == 0 {
					continue
				}
				linked := evidencedomain.LinkedChain{
					Scope: chain.Scope, VirployeeID: chain.VirployeeID,
					EventCount: len(linkedTimeline), Timeline: linkedTimeline,
				}
				if chain.Integrity != nil {
					linked.Integrity = evidenceIntegrity(chain.Integrity)
				}
				pack.LinkedChains = append(pack.LinkedChains, linked)
				pack.Subject.ChainEventCount += chain.EventCount
				pack.EventCount += len(linkedTimeline)
			}
		}
	}

	if u.signer != nil {
		if err := u.signer.SignPack(&pack); err != nil {
			return evidencedomain.EvidencePack{}, err
		}
	} else {
		pack.Signature = evidencedomain.Signature{Algorithm: "none"}
	}
	return pack, nil
}

func evidenceTimelineForSubject(timeline []audit.TimelineEntry, subject string) []evidencedomain.TimelineEvent {
	out := make([]evidencedomain.TimelineEvent, 0)
	for _, event := range timeline {
		if event.SubjectID != subject {
			continue
		}
		out = append(out, evidencedomain.TimelineEvent{
			Event: event.Event, Actor: event.Actor, Subject: event.Subject, At: event.At,
			Summary: event.Summary, Data: event.Data, EventHash: event.EventHash,
		})
	}
	return out
}

func evidenceIntegrity(in *audit.IntegrityOutput) evidencedomain.Integrity {
	return evidencedomain.Integrity{
		Status: in.Status, CheckedEvents: in.CheckedEvents, FirstHash: in.FirstHash,
		LastHash: in.LastHash, Signed: in.Signed, Error: in.Error,
	}
}
