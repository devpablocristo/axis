package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/devpablocristo/platform/concurrency/go/worker"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

// Repository port de persistencia para memoria operativa.
type Repository interface {
	Upsert(ctx context.Context, e domain.MemoryEntry) (domain.MemoryEntry, error)
	Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error)
	GetByScopeKey(ctx context.Context, orgID, productSurface string, scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) (domain.MemoryEntry, error)
	Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error)
	Delete(ctx context.Context, id uuid.UUID) error
	PurgeExpired(ctx context.Context) (int64, error)
	CountByScope(ctx context.Context, scopeType domain.ScopeType, scopeID string) (int, error)
}

// FindQuery filtros de búsqueda de memoria.
type FindQuery struct {
	OrgID          string
	UserID         string
	ProductSurface string
	ScopeType      domain.ScopeType
	ScopeID        string
	Kind           domain.MemoryKind
	MemoryType     domain.MemoryType
	Limit          int
}

// UpsertInput datos para crear o actualizar una entrada de memoria.
type UpsertInput struct {
	OrgID           string
	UserID          string
	ProductSurface  string
	Kind            domain.MemoryKind
	MemoryType      domain.MemoryType
	Classification  domain.MemoryClass
	ScopeType       domain.ScopeType
	ScopeID         string
	Key             string
	PayloadJSON     json.RawMessage
	ContentText     string
	ProvenanceJSON  json.RawMessage
	Confidence      float64
	RetentionPolicy string
	Version         int // 0 = insert, >0 = update con versión optimista
	TTLDays         int // 0 = usar default por kind
}

// defaultPerScopeQuota es el tope de entradas vivas por (scope_type, scope_id).
// Sin tope, una org maliciosa podría inflar la tabla via /v1/memory hasta DoS.
// Configurable via WithPerScopeQuota desde wire.
const defaultPerScopeQuota = 1000

// Usecases lógica de negocio de memoria operativa.
type Usecases struct {
	repo          Repository
	perScopeQuota int
}

// NewUsecases crea una nueva instancia de Usecases con quota default.
func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo, perScopeQuota: defaultPerScopeQuota}
}

// WithPerScopeQuota override del tope de entradas vivas por scope. <=0 = sin
// límite (no recomendado en multi-tenant).
func (uc *Usecases) WithPerScopeQuota(n int) *Usecases {
	uc.perScopeQuota = n
	return uc
}

// Upsert crea o actualiza una entrada de memoria.
func (uc *Usecases) Upsert(ctx context.Context, in UpsertInput) (domain.MemoryEntry, error) {
	if in.OrgID == "" || in.ProductSurface == "" {
		return domain.MemoryEntry{}, fmt.Errorf("org_id and product_surface are required")
	}
	if in.ScopeType == "" || in.ScopeID == "" {
		return domain.MemoryEntry{}, fmt.Errorf("scope_type and scope_id are required")
	}
	if in.ScopeType == domain.ScopeUser && in.UserID == "" {
		return domain.MemoryEntry{}, fmt.Errorf("user_id is required for user memory")
	}
	if in.Kind == "" {
		return domain.MemoryEntry{}, fmt.Errorf("kind is required")
	}
	if in.Key == "" {
		return domain.MemoryEntry{}, fmt.Errorf("key is required")
	}

	ttl := in.TTLDays
	if ttl == 0 {
		ttl = domain.DefaultRetentionDays(in.Kind)
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().UTC().AddDate(0, 0, ttl)
		expiresAt = &t
	}

	if len(in.PayloadJSON) == 0 {
		in.PayloadJSON = json.RawMessage(`{}`)
	}
	if len(in.ProvenanceJSON) == 0 {
		in.ProvenanceJSON = json.RawMessage(`{}`)
	}
	if in.Classification == "" {
		in.Classification = domain.ClassForKind(in.Kind)
	}
	if in.MemoryType == "" {
		in.MemoryType = domain.TypeForKind(in.Kind)
	}
	if in.Confidence <= 0 {
		in.Confidence = 1
	}
	if in.RetentionPolicy == "" {
		in.RetentionPolicy = "default"
	}

	entry := domain.MemoryEntry{
		OrgID:           in.OrgID,
		UserID:          in.UserID,
		ProductSurface:  in.ProductSurface,
		Kind:            in.Kind,
		MemoryType:      in.MemoryType,
		Classification:  in.Classification,
		ScopeType:       in.ScopeType,
		ScopeID:         in.ScopeID,
		Key:             in.Key,
		PayloadJSON:     in.PayloadJSON,
		ContentText:     in.ContentText,
		ProvenanceJSON:  in.ProvenanceJSON,
		Confidence:      in.Confidence,
		RetentionPolicy: in.RetentionPolicy,
		Version:         in.Version,
		ExpiresAt:       expiresAt,
	}

	current, err := uc.repo.GetByScopeKey(ctx, in.OrgID, in.ProductSurface, in.ScopeType, in.ScopeID, in.Kind, in.Key)
	switch {
	case err == nil:
		if in.Version > 0 && current.Version != in.Version {
			return domain.MemoryEntry{}, ErrVersionConflict
		}
		entry.ID = current.ID
		entry.CreatedAt = current.CreatedAt
		if in.Version > 0 {
			entry.Version = in.Version
		} else {
			entry.Version = current.Version
		}
	case IsNotFound(err):
		if in.Version > 0 {
			return domain.MemoryEntry{}, ErrVersionConflict
		}
		// Camino de inserción: validar quota por scope. No se aplica a updates
		// porque no agrandan la tabla.
		if uc.perScopeQuota > 0 {
			n, cErr := uc.repo.CountByScope(ctx, in.ScopeType, in.ScopeID)
			if cErr != nil {
				return domain.MemoryEntry{}, fmt.Errorf("check memory quota: %w", cErr)
			}
			if n >= uc.perScopeQuota {
				return domain.MemoryEntry{}, ErrQuotaExceeded
			}
		}
	case err != nil:
		return domain.MemoryEntry{}, fmt.Errorf("lookup memory entry: %w", err)
	}

	result, err := uc.repo.Upsert(ctx, entry)
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("upsert memory: %w", err)
	}
	return result, nil
}

// Get obtiene una entrada de memoria por ID.
func (uc *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error) {
	entry, err := uc.repo.Get(ctx, id)
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("get memory: %w", err)
	}
	return entry, nil
}

// Find busca entradas de memoria por scope y kind.
func (uc *Usecases) Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error) {
	if q.OrgID == "" || q.ProductSurface == "" {
		return nil, fmt.Errorf("org_id and product_surface are required")
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	entries, err := uc.repo.Find(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("find memory: %w", err)
	}
	return entries, nil
}

// Delete elimina una entrada de memoria por ID.
func (uc *Usecases) Delete(ctx context.Context, id uuid.UUID) error {
	if err := uc.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	return nil
}

// RunPurgeLoop ejecuta purga periódica de entradas expiradas.
func (uc *Usecases) RunPurgeLoop(ctx context.Context, interval time.Duration) {
	worker.RunPeriodic(ctx, interval, "memory-purge", func(c context.Context) {
		purged, err := uc.repo.PurgeExpired(c)
		if err != nil {
			slog.Error("purge expired memory", "error", err)
			return
		}
		if purged > 0 {
			slog.Info("purged expired memory entries", "count", purged)
		}
	})
}
