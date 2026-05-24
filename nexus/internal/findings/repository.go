package findings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/devpablocristo/nexus/internal/findings/usecases/domain"
)

var ErrNotFound = domainerr.NotFound("not found")

type RuleFilter struct {
	OrgID           string
	OwnerSystem     string
	SourceSystem    string
	FactType        string
	IncludeArchived bool
	ArchivedOnly    bool
}

type FindingFilter struct {
	OrgID         string
	OwnerSystem   string
	SourceSystem  string
	FactType      string
	SubjectID     string
	SourceEventID string
	Status        string
	Limit         int
}

type Repository interface {
	UpsertRule(ctx context.Context, rule domain.FindingRule) (domain.FindingRule, error)
	GetRule(ctx context.Context, id uuid.UUID) (domain.FindingRule, error)
	ListRules(ctx context.Context, filter RuleFilter) ([]domain.FindingRule, error)
	UpdateRule(ctx context.Context, rule domain.FindingRule) (domain.FindingRule, error)
	SoftDelete(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error
	Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error
	HardDelete(ctx context.Context, tenantID string, resourceID uuid.UUID) error
	IsArchived(ctx context.Context, tenantID string, resourceID uuid.UUID) (bool, error)
	GetEvaluationBySource(ctx context.Context, orgID, sourceSystem, factType, sourceEventID string) (domain.FactEvaluation, error)
	GetEvaluation(ctx context.Context, id uuid.UUID) (domain.FactEvaluation, error)
	CreateEvaluationWithFindings(ctx context.Context, evaluation domain.FactEvaluation, findings []domain.Finding) (domain.FactEvaluation, []domain.Finding, error)
	ListFindings(ctx context.Context, filter FindingFilter) ([]domain.Finding, error)
	GetFinding(ctx context.Context, id uuid.UUID) (domain.Finding, error)
	UpdateFindingStatus(ctx context.Context, id uuid.UUID, status domain.FindingStatus, note string) (domain.Finding, error)
}

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectRuleSQL = `
	SELECT id, org_id, owner_system, source_system, fact_type, code, name, description,
	       expression, severity, title, message, recommendation, mode, enabled, priority,
	       archived_at, created_at, updated_at
	FROM finding_rules`

const selectEvaluationSQL = `
	SELECT id, org_id, owner_system, source_system, fact_type, source_event_id,
	       subject_type, subject_id, facts_json, created_at
	FROM fact_evaluations`

const selectFindingSQL = `
	SELECT id, org_id, evaluation_id, rule_id, owner_system, source_system, fact_type,
	       source_event_id, subject_type, subject_id, code, severity, title, message,
	       recommendation, evidence_json, status, resolution_note, created_at, updated_at
	FROM findings`

func (r *PostgresRepository) UpsertRule(ctx context.Context, rule domain.FindingRule) (domain.FindingRule, error) {
	now := time.Now().UTC()
	if rule.ID == uuid.Nil {
		rule.ID = uuid.New()
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now

	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO finding_rules (
			id, org_id, owner_system, source_system, fact_type, code, name, description,
			expression, severity, title, message, recommendation, mode, enabled, priority,
			created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		ON CONFLICT (org_id, owner_system, code)
		DO UPDATE SET
			source_system = EXCLUDED.source_system,
			fact_type = EXCLUDED.fact_type,
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			expression = EXCLUDED.expression,
			severity = EXCLUDED.severity,
			title = EXCLUDED.title,
			message = EXCLUDED.message,
			recommendation = EXCLUDED.recommendation,
			mode = EXCLUDED.mode,
			enabled = EXCLUDED.enabled,
			priority = EXCLUDED.priority,
			archived_at = NULL,
			updated_at = EXCLUDED.updated_at
		RETURNING id, org_id, owner_system, source_system, fact_type, code, name, description,
		          expression, severity, title, message, recommendation, mode, enabled, priority,
		          archived_at, created_at, updated_at
	`, rule.ID, rule.OrgID, rule.OwnerSystem, rule.SourceSystem, rule.FactType, rule.Code, rule.Name, rule.Description,
		rule.Expression, rule.Severity, rule.Title, rule.Message, rule.Recommendation, rule.Mode, rule.Enabled, rule.Priority,
		rule.CreatedAt, rule.UpdatedAt)
	return scanRule(row)
}

func (r *PostgresRepository) GetRule(ctx context.Context, id uuid.UUID) (domain.FindingRule, error) {
	row := r.db.Pool().QueryRow(ctx, selectRuleSQL+` WHERE id = $1`, id)
	rule, err := scanRule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FindingRule{}, ErrNotFound
		}
		return domain.FindingRule{}, err
	}
	return rule, nil
}

func (r *PostgresRepository) ListRules(ctx context.Context, filter RuleFilter) ([]domain.FindingRule, error) {
	query := selectRuleSQL + ` WHERE org_id = $1`
	args := []any{filter.OrgID}
	if filter.OwnerSystem != "" {
		query += fmt.Sprintf(` AND owner_system = $%d`, len(args)+1)
		args = append(args, filter.OwnerSystem)
	}
	if filter.SourceSystem != "" {
		query += fmt.Sprintf(` AND source_system = $%d`, len(args)+1)
		args = append(args, filter.SourceSystem)
	}
	if filter.FactType != "" {
		query += fmt.Sprintf(` AND fact_type = $%d`, len(args)+1)
		args = append(args, filter.FactType)
	}
	if filter.ArchivedOnly {
		query += ` AND archived_at IS NOT NULL`
	} else if !filter.IncludeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY priority ASC, code ASC`

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list finding rules: %w", err)
	}
	defer rows.Close()

	out := make([]domain.FindingRule, 0)
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateRule(ctx context.Context, rule domain.FindingRule) (domain.FindingRule, error) {
	rule.UpdatedAt = time.Now().UTC()
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE finding_rules SET
			owner_system = $2,
			source_system = $3,
			fact_type = $4,
			code = $5,
			name = $6,
			description = $7,
			expression = $8,
			severity = $9,
			title = $10,
			message = $11,
			recommendation = $12,
			mode = $13,
			enabled = $14,
			priority = $15,
			updated_at = $16
		WHERE id = $1
		RETURNING id, org_id, owner_system, source_system, fact_type, code, name, description,
		          expression, severity, title, message, recommendation, mode, enabled, priority,
		          archived_at, created_at, updated_at
	`, rule.ID, rule.OwnerSystem, rule.SourceSystem, rule.FactType, rule.Code, rule.Name, rule.Description,
		rule.Expression, rule.Severity, rule.Title, rule.Message, rule.Recommendation, rule.Mode, rule.Enabled,
		rule.Priority, rule.UpdatedAt)
	updated, err := scanRule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FindingRule{}, ErrNotFound
		}
		return domain.FindingRule{}, err
	}
	return updated, nil
}

func (r *PostgresRepository) SoftDelete(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error {
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE finding_rules SET archived_at = $3, updated_at = $3
		WHERE id = $1 AND org_id = $2 AND archived_at IS NULL
	`, resourceID, tenantID, at.UTC())
	if err != nil {
		return fmt.Errorf("archive finding rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE finding_rules SET archived_at = NULL, updated_at = $3
		WHERE id = $1 AND org_id = $2 AND archived_at IS NOT NULL
	`, resourceID, tenantID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("restore finding rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) HardDelete(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM finding_rules WHERE id = $1 AND org_id = $2`, resourceID, tenantID)
	if err != nil {
		return fmt.Errorf("delete finding rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) IsArchived(ctx context.Context, tenantID string, resourceID uuid.UUID) (bool, error) {
	var archived bool
	err := r.db.Pool().QueryRow(ctx, `
		SELECT archived_at IS NOT NULL FROM finding_rules WHERE id = $1 AND org_id = $2
	`, resourceID, tenantID).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	return archived, nil
}

func (r *PostgresRepository) GetEvaluationBySource(ctx context.Context, orgID, sourceSystem, factType, sourceEventID string) (domain.FactEvaluation, error) {
	row := r.db.Pool().QueryRow(ctx, selectEvaluationSQL+`
		WHERE org_id = $1 AND source_system = $2 AND fact_type = $3 AND source_event_id = $4
	`, orgID, sourceSystem, factType, sourceEventID)
	eval, err := scanEvaluation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FactEvaluation{}, ErrNotFound
		}
		return domain.FactEvaluation{}, err
	}
	return eval, nil
}

func (r *PostgresRepository) GetEvaluation(ctx context.Context, id uuid.UUID) (domain.FactEvaluation, error) {
	row := r.db.Pool().QueryRow(ctx, selectEvaluationSQL+` WHERE id = $1`, id)
	eval, err := scanEvaluation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FactEvaluation{}, ErrNotFound
		}
		return domain.FactEvaluation{}, err
	}
	return eval, nil
}

func (r *PostgresRepository) CreateEvaluationWithFindings(ctx context.Context, evaluation domain.FactEvaluation, items []domain.Finding) (domain.FactEvaluation, []domain.Finding, error) {
	now := time.Now().UTC()
	if evaluation.ID == uuid.Nil {
		evaluation.ID = uuid.New()
	}
	if evaluation.CreatedAt.IsZero() {
		evaluation.CreatedAt = now
	}
	factsJSON, err := json.Marshal(nonNilMap(evaluation.Facts))
	if err != nil {
		return domain.FactEvaluation{}, nil, fmt.Errorf("marshal facts: %w", err)
	}

	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return domain.FactEvaluation{}, nil, fmt.Errorf("begin findings tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO fact_evaluations (
			id, org_id, owner_system, source_system, fact_type, source_event_id,
			subject_type, subject_id, facts_json, created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (org_id, source_system, fact_type, source_event_id) DO NOTHING
		RETURNING id, org_id, owner_system, source_system, fact_type, source_event_id,
		          subject_type, subject_id, facts_json, created_at
	`, evaluation.ID, evaluation.OrgID, evaluation.OwnerSystem, evaluation.SourceSystem, evaluation.FactType,
		evaluation.SourceEventID, evaluation.SubjectType, evaluation.SubjectID, factsJSON, evaluation.CreatedAt)
	created, err := scanEvaluation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.Rollback(ctx); err != nil {
				return domain.FactEvaluation{}, nil, err
			}
			existing, err := r.GetEvaluationBySource(ctx, evaluation.OrgID, evaluation.SourceSystem, evaluation.FactType, evaluation.SourceEventID)
			if err != nil {
				return domain.FactEvaluation{}, nil, err
			}
			findings, err := r.ListFindings(ctx, FindingFilter{OrgID: existing.OrgID, SourceSystem: existing.SourceSystem, FactType: existing.FactType, SourceEventID: existing.SourceEventID, Limit: 1000})
			return existing, findings, err
		}
		return domain.FactEvaluation{}, nil, err
	}

	out := make([]domain.Finding, 0, len(items))
	for _, item := range items {
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
		item.EvaluationID = created.ID
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		evidenceJSON, err := json.Marshal(nonNilMap(item.Evidence))
		if err != nil {
			return domain.FactEvaluation{}, nil, fmt.Errorf("marshal finding evidence: %w", err)
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO findings (
				id, org_id, evaluation_id, rule_id, owner_system, source_system, fact_type,
				source_event_id, subject_type, subject_id, code, severity, title, message,
				recommendation, evidence_json, status, resolution_note, created_at, updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
			ON CONFLICT (evaluation_id, code) DO UPDATE SET
				severity = EXCLUDED.severity,
				title = EXCLUDED.title,
				message = EXCLUDED.message,
				recommendation = EXCLUDED.recommendation,
				evidence_json = EXCLUDED.evidence_json,
				status = EXCLUDED.status,
				updated_at = EXCLUDED.updated_at
			RETURNING id, org_id, evaluation_id, rule_id, owner_system, source_system, fact_type,
			          source_event_id, subject_type, subject_id, code, severity, title, message,
			          recommendation, evidence_json, status, resolution_note, created_at, updated_at
		`, item.ID, item.OrgID, item.EvaluationID, item.RuleID, item.OwnerSystem, item.SourceSystem, item.FactType,
			item.SourceEventID, item.SubjectType, item.SubjectID, item.Code, item.Severity, item.Title, item.Message,
			item.Recommendation, evidenceJSON, item.Status, item.ResolutionNote, item.CreatedAt, item.UpdatedAt)
		stored, err := scanFinding(row)
		if err != nil {
			return domain.FactEvaluation{}, nil, err
		}
		out = append(out, stored)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FactEvaluation{}, nil, fmt.Errorf("commit findings tx: %w", err)
	}
	return created, out, nil
}

func (r *PostgresRepository) ListFindings(ctx context.Context, filter FindingFilter) ([]domain.Finding, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := selectFindingSQL + ` WHERE org_id = $1`
	args := []any{filter.OrgID}
	if filter.OwnerSystem != "" {
		query += fmt.Sprintf(` AND owner_system = $%d`, len(args)+1)
		args = append(args, filter.OwnerSystem)
	}
	if filter.SourceSystem != "" {
		query += fmt.Sprintf(` AND source_system = $%d`, len(args)+1)
		args = append(args, filter.SourceSystem)
	}
	if filter.FactType != "" {
		query += fmt.Sprintf(` AND fact_type = $%d`, len(args)+1)
		args = append(args, filter.FactType)
	}
	if filter.SubjectID != "" {
		query += fmt.Sprintf(` AND subject_id = $%d`, len(args)+1)
		args = append(args, filter.SubjectID)
	}
	if filter.SourceEventID != "" {
		query += fmt.Sprintf(` AND source_event_id = $%d`, len(args)+1)
		args = append(args, filter.SourceEventID)
	}
	if filter.Status != "" {
		query += fmt.Sprintf(` AND status = $%d`, len(args)+1)
		args = append(args, filter.Status)
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Finding, 0)
	for rows.Next() {
		item, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetFinding(ctx context.Context, id uuid.UUID) (domain.Finding, error) {
	row := r.db.Pool().QueryRow(ctx, selectFindingSQL+` WHERE id = $1`, id)
	item, err := scanFinding(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Finding{}, ErrNotFound
		}
		return domain.Finding{}, err
	}
	return item, nil
}

func (r *PostgresRepository) UpdateFindingStatus(ctx context.Context, id uuid.UUID, status domain.FindingStatus, note string) (domain.Finding, error) {
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE findings SET status = $2, resolution_note = $3, updated_at = $4
		WHERE id = $1
		RETURNING id, org_id, evaluation_id, rule_id, owner_system, source_system, fact_type,
		          source_event_id, subject_type, subject_id, code, severity, title, message,
		          recommendation, evidence_json, status, resolution_note, created_at, updated_at
	`, id, status, note, time.Now().UTC())
	item, err := scanFinding(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Finding{}, ErrNotFound
		}
		return domain.Finding{}, err
	}
	return item, nil
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanRule(row scanRow) (domain.FindingRule, error) {
	var rule domain.FindingRule
	if err := row.Scan(
		&rule.ID, &rule.OrgID, &rule.OwnerSystem, &rule.SourceSystem, &rule.FactType, &rule.Code,
		&rule.Name, &rule.Description, &rule.Expression, &rule.Severity, &rule.Title, &rule.Message,
		&rule.Recommendation, &rule.Mode, &rule.Enabled, &rule.Priority, &rule.ArchivedAt,
		&rule.CreatedAt, &rule.UpdatedAt,
	); err != nil {
		return domain.FindingRule{}, err
	}
	return rule, nil
}

func scanEvaluation(row scanRow) (domain.FactEvaluation, error) {
	var item domain.FactEvaluation
	var factsJSON []byte
	if err := row.Scan(
		&item.ID, &item.OrgID, &item.OwnerSystem, &item.SourceSystem, &item.FactType,
		&item.SourceEventID, &item.SubjectType, &item.SubjectID, &factsJSON, &item.CreatedAt,
	); err != nil {
		return domain.FactEvaluation{}, err
	}
	if err := json.Unmarshal(factsJSON, &item.Facts); err != nil {
		return domain.FactEvaluation{}, err
	}
	if item.Facts == nil {
		item.Facts = map[string]any{}
	}
	return item, nil
}

func scanFinding(row scanRow) (domain.Finding, error) {
	var item domain.Finding
	var evidenceJSON []byte
	if err := row.Scan(
		&item.ID, &item.OrgID, &item.EvaluationID, &item.RuleID, &item.OwnerSystem, &item.SourceSystem,
		&item.FactType, &item.SourceEventID, &item.SubjectType, &item.SubjectID, &item.Code,
		&item.Severity, &item.Title, &item.Message, &item.Recommendation, &evidenceJSON,
		&item.Status, &item.ResolutionNote, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domain.Finding{}, err
	}
	if err := json.Unmarshal(evidenceJSON, &item.Evidence); err != nil {
		return domain.Finding{}, err
	}
	if item.Evidence == nil {
		item.Evidence = map[string]any{}
	}
	return item, nil
}

func nonNilMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}
