package securityevals

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) SaveReport(ctx context.Context, report Report) (Report, error) {
	if report.Suite == "" {
		report.Suite = "security-adversarial"
	}
	raw := report.Raw
	if raw == nil {
		raw = map[string]any{"results": report.Results}
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return Report{}, fmt.Errorf("marshal security eval report: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_security_eval_reports
			(org_id, suite, status, score, threshold, report_json, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, org_id, suite, status, score, threshold, report_json, created_by, created_at
	`, strings.TrimSpace(report.OrgID), strings.TrimSpace(report.Suite), report.Status, report.Score, report.Threshold, payload, strings.TrimSpace(report.CreatedBy))
	return scanReport(row)
}

func (r *PostgresRepository) ListReports(ctx context.Context, orgID, suite string, limit int) ([]Report, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `
		SELECT id, org_id, suite, status, score, threshold, report_json, created_by, created_at
		FROM companion_security_eval_reports
		WHERE true`
	args := []any{}
	if orgID = strings.TrimSpace(orgID); orgID != "" {
		args = append(args, orgID)
		query += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	if suite = strings.TrimSpace(suite); suite != "" {
		args = append(args, suite)
		query += fmt.Sprintf(" AND suite = $%d", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list security eval reports: %w", err)
	}
	defer rows.Close()
	out := make([]Report, 0)
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, report)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReport(row rowScanner) (Report, error) {
	var (
		report Report
		raw    []byte
	)
	if err := row.Scan(&report.ID, &report.OrgID, &report.Suite, &report.Status, &report.Score, &report.Threshold, &raw, &report.CreatedBy, &report.CreatedAt); err != nil {
		return Report{}, err
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &report.Raw)
		if resultsRaw, err := json.Marshal(report.Raw["results"]); err == nil {
			_ = json.Unmarshal(resultsRaw, &report.Results)
		}
	}
	if report.ID == uuid.Nil {
		report.ID = uuid.New()
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	return report, nil
}
