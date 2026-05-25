package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

const (
	ManifestStatusDraft      = "draft"
	ManifestStatusActive     = "active"
	ManifestStatusDeprecated = "deprecated"

	ManifestSourceGenerated = "generated"
	ManifestSourceImported  = "imported"

	ConformanceStatusPassed = "passed"
	ConformanceStatusFailed = "failed"
)

var ErrManifestNotFound = errors.New("capability manifest not found")

type ManifestRecord struct {
	ID         uuid.UUID `json:"id"`
	Manifest   Manifest  `json:"manifest"`
	Status     string    `json:"status"`
	Source     string    `json:"source"`
	ImportedBy string    `json:"imported_by,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type ManifestFilter struct {
	CapabilityID string
	Status       string
	Limit        int
}

type ConformanceRun struct {
	ID           uuid.UUID       `json:"id"`
	OrgID        string          `json:"org_id,omitempty"`
	CapabilityID string          `json:"capability_id"`
	Version      string          `json:"version"`
	Status       string          `json:"status"`
	Checks       map[string]bool `json:"checks"`
	Errors       []string        `json:"errors,omitempty"`
	Evidence     map[string]any  `json:"evidence,omitempty"`
	CreatedBy    string          `json:"created_by,omitempty"`
	CreatedAt    time.Time       `json:"created_at,omitempty"`
}

type Repository interface {
	UpsertManifest(ctx context.Context, record ManifestRecord) (ManifestRecord, error)
	GetManifest(ctx context.Context, capabilityID, version string) (ManifestRecord, error)
	ListManifests(ctx context.Context, filter ManifestFilter) ([]ManifestRecord, error)
	UpdateManifestStatus(ctx context.Context, capabilityID, version, status string) (ManifestRecord, error)
	SaveConformanceRun(ctx context.Context, run ConformanceRun) (ConformanceRun, error)
	ListConformanceRuns(ctx context.Context, orgID, capabilityID string, limit int) ([]ConformanceRun, error)
}

type CapabilityManifestRepository interface {
	Repository
}

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) UpsertManifest(ctx context.Context, record ManifestRecord) (ManifestRecord, error) {
	record.Manifest = record.Manifest.Normalize()
	if err := record.Manifest.Validate(); err != nil {
		return ManifestRecord{}, err
	}
	record.Status = normalizeManifestStatus(record.Status)
	record.Source = normalizeManifestSource(record.Source)
	raw, err := json.Marshal(record.Manifest)
	if err != nil {
		return ManifestRecord{}, fmt.Errorf("marshal capability manifest: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_capability_manifests
			(capability_id, version, status, source, manifest_json, imported_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (capability_id, version) DO UPDATE SET
			status = EXCLUDED.status,
			source = EXCLUDED.source,
			manifest_json = EXCLUDED.manifest_json,
			imported_by = EXCLUDED.imported_by,
			updated_at = now()
		RETURNING id, capability_id, version, status, source, manifest_json, imported_by, created_at, updated_at
	`, record.Manifest.CapabilityID, record.Manifest.Version, record.Status, record.Source, raw, strings.TrimSpace(record.ImportedBy))
	return scanManifestRecord(row)
}

func (r *PostgresRepository) GetManifest(ctx context.Context, capabilityID, version string) (ManifestRecord, error) {
	row := r.db.Pool().QueryRow(ctx, selectCapabilityManifest+`
		WHERE capability_id = $1 AND version = $2
	`, strings.TrimSpace(capabilityID), strings.TrimSpace(version))
	record, err := scanManifestRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManifestRecord{}, ErrManifestNotFound
		}
		return ManifestRecord{}, fmt.Errorf("get capability manifest: %w", err)
	}
	return record, nil
}

func (r *PostgresRepository) ListManifests(ctx context.Context, filter ManifestFilter) ([]ManifestRecord, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := selectCapabilityManifest + ` WHERE true`
	args := []any{}
	if value := strings.TrimSpace(filter.CapabilityID); value != "" {
		args = append(args, value)
		query += fmt.Sprintf(` AND capability_id = $%d`, len(args))
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		args = append(args, value)
		query += fmt.Sprintf(` AND status = $%d`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY capability_id ASC, version DESC LIMIT $%d`, len(args))
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list capability manifests: %w", err)
	}
	defer rows.Close()
	out := make([]ManifestRecord, 0)
	for rows.Next() {
		record, err := scanManifestRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateManifestStatus(ctx context.Context, capabilityID, version, status string) (ManifestRecord, error) {
	status = normalizeManifestStatus(status)
	row := r.db.Pool().QueryRow(ctx, selectCapabilityManifest+`
		WHERE capability_id = $1 AND version = $2
	`, strings.TrimSpace(capabilityID), strings.TrimSpace(version))
	if _, err := scanManifestRecord(row); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManifestRecord{}, ErrManifestNotFound
		}
		return ManifestRecord{}, err
	}
	row = r.db.Pool().QueryRow(ctx, `
		UPDATE companion_capability_manifests
		SET status = $3, updated_at = now()
		WHERE capability_id = $1 AND version = $2
		RETURNING id, capability_id, version, status, source, manifest_json, imported_by, created_at, updated_at
	`, strings.TrimSpace(capabilityID), strings.TrimSpace(version), status)
	return scanManifestRecord(row)
}

func (r *PostgresRepository) SaveConformanceRun(ctx context.Context, run ConformanceRun) (ConformanceRun, error) {
	run.CapabilityID = strings.TrimSpace(run.CapabilityID)
	run.Version = strings.TrimSpace(run.Version)
	if run.CapabilityID == "" || run.Version == "" {
		return ConformanceRun{}, fmt.Errorf("capability_id and version are required")
	}
	if run.Status != ConformanceStatusPassed && run.Status != ConformanceStatusFailed {
		run.Status = ConformanceStatusFailed
	}
	checksJSON, err := json.Marshal(run.Checks)
	if err != nil {
		return ConformanceRun{}, fmt.Errorf("marshal conformance checks: %w", err)
	}
	errorsJSON, err := json.Marshal(run.Errors)
	if err != nil {
		return ConformanceRun{}, fmt.Errorf("marshal conformance errors: %w", err)
	}
	evidence := run.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return ConformanceRun{}, fmt.Errorf("marshal conformance evidence: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_capability_conformance_runs
			(org_id, capability_id, version, status, checks_json, errors_json, evidence_json, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, org_id, capability_id, version, status, checks_json, errors_json, evidence_json, created_by, created_at
	`, strings.TrimSpace(run.OrgID), run.CapabilityID, run.Version, run.Status, checksJSON, errorsJSON, evidenceJSON, strings.TrimSpace(run.CreatedBy))
	return scanConformanceRun(row)
}

func (r *PostgresRepository) ListConformanceRuns(ctx context.Context, orgID, capabilityID string, limit int) ([]ConformanceRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := selectConformanceRun + ` WHERE true`
	args := []any{}
	if orgID = strings.TrimSpace(orgID); orgID != "" {
		args = append(args, orgID)
		query += fmt.Sprintf(` AND org_id = $%d`, len(args))
	}
	if capabilityID = strings.TrimSpace(capabilityID); capabilityID != "" {
		args = append(args, capabilityID)
		query += fmt.Sprintf(` AND capability_id = $%d`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args))
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conformance runs: %w", err)
	}
	defer rows.Close()
	out := make([]ConformanceRun, 0)
	for rows.Next() {
		run, err := scanConformanceRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

const selectCapabilityManifest = `
	SELECT id, capability_id, version, status, source, manifest_json, imported_by, created_at, updated_at
	FROM companion_capability_manifests`

const selectConformanceRun = `
	SELECT id, org_id, capability_id, version, status, checks_json, errors_json, evidence_json, created_by, created_at
	FROM companion_capability_conformance_runs`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanManifestRecord(row rowScanner) (ManifestRecord, error) {
	var (
		record ManifestRecord
		raw    []byte
		id     string
	)
	if err := row.Scan(&record.ID, &id, &record.Manifest.Version, &record.Status, &record.Source, &raw, &record.ImportedBy, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return ManifestRecord{}, err
	}
	if err := json.Unmarshal(raw, &record.Manifest); err != nil {
		return ManifestRecord{}, fmt.Errorf("unmarshal capability manifest: %w", err)
	}
	if record.Manifest.CapabilityID == "" {
		record.Manifest.CapabilityID = id
	}
	record.Manifest = record.Manifest.Normalize()
	return record, nil
}

func scanConformanceRun(row rowScanner) (ConformanceRun, error) {
	var (
		run         ConformanceRun
		checksRaw   []byte
		errorsRaw   []byte
		evidenceRaw []byte
	)
	if err := row.Scan(&run.ID, &run.OrgID, &run.CapabilityID, &run.Version, &run.Status, &checksRaw, &errorsRaw, &evidenceRaw, &run.CreatedBy, &run.CreatedAt); err != nil {
		return ConformanceRun{}, err
	}
	if len(checksRaw) > 0 {
		_ = json.Unmarshal(checksRaw, &run.Checks)
	}
	if len(errorsRaw) > 0 {
		_ = json.Unmarshal(errorsRaw, &run.Errors)
	}
	if len(evidenceRaw) > 0 {
		_ = json.Unmarshal(evidenceRaw, &run.Evidence)
	}
	if run.Checks == nil {
		run.Checks = map[string]bool{}
	}
	if run.Evidence == nil {
		run.Evidence = map[string]any{}
	}
	return run, nil
}

func normalizeManifestStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case ManifestStatusDraft, ManifestStatusDeprecated:
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ManifestStatusActive
	}
}

func normalizeManifestSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case ManifestSourceImported:
		return ManifestSourceImported
	default:
		return ManifestSourceGenerated
	}
}
