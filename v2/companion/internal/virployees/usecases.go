package virployees

import (
	"context"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

const (
	ResourceTypeVirployee = "virployee"
	DefaultTenantID       = "default"
	DefaultActorID        = "system"
)

type RepositoryPort interface {
	lifecycle.RepositoryPort

	Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.Virployee, error)
	List(ctx context.Context, tenantID string, state domain.State) ([]domain.Virployee, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Virployee, error)
	Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error)
	Trash(ctx context.Context, tenantID string, id uuid.UUID, at time.Time, purgeAfter *time.Time) error
	RestoreTrashed(ctx context.Context, tenantID string, id uuid.UUID) error
	State(ctx context.Context, tenantID string, id uuid.UUID) (domain.State, error)
}

type UseCases struct {
	repo      RepositoryPort
	lifecycle *lifecycle.Service
}

func NewUseCases(repo RepositoryPort) (*UseCases, error) {
	policy := &lifecycle.ArchivePolicy{
		ResourceType:    ResourceTypeVirployee,
		AllowArchive:    true,
		AllowHardDelete: true,
		RequireReason:   false,
		RetentionDays:   30,
	}
	service, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{ResourceTypeVirployee: repo},
		noopLifecycleAudit{},
		lifecycle.NewStaticPolicyRegistry(policy),
	)
	if err != nil {
		return nil, err
	}
	return &UseCases{repo: repo, lifecycle: service}, nil
}

func (u *UseCases) Create(ctx context.Context, input domain.CreateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Create(ctx, DefaultTenantID, normalized)
}

func (u *UseCases) ListActive(ctx context.Context) ([]domain.Virployee, error) {
	return u.repo.List(ctx, DefaultTenantID, domain.StateActive)
}

func (u *UseCases) ListArchived(ctx context.Context) ([]domain.Virployee, error) {
	return u.repo.List(ctx, DefaultTenantID, domain.StateArchived)
}

func (u *UseCases) ListTrash(ctx context.Context) ([]domain.Virployee, error) {
	return u.repo.List(ctx, DefaultTenantID, domain.StateTrashed)
}

func (u *UseCases) Get(ctx context.Context, id uuid.UUID) (domain.Virployee, error) {
	return u.repo.Get(ctx, DefaultTenantID, id)
}

func (u *UseCases) Update(ctx context.Context, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Update(ctx, DefaultTenantID, id, normalized)
}

func (u *UseCases) Archive(ctx context.Context, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.SoftDelete(ctx, &lifecycle.ArchiveRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     DefaultTenantID,
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Unarchive(ctx context.Context, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     DefaultTenantID,
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Trash(ctx context.Context, id uuid.UUID, actor, reason string) error {
	now := time.Now().UTC()
	purgeAfter := now.AddDate(0, 0, 30)
	return u.repo.Trash(ctx, DefaultTenantID, id, now, &purgeAfter)
}

func (u *UseCases) Restore(ctx context.Context, id uuid.UUID, actor, reason string) error {
	return u.repo.RestoreTrashed(ctx, DefaultTenantID, id)
}

func (u *UseCases) Purge(ctx context.Context, id uuid.UUID, actor, reason string) error {
	state, err := u.repo.State(ctx, DefaultTenantID, id)
	if err != nil {
		return err
	}
	if state != domain.StateTrashed {
		return domainerr.Conflict("virployee must be trashed before purge")
	}
	return u.lifecycle.HardDelete(ctx, &lifecycle.HardDeleteRequest{
		ResourceType:   ResourceTypeVirployee,
		ResourceID:     id,
		TenantID:       DefaultTenantID,
		Actor:          normalizeActor(actor),
		Reason:         strings.TrimSpace(reason),
		MustBeArchived: false,
	})
}

func normalizeActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return DefaultActorID
	}
	return actor
}

type noopLifecycleAudit struct{}

func (noopLifecycleAudit) Append(context.Context, lifecycle.ArchiveAudit) error {
	return nil
}
