package identity

import (
	"context"

	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
)

type RepositoryPort interface {
	Ensure(ctx context.Context, input domain.EnsureInput) (domain.User, error)
	Get(ctx context.Context, id string) (domain.User, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) Ensure(ctx context.Context, input domain.EnsureInput) (domain.User, error) {
	normalized, err := domain.NormalizeEnsureInput(input)
	if err != nil {
		return domain.User{}, err
	}
	return u.repo.Ensure(ctx, normalized)
}

func (u *UseCases) Get(ctx context.Context, id string) (domain.User, error) {
	return u.repo.Get(ctx, id)
}
