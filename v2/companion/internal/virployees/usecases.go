package virployees

import (
	"context"
	"fmt"
	"strings"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	profiletemplatedomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
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
}

type JobRoleReaderPort interface {
	EnsureActive(ctx context.Context, tenantID string, id uuid.UUID) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (jobroledomain.JobRole, error)
}

type CapabilityValidatorPort interface {
	EnsureAssignable(ctx context.Context, tenantID string, ids []uuid.UUID, autonomy domain.AutonomyLevel) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (capabilitydomain.Capability, error)
}

type ProfileTemplateReaderPort interface {
	EnsureUsableByVirployee(ctx context.Context, tenantID string, id uuid.UUID, autonomy domain.AutonomyLevel) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (profiletemplatedomain.ProfileTemplate, error)
}

type UseCases struct {
	repo             RepositoryPort
	jobRoles         JobRoleReaderPort
	capabilities     CapabilityValidatorPort
	profileTemplates ProfileTemplateReaderPort
	lifecycle        *lifecycle.Service
}

func NewUseCases(repo RepositoryPort, jobRoles ...JobRoleReaderPort) (*UseCases, error) {
	policy := &lifecycle.LifecyclePolicy{
		ResourceType:  ResourceTypeVirployee,
		AllowArchive:  true,
		AllowTrash:    true,
		AllowPurge:    true,
		RequireReason: false,
		RetentionDays: 30,
	}
	service, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{ResourceTypeVirployee: repo},
		noopLifecycleAudit{},
		lifecycle.NewStaticPolicyRegistry(policy),
	)
	if err != nil {
		return nil, err
	}
	reader := JobRoleReaderPort(noopJobRoleReader{})
	if len(jobRoles) > 0 && jobRoles[0] != nil {
		reader = jobRoles[0]
	}
	return &UseCases{
		repo:             repo,
		jobRoles:         reader,
		capabilities:     noopCapabilityValidator{},
		profileTemplates: noopProfileTemplateReader{},
		lifecycle:        service,
	}, nil
}

func (u *UseCases) SetCapabilityValidator(validator CapabilityValidatorPort) {
	if validator == nil {
		u.capabilities = noopCapabilityValidator{}
		return
	}
	u.capabilities = validator
}

func (u *UseCases) SetProfileTemplateReader(reader ProfileTemplateReaderPort) {
	if reader == nil {
		u.profileTemplates = noopProfileTemplateReader{}
		return
	}
	u.profileTemplates = reader
}

func (u *UseCases) Create(ctx context.Context, tenantID string, input domain.CreateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := u.jobRoles.EnsureActive(ctx, normalizeTenantID(tenantID), normalized.JobRoleID); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.profileTemplates.EnsureUsableByVirployee(ctx, normalizeTenantID(tenantID), normalized.ProfileTemplateID, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.capabilities.EnsureAssignable(ctx, normalizeTenantID(tenantID), normalized.CapabilityIDs, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Create(ctx, normalizeTenantID(tenantID), normalized)
}

func (u *UseCases) ListActive(ctx context.Context, tenantID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateActive)
}

func (u *UseCases) ListArchived(ctx context.Context, tenantID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateArchived)
}

func (u *UseCases) ListTrash(ctx context.Context, tenantID string) ([]domain.Virployee, error) {
	return u.repo.List(ctx, normalizeTenantID(tenantID), domain.StateTrashed)
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Virployee, error) {
	return u.repo.Get(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) RuntimeContext(ctx context.Context, tenantID string, id uuid.UUID) (runtimecontext.Context, error) {
	tenantID = normalizeTenantID(tenantID)
	virployee, err := u.repo.Get(ctx, tenantID, id)
	if err != nil {
		return runtimecontext.Context{}, err
	}

	jobRole, err := u.jobRoles.Get(ctx, tenantID, virployee.JobRoleID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtimecontext.Context{}, domainerr.Validation("job_role_id must reference an active job role in the same tenant")
		}
		return runtimecontext.Context{}, err
	}
	if jobRole.State() != jobroledomain.StateActive {
		return runtimecontext.Context{}, domainerr.Validation("job_role_id must reference an active job role in the same tenant")
	}

	profileTemplate, err := u.profileTemplates.Get(ctx, tenantID, virployee.ProfileTemplateID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return runtimecontext.Context{}, domainerr.Validation("profile_template_id must reference an active profile template in the same tenant")
		}
		return runtimecontext.Context{}, err
	}
	if profileTemplate.State() != profiletemplatedomain.StateActive {
		return runtimecontext.Context{}, domainerr.Validation("profile_template_id must reference an active profile template in the same tenant")
	}
	if !profileTemplate.MaxAutonomy.Allows(virployee.Autonomy) {
		return runtimecontext.Context{}, domainerr.Validation(fmt.Sprintf(
			"profile template %s allows max autonomy %s; virployee autonomy %s exceeds it",
			profileTemplate.Name,
			profileTemplate.MaxAutonomy,
			virployee.Autonomy,
		))
	}

	capabilities := make([]capabilitydomain.Capability, 0, len(virployee.CapabilityIDs))
	for _, capabilityID := range virployee.CapabilityIDs {
		capability, err := u.capabilities.Get(ctx, tenantID, capabilityID)
		if err != nil {
			if domainerr.IsNotFound(err) {
				return runtimecontext.Context{}, domainerr.Validation("capability_ids must reference active capabilities in the same tenant")
			}
			return runtimecontext.Context{}, err
		}
		if capability.State() != capabilitydomain.StateActive {
			return runtimecontext.Context{}, domainerr.Validation("capability_ids must reference active capabilities in the same tenant")
		}
		if !virployee.Autonomy.Allows(capability.RequiredAutonomy) {
			return runtimecontext.Context{}, domainerr.Validation(fmt.Sprintf(
				"capability %s requires autonomy %s; virployee autonomy %s does not allow it",
				capability.CapabilityKey,
				capability.RequiredAutonomy,
				virployee.Autonomy,
			))
		}
		capabilities = append(capabilities, capability)
	}

	return runtimecontext.Context{
		Virployee:       virployee,
		JobRole:         jobRole,
		ProfileTemplate: profileTemplate,
		Capabilities:    capabilities,
	}, nil
}

func (u *UseCases) DryRun(ctx context.Context, tenantID string, id uuid.UUID, input string) (dryrun.Result, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return dryrun.Result{}, domainerr.Validation("input is required")
	}
	runtimeCtx, err := u.RuntimeContext(ctx, tenantID, id)
	if err != nil {
		return dryrun.Result{}, err
	}
	return dryrun.Evaluate(input, runtimeCtx), nil
}

func (u *UseCases) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := u.jobRoles.EnsureActive(ctx, normalizeTenantID(tenantID), normalized.JobRoleID); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.profileTemplates.EnsureUsableByVirployee(ctx, normalizeTenantID(tenantID), normalized.ProfileTemplateID, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	if err := u.capabilities.EnsureAssignable(ctx, normalizeTenantID(tenantID), normalized.CapabilityIDs, normalized.Autonomy); err != nil {
		return domain.Virployee{}, err
	}
	return u.repo.Update(ctx, normalizeTenantID(tenantID), id, normalized)
}

func (u *UseCases) Archive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Archive(ctx, &lifecycle.ArchiveRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Unarchive(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Unarchive(ctx, &lifecycle.UnarchiveRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Trash(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Trash(ctx, &lifecycle.TrashRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Restore(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: ResourceTypeVirployee,
		ResourceID:   id,
		TenantID:     normalizeTenantID(tenantID),
		Actor:        normalizeActor(actor),
		Reason:       strings.TrimSpace(reason),
	})
}

func (u *UseCases) Purge(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string) error {
	return u.lifecycle.Purge(ctx, &lifecycle.PurgeRequest{
		ResourceType:  ResourceTypeVirployee,
		ResourceID:    id,
		TenantID:      normalizeTenantID(tenantID),
		Actor:         normalizeActor(actor),
		Reason:        strings.TrimSpace(reason),
		MustBeTrashed: true,
	})
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

type noopJobRoleReader struct{}

func (noopJobRoleReader) EnsureActive(context.Context, string, uuid.UUID) error {
	return nil
}

func (noopJobRoleReader) Get(context.Context, string, uuid.UUID) (jobroledomain.JobRole, error) {
	return jobroledomain.JobRole{}, domainerr.NotFound("job role not found")
}

type noopCapabilityValidator struct{}

func (noopCapabilityValidator) EnsureAssignable(context.Context, string, []uuid.UUID, domain.AutonomyLevel) error {
	return nil
}

func (noopCapabilityValidator) Get(context.Context, string, uuid.UUID) (capabilitydomain.Capability, error) {
	return capabilitydomain.Capability{}, domainerr.NotFound("capability not found")
}

type noopProfileTemplateReader struct{}

func (noopProfileTemplateReader) EnsureUsableByVirployee(context.Context, string, uuid.UUID, domain.AutonomyLevel) error {
	return nil
}

func (noopProfileTemplateReader) Get(context.Context, string, uuid.UUID) (profiletemplatedomain.ProfileTemplate, error) {
	return profiletemplatedomain.ProfileTemplate{}, domainerr.NotFound("profile template not found")
}
