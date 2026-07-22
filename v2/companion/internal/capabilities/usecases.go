package capabilities

import (
	"context"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/secrets"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

const (
	ResourceTypeCapability = "capability"
	DefaultTenantID        = "default"
	DefaultActorID         = "system"
)

type RepositoryPort interface {
	lifecycle.RepositoryPort

	Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.Capability, error)
	List(ctx context.Context, tenantID string, state domain.State) ([]domain.Capability, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Capability, error)
	Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Capability, error)
	UpdateManifest(ctx context.Context, tenantID string, id uuid.UUID, manifest domain.Manifest, manifestHash string) (domain.Capability, error)
	SaveConformance(ctx context.Context, tenantID string, id uuid.UUID, expected domain.Capability, report domain.ConformanceReport) (domain.Capability, error)
	Activate(ctx context.Context, tenantID string, id uuid.UUID, manifestHash string) (domain.Capability, error)
	HasActiveVirployeeAssignments(ctx context.Context, tenantID string, id uuid.UUID) (bool, error)
}

type UseCases struct {
	repo      RepositoryPort
	lifecycle *lifecycle.Service
	quotas    QuotaPolicyChecker
}

func (u *UseCases) SetQuotaPolicyChecker(checker QuotaPolicyChecker) { u.quotas = checker }

func NewUseCases(repo RepositoryPort) (*UseCases, error) {
	policy := &lifecycle.LifecyclePolicy{
		ResourceType:  ResourceTypeCapability,
		AllowArchive:  true,
		AllowTrash:    true,
		AllowPurge:    true,
		RequireReason: false,
		RetentionDays: 30,
	}
	service, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{ResourceTypeCapability: repo},
		noopLifecycleAudit{},
		lifecycle.NewStaticPolicyRegistry(policy),
	)
	if err != nil {
		return nil, err
	}
	return &UseCases{repo: repo, lifecycle: service}, nil
}

func (u *UseCases) Create(ctx context.Context, tenantID string, input domain.CreateInput) (domain.Capability, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Capability{}, err
	}
	return u.repo.Create(ctx, normalizeTenantID(tenantID), normalized)
}

func (u *UseCases) ListActive(ctx context.Context, tenantID string) ([]domain.Capability, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateActive)
}

func (u *UseCases) ListArchived(ctx context.Context, tenantID string) ([]domain.Capability, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateArchived)
}

func (u *UseCases) ListTrash(ctx context.Context, tenantID string) ([]domain.Capability, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateTrashed)
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Capability, error) {
	return u.repo.Get(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.UpdateInput) (domain.Capability, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Capability{}, err
	}
	return u.repo.Update(ctx, normalizeTenantID(tenantID), id, normalized)
}

func (u *UseCases) UpdateManifest(ctx context.Context, tenantID string, id uuid.UUID, input domain.ManifestInput) (domain.Capability, error) {
	tenantID = normalizeTenantID(tenantID)
	capability, err := u.repo.Get(ctx, tenantID, id)
	if err != nil {
		return domain.Capability{}, err
	}
	if capability.State() != domain.StateActive {
		return domain.Capability{}, domainerr.Conflict("capability is not lifecycle-active")
	}
	manifest, manifestHash, err := domain.NormalizeManifest(input)
	if err != nil {
		return domain.Capability{}, err
	}
	for _, ref := range manifest.SecretRefs {
		if !secrets.ValidRef(ref) {
			return domain.Capability{}, domainerr.Validation("secret_refs must contain only Secret Manager references")
		}
	}
	return u.repo.UpdateManifest(ctx, tenantID, id, manifest, manifestHash)
}

func (u *UseCases) Conform(ctx context.Context, tenantID string, id uuid.UUID) (domain.Capability, domain.ConformanceReport, error) {
	tenantID = normalizeTenantID(tenantID)
	capability, err := u.repo.Get(ctx, tenantID, id)
	if err != nil {
		return domain.Capability{}, domain.ConformanceReport{}, err
	}
	if capability.State() != domain.StateActive {
		return domain.Capability{}, domain.ConformanceReport{}, domainerr.Conflict("capability is not lifecycle-active")
	}
	report, err := validateConformance(ctx, capability, u.quotas)
	if err != nil {
		return domain.Capability{}, domain.ConformanceReport{}, err
	}
	updated, err := u.repo.SaveConformance(ctx, tenantID, id, capability, report)
	return updated, report, err
}

func (u *UseCases) Activate(ctx context.Context, tenantID string, id uuid.UUID) (domain.Capability, domain.ConformanceReport, error) {
	tenantID = normalizeTenantID(tenantID)
	capability, err := u.repo.Get(ctx, tenantID, id)
	if err != nil {
		return domain.Capability{}, domain.ConformanceReport{}, err
	}
	if capability.PromotionState != domain.PromotionConformant || capability.ManifestHash == "" || capability.ConformedHash != capability.ManifestHash {
		return domain.Capability{}, capability.ConformanceReport, domainerr.Conflict("capability must be conformant for its current manifest before activation")
	}
	report, err := validateConformance(ctx, capability, u.quotas)
	if err != nil {
		return domain.Capability{}, domain.ConformanceReport{}, err
	}
	if !report.Conformant {
		updated, saveErr := u.repo.SaveConformance(ctx, tenantID, id, capability, report)
		return updated, report, saveErr
	}
	activated, err := u.repo.Activate(ctx, tenantID, id, capability.ManifestHash)
	return activated, report, err
}

func (u *UseCases) Archive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	tenantID = normalizeTenantID(tenantID)
	if err := u.ensureNotAssigned(ctx, tenantID, id); err != nil {
		return err
	}
	return u.lifecycle.Archive(ctx, &lifecycle.ArchiveRequest{
		ResourceType: ResourceTypeCapability,
		ResourceID:   id,
		TenantID:     tenantID,
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Unarchive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Unarchive(ctx, &lifecycle.UnarchiveRequest{
		ResourceType: ResourceTypeCapability,
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
		ResourceType: ResourceTypeCapability,
		ResourceID:   id,
		TenantID:     tenantID,
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Restore(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: ResourceTypeCapability,
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
		ResourceType:  ResourceTypeCapability,
		ResourceID:    id,
		TenantID:      tenantID,
		Actor:         normalizeActor(actor),
		Reason:        strings.TrimSpace(reason),
		MustBeTrashed: true,
	})
}

func (u *UseCases) EnsureAssignable(ctx context.Context, tenantID string, ids []uuid.UUID, autonomy virployeedomain.AutonomyLevel) error {
	tenantID = normalizeTenantID(tenantID)
	for _, id := range ids {
		capability, err := u.repo.Get(ctx, tenantID, id)
		if err != nil {
			if domainerr.IsNotFound(err) {
				return domainerr.Validation("capability_ids must reference active capabilities in the same tenant")
			}
			return err
		}
		if capability.State() != domain.StateActive {
			return domainerr.Validation("capability_ids must reference active capabilities in the same tenant")
		}
		if capability.PromotionState != domain.PromotionActive {
			return domainerr.Validation("capability_ids must reference conformant and promoted capabilities in the same tenant")
		}
		if !autonomy.Allows(capability.RequiredAutonomy) {
			return domainerr.Validation("capability " + capability.CapabilityKey + " requires autonomy " + string(capability.RequiredAutonomy) + "; virployee autonomy " + string(autonomy) + " does not allow it")
		}
	}
	return nil
}

func (u *UseCases) ensureNotAssigned(ctx context.Context, tenantID string, id uuid.UUID) error {
	assigned, err := u.repo.HasActiveVirployeeAssignments(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if assigned {
		return domainerr.Conflict("capability is assigned to active virployees")
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
