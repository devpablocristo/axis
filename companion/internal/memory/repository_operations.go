package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

func (r *PostgresRepository) ListConflicts(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Pool().Query(ctx, selectMemory+`
		WHERE org_id = $1 AND product_surface = $2 AND status = 'conflict'
		ORDER BY updated_at DESC
		LIMIT $3
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), limit)
	if err != nil {
		return nil, fmt.Errorf("list memory conflicts: %w", err)
	}
	defer rows.Close()
	out := make([]domain.MemoryEntry, 0)
	for rows.Next() {
		entry, err := scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) CreateReview(ctx context.Context, in CreateReviewInput) (MemoryReview, error) {
	if len(in.ProposedPayload) == 0 {
		in.ProposedPayload = json.RawMessage(`{}`)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_memory_reviews
			(org_id, product_surface, memory_id, review_type, status, reason,
			 proposed_content, proposed_payload, created_by)
		VALUES ($1,$2,$3,$4,'open',$5,$6,$7,$8)
		RETURNING id, org_id, product_surface, memory_id, review_type, status, reason,
		          proposed_content, proposed_payload, created_by, decided_by,
		          created_at, updated_at, decided_at
	`, strings.TrimSpace(in.OrgID), strings.TrimSpace(in.ProductSurface), in.MemoryID,
		strings.TrimSpace(in.ReviewType), strings.TrimSpace(in.Reason), in.ProposedContent,
		in.ProposedPayload, strings.TrimSpace(in.CreatedBy))
	review, err := scanMemoryReview(row)
	if err != nil {
		return MemoryReview{}, fmt.Errorf("create memory review: %w", err)
	}
	return review, nil
}

func (r *PostgresRepository) ListReviews(ctx context.Context, orgID, productSurface, status string, limit int) ([]MemoryReview, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `
		SELECT id, org_id, product_surface, memory_id, review_type, status, reason,
		       proposed_content, proposed_payload, created_by, decided_by,
		       created_at, updated_at, decided_at
		FROM companion_memory_reviews
		WHERE org_id = $1 AND product_surface = $2`
	args := []any{strings.TrimSpace(orgID), strings.TrimSpace(productSurface)}
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		query += fmt.Sprintf(` AND status = $%d`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args))
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list memory reviews: %w", err)
	}
	defer rows.Close()
	out := make([]MemoryReview, 0)
	for rows.Next() {
		review, err := scanMemoryReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, review)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateReviewStatus(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, status, decidedBy string) (MemoryReview, error) {
	status = strings.TrimSpace(status)
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE companion_memory_reviews
		SET status = $5,
		    decided_by = $6,
		    decided_at = now(),
		    updated_at = now()
		WHERE id = $1
		  AND org_id = $2
		  AND product_surface = $3
		  AND status IN ('open','approved')
		RETURNING id, org_id, product_surface, memory_id, review_type, status, reason,
		          proposed_content, proposed_payload, created_by, decided_by,
		          created_at, updated_at, decided_at
	`, reviewID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), status, strings.TrimSpace(decidedBy))
	review, err := scanMemoryReview(row)
	if err != nil {
		if errorsIsNoRows(err) {
			return MemoryReview{}, ErrNotFound
		}
		return MemoryReview{}, fmt.Errorf("update memory review status: %w", err)
	}
	return review, nil
}

func (r *PostgresRepository) ApplyReview(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, decidedBy string) (MemoryReview, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return MemoryReview{}, fmt.Errorf("begin apply memory review: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	row := tx.QueryRow(ctx, `
		SELECT id, org_id, product_surface, memory_id, review_type, status, reason,
		       proposed_content, proposed_payload, created_by, decided_by,
		       created_at, updated_at, decided_at
		FROM companion_memory_reviews
		WHERE id = $1 AND org_id = $2 AND product_surface = $3
		FOR UPDATE
	`, reviewID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface))
	review, err := scanMemoryReview(row)
	if err != nil {
		if errorsIsNoRows(err) {
			return MemoryReview{}, ErrNotFound
		}
		return MemoryReview{}, fmt.Errorf("get memory review: %w", err)
	}
	if review.MemoryID == nil || *review.MemoryID == uuid.Nil {
		return MemoryReview{}, fmt.Errorf("memory review has no memory_id")
	}
	if review.Status != "open" && review.Status != "approved" {
		return MemoryReview{}, fmt.Errorf("memory review status %q cannot be applied", review.Status)
	}
	memoryID := *review.MemoryID
	var action string
	switch review.ReviewType {
	case "correction", "conflict":
		action = "review_apply_correction"
		_, err = tx.Exec(ctx, `
			UPDATE companion_memory_entries
			SET content_text = CASE WHEN btrim($2) <> '' THEN $2 ELSE content_text END,
			    payload_json = CASE WHEN $3::jsonb <> '{}'::jsonb THEN $3::jsonb ELSE payload_json END,
			    status = 'active',
			    conflict_group_id = NULL,
			    version = version + 1,
			    updated_at = now(),
			    last_verified_at = now()
			WHERE id = $1 AND org_id = $4 AND product_surface = $5
		`, memoryID, review.ProposedContent, review.ProposedPayload, review.OrgID, review.ProductSurface)
	case "invalidation":
		action = "review_apply_invalidation"
		_, err = tx.Exec(ctx, `
			UPDATE companion_memory_entries
			SET status = 'forgotten',
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1 AND org_id = $2 AND product_surface = $3
		`, memoryID, review.OrgID, review.ProductSurface)
	case "deletion":
		action = "review_apply_deletion"
		_, err = tx.Exec(ctx, `
			DELETE FROM companion_memory_entries
			WHERE id = $1 AND org_id = $2 AND product_surface = $3
		`, memoryID, review.OrgID, review.ProductSurface)
	default:
		return MemoryReview{}, fmt.Errorf("unsupported memory review type %q", review.ReviewType)
	}
	if err != nil {
		return MemoryReview{}, fmt.Errorf("apply memory review mutation: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"review_id":        review.ID.String(),
		"review_type":      review.ReviewType,
		"reason":           review.Reason,
		"decided_by":       decidedBy,
		"proposed_payload": json.RawMessage(defaultMemoryRaw(review.ProposedPayload)),
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO companion_memory_audit
			(memory_id, org_id, product_surface, action, status, payload_json)
		VALUES ($1,$2,$3,$4,'applied',$5)
	`, memoryID, review.OrgID, review.ProductSurface, action, payload)
	if err != nil {
		return MemoryReview{}, fmt.Errorf("audit memory review apply: %w", err)
	}
	row = tx.QueryRow(ctx, `
		UPDATE companion_memory_reviews
		SET status = 'applied',
		    decided_by = $4,
		    decided_at = now(),
		    updated_at = now()
		WHERE id = $1 AND org_id = $2 AND product_surface = $3
		RETURNING id, org_id, product_surface, memory_id, review_type, status, reason,
		          proposed_content, proposed_payload, created_by, decided_by,
		          created_at, updated_at, decided_at
	`, reviewID, review.OrgID, review.ProductSurface, strings.TrimSpace(decidedBy))
	applied, err := scanMemoryReview(row)
	if err != nil {
		return MemoryReview{}, fmt.Errorf("mark memory review applied: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MemoryReview{}, fmt.Errorf("commit memory review apply: %w", err)
	}
	return applied, nil
}

func (r *PostgresRepository) ListAudit(ctx context.Context, orgID, productSurface string, limit int) ([]MemoryAuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, memory_id, org_id, product_surface, action, status, payload_json, created_at
		FROM companion_memory_audit
		WHERE org_id = $1 AND product_surface = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), limit)
	if err != nil {
		return nil, fmt.Errorf("list memory audit: %w", err)
	}
	defer rows.Close()
	out := make([]MemoryAuditEntry, 0)
	for rows.Next() {
		var (
			entry MemoryAuditEntry
			raw   []byte
		)
		if err := rows.Scan(&entry.ID, &entry.MemoryID, &entry.OrgID, &entry.ProductSurface, &entry.Action, &entry.Status, &raw, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entry.Payload = json.RawMessage(raw)
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListSummaries(ctx context.Context, orgID, productSurface string, limit int) ([]MemorySummary, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, org_id, product_surface, scope_type, scope_id, summary_type,
		       version, content_text, source_count, payload_json, created_at
		FROM companion_memory_summaries
		WHERE org_id = $1 AND product_surface = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), limit)
	if err != nil {
		return nil, fmt.Errorf("list memory summaries: %w", err)
	}
	defer rows.Close()
	out := make([]MemorySummary, 0)
	for rows.Next() {
		var (
			summary MemorySummary
			raw     []byte
		)
		if err := rows.Scan(&summary.ID, &summary.OrgID, &summary.ProductSurface, &summary.ScopeType, &summary.ScopeID, &summary.SummaryType, &summary.Version, &summary.ContentText, &summary.SourceCount, &raw, &summary.CreatedAt); err != nil {
			return nil, err
		}
		summary.Payload = json.RawMessage(raw)
		out = append(out, summary)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) CreateSummary(ctx context.Context, summary MemorySummary) (MemorySummary, error) {
	if len(summary.Payload) == 0 {
		summary.Payload = json.RawMessage(`{}`)
	}
	if summary.Version <= 0 {
		var next int
		err := r.db.Pool().QueryRow(ctx, `
			SELECT COALESCE(MAX(version), 0) + 1
			FROM companion_memory_summaries
			WHERE org_id = $1 AND product_surface = $2 AND scope_type = $3 AND scope_id = $4 AND summary_type = $5
		`, summary.OrgID, summary.ProductSurface, summary.ScopeType, summary.ScopeID, firstNonEmptyString(summary.SummaryType, "compaction")).Scan(&next)
		if err != nil {
			return MemorySummary{}, fmt.Errorf("next memory summary version: %w", err)
		}
		summary.Version = next
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_memory_summaries
			(org_id, product_surface, scope_type, scope_id, summary_type, version,
			 content_text, source_count, payload_json)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, org_id, product_surface, scope_type, scope_id, summary_type,
		          version, content_text, source_count, payload_json, created_at
	`, strings.TrimSpace(summary.OrgID), strings.TrimSpace(summary.ProductSurface), strings.TrimSpace(summary.ScopeType),
		strings.TrimSpace(summary.ScopeID), firstNonEmptyString(summary.SummaryType, "compaction"), summary.Version,
		summary.ContentText, summary.SourceCount, summary.Payload)
	var (
		out MemorySummary
		raw []byte
	)
	if err := row.Scan(&out.ID, &out.OrgID, &out.ProductSurface, &out.ScopeType, &out.ScopeID, &out.SummaryType, &out.Version, &out.ContentText, &out.SourceCount, &raw, &out.CreatedAt); err != nil {
		return MemorySummary{}, fmt.Errorf("create memory summary: %w", err)
	}
	out.Payload = json.RawMessage(raw)
	return out, nil
}

func (r *PostgresRepository) ExportByOrg(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	rows, err := r.db.Pool().Query(ctx, selectMemory+`
		WHERE org_id = $1 AND product_surface = $2
		ORDER BY updated_at DESC
		LIMIT $3
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), limit)
	if err != nil {
		return nil, fmt.Errorf("export memory: %w", err)
	}
	defer rows.Close()
	out := make([]domain.MemoryEntry, 0)
	for rows.Next() {
		entry, err := scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (r *PostgresRepository) DeleteByOrg(ctx context.Context, orgID, productSurface string) (int64, error) {
	tag, err := r.db.Pool().Exec(ctx, `
		DELETE FROM companion_memory_entries
		WHERE org_id = $1 AND product_surface = $2
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface))
	if err != nil {
		return 0, fmt.Errorf("delete memory by org: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanMemoryReview(row rowScanner) (MemoryReview, error) {
	var (
		review MemoryReview
		raw    []byte
	)
	if err := row.Scan(&review.ID, &review.OrgID, &review.ProductSurface, &review.MemoryID,
		&review.ReviewType, &review.Status, &review.Reason, &review.ProposedContent, &raw,
		&review.CreatedBy, &review.DecidedBy, &review.CreatedAt, &review.UpdatedAt, &review.DecidedAt); err != nil {
		return MemoryReview{}, err
	}
	review.ProposedPayload = json.RawMessage(raw)
	if review.ProposedPayload == nil {
		review.ProposedPayload = json.RawMessage(`{}`)
	}
	return review, nil
}

func errorsIsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}

func defaultMemoryRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
