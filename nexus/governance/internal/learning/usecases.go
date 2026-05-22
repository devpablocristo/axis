package learning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	learningdomain "github.com/devpablocristo/nexus/governance/internal/learning/usecases/domain"
	"github.com/google/uuid"
)

type Usecases struct {
	repo          Repository
	policyCreator PolicyCreator
	analyzer      PatternAnalyzer
	proposer      PolicyProposer
}

func NewUsecases(repo Repository, policyCreator PolicyCreator) *Usecases {
	return &Usecases{repo: repo, policyCreator: policyCreator}
}

func (u *Usecases) WithAnalyzer(a PatternAnalyzer) *Usecases {
	u.analyzer = a
	return u
}

func (u *Usecases) WithProposer(p PolicyProposer) *Usecases {
	u.proposer = p
	return u
}

func (u *Usecases) ListPendingProposals(ctx context.Context, limit int, orgID *string, allowAll bool) ([]learningdomain.PolicyProposal, error) {
	return u.repo.ListPendingProposals(ctx, limit, orgID, allowAll)
}

func (u *Usecases) GetProposalByID(ctx context.Context, id uuid.UUID) (learningdomain.PolicyProposal, error) {
	return u.repo.GetProposalByID(ctx, id)
}

func (u *Usecases) AcceptProposal(ctx context.Context, id uuid.UUID, decidedBy string) (policyID *uuid.UUID, err error) {
	p, err := u.repo.GetProposalByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get proposal: %w", err)
	}
	// Idempotencia: si una corrida previa creó la policy pero el UpdateProposal
	// falló, PolicyID quedó seteado. Devolvemos el id existente sin recrear
	// la policy, evitando duplicados (el status puede seguir siendo pending
	// hasta que un nuevo accept logre persistir).
	if p.PolicyID != nil {
		return p.PolicyID, nil
	}
	if p.Status != learningdomain.ProposalStatusPending {
		return nil, ErrNotPending
	}
	createdID, err := u.policyCreator.CreateFromProposal(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("create policy from proposal: %w", err)
	}
	now := time.Now().UTC()
	p.Status = learningdomain.ProposalStatusAccepted
	p.DecidedBy = &decidedBy
	p.DecidedAt = &now
	p.PolicyID = &createdID
	if _, err := u.repo.UpdateProposal(ctx, p); err != nil {
		// La policy ya existe en DB pero el proposal quedó inconsistente.
		// Devolvemos el error para que el caller reaccione (no se traga) y
		// además logueamos el id de la policy huérfana para que ops pueda
		// hacer reconciliación manual si la siguiente retry no llega.
		slog.Error("update proposal after accept failed: policy created but proposal not marked accepted",
			"error", err, "proposal_id", id, "orphan_policy_id", createdID)
		return &createdID, fmt.Errorf("update proposal: %w", err)
	}
	return &createdID, nil
}

func (u *Usecases) DismissProposal(ctx context.Context, id uuid.UUID, decidedBy string) error {
	p, err := u.repo.GetProposalByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get proposal: %w", err)
	}
	if p.Status != learningdomain.ProposalStatusPending {
		return ErrNotPending
	}
	now := time.Now().UTC()
	p.Status = learningdomain.ProposalStatusDismissed
	p.DecidedBy = &decidedBy
	p.DecidedAt = &now
	if _, err := u.repo.UpdateProposal(ctx, p); err != nil {
		return fmt.Errorf("update proposal: %w", err)
	}
	return nil
}

// IngestProposal persiste un candidate proposal generado externamente (típicamente
// por Companion governance-assist con asistencia LLM). El candidato llega con
// los campos ya armados; Nexus solo valida shape mínima y guarda con status=pending.
// La decisión final sigue siendo humana via accept/dismiss.
func (u *Usecases) IngestProposal(ctx context.Context, candidate learningdomain.PolicyProposal) (learningdomain.PolicyProposal, error) {
	if candidate.ProposedExpression == "" {
		return learningdomain.PolicyProposal{}, fmt.Errorf("proposed_expression is required")
	}
	if candidate.ProposedEffect == "" {
		return learningdomain.PolicyProposal{}, fmt.Errorf("proposed_effect is required")
	}
	if candidate.ProposedName == "" {
		return learningdomain.PolicyProposal{}, fmt.Errorf("proposed_name is required")
	}
	if candidate.ID == uuid.Nil {
		candidate.ID = uuid.New()
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now().UTC()
	}
	candidate.Status = learningdomain.ProposalStatusPending
	candidate.DecidedBy = nil
	candidate.DecidedAt = nil
	candidate.PolicyID = nil
	if candidate.ProposedPriority <= 0 {
		candidate.ProposedPriority = 100
	}
	saved, err := u.repo.CreateProposal(ctx, candidate)
	if err != nil {
		return learningdomain.PolicyProposal{}, fmt.Errorf("create proposal: %w", err)
	}
	return saved, nil
}

// AnalyzeAndPropose detecta patrones y genera propuestas.
// Thresholds del RFC: minSamples=50, minApprovalRate=0.90
func (u *Usecases) AnalyzeAndPropose(ctx context.Context) (int, error) {
	if u.analyzer == nil || u.proposer == nil {
		return 0, errors.New("analyzer or proposer not configured")
	}
	patterns, err := u.analyzer.Analyze(ctx, 14, 50, 0.90)
	if err != nil {
		return 0, fmt.Errorf("analyze patterns: %w", err)
	}
	created := 0
	for _, pattern := range patterns {
		proposal, err := u.proposer.GenerateProposal(ctx, pattern)
		if err != nil {
			slog.Error("generate proposal failed", "error", err, "action_type", pattern.ActionType)
			continue
		}
		if _, err := u.repo.CreateProposal(ctx, *proposal); err != nil {
			slog.Error("save proposal failed", "error", err, "action_type", pattern.ActionType)
			continue
		}
		created++
	}
	return created, nil
}
