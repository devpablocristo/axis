package memories

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Repository interface {
	ListMemories(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]Memory, error)
	GetMemory(ctx context.Context, tenantID, orgID, productSurface, memoryID string) (Memory, error)
	CreateMemory(ctx context.Context, memory Memory, actorID string) (Memory, error)
	UpdateMemory(ctx context.Context, memory Memory, actorID string) (Memory, error)
	SetMemoryStatus(ctx context.Context, tenantID, orgID, productSurface, memoryID string, status MemoryStatus, actorID string) (Memory, error)
	ListEntries(ctx context.Context, tenantID, orgID, productSurface, memoryID string) ([]MemoryEntry, error)
	CreateEntry(ctx context.Context, tenantID, orgID, productSurface, memoryID string, entry MemoryEntry, actorID string) (MemoryEntry, error)
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) ListMemories(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]Memory, error) {
	if tenantID == "" || orgID == "" || productSurface == "" {
		return nil, fmt.Errorf("%w: tenant_id, org_id and product_surface are required", ErrValidation)
	}
	return u.repo.ListMemories(ctx, tenantID, orgID, productSurface, normalizeLifecycle(lifecycle))
}

func (u *Usecases) GetMemory(ctx context.Context, tenantID, orgID, productSurface, memoryID string) (Memory, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || memoryID == "" {
		return Memory{}, fmt.Errorf("%w: tenant_id, org_id, product_surface and memory_id are required", ErrValidation)
	}
	return u.repo.GetMemory(ctx, tenantID, orgID, productSurface, memoryID)
}

func (u *Usecases) CreateMemory(ctx context.Context, memory Memory, actorID string) (Memory, error) {
	memory = normalizeMemory(memory)
	if err := validateMemory(memory); err != nil {
		return Memory{}, err
	}
	if memory.Status == MemoryStatusArchived {
		return Memory{}, fmt.Errorf("%w: create cannot set archived status", ErrValidation)
	}
	return u.repo.CreateMemory(ctx, memory, actorID)
}

func (u *Usecases) UpdateMemory(ctx context.Context, memory Memory, actorID string) (Memory, error) {
	memory = normalizeMemory(memory)
	if err := validateMemory(memory); err != nil {
		return Memory{}, err
	}
	if memory.MemoryID == uuid.Nil {
		return Memory{}, fmt.Errorf("%w: memory_id is required", ErrValidation)
	}
	if memory.Status == MemoryStatusArchived {
		return Memory{}, fmt.Errorf("%w: update cannot set archived status; use status endpoint", ErrValidation)
	}
	return u.repo.UpdateMemory(ctx, memory, actorID)
}

func (u *Usecases) SetMemoryStatus(ctx context.Context, tenantID, orgID, productSurface, memoryID string, status MemoryStatus, actorID string) (Memory, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || memoryID == "" {
		return Memory{}, fmt.Errorf("%w: tenant_id, org_id, product_surface and memory_id are required", ErrValidation)
	}
	if status != MemoryStatusActive && status != MemoryStatusDisabled && status != MemoryStatusArchived {
		return Memory{}, fmt.Errorf("%w: invalid memory status", ErrValidation)
	}
	return u.repo.SetMemoryStatus(ctx, tenantID, orgID, productSurface, memoryID, status, actorID)
}

func (u *Usecases) ListEntries(ctx context.Context, tenantID, orgID, productSurface, memoryID string) ([]MemoryEntry, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || memoryID == "" {
		return nil, fmt.Errorf("%w: tenant_id, org_id, product_surface and memory_id are required", ErrValidation)
	}
	return u.repo.ListEntries(ctx, tenantID, orgID, productSurface, memoryID)
}

func (u *Usecases) CreateEntry(ctx context.Context, tenantID, orgID, productSurface, memoryID string, entry MemoryEntry, actorID string) (MemoryEntry, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || memoryID == "" {
		return MemoryEntry{}, fmt.Errorf("%w: tenant_id, org_id, product_surface and memory_id are required", ErrValidation)
	}
	entry = normalizeEntry(entry)
	if entry.Confidence < 0 || entry.Confidence > 1 {
		return MemoryEntry{}, fmt.Errorf("%w: confidence must be between 0 and 1", ErrValidation)
	}
	if entry.ContentText == "" {
		return MemoryEntry{}, fmt.Errorf("%w: content_text is required", ErrValidation)
	}
	return u.repo.CreateEntry(ctx, tenantID, orgID, productSurface, memoryID, entry, actorID)
}

func validateMemory(memory Memory) error {
	if memory.TenantID == uuid.Nil || memory.OrgID == "" || memory.ProductSurface == "" {
		return fmt.Errorf("%w: tenant_id, org_id and product_surface are required", ErrValidation)
	}
	if memory.Status != MemoryStatusActive && memory.Status != MemoryStatusDisabled && memory.Status != MemoryStatusArchived {
		return fmt.Errorf("%w: invalid memory status", ErrValidation)
	}
	if memory.Policy.RetentionDays < 0 {
		return fmt.Errorf("%w: retention_days must be greater than or equal to zero", ErrValidation)
	}
	return nil
}

func normalizeLifecycle(lifecycle string) string {
	switch lifecycle {
	case "archived", "all":
		return lifecycle
	default:
		return "active"
	}
}
