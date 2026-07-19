package learning

import (
	"context"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	Create(ctx context.Context, tenantID string, input NormalizedCreateInput) (Proposal, error)
	List(ctx context.Context, tenantID, status string, virployeeID *uuid.UUID) ([]Proposal, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (Proposal, error)
	Candidates(ctx context.Context, tenantID string, minExecutions int) ([]Candidate, error)
	LatestForPair(ctx context.Context, tenantID string, virployeeID uuid.UUID, capabilityKey string) (*Proposal, error)
	SuccessfulExecutionTraceIDs(ctx context.Context, tenantID string, virployeeID uuid.UUID, capabilityKey string, limit int) ([]string, error)
}

const (
	defaultMinExecutions = 3
	maxSourceTraceIDs    = 20
)

type UseCases struct {
	repo          RepositoryPort
	minExecutions int
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo, minExecutions: defaultMinExecutions}
}

// SetMinExecutions overrides the default scan threshold (config, gate G4.1:
// thresholds are configuration, never hardcoded per v1).
func (u *UseCases) SetMinExecutions(n int) {
	if n >= 1 {
		u.minExecutions = n
	}
}

// Ingest files a proposal into the inbox as pending. It is the ONLY entry
// point for proposals (analyzer in PR2, LLM enricher in PR5) and never touches
// memories: installation happens exclusively through the human Accept (PR3).
func (u *UseCases) Ingest(ctx context.Context, tenantID string, input CreateInput) (Proposal, error) {
	normalized, err := NormalizeCreateInput(input)
	if err != nil {
		return Proposal{}, err
	}
	return u.repo.Create(ctx, tenantID, normalized)
}

func (u *UseCases) List(ctx context.Context, tenantID, statusFilter string, virployeeID *uuid.UUID) ([]Proposal, error) {
	status, err := NormalizeStatusFilter(statusFilter)
	if err != nil {
		return nil, err
	}
	return u.repo.List(ctx, tenantID, status, virployeeID)
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (Proposal, error) {
	return u.repo.Get(ctx, tenantID, id)
}

// ProposalRef is a slim pointer to a filed proposal; the inbox List endpoint
// serves the full objects.
type ProposalRef struct {
	ID            uuid.UUID `json:"id"`
	VirployeeID   uuid.UUID `json:"virployee_id"`
	CapabilityKey string    `json:"capability_key"`
	Title         string    `json:"title"`
}

// ScanResult summarizes one analyzer pass.
type ScanResult struct {
	Threshold  int           `json:"threshold"`
	Candidates int           `json:"candidates"`
	Proposed   int           `json:"proposed"`
	Skipped    int           `json:"skipped"`
	Proposals  []ProposalRef `json:"proposals"`
}

// Scan runs the tenant-scoped analyzer: find (virployee, capability) pairs
// with enough successful executions and file a proposal for each, unless the
// pair already has a pending/accepted proposal (or a dismissal with no new
// evidence since). Deterministic — no LLM involved. The per-call override can
// only RAISE the configured threshold: thresholds are governance configuration
// (gate G4.1) and a caller must not be able to lower the floor and flood the
// review inbox with one-run "learnings".
func (u *UseCases) Scan(ctx context.Context, tenantID string, minExecutions int) (ScanResult, error) {
	if minExecutions < 0 {
		return ScanResult{}, domainerr.Validation("min_executions must be at least 1")
	}
	threshold := u.minExecutions
	if minExecutions > threshold {
		threshold = minExecutions
	}

	candidates, err := u.repo.Candidates(ctx, tenantID, threshold)
	if err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{Threshold: threshold, Candidates: len(candidates), Proposals: []ProposalRef{}}
	for _, candidate := range candidates {
		virployeeID, err := uuid.Parse(candidate.VirployeeID)
		if err != nil {
			result.Skipped++
			continue
		}
		latest, err := u.repo.LatestForPair(ctx, tenantID, virployeeID, candidate.CapabilityKey)
		if err != nil {
			return ScanResult{}, err
		}
		if !ShouldPropose(latest, candidate.Succeeded) {
			result.Skipped++
			continue
		}
		sources, err := u.repo.SuccessfulExecutionTraceIDs(ctx, tenantID, virployeeID, candidate.CapabilityKey, maxSourceTraceIDs)
		if err != nil {
			return ScanResult{}, err
		}
		title, content := Distill(candidate)
		proposal, err := u.Ingest(ctx, tenantID, CreateInput{
			VirployeeID:        virployeeID,
			CapabilityKey:      candidate.CapabilityKey,
			Title:              title,
			Content:            content,
			Evidence:           BuildEvidence(candidate),
			SourceTraceIDs:     sources,
			ProposedBy:         ProposedByAnalyzer,
			SucceededWatermark: candidate.Succeeded,
		})
		if err != nil {
			// One bad candidate must not sink the whole pass: the pending-unique
			// index may race a concurrent scan (conflict), and a malformed
			// candidate (e.g. a legacy capability key from a future executor)
			// fails normalization — both are per-candidate skips, not failures.
			if domainerr.IsConflict(err) || domainerr.IsValidation(err) {
				result.Skipped++
				continue
			}
			return ScanResult{}, err
		}
		result.Proposed++
		result.Proposals = append(result.Proposals, ProposalRef{
			ID:            proposal.ID,
			VirployeeID:   proposal.VirployeeID,
			CapabilityKey: proposal.CapabilityKey,
			Title:         proposal.Title,
		})
	}
	return result, nil
}
