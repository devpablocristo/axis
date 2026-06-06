package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/platform/concurrency/go/worker"
	"github.com/devpablocristo/platform/errors/go/domainerr"
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
	AgentID        string
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
	AgentID         string
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
	Source          string
	Supersede       bool
	LastVerifiedAt  *time.Time
	AllowPoisoned   bool
}

type SearchQuery struct {
	FindQuery
	Query            string
	MinConfidence    float64
	IncludeUntrusted bool
}

type SearchResult struct {
	Entry   domain.MemoryEntry `json:"entry"`
	Score   float64            `json:"score"`
	Reasons []string           `json:"reasons,omitempty"`
}

type ProductInstallationGuard interface {
	RequireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error
}

// defaultPerScopeQuota es el tope de entradas vivas por (scope_type, scope_id).
// Sin tope, una org maliciosa podría inflar la tabla via /v1/memory hasta DoS.
// Configurable via WithPerScopeQuota desde wire.
const defaultPerScopeQuota = 1000

// Usecases lógica de negocio de memoria operativa.
type Usecases struct {
	repo              Repository
	perScopeQuota     int
	embedder          EmbeddingProvider
	vectors           VectorStore
	ranker            MemoryRanker
	curator           MemoryCurator
	installationGuard ProductInstallationGuard
}

// NewUsecases crea una nueva instancia de Usecases con quota default.
func NewUsecases(repo Repository) *Usecases {
	return &Usecases{
		repo:          repo,
		perScopeQuota: defaultPerScopeQuota,
		embedder:      NewHashEmbeddingProvider(),
		ranker:        NewDefaultMemoryRanker(),
		curator:       NewDefaultMemoryCurator(),
	}
}

// WithPerScopeQuota override del tope de entradas vivas por scope. <=0 = sin
// límite (no recomendado en multi-tenant).
func (uc *Usecases) WithPerScopeQuota(n int) *Usecases {
	uc.perScopeQuota = n
	return uc
}

func (uc *Usecases) WithEmbeddingProvider(provider EmbeddingProvider) *Usecases {
	if provider != nil {
		uc.embedder = provider
	}
	return uc
}

func (uc *Usecases) WithVectorStore(store VectorStore) *Usecases {
	uc.vectors = store
	return uc
}

func (uc *Usecases) WithMemoryRanker(ranker MemoryRanker) *Usecases {
	if ranker != nil {
		uc.ranker = ranker
	}
	return uc
}

func (uc *Usecases) WithMemoryCurator(curator MemoryCurator) *Usecases {
	if curator != nil {
		uc.curator = curator
	}
	return uc
}

func (uc *Usecases) WithProductInstallationGuard(guard ProductInstallationGuard) *Usecases {
	uc.installationGuard = guard
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
	if err := uc.requireActiveInstallation(ctx, in.OrgID, in.ProductSurface, "memory_write"); err != nil {
		return domain.MemoryEntry{}, err
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
	poisoningFlags := uc.curator.DetectPoisoning(in.ContentText, in.PayloadJSON)
	if len(poisoningFlags) > 0 && !in.AllowPoisoned {
		return domain.MemoryEntry{}, fmt.Errorf("%w: %s", ErrMemoryPoisoning, strings.Join(poisoningFlags, ","))
	}
	now := time.Now().UTC()
	if in.LastVerifiedAt == nil {
		in.LastVerifiedAt = &now
	}
	embedding, err := uc.embedder.Embed(ctx, EmbeddingInput{
		OrgID:          in.OrgID,
		ProductSurface: in.ProductSurface,
		AgentID:        in.AgentID,
		Text:           in.ContentText,
	})
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("embed memory: %w", err)
	}
	if embedding.Model == "" {
		embedding.Model = defaultEmbeddingModel
	}
	if embedding.Namespace == "" {
		embedding.Namespace = memoryNamespace(in.OrgID, in.ProductSurface, in.AgentID)
	}
	embeddingJSON, err := json.Marshal(embedding.Vector)
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("marshal memory embedding: %w", err)
	}

	entry := domain.MemoryEntry{
		OrgID:              in.OrgID,
		UserID:             in.UserID,
		ProductSurface:     in.ProductSurface,
		Kind:               in.Kind,
		MemoryType:         in.MemoryType,
		Classification:     in.Classification,
		ScopeType:          in.ScopeType,
		ScopeID:            in.ScopeID,
		Key:                in.Key,
		PayloadJSON:        in.PayloadJSON,
		ContentText:        in.ContentText,
		ProvenanceJSON:     in.ProvenanceJSON,
		Confidence:         in.Confidence,
		TrustScore:         uc.curator.TrustScore(in.Confidence, poisoningFlags),
		RetentionPolicy:    in.RetentionPolicy,
		Status:             "active",
		Source:             strings.TrimSpace(in.Source),
		EmbeddingNamespace: embedding.Namespace,
		EmbeddingModel:     embedding.Model,
		EmbeddingJSON:      embeddingJSON,
		LastVerifiedAt:     in.LastVerifiedAt,
		ConfidenceDecayAt:  confidenceDecayAt(in.Kind, now),
		PoisoningFlags:     poisoningFlags,
		Version:            in.Version,
		ExpiresAt:          expiresAt,
	}

	current, err := uc.repo.GetByScopeKey(ctx, in.OrgID, in.ProductSurface, in.ScopeType, in.ScopeID, in.Kind, in.Key)
	switch {
	case err == nil:
		if uc.curator.ShouldRequireConflictReview(current, entry) && !in.Supersede {
			entry.Status = "conflict"
			conflictID := uuid.New()
			entry.ConflictGroupID = &conflictID
			return domain.MemoryEntry{}, fmt.Errorf("%w: key %s", ErrMemoryConflict, in.Key)
		}
		if in.Version > 0 && current.Version != in.Version {
			return domain.MemoryEntry{}, ErrVersionConflict
		}
		entry.ID = current.ID
		entry.CreatedAt = current.CreatedAt
		if in.Supersede {
			supersedes := current.ID
			entry.SupersedesID = &supersedes
		}
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
	if uc.vectors != nil {
		if err := uc.vectors.UpsertVector(ctx, VectorRecord{
			MemoryID:       result.ID,
			OrgID:          result.OrgID,
			ProductSurface: result.ProductSurface,
			AgentID:        in.AgentID,
			Namespace:      result.EmbeddingNamespace,
			EmbeddingModel: result.EmbeddingModel,
			Embedding:      embedding.Vector,
			ContentHash:    embedding.ContentHash,
		}); err != nil {
			return domain.MemoryEntry{}, fmt.Errorf("upsert memory vector: %w", err)
		}
	}
	return result, nil
}

func (uc *Usecases) requireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error {
	if uc.installationGuard == nil {
		return nil
	}
	if err := uc.installationGuard.RequireActiveInstallation(ctx, orgID, productSurface, reason); err != nil {
		if errors.Is(err, products.ErrValidation) {
			return domainerr.Validation(err.Error())
		}
		return domainerr.Forbidden(err.Error())
	}
	return nil
}

func (uc *Usecases) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if strings.TrimSpace(q.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	queryEmbedding := []float64(nil)
	queryEmbeddingModel := defaultEmbeddingModel
	if uc.embedder != nil {
		embedding, err := uc.embedder.Embed(ctx, EmbeddingInput{
			OrgID:          q.OrgID,
			ProductSurface: q.ProductSurface,
			AgentID:        q.AgentID,
			Text:           q.Query,
		})
		if err != nil {
			return nil, fmt.Errorf("embed memory search: %w", err)
		}
		queryEmbedding = embedding.Vector
		if strings.TrimSpace(embedding.Model) != "" {
			queryEmbeddingModel = embedding.Model
		}
	}
	entries, err := uc.searchEntries(ctx, q, queryEmbedding, queryEmbeddingModel)
	if err != nil {
		return nil, err
	}
	return uc.ranker.Rank(q.Query, queryEmbedding, entries, q.MinConfidence, q.IncludeUntrusted, q.Limit), nil
}

func (uc *Usecases) searchEntries(ctx context.Context, q SearchQuery, queryEmbedding []float64, queryEmbeddingModel string) ([]domain.MemoryEntry, error) {
	if uc.vectors != nil && len(queryEmbedding) > 0 {
		namespace := memoryNamespace(q.OrgID, q.ProductSurface, q.AgentID)
		matches, err := uc.vectors.SearchVectors(ctx, VectorSearchQuery{
			OrgID:          q.OrgID,
			ProductSurface: q.ProductSurface,
			AgentID:        q.AgentID,
			Namespace:      namespace,
			EmbeddingModel: firstNonEmptyString(queryEmbeddingModel, defaultEmbeddingModel),
			Embedding:      queryEmbedding,
			Limit:          maxInt(q.Limit*5, q.Limit),
		})
		if err != nil {
			return nil, err
		}
		entries := make([]domain.MemoryEntry, 0, len(matches))
		for _, match := range matches {
			entry, err := uc.repo.Get(ctx, match.MemoryID)
			if err != nil {
				if IsNotFound(err) {
					continue
				}
				return nil, fmt.Errorf("get vector memory result: %w", err)
			}
			if memoryEntryMatchesFind(entry, q.FindQuery) {
				entries = append(entries, entry)
			}
		}
		if len(entries) > 0 {
			return entries, nil
		}
	}
	find := q.FindQuery
	find.Limit = maxInt(q.Limit*5, q.Limit)
	return uc.Find(ctx, find)
}

func memoryEntryMatchesFind(entry domain.MemoryEntry, q FindQuery) bool {
	if strings.TrimSpace(entry.OrgID) != strings.TrimSpace(q.OrgID) || strings.TrimSpace(entry.ProductSurface) != strings.TrimSpace(q.ProductSurface) {
		return false
	}
	if entry.ScopeType != q.ScopeType || strings.TrimSpace(entry.ScopeID) != strings.TrimSpace(q.ScopeID) {
		return false
	}
	if q.UserID != "" && entry.UserID != "" && strings.TrimSpace(entry.UserID) != strings.TrimSpace(q.UserID) {
		return false
	}
	if q.Kind != "" && entry.Kind != q.Kind {
		return false
	}
	if q.MemoryType != "" && entry.MemoryType != q.MemoryType {
		return false
	}
	return true
}

func shouldRequireConflictReview(current, next domain.MemoryEntry) bool {
	if current.Kind != domain.MemorySemanticFact && current.Kind != domain.MemoryTenantKnowledge && current.Kind != domain.MemoryBusinessContext {
		return false
	}
	if strings.TrimSpace(current.ContentText) == "" || strings.TrimSpace(next.ContentText) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(current.ContentText), strings.TrimSpace(next.ContentText)) {
		return false
	}
	return current.Confidence >= 0.7 && next.Confidence >= 0.7
}

func detectMemoryPoisoning(content string, payload json.RawMessage) []string {
	text := strings.ToLower(content + " " + string(payload))
	var flags []string
	for label, patterns := range map[string][]string{
		"instruction_override": {"ignore previous instructions", "system override", "developer message"},
		"permanent_rule":       {"remember this as a permanent rule", "memoriza esta regla permanente", "store this instruction forever"},
		"approval_bypass":      {"skip nexus", "bypass approval", "sin aprobación"},
		"secret_material":      {"authorization: bearer", "client_secret=", "private_key"},
	} {
		for _, pattern := range patterns {
			if strings.Contains(text, pattern) {
				flags = append(flags, label)
				break
			}
		}
	}
	sort.Strings(flags)
	return flags
}

func memoryTrustScore(confidence float64, flags []string) float64 {
	if confidence <= 0 {
		confidence = 1
	}
	score := confidence
	if len(flags) > 0 {
		score *= 0.2
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func confidenceDecayAt(kind domain.MemoryKind, now time.Time) *time.Time {
	days := domain.DefaultRetentionDays(kind)
	if days <= 0 {
		return nil
	}
	t := now.AddDate(0, 0, days/2)
	return &t
}

func decayedConfidence(entry domain.MemoryEntry, now time.Time) float64 {
	confidence := entry.Confidence
	if confidence <= 0 {
		confidence = 1
	}
	if entry.ConfidenceDecayAt != nil && now.After(*entry.ConfidenceDecayAt) {
		confidence *= 0.85
	}
	if entry.TrustScore > 0 && entry.TrustScore < confidence {
		confidence = entry.TrustScore
	}
	return confidence
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
