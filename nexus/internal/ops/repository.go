package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/google/uuid"
)

type Summary struct {
	ContractsActive          int    `json:"contracts_active"`
	ContractReportsFailed    int    `json:"contract_reports_failed"`
	CallbackPending          int    `json:"callback_pending"`
	CallbackDead             int    `json:"callback_dead"`
	CallbackStuck            int    `json:"callback_stuck"`
	AuditIntegrityFailed     int    `json:"audit_integrity_failed"`
	RateLimitRules           int    `json:"rate_limit_rules"`
	RateLimitBlocks24h       int    `json:"rate_limit_blocks_24h"`
	LegalHoldsActive         int    `json:"legal_holds_active"`
	ExportsFailed            int    `json:"exports_failed"`
	ReconciliationCritical   int    `json:"reconciliation_critical"`
	ReconciliationLastStatus string `json:"reconciliation_last_status,omitempty"`
}

type CallbackDelivery struct {
	ID             uuid.UUID  `json:"id"`
	OutboxEventID  uuid.UUID  `json:"outbox_event_id"`
	OrgID          *string    `json:"org_id,omitempty"`
	EventType      string     `json:"event_type"`
	SubjectType    string     `json:"subject_type"`
	SubjectID      string     `json:"subject_id"`
	TargetURL      string     `json:"target_url"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	LastError      string     `json:"last_error,omitempty"`
	ResponseStatus *int       `json:"response_status,omitempty"`
	LeasedUntil    *time.Time `json:"leased_until,omitempty"`
	PoisonReason   string     `json:"poison_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type LegalHold struct {
	ID          uuid.UUID  `json:"id"`
	OrgID       *string    `json:"org_id,omitempty"`
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id"`
	Reason      string     `json:"reason"`
	Status      string     `json:"status"`
	CreatedBy   string     `json:"created_by"`
	ReleasedBy  string     `json:"released_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ReleasedAt  *time.Time `json:"released_at,omitempty"`
}

type ExportJob struct {
	ID           uuid.UUID      `json:"id"`
	OrgID        *string        `json:"org_id,omitempty"`
	ExportType   string         `json:"export_type"`
	Status       string         `json:"status"`
	SubjectType  string         `json:"subject_type"`
	SubjectID    string         `json:"subject_id"`
	RequestedBy  string         `json:"requested_by"`
	Manifest     map[string]any `json:"manifest"`
	ManifestHash string         `json:"manifest_hash"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
}

type ReconciliationRun struct {
	ID           uuid.UUID `json:"id"`
	OrgID        *string   `json:"org_id,omitempty"`
	Status       string    `json:"status"`
	CheckedItems int       `json:"checked_items"`
	FindingCount int       `json:"finding_count"`
	ReportHash   string    `json:"report_hash"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at"`
}

type ReconciliationFinding struct {
	ID          uuid.UUID      `json:"id"`
	RunID       uuid.UUID      `json:"run_id"`
	OrgID       *string        `json:"org_id,omitempty"`
	Severity    string         `json:"severity"`
	FindingType string         `json:"finding_type"`
	SubjectType string         `json:"subject_type"`
	SubjectID   string         `json:"subject_id"`
	Message     string         `json:"message"`
	Data        map[string]any `json:"data"`
	CreatedAt   time.Time      `json:"created_at"`
}

type ReconciliationReport struct {
	Run      ReconciliationRun       `json:"run"`
	Findings []ReconciliationFinding `json:"findings"`
}

type Repository struct {
	db *sharedpostgres.DB
}

func NewRepository(db *sharedpostgres.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Summary(ctx context.Context) (Summary, error) {
	var out Summary
	if err := r.db.Pool().QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM governance_contracts WHERE status = 'active'),
			(SELECT count(*) FROM governance_contract_validation_reports WHERE valid = false),
			(SELECT count(*) FROM nexus_callback_deliveries WHERE status IN ('pending', 'delivering')),
			(SELECT count(*) FROM nexus_callback_deliveries WHERE status = 'dead'),
			(SELECT count(*) FROM nexus_callback_deliveries WHERE status = 'delivering' AND leased_until < now()),
			(SELECT count(*) FROM audit_integrity_checks WHERE status = 'failed'),
			(SELECT count(*) FROM nexus_rate_limit_rules WHERE enabled = true),
			(SELECT count(*) FROM nexus_rate_limit_decisions WHERE allowed = false AND created_at > now() - interval '24 hours'),
			(SELECT count(*) FROM governance_legal_holds WHERE status = 'active'),
			(SELECT count(*) FROM governance_export_jobs WHERE status = 'failed'),
			(SELECT count(*) FROM nexus_reconciliation_findings WHERE severity = 'critical'),
			COALESCE((SELECT status FROM nexus_reconciliation_runs ORDER BY created_at DESC LIMIT 1), '')
	`).Scan(&out.ContractsActive, &out.ContractReportsFailed, &out.CallbackPending, &out.CallbackDead,
		&out.CallbackStuck, &out.AuditIntegrityFailed, &out.RateLimitRules, &out.RateLimitBlocks24h,
		&out.LegalHoldsActive, &out.ExportsFailed, &out.ReconciliationCritical, &out.ReconciliationLastStatus); err != nil {
		return Summary{}, fmt.Errorf("load governance ops summary: %w", err)
	}
	return out, nil
}

func (r *Repository) ListCallbackDeliveries(ctx context.Context, status string, limit int, orgID *string, allowAll bool) ([]CallbackDelivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `
		SELECT d.id, d.outbox_event_id, e.org_id, e.event_type, e.subject_type, e.subject_id,
		       d.target_url, d.status, d.attempts, d.max_attempts, d.next_attempt_at,
		       COALESCE(d.last_error, ''), d.response_status, d.leased_until,
		       COALESCE(d.poison_reason, ''), d.created_at, d.updated_at
		FROM nexus_callback_deliveries d
		JOIN nexus_outbox_events e ON e.id = d.outbox_event_id
		WHERE 1=1`
	args := []any{}
	arg := 1
	if strings.TrimSpace(status) != "" {
		query += fmt.Sprintf(" AND d.status = $%d", arg)
		args = append(args, strings.TrimSpace(status))
		arg++
	}
	if !allowAll {
		if orgID == nil || strings.TrimSpace(*orgID) == "" {
			return []CallbackDelivery{}, nil
		}
		query += fmt.Sprintf(" AND e.org_id = $%d", arg)
		args = append(args, strings.TrimSpace(*orgID))
		arg++
	}
	query += fmt.Sprintf(" ORDER BY d.created_at DESC LIMIT $%d", arg)
	args = append(args, limit)
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list callback deliveries: %w", err)
	}
	defer rows.Close()
	out := make([]CallbackDelivery, 0)
	for rows.Next() {
		var item CallbackDelivery
		if err := rows.Scan(&item.ID, &item.OutboxEventID, &item.OrgID, &item.EventType, &item.SubjectType, &item.SubjectID,
			&item.TargetURL, &item.Status, &item.Attempts, &item.MaxAttempts, &item.NextAttemptAt, &item.LastError,
			&item.ResponseStatus, &item.LeasedUntil, &item.PoisonReason, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan callback delivery: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) RetryCallbackDelivery(ctx context.Context, id uuid.UUID, actorID string, orgID *string, allowAll bool) error {
	actorID = firstNonEmpty(actorID, "system")
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin callback retry tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var deliveryOrgID *string
	if err := tx.QueryRow(ctx, `
		SELECT e.org_id
		FROM nexus_callback_deliveries d
		JOIN nexus_outbox_events e ON e.id = d.outbox_event_id
		WHERE d.id = $1
		FOR UPDATE
	`, id).Scan(&deliveryOrgID); err != nil {
		return fmt.Errorf("get callback delivery: %w", err)
	}
	if !allowAll {
		if orgID == nil || deliveryOrgID == nil || strings.TrimSpace(*orgID) != strings.TrimSpace(*deliveryOrgID) {
			return fmt.Errorf("callback delivery org is not allowed")
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE nexus_callback_deliveries
		SET status = 'pending', next_attempt_at = now(), lease_owner = NULL, leased_until = NULL,
		    poison_reason = NULL, updated_at = now()
		WHERE id = $1
	`, id); err != nil {
		return fmt.Errorf("retry callback delivery: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO nexus_callback_delivery_events (delivery_id, org_id, event_type, actor_id, data)
		VALUES ($1,$2,'manual_retry',$3,'{}'::jsonb)
	`, id, deliveryOrgID, actorID); err != nil {
		return fmt.Errorf("record callback retry event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit callback retry tx: %w", err)
	}
	return nil
}

func (r *Repository) CreateLegalHold(ctx context.Context, hold LegalHold) (LegalHold, error) {
	if hold.ID == uuid.Nil {
		hold.ID = uuid.New()
	}
	if hold.CreatedAt.IsZero() {
		hold.CreatedAt = time.Now().UTC()
	}
	if hold.CreatedBy == "" {
		hold.CreatedBy = "system"
	}
	if hold.Status == "" {
		hold.Status = "active"
	}
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO governance_legal_holds
			(id, org_id, subject_type, subject_id, reason, status, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, org_id, subject_type, subject_id, reason, status, created_by, COALESCE(released_by, ''), created_at, released_at
	`, hold.ID, normalizedOrg(hold.OrgID), hold.SubjectType, hold.SubjectID, hold.Reason, hold.Status, hold.CreatedBy, hold.CreatedAt).
		Scan(&hold.ID, &hold.OrgID, &hold.SubjectType, &hold.SubjectID, &hold.Reason, &hold.Status, &hold.CreatedBy,
			&hold.ReleasedBy, &hold.CreatedAt, &hold.ReleasedAt)
	if err != nil {
		return LegalHold{}, fmt.Errorf("create legal hold: %w", err)
	}
	return hold, nil
}

func (r *Repository) ListLegalHolds(ctx context.Context, orgID *string, allowAll bool) ([]LegalHold, error) {
	query := `SELECT id, org_id, subject_type, subject_id, reason, status, created_by, COALESCE(released_by, ''), created_at, released_at FROM governance_legal_holds WHERE 1=1`
	args := []any{}
	if !allowAll {
		if orgID == nil || strings.TrimSpace(*orgID) == "" {
			return []LegalHold{}, nil
		}
		query += ` AND org_id = $1`
		args = append(args, strings.TrimSpace(*orgID))
	}
	query += ` ORDER BY created_at DESC LIMIT 500`
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list legal holds: %w", err)
	}
	defer rows.Close()
	out := make([]LegalHold, 0)
	for rows.Next() {
		var hold LegalHold
		if err := rows.Scan(&hold.ID, &hold.OrgID, &hold.SubjectType, &hold.SubjectID, &hold.Reason, &hold.Status,
			&hold.CreatedBy, &hold.ReleasedBy, &hold.CreatedAt, &hold.ReleasedAt); err != nil {
			return nil, fmt.Errorf("scan legal hold: %w", err)
		}
		out = append(out, hold)
	}
	return out, rows.Err()
}

func (r *Repository) CreateExportJob(ctx context.Context, job ExportJob) (ExportJob, error) {
	now := time.Now().UTC()
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.CompletedAt == nil {
		job.CompletedAt = &now
	}
	if job.Status == "" {
		job.Status = "completed"
	}
	if job.RequestedBy == "" {
		job.RequestedBy = "system"
	}
	if job.Manifest == nil {
		job.Manifest = map[string]any{}
	}
	job.Manifest["export_id"] = job.ID.String()
	job.Manifest["org_id"] = stringPtr(job.OrgID)
	job.Manifest["export_type"] = job.ExportType
	job.Manifest["subject_type"] = job.SubjectType
	job.Manifest["subject_id"] = job.SubjectID
	job.Manifest["created_at"] = job.CreatedAt.UTC().Format(time.RFC3339Nano)
	hash, raw, err := hashJSON(job.Manifest)
	if err != nil {
		return ExportJob{}, err
	}
	job.ManifestHash = hash
	err = r.db.Pool().QueryRow(ctx, `
		INSERT INTO governance_export_jobs
			(id, org_id, export_type, status, subject_type, subject_id, requested_by, manifest, manifest_hash, created_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, org_id, export_type, status, subject_type, subject_id, requested_by, manifest, manifest_hash, COALESCE(error_message, ''), created_at, completed_at
	`, job.ID, normalizedOrg(job.OrgID), job.ExportType, job.Status, job.SubjectType, job.SubjectID,
		job.RequestedBy, raw, job.ManifestHash, job.CreatedAt, job.CompletedAt).
		Scan(&job.ID, &job.OrgID, &job.ExportType, &job.Status, &job.SubjectType, &job.SubjectID, &job.RequestedBy,
			&raw, &job.ManifestHash, &job.ErrorMessage, &job.CreatedAt, &job.CompletedAt)
	if err != nil {
		return ExportJob{}, fmt.Errorf("create export job: %w", err)
	}
	_ = json.Unmarshal(raw, &job.Manifest)
	return job, nil
}

func (r *Repository) ListExportJobs(ctx context.Context, orgID *string, allowAll bool) ([]ExportJob, error) {
	query := `SELECT id, org_id, export_type, status, subject_type, subject_id, requested_by, manifest, manifest_hash, COALESCE(error_message, ''), created_at, completed_at FROM governance_export_jobs WHERE 1=1`
	args := []any{}
	if !allowAll {
		if orgID == nil || strings.TrimSpace(*orgID) == "" {
			return []ExportJob{}, nil
		}
		query += ` AND org_id = $1`
		args = append(args, strings.TrimSpace(*orgID))
	}
	query += ` ORDER BY created_at DESC LIMIT 200`
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list export jobs: %w", err)
	}
	defer rows.Close()
	out := make([]ExportJob, 0)
	for rows.Next() {
		var job ExportJob
		var raw []byte
		if err := rows.Scan(&job.ID, &job.OrgID, &job.ExportType, &job.Status, &job.SubjectType, &job.SubjectID,
			&job.RequestedBy, &raw, &job.ManifestHash, &job.ErrorMessage, &job.CreatedAt, &job.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan export job: %w", err)
		}
		_ = json.Unmarshal(raw, &job.Manifest)
		out = append(out, job)
	}
	return out, rows.Err()
}

func (r *Repository) RunReconciliation(ctx context.Context, orgID *string, actorID string) (ReconciliationReport, error) {
	findings := make([]ReconciliationFinding, 0)
	addFinding := func(severity, typ, subjectType, subjectID, message string, data map[string]any) {
		if data == nil {
			data = make(map[string]any)
		}
		findings = append(findings, ReconciliationFinding{
			ID: uuid.New(), OrgID: orgID, Severity: severity, FindingType: typ,
			SubjectType: subjectType, SubjectID: subjectID, Message: message, Data: data,
			CreatedAt: time.Now().UTC(),
		})
	}

	var failedAudit int
	failedAudit, _ = r.countScoped(ctx, `audit_integrity_checks`, `status = 'failed'`, nil, false)
	if failedAudit > 0 {
		addFinding("critical", "audit_integrity_failed", "audit_integrity_checks", "latest", "failed audit integrity checks exist", map[string]any{"count": failedAudit})
	}
	var deadCallbacks int
	deadCallbacks, _ = r.countScoped(ctx, `nexus_callback_deliveries d JOIN nexus_outbox_events e ON e.id = d.outbox_event_id`, `d.status = 'dead'`, orgID, true)
	if deadCallbacks > 0 {
		addFinding("warning", "dead_callbacks", "callback_delivery", "dead", "dead callback deliveries require operator review", map[string]any{"count": deadCallbacks})
	}
	var stuckCallbacks int
	stuckCallbacks, _ = r.countScoped(ctx, `nexus_callback_deliveries d JOIN nexus_outbox_events e ON e.id = d.outbox_event_id`, `d.status = 'delivering' AND d.leased_until < now()`, orgID, true)
	if stuckCallbacks > 0 {
		addFinding("warning", "stuck_callbacks", "callback_delivery", "stuck", "callback deliveries have expired leases", map[string]any{"count": stuckCallbacks})
	}
	var expiredApprovals int
	expiredApprovals, _ = r.countScoped(ctx, `approvals`, `status = 'pending' AND expires_at < now()`, orgID, false)
	if expiredApprovals > 0 {
		addFinding("warning", "expired_pending_approvals", "approval", "expired", "pending approvals are expired and need reconciliation", map[string]any{"count": expiredApprovals})
	}

	reportData := map[string]any{
		"org_id":   stringPtr(orgID),
		"findings": len(findings),
		"types":    findingTypes(findings),
	}
	reportHash, _, err := hashJSON(reportData)
	if err != nil {
		return ReconciliationReport{}, err
	}
	now := time.Now().UTC()
	run := ReconciliationRun{
		ID: uuid.New(), OrgID: orgID, Status: "completed", CheckedItems: 4,
		FindingCount: len(findings), ReportHash: reportHash, CreatedBy: firstNonEmpty(actorID, "system"),
		CreatedAt: now, CompletedAt: now,
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return ReconciliationReport{}, fmt.Errorf("begin reconciliation tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO nexus_reconciliation_runs
			(id, org_id, status, checked_items, finding_count, report_hash, created_by, created_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, run.ID, normalizedOrg(run.OrgID), run.Status, run.CheckedItems, run.FindingCount, run.ReportHash, run.CreatedBy, run.CreatedAt, run.CompletedAt); err != nil {
		return ReconciliationReport{}, fmt.Errorf("insert reconciliation run: %w", err)
	}
	for i := range findings {
		findings[i].RunID = run.ID
		raw, _ := json.Marshal(findings[i].Data)
		if _, err := tx.Exec(ctx, `
			INSERT INTO nexus_reconciliation_findings
				(id, run_id, org_id, severity, finding_type, subject_type, subject_id, message, data, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, findings[i].ID, findings[i].RunID, normalizedOrg(findings[i].OrgID), findings[i].Severity, findings[i].FindingType,
			findings[i].SubjectType, findings[i].SubjectID, findings[i].Message, raw, findings[i].CreatedAt); err != nil {
			return ReconciliationReport{}, fmt.Errorf("insert reconciliation finding: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationReport{}, fmt.Errorf("commit reconciliation tx: %w", err)
	}
	return ReconciliationReport{Run: run, Findings: findings}, nil
}

func hashJSON(value any) (string, []byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", nil, fmt.Errorf("marshal hash payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}

func normalizedOrg(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return strings.TrimSpace(*value)
}

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func findingTypes(findings []ReconciliationFinding) []string {
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, finding.FindingType)
	}
	return out
}

func (r *Repository) countScoped(ctx context.Context, tableExpr, predicate string, orgID *string, orgColumnOnE bool) (int, error) {
	orgColumn := "org_id"
	if orgColumnOnE {
		orgColumn = "e.org_id"
	}
	var count int
	if orgID == nil || strings.TrimSpace(*orgID) == "" {
		err := r.db.Pool().QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s`, tableExpr, predicate)).Scan(&count)
		return count, err
	}
	err := r.db.Pool().QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s AND %s = $1`, tableExpr, predicate, orgColumn), strings.TrimSpace(*orgID)).Scan(&count)
	return count, err
}
