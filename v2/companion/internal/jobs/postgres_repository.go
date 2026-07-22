package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const jobColumns = `
    id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status,
    priority, attempts, max_attempts, run_after, lease_owner, lease_until, locked_at,
    heartbeat_at, deadline_at, timeout_seconds, last_error_code, evidence_json,
    created_at, updated_at, completed_at`

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Enqueue(ctx context.Context, input EnqueueInput) (Job, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Job{}, false, fmt.Errorf("begin enqueue job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	job, inserted, err := r.EnqueueTx(ctx, tx, input)
	if err != nil {
		return Job{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("commit enqueue job: %w", err)
	}
	return job, inserted, nil
}

// EnqueueTx persists a job inside the caller's transaction. It is used when a
// domain record and the durable work needed to advance it must become visible
// atomically. The caller owns commit/rollback.
func (r *PostgresRepository) EnqueueTx(ctx context.Context, tx pgx.Tx, input EnqueueInput) (Job, bool, error) {
	input, err := NormalizeEnqueueInput(input)
	if err != nil {
		return Job{}, false, err
	}

	row := tx.QueryRow(ctx, `
        INSERT INTO companion_jobs
            (id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json,
             status, priority, max_attempts, run_after, deadline_at, timeout_seconds)
        VALUES ($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9,$10,$11,$12)
        ON CONFLICT (org_id, product_surface, kind, dedupe_key) DO NOTHING
        RETURNING `+jobColumns,
		input.ID, input.OrgID, input.ProductSurface, input.Kind, input.ShardKey,
		input.DedupeKey, input.Payload, input.Priority, input.MaxAttempts, input.RunAfter,
		input.DeadlineAt, durationSeconds(input.Timeout))
	job, scanErr := scanJob(row)
	if scanErr == nil {
		if err := recordEvent(ctx, tx, job.ID, "queued", json.RawMessage(`{"source":"enqueue"}`)); err != nil {
			return Job{}, false, err
		}
		return job, true, nil
	}

	if !errors.Is(scanErr, pgx.ErrNoRows) {
		return Job{}, false, fmt.Errorf("enqueue job: %w", scanErr)
	}
	existing, err := getByDedupe(ctx, tx, input)
	if err != nil {
		return Job{}, false, err
	}
	if input.ReplacePayload && existing.Status == StatusQueued {
		row = tx.QueryRow(ctx, `
            UPDATE companion_jobs
            SET payload_json=$2, run_after=$3, priority=$4, deadline_at=$5,
                timeout_seconds=$6, updated_at=now()
            WHERE id=$1 AND status='queued'
            RETURNING `+jobColumns,
			existing.ID, input.Payload, input.RunAfter, input.Priority, input.DeadlineAt,
			durationSeconds(input.Timeout))
		existing, err = scanJob(row)
		if err != nil {
			return Job{}, false, fmt.Errorf("replace queued job: %w", err)
		}
		if err := recordEvent(ctx, tx, existing.ID, "payload_replaced", json.RawMessage(`{}`)); err != nil {
			return Job{}, false, err
		}
	}
	return existing, false, nil
}

func (r *PostgresRepository) Claim(ctx context.Context, options ClaimOptions) ([]Job, error) {
	options = NormalizeClaimOptions(options)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim jobs: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE companion_worker_controls
		SET state='half_open', revision=revision+1, changed_by='system:circuit_breaker',
			reason_code='cooldown_elapsed', updated_at=now()
		WHERE state='open' AND opened_until<=now()
	`); err != nil {
		return nil, fmt.Errorf("advance worker circuit breakers: %w", err)
	}

	rows, err := tx.Query(ctx, `
        WITH candidates AS (
            SELECT job.id, job.priority, job.run_after, job.created_at,
                   COALESCE(control.state,'closed') AS control_state,
                   row_number() OVER (
                       PARTITION BY job.org_id,job.product_surface,job.kind
                       ORDER BY job.priority DESC,job.run_after,job.created_at,job.id
                   ) AS control_rank
            FROM companion_jobs AS job
            LEFT JOIN companion_worker_controls AS control
              ON control.org_id=job.org_id
             AND control.product_surface=job.product_surface
             AND control.kind=job.kind
            WHERE job.status='queued'
              AND job.attempts < job.max_attempts
              AND job.run_after <= now()
              AND (cardinality($2::text[]) = 0 OR job.kind = ANY($2::text[]))
              AND ($3::int <= 0 OR mod(abs(hashtext(job.shard_key)), $3::int) = $4::int)
              AND COALESCE(control.state,'closed') NOT IN ('paused','open')
        ), picked AS (
            SELECT job.id
            FROM companion_jobs AS job
            JOIN candidates ON candidates.id=job.id
            WHERE candidates.control_state<>'half_open' OR candidates.control_rank=1
            ORDER BY candidates.priority DESC,candidates.run_after,candidates.created_at
            LIMIT $5
            FOR UPDATE OF job SKIP LOCKED
        )
        UPDATE companion_jobs AS job
        SET status='running', attempts=attempts+1, lease_owner=$1,
            lease_until=now()+make_interval(secs => $6),
            locked_at=COALESCE(locked_at, now()), heartbeat_at=now(), updated_at=now()
        FROM picked
        WHERE job.id=picked.id
        RETURNING `+prefixedJobColumns("job"),
		options.WorkerID, options.Kinds, options.ShardCount, options.ShardIndex,
		options.BatchSize, durationSeconds(options.LeaseDuration))
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	defer rows.Close()
	claimed := make([]Job, 0, options.BatchSize)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claimed job: %w", err)
		}
		claimed = append(claimed, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed jobs: %w", err)
	}
	rows.Close()
	metadata, _ := json.Marshal(map[string]string{"worker_id": options.WorkerID})
	for _, job := range claimed {
		if err := recordEvent(ctx, tx, job.ID, "claimed", metadata); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim jobs: %w", err)
	}
	return claimed, nil
}

func (r *PostgresRepository) Heartbeat(ctx context.Context, jobID uuid.UUID, workerID string, lease time.Duration) error {
	cancelled, err := r.pool.Exec(ctx, `
		UPDATE companion_jobs SET status='cancelled',lease_owner='',lease_until=NULL,
			completed_at=now(),updated_at=now()
		WHERE id=$1 AND lease_owner=$2 AND status='cancel_requested'
	`, jobID, strings.TrimSpace(workerID))
	if err != nil {
		return fmt.Errorf("acknowledge job cancellation: %w", err)
	}
	if cancelled.RowsAffected() > 0 {
		return ErrJobCancelled
	}
	tag, err := r.pool.Exec(ctx, `
        UPDATE companion_jobs
        SET heartbeat_at=now(), lease_until=now()+make_interval(secs => $3), updated_at=now()
        WHERE id=$1 AND lease_owner=$2 AND status='running'
    `, jobID, strings.TrimSpace(workerID), durationSeconds(defaultDuration(lease, DefaultLease)))
	if err != nil {
		return fmt.Errorf("heartbeat job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

func (r *PostgresRepository) Complete(ctx context.Context, jobID uuid.UUID, workerID string, evidence json.RawMessage) error {
	evidence = defaultJSON(evidence)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin complete job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var finalStatus Status
	var orgID, productSurface, kind string
	err = tx.QueryRow(ctx, `
        UPDATE companion_jobs
        SET status=CASE WHEN status='cancel_requested' THEN 'cancelled' ELSE 'succeeded' END,
			lease_owner='', lease_until=NULL, evidence_json=CASE WHEN status='running' THEN $3 ELSE evidence_json END,
            completed_at=now(), updated_at=now()
		WHERE id=$1 AND lease_owner=$2 AND status IN('running','cancel_requested')
		RETURNING status,org_id,product_surface,kind
	`, jobID, strings.TrimSpace(workerID), evidence).Scan(&finalStatus, &orgID, &productSurface, &kind)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return fmt.Errorf("complete job: %w", err)
	}
	if err := recordEvent(ctx, tx, jobID, string(finalStatus), json.RawMessage(`{}`)); err != nil {
		return err
	}
	if finalStatus == StatusSucceeded {
		if err := closeCircuitAfterProbe(ctx, tx, "companion_worker_controls", orgID, productSurface, kind); err != nil {
			return err
		}
	}
	return commitTx(ctx, tx, "complete job")
}

func (r *PostgresRepository) Fail(ctx context.Context, input FailInput) (Job, error) {
	input.ErrorCode = NormalizeErrorCode(input.ErrorCode)
	input.Evidence = defaultJSON(input.Evidence)
	input.Backoff = defaultDuration(input.Backoff, time.Second)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin fail job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row := tx.QueryRow(ctx, `
        UPDATE companion_jobs
        SET status=CASE WHEN status='cancel_requested' THEN 'cancelled' WHEN $4 AND attempts < max_attempts THEN 'queued' ELSE 'dead_letter' END,
            run_after=CASE WHEN $4 AND attempts < max_attempts
                           THEN now()+make_interval(secs => $5) ELSE run_after END,
            lease_owner='', lease_until=NULL, last_error_code=$3, evidence_json=$6,
            completed_at=CASE WHEN status<>'cancel_requested' AND $4 AND attempts < max_attempts THEN NULL ELSE now() END,
            updated_at=now()
		WHERE id=$1 AND lease_owner=$2 AND status IN('running','cancel_requested')
        RETURNING `+jobColumns,
		input.JobID, strings.TrimSpace(input.WorkerID), input.ErrorCode, input.Retryable,
		durationSeconds(input.Backoff), input.Evidence)
	job, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("fail job: %w", err)
	}
	event := "retry_scheduled"
	switch job.Status {
	case StatusDeadLetter:
		event = "dead_letter"
	case StatusCancelled:
		event = "cancelled"
	}
	metadata, _ := json.Marshal(map[string]string{"error_code": input.ErrorCode})
	if err := recordEvent(ctx, tx, job.ID, event, metadata); err != nil {
		return Job{}, err
	}
	if input.Retryable && job.Status != StatusCancelled {
		if err := recordCircuitFailure(ctx, tx, "companion_worker_controls", "companion_job_definitions", job); err != nil {
			return Job{}, err
		}
	}
	if err := commitTx(ctx, tx, "fail job"); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (r *PostgresRepository) Cancel(ctx context.Context, orgID string, jobID uuid.UUID, reasonCode string) error {
	reasonCode = NormalizeErrorCode(reasonCode)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cancel job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status Status
	err = tx.QueryRow(ctx, `
        UPDATE companion_jobs
		SET status=CASE WHEN status='running' THEN 'cancel_requested' ELSE 'cancelled' END,
			cancel_requested_at=now(), last_error_code=$3,
			lease_owner=CASE WHEN status='running' THEN lease_owner ELSE '' END,
			lease_until=CASE WHEN status='running' THEN lease_until ELSE NULL END,
			completed_at=CASE WHEN status='running' THEN NULL ELSE now() END,updated_at=now()
        WHERE org_id=$1 AND id=$2 AND status IN ('queued','running')
		RETURNING status
	`, strings.TrimSpace(orgID), jobID, reasonCode).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return fmt.Errorf("cancel job: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"reason_code": reasonCode})
	if err := recordEvent(ctx, tx, jobID, string(status), metadata); err != nil {
		return err
	}
	return commitTx(ctx, tx, "cancel job")
}

func (r *PostgresRepository) Get(ctx context.Context, orgID string, jobID uuid.UUID) (Job, error) {
	job, err := scanJob(r.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM companion_jobs WHERE org_id=$1 AND id=$2`, strings.TrimSpace(orgID), jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (r *PostgresRepository) List(ctx context.Context, orgID, productSurface, status string, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT ` + jobColumns + ` FROM companion_jobs WHERE org_id=$1`
	args := []any{strings.TrimSpace(orgID)}
	if productSurface = strings.TrimSpace(strings.ToLower(productSurface)); productSurface != "" {
		args = append(args, productSurface)
		query += fmt.Sprintf(" AND product_surface=$%d", len(args))
	}
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND status=$%d", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	result := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) RecoverExpiredLeases(ctx context.Context, limit int) (RecoveryResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("begin recover leases: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
        WITH picked AS (
            SELECT id
            FROM companion_jobs
            WHERE status='running' AND lease_until < now()
            ORDER BY lease_until, id
            LIMIT $1
            FOR UPDATE SKIP LOCKED
        )
        UPDATE companion_jobs AS job
        SET status=CASE WHEN attempts < max_attempts THEN 'queued' ELSE 'dead_letter' END,
            lease_owner='', lease_until=NULL, heartbeat_at=NULL,
            run_after=CASE WHEN attempts < max_attempts THEN now() ELSE run_after END,
            last_error_code='lease_expired',
            completed_at=CASE WHEN attempts < max_attempts THEN NULL ELSE now() END,
            updated_at=now()
        FROM picked
        WHERE job.id=picked.id
        RETURNING job.id, job.status
    `, limit)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("recover expired leases: %w", err)
	}
	var result RecoveryResult
	type recoveredJob struct {
		id     uuid.UUID
		status Status
	}
	recovered := make([]recoveredJob, 0)
	for rows.Next() {
		var item recoveredJob
		if err := rows.Scan(&item.id, &item.status); err != nil {
			return RecoveryResult{}, err
		}
		if item.status == StatusQueued {
			result.Requeued++
		} else {
			result.DeadLetter++
		}
		recovered = append(recovered, item)
	}
	if err := rows.Err(); err != nil {
		return RecoveryResult{}, err
	}
	rows.Close()
	for _, item := range recovered {
		event := "lease_recovered"
		if item.status == StatusDeadLetter {
			event = "dead_letter"
		}
		if err := recordEvent(ctx, tx, item.id, event, json.RawMessage(`{"error_code":"lease_expired"}`)); err != nil {
			return RecoveryResult{}, err
		}
	}
	if err := commitTx(ctx, tx, "recover leases"); err != nil {
		return RecoveryResult{}, err
	}
	return result, nil
}

func (r *PostgresRepository) ReplayDeadLetter(ctx context.Context, orgID string, jobID uuid.UUID, runAfter time.Time) (Job, error) {
	if runAfter.IsZero() {
		runAfter = time.Now().UTC()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin replay job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	job, err := scanJob(tx.QueryRow(ctx, `
        UPDATE companion_jobs
        SET status='queued', attempts=0, run_after=$3, lease_owner='', lease_until=NULL,
            locked_at=NULL, heartbeat_at=NULL, last_error_code='', completed_at=NULL, updated_at=now()
        WHERE org_id=$1 AND id=$2 AND status='dead_letter'
        RETURNING `+jobColumns,
		strings.TrimSpace(orgID), jobID, runAfter.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("replay job: %w", err)
	}
	if err := recordEvent(ctx, tx, job.ID, "replayed", json.RawMessage(`{}`)); err != nil {
		return Job{}, err
	}
	if err := commitTx(ctx, tx, "replay job"); err != nil {
		return Job{}, err
	}
	return job, nil
}

func getByDedupe(ctx context.Context, tx pgx.Tx, input EnqueueInput) (Job, error) {
	job, err := scanJob(tx.QueryRow(ctx, `
        SELECT `+jobColumns+`
        FROM companion_jobs
        WHERE org_id=$1 AND product_surface=$2 AND kind=$3 AND dedupe_key=$4
        ORDER BY created_at
        LIMIT 1
    `, input.OrgID, input.ProductSurface, input.Kind, input.DedupeKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job by dedupe: %w", err)
	}
	return job, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var payload, evidence []byte
	var status string
	err := row.Scan(
		&job.ID, &job.OrgID, &job.ProductSurface, &job.Kind, &job.ShardKey,
		&job.DedupeKey, &payload, &status, &job.Priority, &job.Attempts,
		&job.MaxAttempts, &job.RunAfter, &job.LeaseOwner, &job.LeaseUntil, &job.LockedAt,
		&job.HeartbeatAt, &job.DeadlineAt, &job.TimeoutSeconds, &job.LastErrorCode,
		&evidence, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	)
	if err != nil {
		return Job{}, err
	}
	job.Payload = json.RawMessage(payload)
	job.Evidence = json.RawMessage(evidence)
	job.Status = Status(status)
	return job, nil
}

func recordEvent(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, event string, metadata json.RawMessage) error {
	_, err := tx.Exec(ctx, `
        INSERT INTO companion_job_events (job_id, event, metadata_json)
        VALUES ($1,$2,$3)
    `, jobID, event, defaultJSON(metadata))
	if err != nil {
		return fmt.Errorf("record job event: %w", err)
	}
	return nil
}

func commitTx(ctx context.Context, tx pgx.Tx, action string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", action, err)
	}
	return nil
}

func durationSeconds(value time.Duration) int {
	seconds := int(value / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func defaultJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return json.RawMessage(`{}`)
	}
	return value
}

func prefixedJobColumns(prefix string) string {
	columns := strings.Split(strings.ReplaceAll(jobColumns, "\n", " "), ",")
	for index, column := range columns {
		columns[index] = prefix + "." + strings.TrimSpace(column)
	}
	return strings.Join(columns, ", ")
}

func closeCircuitAfterProbe(ctx context.Context, tx pgx.Tx, controlsTable, orgID, productSurface, kind string) error {
	query := fmt.Sprintf(`UPDATE %s SET state='closed',failure_count=0,failure_window_started_at=NULL,
		opened_until=NULL,revision=revision+1,changed_by='system:circuit_breaker',reason_code='probe_succeeded',updated_at=now()
		WHERE org_id=$1 AND product_surface=$2 AND kind=$3 AND state='half_open'`, controlsTable)
	if _, err := tx.Exec(ctx, query, orgID, productSurface, kind); err != nil {
		return fmt.Errorf("close worker circuit after probe: %w", err)
	}
	return nil
}

func recordCircuitFailure(ctx context.Context, tx pgx.Tx, controlsTable, definitionsTable string, job Job) error {
	query := fmt.Sprintf(`
		INSERT INTO %s(org_id,product_surface,kind,state,failure_count,failure_window_started_at,
			opened_until,changed_by,reason_code)
		SELECT $1,$2,$3,'closed',1,now(),NULL,'system:circuit_breaker','dependency_failure'
		WHERE NOT COALESCE((SELECT protected FROM %s WHERE product_surface=$2 AND kind=$3),false)
		ON CONFLICT(org_id,product_surface,kind) DO UPDATE SET
			failure_count=CASE WHEN %s.failure_window_started_at>=now()-interval '1 minute'
				THEN %s.failure_count+1 ELSE 1 END,
			failure_window_started_at=CASE WHEN %s.failure_window_started_at>=now()-interval '1 minute'
				THEN %s.failure_window_started_at ELSE now() END,
			state=CASE
				WHEN %s.state='paused' THEN 'paused'
				WHEN %s.state='half_open' OR
					(CASE WHEN %s.failure_window_started_at>=now()-interval '1 minute' THEN %s.failure_count+1 ELSE 1 END)>=5
				THEN 'open' ELSE %s.state END,
			opened_until=CASE
				WHEN %s.state='half_open' OR
					(CASE WHEN %s.failure_window_started_at>=now()-interval '1 minute' THEN %s.failure_count+1 ELSE 1 END)>=5
				THEN now()+interval '1 minute' ELSE %s.opened_until END,
			revision=%s.revision+1,changed_by='system:circuit_breaker',reason_code='dependency_failure',updated_at=now()
	`, controlsTable, definitionsTable,
		controlsTable, controlsTable, controlsTable, controlsTable,
		controlsTable, controlsTable, controlsTable, controlsTable, controlsTable,
		controlsTable, controlsTable, controlsTable, controlsTable, controlsTable)
	if _, err := tx.Exec(ctx, query, job.OrgID, job.ProductSurface, job.Kind); err != nil {
		return fmt.Errorf("record worker circuit failure: %w", err)
	}
	return nil
}
