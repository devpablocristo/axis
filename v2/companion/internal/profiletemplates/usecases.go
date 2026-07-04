package profiletemplates

import (
	"context"
	"fmt"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

const (
	ResourceTypeProfileTemplate = "profile_template"
	DefaultTenantID             = "default"
	DefaultActorID              = "system"
)

type RepositoryPort interface {
	lifecycle.RepositoryPort

	Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.ProfileTemplate, error)
	List(ctx context.Context, tenantID string, state domain.State) ([]domain.ProfileTemplate, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.ProfileTemplate, error)
	Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ProfileTemplate, error)
	HasActiveVirployeeAssignments(ctx context.Context, tenantID string, id uuid.UUID) (bool, error)
}

type UseCases struct {
	repo      RepositoryPort
	lifecycle *lifecycle.Service
}

func NewUseCases(repo RepositoryPort) (*UseCases, error) {
	policy := &lifecycle.LifecyclePolicy{
		ResourceType:  ResourceTypeProfileTemplate,
		AllowArchive:  true,
		AllowTrash:    true,
		AllowPurge:    true,
		RequireReason: false,
		RetentionDays: 30,
	}
	service, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{ResourceTypeProfileTemplate: repo},
		noopLifecycleAudit{},
		lifecycle.NewStaticPolicyRegistry(policy),
	)
	if err != nil {
		return nil, err
	}
	return &UseCases{repo: repo, lifecycle: service}, nil
}

func (u *UseCases) Create(ctx context.Context, tenantID string, input domain.CreateInput) (domain.ProfileTemplate, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.ProfileTemplate{}, err
	}
	return u.repo.Create(ctx, normalizeTenantID(tenantID), normalized)
}

func (u *UseCases) ListActive(ctx context.Context, tenantID string) ([]domain.ProfileTemplate, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateActive)
}

func (u *UseCases) ListArchived(ctx context.Context, tenantID string) ([]domain.ProfileTemplate, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateArchived)
}

func (u *UseCases) ListTrash(ctx context.Context, tenantID string) ([]domain.ProfileTemplate, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateTrashed)
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.ProfileTemplate, error) {
	return u.repo.Get(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.UpdateInput) (domain.ProfileTemplate, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.ProfileTemplate{}, err
	}
	return u.repo.Update(ctx, normalizeTenantID(tenantID), id, normalized)
}

func (u *UseCases) Archive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	tenantID = normalizeTenantID(tenantID)
	if err := u.ensureNotAssigned(ctx, tenantID, id); err != nil {
		return err
	}
	return u.lifecycle.Archive(ctx, &lifecycle.ArchiveRequest{
		ResourceType: ResourceTypeProfileTemplate,
		ResourceID:   id,
		TenantID:     tenantID,
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Unarchive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Unarchive(ctx, &lifecycle.UnarchiveRequest{
		ResourceType: ResourceTypeProfileTemplate,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Trash(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	tenantID = normalizeTenantID(tenantID)
	if err := u.ensureNotAssigned(ctx, tenantID, id); err != nil {
		return err
	}
	return u.lifecycle.Trash(ctx, &lifecycle.TrashRequest{
		ResourceType: ResourceTypeProfileTemplate,
		ResourceID:   id,
		TenantID:     tenantID,
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Restore(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: ResourceTypeProfileTemplate,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Purge(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	tenantID = normalizeTenantID(tenantID)
	if err := u.ensureNotAssigned(ctx, tenantID, id); err != nil {
		return err
	}
	return u.lifecycle.Purge(ctx, &lifecycle.PurgeRequest{
		ResourceType:  ResourceTypeProfileTemplate,
		ResourceID:    id,
		TenantID:      tenantID,
		Actor:         normalizeActor(actor),
		Reason:        strings.TrimSpace(reason),
		MustBeTrashed: true,
	})
}

func (u *UseCases) EnsureUsableByVirployee(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	autonomy virployeedomain.AutonomyLevel,
) error {
	profile, err := u.repo.Get(ctx, normalizeTenantID(tenantID), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domainerr.Validation("profile_template_id must reference an active profile template in the same tenant")
		}
		return err
	}
	if profile.State() != domain.StateActive {
		return domainerr.Validation("profile_template_id must reference an active profile template in the same tenant")
	}
	if !profile.MaxAutonomy.Allows(autonomy) {
		return domainerr.Validation(fmt.Sprintf(
			"profile template %s allows max autonomy %s; virployee autonomy %s exceeds it",
			profile.Name,
			profile.MaxAutonomy,
			autonomy,
		))
	}
	return nil
}

func (u *UseCases) ensureNotAssigned(ctx context.Context, tenantID string, id uuid.UUID) error {
	assigned, err := u.repo.HasActiveVirployeeAssignments(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if assigned {
		return domainerr.Conflict("profile template is assigned to active virployees")
	}
	return nil
}

func normalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return DefaultTenantID
	}
	return tenantID
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
