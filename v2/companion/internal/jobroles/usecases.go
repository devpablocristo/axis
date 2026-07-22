package jobroles

import (
	"context"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

const (
	ResourceTypeJobRole = "job_role"
	DefaultOrgID        = "default"
	DefaultActorID      = "system"
)

type RepositoryPort interface {
	lifecycle.RepositoryPort

	Create(ctx context.Context, orgID string, input domain.NormalizedCreateInput) (domain.JobRole, error)
	List(ctx context.Context, orgID string, state domain.State) ([]domain.JobRole, error)
	Get(ctx context.Context, orgID string, id uuid.UUID) (domain.JobRole, error)
	Update(ctx context.Context, orgID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.JobRole, error)
}

type UseCases struct {
	repo      RepositoryPort
	lifecycle *lifecycle.Service
}

func NewUseCases(repo RepositoryPort) (*UseCases, error) {
	policy := &lifecycle.LifecyclePolicy{
		ResourceType:  ResourceTypeJobRole,
		AllowArchive:  true,
		AllowTrash:    true,
		AllowPurge:    true,
		RequireReason: false,
		RetentionDays: 30,
	}
	service, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{ResourceTypeJobRole: repo},
		noopLifecycleAudit{},
		lifecycle.NewStaticPolicyRegistry(policy),
	)
	if err != nil {
		return nil, err
	}
	return &UseCases{repo: repo, lifecycle: service}, nil
}

func (u *UseCases) Create(ctx context.Context, orgID string, input domain.CreateInput) (domain.JobRole, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.JobRole{}, err
	}
	return u.repo.Create(ctx, normalizeOrgID(orgID), normalized)
}

func (u *UseCases) ListActive(ctx context.Context, orgID string) ([]domain.JobRole, error) {
	return u.repo.List(ctx, normalizeOrgID(orgID), domain.StateActive)
}

func (u *UseCases) ListArchived(ctx context.Context, orgID string) ([]domain.JobRole, error) {
	return u.repo.List(ctx, normalizeOrgID(orgID), domain.StateArchived)
}

func (u *UseCases) ListTrash(ctx context.Context, orgID string) ([]domain.JobRole, error) {
	return u.repo.List(ctx, normalizeOrgID(orgID), domain.StateTrashed)
}

func (u *UseCases) Get(ctx context.Context, orgID string, id uuid.UUID) (domain.JobRole, error) {
	return u.repo.Get(ctx, normalizeOrgID(orgID), id)
}

func (u *UseCases) Update(ctx context.Context, orgID string, id uuid.UUID, input domain.UpdateInput) (domain.JobRole, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.JobRole{}, err
	}
	return u.repo.Update(ctx, normalizeOrgID(orgID), id, normalized)
}

func (u *UseCases) Archive(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Archive(ctx, &lifecycle.ArchiveRequest{
		ResourceType: ResourceTypeJobRole,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Unarchive(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Unarchive(ctx, &lifecycle.UnarchiveRequest{
		ResourceType: ResourceTypeJobRole,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Trash(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Trash(ctx, &lifecycle.TrashRequest{
		ResourceType: ResourceTypeJobRole,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Restore(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: ResourceTypeJobRole,
		ResourceID:   id,
		TenantID:     normalizeOrgID(orgID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Purge(ctx context.Context, orgID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Purge(ctx, &lifecycle.PurgeRequest{
		ResourceType:  ResourceTypeJobRole,
		ResourceID:    id,
		TenantID:      normalizeOrgID(orgID),
		Actor:         normalizeActor(actor),
		Reason:        strings.TrimSpace(reason),
		MustBeTrashed: true,
	})
}

func (u *UseCases) EnsureActive(ctx context.Context, orgID string, id uuid.UUID) error {
	role, err := u.repo.Get(ctx, normalizeOrgID(orgID), id)
	if err != nil {
		return err
	}
	if role.State() != domain.StateActive {
		return domainerr.Conflict("job role is not active")
	}
	return nil
}

func normalizeOrgID(orgID string) string {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return DefaultOrgID
	}
	return orgID
}

func normalizeActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return DefaultActorID
	}
	return actor
}

type noopLifecycleAudit struct{}

func (noopLifecycleAudit) Append(context.Context, lifecycle.AuditEvent) error {
	return nil
}
