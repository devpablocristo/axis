package memories

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("memory not found")
	ErrValidation = errors.New("memory validation failed")
	ErrConflict   = errors.New("memory conflict")
)

type MemoryStatus string

const (
	MemoryStatusActive   MemoryStatus = "active"
	MemoryStatusDisabled MemoryStatus = "disabled"
	MemoryStatusArchived MemoryStatus = "archived"
)

type MemoryPolicy struct {
	EnabledByDefault bool `json:"enabled_by_default"`
	RetentionDays    int  `json:"retention_days"`
	AllowUserMemory  bool `json:"allow_user_memory"`
	AllowTaskMemory  bool `json:"allow_task_memory"`
	AllowTenantMemory bool `json:"allow_tenant_memory"`
}

type Memory struct {
	MemoryID        uuid.UUID    `json:"memory_id"`
	TenantID        uuid.UUID    `json:"tenant_id"`
	OrgID           string       `json:"org_id,omitempty"`
	ProductSurface  string       `json:"product_surface,omitempty"`
	OwnerEmployeeID *uuid.UUID   `json:"owner_employee_id,omitempty"`
	Policy          MemoryPolicy `json:"policy"`
	Status          MemoryStatus `json:"status"`
	CreatedAt       time.Time    `json:"created_at,omitempty"`
	UpdatedAt       time.Time    `json:"updated_at,omitempty"`
	ArchivedAt      *time.Time   `json:"archived_at,omitempty"`
	Version         int          `json:"version,omitempty"`
}

type MemoryEntryKind string

const (
	MemoryEntryKindFact       MemoryEntryKind = "fact"
	MemoryEntryKindPreference MemoryEntryKind = "preference"
	MemoryEntryKindSummary    MemoryEntryKind = "summary"
	MemoryEntryKindProcedure  MemoryEntryKind = "procedure"
	MemoryEntryKindNote       MemoryEntryKind = "note"
)

type MemoryEntryStatus string

const (
	MemoryEntryStatusActive   MemoryEntryStatus = "active"
	MemoryEntryStatusArchived MemoryEntryStatus = "archived"
)

type MemoryEntry struct {
	MemoryEntryID uuid.UUID         `json:"memory_entry_id"`
	MemoryID      uuid.UUID         `json:"memory_id"`
	Kind          MemoryEntryKind   `json:"kind"`
	ContentText   string            `json:"content_text"`
	Confidence    float64           `json:"confidence"`
	Status        MemoryEntryStatus `json:"status"`
	CreatedAt     time.Time         `json:"created_at,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at,omitempty"`
}

func defaultPolicy() MemoryPolicy {
	return MemoryPolicy{
		EnabledByDefault: true,
		RetentionDays:    365,
		AllowUserMemory:  true,
		AllowTaskMemory:  true,
		AllowTenantMemory: true,
	}
}

func normalizeMemory(memory Memory) Memory {
	memory.OrgID = strings.TrimSpace(memory.OrgID)
	memory.ProductSurface = strings.TrimSpace(strings.ToLower(memory.ProductSurface))
	if memory.ProductSurface == "" {
		memory.ProductSurface = "axis-console"
	}
	if memory.Status == "" {
		memory.Status = MemoryStatusActive
	}
	if memory.Policy.RetentionDays == 0 && !memory.Policy.EnabledByDefault && !memory.Policy.AllowUserMemory && !memory.Policy.AllowTaskMemory && !memory.Policy.AllowTenantMemory {
		memory.Policy = defaultPolicy()
	}
	return memory
}

func normalizeEntry(entry MemoryEntry) MemoryEntry {
	entry.ContentText = strings.TrimSpace(entry.ContentText)
	if entry.Kind == "" {
		entry.Kind = MemoryEntryKindNote
	}
	if entry.Status == "" {
		entry.Status = MemoryEntryStatusActive
	}
	if entry.Confidence == 0 {
		entry.Confidence = 1
	}
	return entry
}
