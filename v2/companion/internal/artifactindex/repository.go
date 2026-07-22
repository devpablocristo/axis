package artifactindex

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Upsert(ctx context.Context, scope artifacts.Scope, chunks []artifacts.Chunk, embeddings []artifacts.Embedding) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	if len(chunks) == 0 || len(chunks) != len(embeddings) {
		return errors.New("chunks and embeddings must have equal non-zero length")
	}
	byChunk := make(map[string]artifacts.Embedding, len(embeddings))
	model := strings.TrimSpace(embeddings[0].Model)
	if model == "" {
		return errors.New("embedding model is required")
	}
	for _, embedding := range embeddings {
		if embedding.Model != model || len(embedding.Values) != Dimensions {
			return errors.New("embedding model or dimensions mismatch")
		}
		if _, exists := byChunk[embedding.ChunkID]; exists {
			return errors.New("duplicate embedding chunk_id")
		}
		byChunk[embedding.ChunkID] = embedding
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	chunkIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		embedding, ok := byChunk[chunk.ID]
		if !ok || strings.TrimSpace(chunk.Text) == "" || strings.TrimSpace(chunk.DocumentID) == "" {
			return errors.New("chunk content or embedding is incomplete")
		}
		locator, err := json.Marshal(chunk.Locator)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO companion_artifact_chunks (
				tenant_id, virployee_id, product_surface, subject_id, repository_generation,
				document_id, chunk_id, content, mime_type, source_sha256, locator,
				source_version, extractor_version, chunker_version,
				embedding_model, embedding_dimensions, embedding, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15,$16,$17::vector,now())
			ON CONFLICT (tenant_id,virployee_id,product_surface,subject_id,repository_generation,embedding_model,chunk_id)
			DO UPDATE SET content=EXCLUDED.content, mime_type=EXCLUDED.mime_type,
				source_sha256=EXCLUDED.source_sha256, locator=EXCLUDED.locator,
				source_version=EXCLUDED.source_version, extractor_version=EXCLUDED.extractor_version,
				chunker_version=EXCLUDED.chunker_version, embedding_dimensions=EXCLUDED.embedding_dimensions,
				embedding=EXCLUDED.embedding, updated_at=now()
		`, scope.TenantID, scope.VirployeeID, scope.ProductSurface, scope.SubjectID, scope.RepositoryGeneration,
			chunk.DocumentID, chunk.ID, chunk.Text, chunk.MIMEType, chunk.SHA256, locator,
			chunk.SourceVersion, chunk.ExtractorVersion, chunk.ChunkerVersion,
			model, Dimensions, vectorLiteral(embedding.Values)); err != nil {
			return err
		}
		chunkIDs = append(chunkIDs, chunk.ID)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM companion_artifact_chunks
		WHERE tenant_id=$1 AND virployee_id=$2 AND product_surface=$3 AND subject_id=$4
		  AND repository_generation=$5 AND embedding_model=$6 AND NOT (chunk_id = ANY($7))
	`, scope.TenantID, scope.VirployeeID, scope.ProductSurface, scope.SubjectID, scope.RepositoryGeneration, model, chunkIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) DeleteGeneration(ctx context.Context, scope artifacts.Scope) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM companion_artifact_chunks
		WHERE tenant_id=$1 AND virployee_id=$2 AND product_surface=$3 AND subject_id=$4 AND repository_generation=$5`,
		scope.TenantID, scope.VirployeeID, scope.ProductSurface, scope.SubjectID, scope.RepositoryGeneration)
	return err
}

func (r *Repository) Search(ctx context.Context, query artifacts.RetrievalQuery, vector []float32, model string) ([]artifacts.RetrievalHit, error) {
	if err := validateScope(query.Scope); err != nil {
		return nil, err
	}
	if len(vector) != Dimensions || strings.TrimSpace(model) == "" || strings.TrimSpace(query.Text) == "" {
		return nil, errors.New("retrieval vector, model and text are required")
	}
	limit := query.Limit
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	rows, err := r.pool.Query(ctx, `
		WITH scoped AS MATERIALIZED (
			SELECT chunk_id, content, mime_type, source_sha256, document_id, locator,
			       source_version, extractor_version, chunker_version,
			       embedding <=> $8::vector AS vector_distance,
			       ts_rank_cd(search_vector, websearch_to_tsquery('simple', $7)) AS text_rank
			FROM companion_artifact_chunks
			WHERE tenant_id=$1 AND virployee_id=$2 AND product_surface=$3 AND subject_id=$4
			  AND repository_generation=$5 AND embedding_model=$6
		)
		SELECT chunk_id, content, mime_type, source_sha256, document_id, locator,
		       source_version, extractor_version, chunker_version,
		       (GREATEST(0, 1-vector_distance) * 0.75 + LEAST(1, text_rank) * 0.25) AS score
		FROM scoped
		ORDER BY score DESC, chunk_id
		LIMIT $9
	`, query.Scope.TenantID, query.Scope.VirployeeID, query.Scope.ProductSurface, query.Scope.SubjectID,
		query.Scope.RepositoryGeneration, model, query.Text, vectorLiteral(vector), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]artifacts.RetrievalHit, 0)
	for rows.Next() {
		var chunk artifacts.Chunk
		var locatorJSON []byte
		var score float64
		if err := rows.Scan(&chunk.ID, &chunk.Text, &chunk.MIMEType, &chunk.SHA256, &chunk.DocumentID,
			&locatorJSON, &chunk.SourceVersion, &chunk.ExtractorVersion, &chunk.ChunkerVersion, &score); err != nil {
			return nil, err
		}
		if string(locatorJSON) != "null" && string(locatorJSON) != "{}" {
			if err := json.Unmarshal(locatorJSON, &chunk.Locator); err != nil {
				return nil, err
			}
		}
		out = append(out, artifacts.RetrievalHit{Chunk: chunk, Score: score})
	}
	return out, rows.Err()
}

func validateScope(scope artifacts.Scope) error {
	if strings.TrimSpace(scope.TenantID) == "" || scope.VirployeeID.String() == "" ||
		strings.TrimSpace(scope.ProductSurface) == "" || strings.TrimSpace(scope.SubjectID) == "" ||
		strings.TrimSpace(scope.RepositoryGeneration) == "" {
		return errors.New("artifact index scope is incomplete")
	}
	return nil
}

func vectorLiteral(values []float32) string {
	var builder strings.Builder
	builder.Grow(len(values) * 10)
	builder.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			builder.WriteByte(',')
		}
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			value = 0
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String()
}
