package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

type MemoryReview struct {
	ID              uuid.UUID       `json:"id"`
	OrgID           string          `json:"org_id"`
	ProductSurface  string          `json:"product_surface"`
	MemoryID        *uuid.UUID      `json:"memory_id,omitempty"`
	ReviewType      string          `json:"review_type"`
	Status          string          `json:"status"`
	Reason          string          `json:"reason,omitempty"`
	ProposedContent string          `json:"proposed_content,omitempty"`
	ProposedPayload json.RawMessage `json:"proposed_payload,omitempty"`
	CreatedBy       string          `json:"created_by,omitempty"`
	DecidedBy       string          `json:"decided_by,omitempty"`
	CreatedAt       time.Time       `json:"created_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty"`
	DecidedAt       *time.Time      `json:"decided_at,omitempty"`
}

type MemoryAuditEntry struct {
	ID             uuid.UUID       `json:"id"`
	MemoryID       uuid.UUID       `json:"memory_id"`
	OrgID          string          `json:"org_id"`
	ProductSurface string          `json:"product_surface"`
	Action         string          `json:"action"`
	Status         string          `json:"status"`
	Payload        json.RawMessage `json:"payload_json"`
	CreatedAt      time.Time       `json:"created_at"`
}

type MemorySummary struct {
	ID             uuid.UUID       `json:"id"`
	OrgID          string          `json:"org_id"`
	ProductSurface string          `json:"product_surface"`
	ScopeType      string          `json:"scope_type"`
	ScopeID        string          `json:"scope_id"`
	SummaryType    string          `json:"summary_type"`
	Version        int             `json:"version"`
	ContentText    string          `json:"content_text"`
	SourceCount    int             `json:"source_count"`
	Payload        json.RawMessage `json:"payload_json"`
	CreatedAt      time.Time       `json:"created_at"`
}

type CreateReviewInput struct {
	OrgID           string
	ProductSurface  string
	MemoryID        uuid.UUID
	ReviewType      string
	Reason          string
	ProposedContent string
	ProposedPayload json.RawMessage
	CreatedBy       string
}

type memoryOpsRepository interface {
	ListConflicts(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error)
	CreateReview(ctx context.Context, in CreateReviewInput) (MemoryReview, error)
	ListReviews(ctx context.Context, orgID, productSurface, status string, limit int) ([]MemoryReview, error)
	UpdateReviewStatus(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, status, decidedBy string) (MemoryReview, error)
	ApplyReview(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, decidedBy string) (MemoryReview, error)
	ListAudit(ctx context.Context, orgID, productSurface string, limit int) ([]MemoryAuditEntry, error)
	ListSummaries(ctx context.Context, orgID, productSurface string, limit int) ([]MemorySummary, error)
	CreateSummary(ctx context.Context, summary MemorySummary) (MemorySummary, error)
	ExportByOrg(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error)
	DeleteByOrg(ctx context.Context, orgID, productSurface string) (int64, error)
}

func (uc *Usecases) ListConflicts(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return nil, fmt.Errorf("memory operations repository is not configured")
	}
	return repo.ListConflicts(ctx, orgID, productSurface, limit)
}

func (uc *Usecases) CreateReview(ctx context.Context, in CreateReviewInput) (MemoryReview, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return MemoryReview{}, fmt.Errorf("memory operations repository is not configured")
	}
	if strings.TrimSpace(in.OrgID) == "" || in.MemoryID == uuid.Nil {
		return MemoryReview{}, fmt.Errorf("org_id and memory_id are required")
	}
	switch strings.TrimSpace(in.ReviewType) {
	case "conflict", "correction", "invalidation", "deletion":
	default:
		return MemoryReview{}, fmt.Errorf("review_type must be conflict, correction, invalidation, or deletion")
	}
	if len(in.ProposedPayload) == 0 {
		in.ProposedPayload = json.RawMessage(`{}`)
	}
	return repo.CreateReview(ctx, in)
}

func (uc *Usecases) ListReviews(ctx context.Context, orgID, productSurface, status string, limit int) ([]MemoryReview, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return nil, fmt.Errorf("memory operations repository is not configured")
	}
	return repo.ListReviews(ctx, orgID, productSurface, status, limit)
}

func (uc *Usecases) UpdateReviewStatus(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, status, decidedBy string) (MemoryReview, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return MemoryReview{}, fmt.Errorf("memory operations repository is not configured")
	}
	switch strings.TrimSpace(status) {
	case "approved", "rejected", "cancelled":
	default:
		return MemoryReview{}, fmt.Errorf("review status must be approved, rejected, or cancelled")
	}
	if strings.TrimSpace(orgID) == "" || reviewID == uuid.Nil {
		return MemoryReview{}, fmt.Errorf("org_id and review_id are required")
	}
	return repo.UpdateReviewStatus(ctx, orgID, productSurface, reviewID, status, decidedBy)
}

func (uc *Usecases) ApplyReview(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, decidedBy string) (MemoryReview, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return MemoryReview{}, fmt.Errorf("memory operations repository is not configured")
	}
	if strings.TrimSpace(orgID) == "" || reviewID == uuid.Nil {
		return MemoryReview{}, fmt.Errorf("org_id and review_id are required")
	}
	return repo.ApplyReview(ctx, orgID, productSurface, reviewID, decidedBy)
}

func (uc *Usecases) ListAudit(ctx context.Context, orgID, productSurface string, limit int) ([]MemoryAuditEntry, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return nil, fmt.Errorf("memory operations repository is not configured")
	}
	return repo.ListAudit(ctx, orgID, productSurface, limit)
}

func (uc *Usecases) ListSummaries(ctx context.Context, orgID, productSurface string, limit int) ([]MemorySummary, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return nil, fmt.Errorf("memory operations repository is not configured")
	}
	return repo.ListSummaries(ctx, orgID, productSurface, limit)
}

func (uc *Usecases) ExportByOrg(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return nil, fmt.Errorf("memory operations repository is not configured")
	}
	return repo.ExportByOrg(ctx, orgID, productSurface, limit)
}

func (uc *Usecases) DeleteByOrg(ctx context.Context, orgID, productSurface string) (int64, error) {
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return 0, fmt.Errorf("memory operations repository is not configured")
	}
	if strings.TrimSpace(orgID) == "" {
		return 0, fmt.Errorf("org_id is required")
	}
	return repo.DeleteByOrg(ctx, orgID, productSurface)
}
