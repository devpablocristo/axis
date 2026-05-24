package agentfleet

import (
	"context"
	"fmt"
)

type Repository interface {
	ListAgents(ctx context.Context, orgID, productSurface string) ([]Agent, error)
	GetAgent(ctx context.Context, orgID, productSurface, agentID string) (Agent, error)
	SaveAgent(ctx context.Context, agent Agent) (Agent, error)
	DisableAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error)
	CreateHandoff(ctx context.Context, handoff Handoff) (Handoff, error)
	ListHandoffs(ctx context.Context, orgID, productSurface string, limit int) ([]Handoff, error)
	UpdateHandoffStatus(ctx context.Context, orgID, productSurface, handoffID, status, changedBy string) (Handoff, error)
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) ListAgents(ctx context.Context, orgID, productSurface string) ([]Agent, error) {
	return u.repo.ListAgents(ctx, orgID, productSurface)
}

func (u *Usecases) GetAgent(ctx context.Context, orgID, productSurface, agentID string) (Agent, error) {
	return u.repo.GetAgent(ctx, orgID, productSurface, agentID)
}

func (u *Usecases) SaveAgent(ctx context.Context, agent Agent) (Agent, error) {
	agent = normalizeAgent(agent)
	if err := validateAgent(agent); err != nil {
		return Agent{}, fmt.Errorf("%w: org_id, agent_id, status and max_autonomy are required", err)
	}
	return u.repo.SaveAgent(ctx, agent)
}

func (u *Usecases) DisableAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	return u.repo.DisableAgent(ctx, orgID, productSurface, agentID, changedBy)
}

func (u *Usecases) CreateHandoff(ctx context.Context, handoff Handoff) (Handoff, error) {
	handoff = normalizeHandoff(handoff)
	if err := validateHandoff(handoff); err != nil {
		return Handoff{}, fmt.Errorf("%w: handoff requires distinct source and target agents", err)
	}
	source, err := u.repo.GetAgent(ctx, handoff.OrgID, handoff.ProductSurface, handoff.FromAgentID)
	if err != nil {
		return Handoff{}, fmt.Errorf("source agent: %w", err)
	}
	if source.Status != StatusActive {
		return Handoff{}, fmt.Errorf("%w: source agent is disabled", ErrValidation)
	}
	target, err := u.repo.GetAgent(ctx, handoff.OrgID, handoff.ProductSurface, handoff.ToAgentID)
	if err != nil {
		return Handoff{}, fmt.Errorf("target agent: %w", err)
	}
	if target.Status != StatusActive {
		return Handoff{}, fmt.Errorf("%w: target agent is disabled", ErrValidation)
	}
	return u.repo.CreateHandoff(ctx, handoff)
}

func (u *Usecases) ListHandoffs(ctx context.Context, orgID, productSurface string, limit int) ([]Handoff, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return u.repo.ListHandoffs(ctx, orgID, productSurface, limit)
}

func (u *Usecases) UpdateHandoffStatus(ctx context.Context, orgID, productSurface, handoffID, status, changedBy string) (Handoff, error) {
	switch status {
	case HandoffAccepted, HandoffRejected, HandoffCompleted, HandoffCancelled:
	default:
		return Handoff{}, fmt.Errorf("%w: invalid handoff status", ErrValidation)
	}
	return u.repo.UpdateHandoffStatus(ctx, orgID, productSurface, handoffID, status, changedBy)
}
