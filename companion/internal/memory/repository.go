package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

// PostgresRepository implementación PostgreSQL del repositorio de memoria.
type PostgresRepository struct {
	db *sharedpostgres.DB
}

// NewPostgresRepository crea un nuevo repositorio de memoria.
func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectMemory = `
	SELECT id, org_id, user_id, product_surface, kind, memory_type, classification, scope_type, scope_id, key,
	       payload_json, content_text, provenance_json, confidence, retention_policy,
	       trust_score, status, source, embedding_namespace, embedding_model, embedding_json,
	       supersedes_id, superseded_by_id, conflict_group_id, last_verified_at,
	       confidence_decay_at, poisoning_flags,
	       version, created_at, updated_at, expires_at
	FROM companion_memory_entries`

// Upsert crea o actualiza una entrada de memoria con versión optimista.
func (r *PostgresRepository) Upsert(ctx context.Context, e domain.MemoryEntry) (domain.MemoryEntry, error) {
	now := time.Now().UTC()

	if e.Version == 0 {
		// Insert nuevo
		e.ID = uuid.New()
		e.Version = 1
		e.CreatedAt = now
		e.UpdatedAt = now

		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO companion_memory_entries
				(id, org_id, user_id, product_surface, kind, memory_type, classification, scope_type, scope_id, key,
				 payload_json, content_text, provenance_json, confidence, retention_policy,
				 trust_score, status, source, embedding_namespace, embedding_model, embedding_json,
				 supersedes_id, superseded_by_id, conflict_group_id, last_verified_at, confidence_decay_at,
				 poisoning_flags, version, created_at, updated_at, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31)
		`, e.ID, e.OrgID, e.UserID, e.ProductSurface, e.Kind, e.MemoryType, e.Classification, e.ScopeType, e.ScopeID, e.Key,
			e.PayloadJSON, e.ContentText, e.ProvenanceJSON, e.Confidence, e.RetentionPolicy,
			e.TrustScore, e.Status, e.Source, e.EmbeddingNamespace, e.EmbeddingModel, e.EmbeddingJSON,
			e.SupersedesID, e.SupersededByID, e.ConflictGroupID, e.LastVerifiedAt, e.ConfidenceDecayAt,
			e.PoisoningFlags, e.Version, e.CreatedAt, e.UpdatedAt, e.ExpiresAt)
		if err != nil {
			return domain.MemoryEntry{}, fmt.Errorf("insert memory: %w", err)
		}
		if err := r.recordAudit(ctx, e, "insert"); err != nil {
			return domain.MemoryEntry{}, err
		}
		return e, nil
	}

	// Update con versión optimista
	newVersion := e.Version + 1
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_memory_entries
		SET org_id = $3, user_id = $4, product_surface = $5, memory_type = $6, classification = $7,
		    payload_json = $8, content_text = $9, provenance_json = $10, confidence = $11,
		    retention_policy = $12, trust_score = $13, status = $14, source = $15,
		    embedding_namespace = $16, embedding_model = $17, embedding_json = $18,
		    supersedes_id = $19, superseded_by_id = $20, conflict_group_id = $21,
		    last_verified_at = $22, confidence_decay_at = $23, poisoning_flags = $24,
		    version = $25, updated_at = $26, expires_at = $27
		WHERE id = $1 AND version = $2
	`, e.ID, e.Version, e.OrgID, e.UserID, e.ProductSurface, e.MemoryType, e.Classification,
		e.PayloadJSON, e.ContentText, e.ProvenanceJSON, e.Confidence, e.RetentionPolicy,
		e.TrustScore, e.Status, e.Source, e.EmbeddingNamespace, e.EmbeddingModel, e.EmbeddingJSON,
		e.SupersedesID, e.SupersededByID, e.ConflictGroupID, e.LastVerifiedAt, e.ConfidenceDecayAt,
		e.PoisoningFlags, newVersion, now, e.ExpiresAt)
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("update memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.MemoryEntry{}, ErrVersionConflict
	}
	e.Version = newVersion
	e.UpdatedAt = now
	if err := r.recordAudit(ctx, e, "update"); err != nil {
		return domain.MemoryEntry{}, err
	}
	return e, nil
}

// Get obtiene una entrada de memoria por ID.
func (r *PostgresRepository) Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error) {
	row := r.db.Pool().QueryRow(ctx, selectMemory+` WHERE id = $1`, id)
	entry, err := scanMemoryEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MemoryEntry{}, ErrNotFound
		}
		return domain.MemoryEntry{}, fmt.Errorf("get memory: %w", err)
	}
	return entry, nil
}

// GetByScopeKey obtiene una entrada de memoria por scope, kind y key.
func (r *PostgresRepository) GetByScopeKey(ctx context.Context, orgID, productSurface string, scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) (domain.MemoryEntry, error) {
	row := r.db.Pool().QueryRow(ctx, selectMemory+` WHERE org_id = $1 AND product_surface = $2 AND scope_type = $3 AND scope_id = $4 AND kind = $5 AND key = $6`,
		orgID, productSurface, scopeType, scopeID, kind, key)
	entry, err := scanMemoryEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MemoryEntry{}, ErrNotFound
		}
		return domain.MemoryEntry{}, fmt.Errorf("get memory by scope key: %w", err)
	}
	return entry, nil
}

// Find busca entradas de memoria por scope y kind.
func (r *PostgresRepository) Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}

	query := selectMemory + ` WHERE org_id = $1 AND product_surface = $2 AND scope_type = $3 AND scope_id = $4 AND status IN ('active','conflict')`
	args := []any{q.OrgID, q.ProductSurface, q.ScopeType, q.ScopeID}
	if q.UserID != "" {
		query += fmt.Sprintf(` AND (user_id = '' OR user_id = $%d)`, len(args)+1)
		args = append(args, q.UserID)
	}

	if q.Kind != "" {
		query += fmt.Sprintf(` AND kind = $%d`, len(args)+1)
		args = append(args, q.Kind)
	}
	if q.MemoryType != "" {
		query += fmt.Sprintf(` AND memory_type = $%d`, len(args)+1)
		args = append(args, q.MemoryType)
	}
	query += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d`, len(args)+1)
	args = append(args, q.Limit)

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find memory: %w", err)
	}
	defer rows.Close()

	var out []domain.MemoryEntry
	for rows.Next() {
		entry, err := scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// Delete elimina una entrada de memoria.
func (r *PostgresRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM companion_memory_entries WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpired elimina entradas expiradas.
func (r *PostgresRepository) PurgeExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Pool().Exec(ctx, `
		DELETE FROM companion_memory_entries WHERE expires_at IS NOT NULL AND expires_at < $1
	`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("purge expired: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountByScope devuelve cuántas entradas vivas existen en (scope_type, scope_id).
// Excluye entradas expiradas (expires_at < now) porque PurgeExpired las va a
// drenar en el próximo loop; contarlas inflaría el quota artificialmente.
func (r *PostgresRepository) CountByScope(ctx context.Context, scopeType domain.ScopeType, scopeID string) (int, error) {
	var n int
	err := r.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM companion_memory_entries
		WHERE scope_type = $1 AND scope_id = $2
		  AND (expires_at IS NULL OR expires_at > $3)
	`, scopeType, scopeID, time.Now().UTC()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count memory by scope: %w", err)
	}
	return n, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemoryEntry(row rowScanner) (domain.MemoryEntry, error) {
	var e domain.MemoryEntry
	var payloadRaw []byte
	var embeddingRaw []byte
	var expiresAt *time.Time

	err := row.Scan(
		&e.ID, &e.OrgID, &e.UserID, &e.ProductSurface, &e.Kind, &e.MemoryType, &e.Classification, &e.ScopeType, &e.ScopeID, &e.Key,
		&payloadRaw, &e.ContentText, &e.ProvenanceJSON, &e.Confidence, &e.RetentionPolicy,
		&e.TrustScore, &e.Status, &e.Source, &e.EmbeddingNamespace, &e.EmbeddingModel, &embeddingRaw,
		&e.SupersedesID, &e.SupersededByID, &e.ConflictGroupID, &e.LastVerifiedAt,
		&e.ConfidenceDecayAt, &e.PoisoningFlags, &e.Version,
		&e.CreatedAt, &e.UpdatedAt, &expiresAt,
	)
	if err != nil {
		return domain.MemoryEntry{}, err
	}
	if payloadRaw != nil {
		e.PayloadJSON = json.RawMessage(payloadRaw)
	}
	if len(e.ProvenanceJSON) == 0 {
		e.ProvenanceJSON = json.RawMessage(`{}`)
	}
	if embeddingRaw != nil {
		e.EmbeddingJSON = json.RawMessage(embeddingRaw)
	}
	if e.ProductSurface == "" {
		e.ProductSurface = "companion"
	}
	if e.MemoryType == "" {
		e.MemoryType = domain.TypeForKind(e.Kind)
	}
	if e.Classification == "" {
		e.Classification = domain.ClassForKind(e.Kind)
	}
	if e.Confidence == 0 {
		e.Confidence = 1
	}
	if e.TrustScore == 0 {
		e.TrustScore = e.Confidence
	}
	if e.RetentionPolicy == "" {
		e.RetentionPolicy = "default"
	}
	if e.Status == "" {
		e.Status = "active"
	}
	if e.EmbeddingNamespace == "" {
		e.EmbeddingNamespace = e.OrgID + ":" + e.ProductSurface
	}
	if e.EmbeddingModel == "" {
		e.EmbeddingModel = "hash-v1"
	}
	if len(e.EmbeddingJSON) == 0 {
		e.EmbeddingJSON = json.RawMessage(`[]`)
	}
	e.ExpiresAt = expiresAt
	return e, nil
}

func (r *PostgresRepository) recordAudit(ctx context.Context, e domain.MemoryEntry, action string) error {
	payload, err := json.Marshal(map[string]any{
		"key":             e.Key,
		"kind":            e.Kind,
		"memory_type":     e.MemoryType,
		"status":          e.Status,
		"confidence":      e.Confidence,
		"trust_score":     e.TrustScore,
		"poisoning_flags": e.PoisoningFlags,
		"version":         e.Version,
	})
	if err != nil {
		return fmt.Errorf("marshal memory audit: %w", err)
	}
	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO companion_memory_audit
			(memory_id, org_id, product_surface, action, status, payload_json)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, e.ID, e.OrgID, e.ProductSurface, action, e.Status, payload)
	if err != nil {
		return fmt.Errorf("record memory audit: %w", err)
	}
	return nil
}
