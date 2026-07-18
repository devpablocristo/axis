package executionstats

import "context"

type RepositoryPort interface {
	TraceRows(ctx context.Context, tenantID string) ([]TraceRow, error)
	ExecutionRows(ctx context.Context, tenantID string) ([]ExecutionRow, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

// List returns the per-capability stats for the tenant, sorted by key.
func (u *UseCases) List(ctx context.Context, tenantID string) ([]CapabilityStats, error) {
	traces, err := u.repo.TraceRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	executions, err := u.repo.ExecutionRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return Merge(traces, executions), nil
}
