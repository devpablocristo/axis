package jobroles

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Repository interface {
	ListJobRoles(ctx context.Context, orgID, productSurface string, lifecycle LifecycleView) ([]JobRole, error)
	GetJobRole(ctx context.Context, orgID, productSurface, jobRoleID string) (JobRole, error)
	UpsertJobRole(ctx context.Context, role JobRole) (JobRole, error)
	ArchiveJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error)
	TrashJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error)
	RestoreJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error)
	ListVersions(ctx context.Context, orgID, productSurface, jobRoleID string, limit int) ([]Version, error)
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) ListJobRoles(ctx context.Context, orgID, productSurface, lifecycle string, includeArchived bool) ([]JobRole, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	if orgID == "" || productSurface == "" {
		return nil, fmt.Errorf("%w: org_id and product_surface are required", ErrValidation)
	}
	return u.repo.ListJobRoles(ctx, orgID, productSurface, normalizeLifecycleView(lifecycle, includeArchived))
}

func (u *Usecases) GetJobRole(ctx context.Context, orgID, productSurface, jobRoleID string) (JobRole, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	jobRoleID = strings.TrimSpace(jobRoleID)
	if orgID == "" || productSurface == "" || jobRoleID == "" {
		return JobRole{}, fmt.Errorf("%w: org_id, product_surface and job_role_id are required", ErrValidation)
	}
	return u.repo.GetJobRole(ctx, orgID, productSurface, jobRoleID)
}

func (u *Usecases) UpsertJobRole(ctx context.Context, role JobRole) (JobRole, error) {
	role = normalizeJobRole(role)
	if err := validateJobRole(role); err != nil {
		return JobRole{}, fmt.Errorf("%w: org_id, product_surface, name, slug, status and default_autonomy_level are required", err)
	}
	if role.Status == "archived" || role.Status == "trash" {
		return JobRole{}, fmt.Errorf("%w: create/update cannot set lifecycle status; use lifecycle endpoints", ErrValidation)
	}
	if role.JobRoleID == "" {
		existing, err := u.repo.GetJobRole(ctx, role.OrgID, role.ProductSurface, role.Slug)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return JobRole{}, err
		}
		if err == nil && existing.JobRoleID != "" {
			return JobRole{}, fmt.Errorf("%w: slug already exists", ErrConflict)
		}
	} else {
		existing, err := u.repo.GetJobRole(ctx, role.OrgID, role.ProductSurface, role.JobRoleID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return JobRole{}, err
		}
		if err == nil {
			if existing.Status == "archived" || existing.Status == "trash" {
				return JobRole{}, fmt.Errorf("%w: job role is not active; restore it before updating", ErrConflict)
			}
			role.ID = existing.ID
			role.JobRoleKey = existing.JobRoleKey
		}
	}
	return u.repo.UpsertJobRole(ctx, role)
}

func (u *Usecases) ArchiveJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	jobRoleID = strings.TrimSpace(jobRoleID)
	if orgID == "" || productSurface == "" || jobRoleID == "" {
		return JobRole{}, fmt.Errorf("%w: org_id, product_surface and job_role_id are required", ErrValidation)
	}
	return u.repo.ArchiveJobRole(ctx, orgID, productSurface, jobRoleID, strings.TrimSpace(actorID))
}

func (u *Usecases) TrashJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	jobRoleID = strings.TrimSpace(jobRoleID)
	if orgID == "" || productSurface == "" || jobRoleID == "" {
		return JobRole{}, fmt.Errorf("%w: org_id, product_surface and job_role_id are required", ErrValidation)
	}
	return u.repo.TrashJobRole(ctx, orgID, productSurface, jobRoleID, strings.TrimSpace(actorID))
}

func (u *Usecases) RestoreJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	jobRoleID = strings.TrimSpace(jobRoleID)
	if orgID == "" || productSurface == "" || jobRoleID == "" {
		return JobRole{}, fmt.Errorf("%w: org_id, product_surface and job_role_id are required", ErrValidation)
	}
	return u.repo.RestoreJobRole(ctx, orgID, productSurface, jobRoleID, strings.TrimSpace(actorID))
}

func (u *Usecases) ListVersions(ctx context.Context, orgID, productSurface, jobRoleID string, limit int) ([]Version, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	jobRoleID = strings.TrimSpace(jobRoleID)
	if orgID == "" || productSurface == "" || jobRoleID == "" {
		return nil, fmt.Errorf("%w: org_id, product_surface and job_role_id are required", ErrValidation)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return u.repo.ListVersions(ctx, orgID, productSurface, jobRoleID, limit)
}
