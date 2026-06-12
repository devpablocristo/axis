package policies

import (
	"context"
	"fmt"

	policydomain "github.com/devpablocristo/nexus/internal/policies/usecases/domain"
	"github.com/google/uuid"
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

func (u *Usecases) ListVersions(ctx context.Context, id uuid.UUID) ([]PolicyVersion, error) {
	repo, ok := u.repo.(interface {
		ListVersions(context.Context, uuid.UUID) ([]PolicyVersion, error)
	})
	if !ok {
		return nil, ErrNotFound
	}
	return repo.ListVersions(ctx, id)
}

func (u *Usecases) RequestPromotion(ctx context.Context, legacyPolicyID uuid.UUID, toVersionID uuid.UUID, actorID, reason string, dryRunReport map[string]any) (PolicyPromotion, error) {
	repo, ok := u.repo.(interface {
		RequestPromotion(context.Context, uuid.UUID, uuid.UUID, string, string, map[string]any) (PolicyPromotion, error)
	})
	if !ok {
		return PolicyPromotion{}, ErrNotFound
	}
	return repo.RequestPromotion(ctx, legacyPolicyID, toVersionID, actorID, reason, dryRunReport)
}

func (u *Usecases) ApprovePromotion(ctx context.Context, promotionID uuid.UUID, actorID string) (PolicyPromotion, error) {
	repo, ok := u.repo.(interface {
		ApprovePromotion(context.Context, uuid.UUID, string) (PolicyPromotion, error)
	})
	if !ok {
		return PolicyPromotion{}, ErrNotFound
	}
	return repo.ApprovePromotion(ctx, promotionID, actorID)
}

func (u *Usecases) EnforcePromotion(ctx context.Context, promotionID uuid.UUID, actorID string) (PolicyPromotion, error) {
	repo, ok := u.repo.(interface {
		EnforcePromotion(context.Context, uuid.UUID, string) (PolicyPromotion, error)
	})
	if !ok {
		return PolicyPromotion{}, ErrNotFound
	}
	return repo.EnforcePromotion(ctx, promotionID, actorID)
}

func (u *Usecases) RollbackPromotion(ctx context.Context, promotionID uuid.UUID, actorID, reason string) (PolicyPromotion, error) {
	repo, ok := u.repo.(interface {
		RollbackPromotion(context.Context, uuid.UUID, string, string) (PolicyPromotion, error)
	})
	if !ok {
		return PolicyPromotion{}, ErrNotFound
	}
	return repo.RollbackPromotion(ctx, promotionID, actorID, reason)
}

func (u *Usecases) ListPromotions(ctx context.Context, legacyPolicyID uuid.UUID) ([]PolicyPromotion, error) {
	repo, ok := u.repo.(interface {
		ListPromotions(context.Context, uuid.UUID) ([]PolicyPromotion, error)
	})
	if !ok {
		return nil, ErrNotFound
	}
	return repo.ListPromotions(ctx, legacyPolicyID)
}

func (u *Usecases) GetPromotion(ctx context.Context, promotionID uuid.UUID) (PolicyPromotion, error) {
	repo, ok := u.repo.(interface {
		GetPromotion(context.Context, uuid.UUID) (PolicyPromotion, error)
	})
	if !ok {
		return PolicyPromotion{}, ErrNotFound
	}
	return repo.GetPromotion(ctx, promotionID)
}

func (u *Usecases) ListChangelog(ctx context.Context, legacyPolicyID uuid.UUID) ([]PolicyChangelogEntry, error) {
	repo, ok := u.repo.(interface {
		ListChangelog(context.Context, uuid.UUID) ([]PolicyChangelogEntry, error)
	})
	if !ok {
		return nil, ErrNotFound
	}
	return repo.ListChangelog(ctx, legacyPolicyID)
}
