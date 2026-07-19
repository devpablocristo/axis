package learning

import (
	"context"

	"github.com/google/uuid"
)

type RepositoryPort interface {
	Create(ctx context.Context, tenantID string, input NormalizedCreateInput) (Proposal, error)
	List(ctx context.Context, tenantID, status string, virployeeID *uuid.UUID) ([]Proposal, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (Proposal, error)
	HasPending(ctx context.Context, tenantID string, virployeeID uuid.UUID, capabilityKey string) (bool, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
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
