package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"sort"
	"strings"
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
	poisoningFlags := detectMemoryPoisoning(in.ContentText, in.PayloadJSON)
	if len(poisoningFlags) > 0 && !in.AllowPoisoned {
		return domain.MemoryEntry{}, fmt.Errorf("%w: %s", ErrMemoryPoisoning, strings.Join(poisoningFlags, ","))
	}
	now := time.Now().UTC()
	if in.LastVerifiedAt == nil {
		in.LastVerifiedAt = &now
	}
	embeddingNamespace := in.OrgID + ":" + in.ProductSurface
	embedding := buildMemoryEmbedding(in.ContentText)
	embeddingJSON, err := json.Marshal(embedding)
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
		TrustScore:         memoryTrustScore(in.Confidence, poisoningFlags),
		RetentionPolicy:    in.RetentionPolicy,
		Status:             "active",
		Source:             strings.TrimSpace(in.Source),
		EmbeddingNamespace: embeddingNamespace,
		EmbeddingModel:     "hash-v1",
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
		if shouldRequireConflictReview(current, entry) && !in.Supersede {
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
	return result, nil
}

func (uc *Usecases) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if strings.TrimSpace(q.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	find := q.FindQuery
	find.Limit = maxInt(q.Limit*5, q.Limit)
	entries, err := uc.Find(ctx, find)
	if err != nil {
		return nil, err
	}
	queryEmbedding := buildMemoryEmbedding(q.Query)
	results := make([]SearchResult, 0, len(entries))
	for _, entry := range entries {
		if entry.Status != "" && entry.Status != "active" && entry.Status != "conflict" {
			continue
		}
		if len(entry.PoisoningFlags) > 0 && !q.IncludeUntrusted {
			continue
		}
		confidence := decayedConfidence(entry, time.Now().UTC())
		if q.MinConfidence > 0 && confidence < q.MinConfidence {
			continue
		}
		score, reasons := scoreMemory(entry, q.Query, queryEmbedding, confidence)
		results = append(results, SearchResult{Entry: entry, Score: score, Reasons: reasons})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > q.Limit {
		results = results[:q.Limit]
	}
	return results, nil
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

func buildMemoryEmbedding(text string) []float64 {
	const dims = 64
	vec := make([]float64, dims)
	for _, token := range strings.Fields(strings.ToLower(text)) {
		token = strings.Trim(token, ".,;:!?()[]{}\"'")
		if token == "" {
			continue
		}
		h := fnv.New32a()
		if _, err := h.Write([]byte(token)); err != nil {
			continue
		}
		vec[int(h.Sum32())%dims]++
	}
	var norm float64
	for _, value := range vec {
		norm += value * value
	}
	if norm == 0 {
		return vec
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] = vec[i] / norm
	}
	return vec
}

func scoreMemory(entry domain.MemoryEntry, query string, queryEmbedding []float64, confidence float64) (float64, []string) {
	var embedding []float64
	if len(entry.EmbeddingJSON) > 0 {
		_ = json.Unmarshal(entry.EmbeddingJSON, &embedding)
	}
	similarity := cosine(queryEmbedding, embedding)
	reasons := []string{"embedding"}
	if strings.Contains(strings.ToLower(entry.ContentText), strings.ToLower(query)) {
		similarity += 0.25
		reasons = append(reasons, "text_match")
	}
	return similarity*0.7 + confidence*0.3, reasons
}

func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := minInt(len(a), len(b))
	var dot float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
	}
	return dot
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
