package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/jobs"
	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

const (
	JobKindMemoryEmbedding  = "memory.embedding"
	JobKindMemoryRetention  = "memory.retention"
	JobKindMemoryDecay      = "memory.decay"
	JobKindMemoryCompaction = "memory.compaction"
)

type jobRegistrar interface {
	Register(kind string, handler jobs.Handler)
}

func (uc *Usecases) RegisterJobHandlers(registrar jobRegistrar) {
	if uc == nil || registrar == nil {
		return
	}
	registrar.Register(JobKindMemoryEmbedding, uc.handleEmbeddingJob)
	registrar.Register(JobKindMemoryRetention, uc.handleRetentionJob)
	registrar.Register(JobKindMemoryDecay, uc.handleDecayJob)
	registrar.Register(JobKindMemoryCompaction, uc.handleCompactionJob)
}

func (uc *Usecases) handleEmbeddingJob(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload struct {
		MemoryID string `json:"memory_id"`
		AgentID  string `json:"agent_id,omitempty"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.Permanent(fmt.Errorf("invalid embedding job payload: %w", err))
	}
	id, err := uuid.Parse(payload.MemoryID)
	if err != nil || id == uuid.Nil {
		return nil, jobs.Permanent(fmt.Errorf("memory_id is required"))
	}
	entry, err := uc.repo.Get(ctx, id)
	if err != nil {
		if IsNotFound(err) {
			return nil, jobs.Permanent(err)
		}
		return nil, err
	}
	embedding, err := uc.embedder.Embed(ctx, EmbeddingInput{
		OrgID:          entry.OrgID,
		ProductSurface: entry.ProductSurface,
		AgentID:        payload.AgentID,
		Text:           entry.ContentText,
	})
	if err != nil {
		return nil, err
	}
	if uc.vectors != nil {
		if err := uc.vectors.UpsertVector(ctx, VectorRecord{
			MemoryID:       entry.ID,
			OrgID:          entry.OrgID,
			ProductSurface: entry.ProductSurface,
			AgentID:        payload.AgentID,
			Namespace:      embedding.Namespace,
			EmbeddingModel: embedding.Model,
			Embedding:      embedding.Vector,
			ContentHash:    embedding.ContentHash,
		}); err != nil {
			return nil, err
		}
	}
	return json.Marshal(map[string]any{
		"memory_id":       entry.ID.String(),
		"namespace":       embedding.Namespace,
		"embedding_model": embedding.Model,
		"dims":            len(embedding.Vector),
	})
}

func (uc *Usecases) handleRetentionJob(ctx context.Context, _ jobs.Job) (json.RawMessage, error) {
	purged, err := uc.repo.PurgeExpired(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"purged": purged})
}

func (uc *Usecases) handleDecayJob(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload struct {
		OrgID          string `json:"org_id"`
		ProductSurface string `json:"product_surface"`
		ScopeType      string `json:"scope_type"`
		ScopeID        string `json:"scope_id"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.Permanent(fmt.Errorf("invalid decay job payload: %w", err))
	}
	return json.Marshal(map[string]any{
		"org_id":          payload.OrgID,
		"product_surface": payload.ProductSurface,
		"status":          "confidence_decay_is_applied_at_retrieval",
	})
}

func (uc *Usecases) handleCompactionJob(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload struct {
		OrgID          string `json:"org_id"`
		ProductSurface string `json:"product_surface"`
		ScopeType      string `json:"scope_type"`
		ScopeID        string `json:"scope_id"`
		Kind           string `json:"kind,omitempty"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.Permanent(fmt.Errorf("invalid compaction job payload: %w", err))
	}
	if payload.OrgID == "" || payload.ProductSurface == "" || payload.ScopeType == "" || payload.ScopeID == "" {
		return nil, jobs.Permanent(fmt.Errorf("org_id, product_surface, scope_type and scope_id are required"))
	}
	entries, err := uc.Find(ctx, FindQuery{
		OrgID:          payload.OrgID,
		ProductSurface: payload.ProductSurface,
		ScopeType:      domain.ScopeType(payload.ScopeType),
		ScopeID:        payload.ScopeID,
		Kind:           domain.MemoryKind(payload.Kind),
		Limit:          50,
	})
	if err != nil {
		return nil, err
	}
	parts := make([]string, 0, len(entries))
	sourceIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		text := strings.TrimSpace(entry.ContentText)
		if text == "" {
			continue
		}
		parts = append(parts, "- "+text)
		sourceIDs = append(sourceIDs, entry.ID.String())
	}
	if len(parts) == 0 {
		return json.Marshal(map[string]any{"status": "no_source_entries", "source_count": 0})
	}
	repo, ok := uc.repo.(memoryOpsRepository)
	if !ok {
		return nil, jobs.Permanent(fmt.Errorf("memory operations repository is not configured"))
	}
	payloadJSON, _ := json.Marshal(map[string]any{"source_memory_ids": sourceIDs})
	summary, err := repo.CreateSummary(ctx, MemorySummary{
		OrgID:          payload.OrgID,
		ProductSurface: payload.ProductSurface,
		ScopeType:      payload.ScopeType,
		ScopeID:        payload.ScopeID,
		SummaryType:    "compaction",
		ContentText:    strings.Join(parts, "\n"),
		SourceCount:    len(sourceIDs),
		Payload:        payloadJSON,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"org_id":          payload.OrgID,
		"product_surface": payload.ProductSurface,
		"scope_type":      payload.ScopeType,
		"scope_id":        payload.ScopeID,
		"summary_id":      summary.ID.String(),
		"summary_version": summary.Version,
		"source_count":    summary.SourceCount,
		"status":          "compacted",
	})
}
