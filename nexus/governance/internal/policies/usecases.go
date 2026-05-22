package policies

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	policydomain "github.com/devpablocristo/nexus/governance/internal/policies/usecases/domain"
)

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) Create(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	return u.repo.Create(ctx, p)
}

func (u *Usecases) GetByID(ctx context.Context, id uuid.UUID) (policydomain.Policy, error) {
	return u.repo.GetByID(ctx, id)
}

func (u *Usecases) List(ctx context.Context, filters ListFilters) ([]policydomain.Policy, error) {
	return u.repo.List(ctx, filters)
}

// ListActive retorna políticas activas (no archivadas, habilitadas) para evaluación.
// orgID filtra por organización (globales + de la org). nil = todas.
func (u *Usecases) ListActive(ctx context.Context, orgID *string) ([]policydomain.Policy, error) {
	return u.repo.List(ctx, ListFilters{OrgID: orgID, EnabledOnly: true})
}

func (u *Usecases) Update(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	return u.repo.Update(ctx, p)
}

func (u *Usecases) DeleteByID(ctx context.Context, id uuid.UUID) error {
	if err := u.repo.DeleteByID(ctx, id); err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	return nil
}

func (u *Usecases) ArchiveByID(ctx context.Context, id uuid.UUID) error {
	if err := u.repo.ArchiveByID(ctx, id); err != nil {
		return fmt.Errorf("archive policy: %w", err)
	}
	return nil
}

func (u *Usecases) RestoreByID(ctx context.Context, id uuid.UUID) error {
	if err := u.repo.RestoreByID(ctx, id); err != nil {
		return fmt.Errorf("restore policy: %w", err)
	}
	return nil
}
