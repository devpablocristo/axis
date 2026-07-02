package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

const (
	defaultEmbeddingModel = "hash-v1"
	vectorBackendJSON     = "json_vector"
	vectorBackendPGVector = "pgvector"
)

type EmbeddingInput struct {
	OrgID          string
	ProductSurface string
	AgentID        string
	Text           string
}

type Embedding struct {
	Model       string
	Vector      []float64
	Namespace   string
	ContentHash string
}

type EmbeddingProvider interface {
	Embed(ctx context.Context, in EmbeddingInput) (Embedding, error)
}

type VectorRecord struct {
	MemoryID       uuid.UUID
	OrgID          string
	ProductSurface string
	AgentID        string
	Namespace      string
	EmbeddingModel string
	Embedding      []float64
	ContentHash    string
}

type VectorSearchQuery struct {
	OrgID          string
	ProductSurface string
	AgentID        string
	Namespace      string
	EmbeddingModel string
	Embedding      []float64
	Limit          int
}

type VectorMatch struct {
	MemoryID uuid.UUID
	Score    float64
}

type VectorStore interface {
	UpsertVector(ctx context.Context, record VectorRecord) error
	SearchVectors(ctx context.Context, query VectorSearchQuery) ([]VectorMatch, error)
}

type MemoryRanker interface {
	Rank(query string, queryEmbedding []float64, entries []domain.MemoryEntry, minConfidence float64, includeUntrusted bool, limit int) []SearchResult
}

type MemoryCurator interface {
	DetectPoisoning(content string, payload json.RawMessage) []string
	TrustScore(confidence float64, flags []string) float64
	ShouldRequireConflictReview(current, next domain.MemoryEntry) bool
}

type HashEmbeddingProvider struct{}

func NewHashEmbeddingProvider() HashEmbeddingProvider {
	return HashEmbeddingProvider{}
}

func (HashEmbeddingProvider) Embed(_ context.Context, in EmbeddingInput) (Embedding, error) {
	vector := buildMemoryEmbedding(in.Text)
	return Embedding{
		Model:       defaultEmbeddingModel,
		Vector:      vector,
		Namespace:   memoryNamespace(in.OrgID, in.ProductSurface, in.AgentID),
		ContentHash: contentHash(in.Text),
	}, nil
}

type PostgresVectorStore struct {
	db *sharedpostgres.DB
}

func NewPostgresVectorStore(db *sharedpostgres.DB) *PostgresVectorStore {
	return &PostgresVectorStore{db: db}
}

func (s *PostgresVectorStore) UpsertVector(ctx context.Context, record VectorRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	record.Namespace = strings.TrimSpace(record.Namespace)
	record.OrgID = strings.TrimSpace(record.OrgID)
	record.ProductSurface = strings.TrimSpace(record.ProductSurface)
	record.EmbeddingModel = strings.TrimSpace(record.EmbeddingModel)
	if record.MemoryID == uuid.Nil || record.OrgID == "" || record.ProductSurface == "" || record.Namespace == "" || record.EmbeddingModel == "" || len(record.Embedding) == 0 {
		return fmt.Errorf("invalid memory vector record")
	}
	raw, err := json.Marshal(record.Embedding)
	if err != nil {
		return fmt.Errorf("marshal memory vector: %w", err)
	}
	if record.ContentHash == "" {
		record.ContentHash = contentHash(string(raw))
	}
	if s.pgvectorAvailable(ctx, len(record.Embedding)) {
		_, err = s.db.Pool().Exec(ctx, `
			INSERT INTO companion_memory_vectors
				(memory_id, org_id, product_surface, agent_id, namespace,
				 embedding_model, embedding_backend, dims, content_hash, embedding_json,
				 embedding_vector, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::vector,now())
			ON CONFLICT (memory_id, namespace, embedding_model) DO UPDATE SET
				org_id = EXCLUDED.org_id,
				product_surface = EXCLUDED.product_surface,
				agent_id = EXCLUDED.agent_id,
				embedding_backend = EXCLUDED.embedding_backend,
				dims = EXCLUDED.dims,
				content_hash = EXCLUDED.content_hash,
				embedding_json = EXCLUDED.embedding_json,
				embedding_vector = EXCLUDED.embedding_vector,
				updated_at = now()
		`, record.MemoryID, record.OrgID, record.ProductSurface, strings.TrimSpace(record.AgentID), record.Namespace,
			record.EmbeddingModel, vectorBackendPGVector, len(record.Embedding), record.ContentHash, raw, pgvectorLiteral(record.Embedding))
		if err == nil {
			return nil
		}
	}
	_, err = s.db.Pool().Exec(ctx, `
		INSERT INTO companion_memory_vectors
			(memory_id, org_id, product_surface, agent_id, namespace,
			 embedding_model, embedding_backend, dims, content_hash, embedding_json, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
		ON CONFLICT (memory_id, namespace, embedding_model) DO UPDATE SET
			org_id = EXCLUDED.org_id,
			product_surface = EXCLUDED.product_surface,
			agent_id = EXCLUDED.agent_id,
			embedding_backend = EXCLUDED.embedding_backend,
			dims = EXCLUDED.dims,
			content_hash = EXCLUDED.content_hash,
			embedding_json = EXCLUDED.embedding_json,
			updated_at = now()
	`, record.MemoryID, record.OrgID, record.ProductSurface, strings.TrimSpace(record.AgentID), record.Namespace,
		record.EmbeddingModel, vectorBackendJSON, len(record.Embedding), record.ContentHash, raw)
	if err != nil {
		return fmt.Errorf("upsert memory vector: %w", err)
	}
	return nil
}

func (s *PostgresVectorStore) SearchVectors(ctx context.Context, query VectorSearchQuery) ([]VectorMatch, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	query.Namespace = strings.TrimSpace(query.Namespace)
	query.EmbeddingModel = strings.TrimSpace(query.EmbeddingModel)
	if query.Namespace == "" || query.EmbeddingModel == "" || len(query.Embedding) == 0 {
		return nil, fmt.Errorf("invalid memory vector search")
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if s.pgvectorAvailable(ctx, len(query.Embedding)) {
		rows, err := s.db.Pool().Query(ctx, `
			SELECT memory_id, 1 - (embedding_vector <=> $3::vector) AS score
			FROM companion_memory_vectors
			WHERE namespace = $1
			  AND embedding_model = $2
			  AND embedding_vector IS NOT NULL
			  AND dims = $4
			ORDER BY embedding_vector <=> $3::vector ASC
			LIMIT $5
		`, query.Namespace, query.EmbeddingModel, pgvectorLiteral(query.Embedding), len(query.Embedding), limit)
		if err == nil {
			defer rows.Close()
			matches := make([]VectorMatch, 0)
			for rows.Next() {
				var match VectorMatch
				if err := rows.Scan(&match.MemoryID, &match.Score); err != nil {
					return nil, err
				}
				matches = append(matches, match)
			}
			return matches, rows.Err()
		}
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT memory_id, embedding_json
		FROM companion_memory_vectors
		WHERE namespace = $1 AND embedding_model = $2
		ORDER BY updated_at DESC
		LIMIT $3
	`, query.Namespace, query.EmbeddingModel, maxInt(limit*10, limit))
	if err != nil {
		return nil, fmt.Errorf("search memory vectors: %w", err)
	}
	defer rows.Close()
	matches := make([]VectorMatch, 0)
	for rows.Next() {
		var (
			id  uuid.UUID
			raw []byte
			vec []float64
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &vec); err != nil {
			continue
		}
		matches = append(matches, VectorMatch{MemoryID: id, Score: cosine(query.Embedding, vec)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (s *PostgresVectorStore) pgvectorAvailable(ctx context.Context, dims int) bool {
	if s == nil || s.db == nil || dims <= 0 {
		return false
	}
	// Migration 0027 creates a dimensionless pgvector column when pgvector is
	// installed. The operator rejects comparisons between different dimensions,
	// so namespace/model isolation keeps queries coherent.
	var exists bool
	err := s.db.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_name = 'companion_memory_vectors'
			  AND column_name = 'embedding_vector'
		)
	`).Scan(&exists)
	return err == nil && exists
}

type DefaultMemoryRanker struct{}

func NewDefaultMemoryRanker() DefaultMemoryRanker {
	return DefaultMemoryRanker{}
}

func (DefaultMemoryRanker) Rank(query string, queryEmbedding []float64, entries []domain.MemoryEntry, minConfidence float64, includeUntrusted bool, limit int) []SearchResult {
	if limit <= 0 {
		limit = 20
	}
	now := time.Now().UTC()
	results := make([]SearchResult, 0, len(entries))
	for _, entry := range entries {
		if entry.Status != "" && entry.Status != "active" && entry.Status != "conflict" {
			continue
		}
		if len(entry.PoisoningFlags) > 0 && !includeUntrusted {
			continue
		}
		confidence := decayedConfidence(entry, now)
		if minConfidence > 0 && confidence < minConfidence {
			continue
		}
		score, reasons := scoreMemory(entry, query, queryEmbedding, confidence)
		results = append(results, SearchResult{Entry: entry, Score: score, Reasons: reasons})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

type DefaultMemoryCurator struct{}

func NewDefaultMemoryCurator() DefaultMemoryCurator {
	return DefaultMemoryCurator{}
}

func (DefaultMemoryCurator) DetectPoisoning(content string, payload json.RawMessage) []string {
	return detectMemoryPoisoning(content, payload)
}

func (DefaultMemoryCurator) TrustScore(confidence float64, flags []string) float64 {
	return memoryTrustScore(confidence, flags)
}

func (DefaultMemoryCurator) ShouldRequireConflictReview(current, next domain.MemoryEntry) bool {
	return shouldRequireConflictReview(current, next)
}

func memoryNamespace(orgID, productSurface, agentID string) string {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	agentID = strings.TrimSpace(agentID)
	if productSurface == "" {
		productSurface = "companion"
	}
	if agentID == "" {
		return orgID + ":" + productSurface
	}
	return orgID + ":" + productSurface + ":" + agentID
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
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
	return normalizeVector(vec)
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

func normalizeVector(vec []float64) []float64 {
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

func pgvectorLiteral(vec []float64) string {
	parts := make([]string, 0, len(vec))
	for _, value := range vec {
		parts = append(parts, fmt.Sprintf("%g", value))
	}
	return "[" + strings.Join(parts, ",") + "]"
}
