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
	"github.com/jackc/pgx/v5/pgconn"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Enqueue(ctx context.Context, in EnqueueInput) (Job, bool, error) {
	in, err := NormalizeEnqueueInput(in)
	if err != nil {
		return Job{}, false, err
	}
	timeoutSeconds := int(in.Timeout.Seconds())
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_jobs
			(id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status, priority,
			 max_attempts, run_after, deadline_at, timeout_seconds)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9,$10,$11,$12)
		RETURNING id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status,
		          priority, attempts, max_attempts, run_after, lease_owner,
		          lease_until, locked_at, heartbeat_at, deadline_at, timeout_seconds,
		          last_error, evidence_json, created_at, updated_at, completed_at
	`, in.ID, in.OrgID, in.ProductSurface, in.Kind, in.ShardKey, in.DedupeKey, in.Payload, in.Priority,
		in.MaxAttempts, in.RunAfter, in.DeadlineAt, timeoutSeconds)
	job, err := scanJob(row)
	if err == nil {
		if eventErr := r.recordEvent(ctx, job.ID, "queued", json.RawMessage(`{"source":"enqueue"}`)); eventErr != nil {
			return Job{}, false, eventErr
		}
		return job, true, nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return Job{}, false, fmt.Errorf("enqueue job: %w", err)
	}
	existing, getErr := r.getByDedupe(ctx, in.DedupeKey)
	if getErr != nil {
		return Job{}, false, getErr
	}
	if in.ReplacePayload && existing.Status == StatusQueued {
		updated, updateErr := r.replaceQueuedPayload(ctx, existing.ID, in.Payload, in.ProductSurface, in.RunAfter, in.Priority)
		if updateErr != nil {
			return Job{}, false, updateErr
		}
		return updated, false, nil
	}
	return existing, false, nil
}

func (r *PostgresRepository) Claim(ctx context.Context, opts ClaimOptions) ([]Job, error) {
	opts = NormalizeClaimOptions(opts)
	leaseInterval := fmt.Sprintf("%d seconds", int(opts.LeaseDuration.Seconds()))
	rows, err := r.db.Pool().Query(ctx, `
		WITH picked AS (
			SELECT id
			FROM companion_jobs
			WHERE status IN ('queued', 'running')
			  AND run_after <= now()
			  AND (status = 'queued' OR lease_until IS NULL OR lease_until < now())
			  AND (cardinality($2::text[]) = 0 OR kind = ANY($2::text[]))
			  AND ($3::int <= 0 OR abs(hashtext(shard_key)) % $3::int = $4::int)
			ORDER BY priority DESC, run_after ASC, created_at ASC
			LIMIT $5
			FOR UPDATE SKIP LOCKED
		)
		UPDATE companion_jobs AS j
		SET status = 'running',
		    attempts = attempts + 1,
		    lease_owner = $1,
		    lease_until = now() + ($6::text)::interval,
		    locked_at = COALESCE(locked_at, now()),
		    heartbeat_at = now(),
		    updated_at = now()
		FROM picked
		WHERE j.id = picked.id
		RETURNING j.id, j.org_id, j.product_surface, j.kind, j.shard_key, j.dedupe_key, j.payload_json, j.status,
		          j.priority, j.attempts, j.max_attempts, j.run_after, j.lease_owner,
		          j.lease_until, j.locked_at, j.heartbeat_at, j.deadline_at, j.timeout_seconds,
		          j.last_error, j.evidence_json, j.created_at, j.updated_at, j.completed_at
	`, opts.WorkerID, opts.Kinds, opts.ShardCount, opts.ShardIndex, opts.BatchSize, leaseInterval)
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	defer rows.Close()
	out := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, job := range out {
		if err := r.recordEvent(ctx, job.ID, "claimed", json.RawMessage(`{"worker_id":`+quoteJSON(opts.WorkerID)+`}`)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *PostgresRepository) Heartbeat(ctx context.Context, jobID uuid.UUID, workerID string, lease time.Duration) error {
	workerID = strings.TrimSpace(workerID)
	if lease <= 0 {
		lease = DefaultLease
	}
	leaseInterval := fmt.Sprintf("%d seconds", int(lease.Seconds()))
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_jobs
		SET heartbeat_at = now(),
		    lease_until = now() + ($3::text)::interval,
		    updated_at = now()
		WHERE id = $1 AND lease_owner = $2 AND status = 'running'
	`, jobID, workerID, leaseInterval)
	if err != nil {
		return fmt.Errorf("heartbeat job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

func (r *PostgresRepository) Complete(ctx context.Context, jobID uuid.UUID, workerID string, evidence json.RawMessage) error {
	if evidence == nil {
		evidence = json.RawMessage(`{}`)
	}
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_jobs
		SET status = 'succeeded',
		    lease_owner = '',
		    lease_until = NULL,
		    evidence_json = $3,
		    completed_at = now(),
		    updated_at = now()
		WHERE id = $1 AND lease_owner = $2 AND status = 'running'
	`, jobID, workerID, evidence)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return r.recordEvent(ctx, jobID, "succeeded", evidence)
}

func (r *PostgresRepository) Fail(ctx context.Context, in FailInput) (Job, error) {
	if in.Evidence == nil {
		in.Evidence = json.RawMessage(`{}`)
	}
	if in.Backoff <= 0 {
		in.Backoff = time.Second
	}
	backoffInterval := fmt.Sprintf("%d seconds", int(in.Backoff.Seconds()))
	statusWhenRetryable := string(StatusQueued)
	if !in.Retryable {
		statusWhenRetryable = string(StatusDeadLetter)
	}
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE companion_jobs
		SET status = CASE
		        WHEN $4::bool AND attempts < max_attempts THEN $7
		        ELSE 'dead_letter'
		    END,
		    run_after = CASE
		        WHEN $4::bool AND attempts < max_attempts THEN now() + ($5::text)::interval
		        ELSE run_after
		    END,
		    lease_owner = '',
		    lease_until = NULL,
		    last_error = $3,
		    evidence_json = $6,
		    completed_at = CASE
		        WHEN $4::bool AND attempts < max_attempts THEN NULL
		        ELSE now()
		    END,
		    updated_at = now()
		WHERE id = $1 AND lease_owner = $2 AND status = 'running'
		RETURNING id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status,
		          priority, attempts, max_attempts, run_after, lease_owner,
		          lease_until, locked_at, heartbeat_at, deadline_at, timeout_seconds,
		          last_error, evidence_json, created_at, updated_at, completed_at
	`, in.JobID, in.WorkerID, errorString(in.Err), in.Retryable, backoffInterval, in.Evidence, statusWhenRetryable)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, fmt.Errorf("fail job: %w", err)
	}
	event := "retry_scheduled"
	if job.Status == StatusDeadLetter {
		event = "dead_letter"
	}
	if err := r.recordEvent(ctx, job.ID, event, in.Evidence); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (r *PostgresRepository) Cancel(ctx context.Context, jobID uuid.UUID, reason string) error {
	payload, err := json.Marshal(map[string]any{"reason": strings.TrimSpace(reason)})
	if err != nil {
		return fmt.Errorf("marshal cancel reason: %w", err)
	}
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_jobs
		SET status = 'cancelled',
		    lease_owner = '',
		    lease_until = NULL,
		    last_error = $2,
		    completed_at = now(),
		    updated_at = now()
		WHERE id = $1 AND status IN ('queued','running')
	`, jobID, strings.TrimSpace(reason))
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return r.recordEvent(ctx, jobID, "cancelled", payload)
}

func (r *PostgresRepository) Get(ctx context.Context, jobID uuid.UUID) (Job, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status,
		       priority, attempts, max_attempts, run_after, lease_owner,
		       lease_until, locked_at, heartbeat_at, deadline_at, timeout_seconds,
		       last_error, evidence_json, created_at, updated_at, completed_at
		FROM companion_jobs
		WHERE id = $1
	`, jobID)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (r *PostgresRepository) List(ctx context.Context, orgID, status string, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `
		SELECT id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status,
		       priority, attempts, max_attempts, run_after, lease_owner,
		       lease_until, locked_at, heartbeat_at, deadline_at, timeout_seconds,
		       last_error, evidence_json, created_at, updated_at, completed_at
		FROM companion_jobs
		WHERE org_id = $1`
	args := []any{strings.TrimSpace(orgID)}
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		query += fmt.Sprintf(` AND status = $%d`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args))
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	out := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) RecoverExpiredLeases(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	tag, err := r.db.Pool().Exec(ctx, `
		WITH expired AS (
			SELECT id
			FROM companion_jobs
			WHERE status = 'running' AND lease_until < now()
			ORDER BY lease_until ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE companion_jobs AS j
		SET status = 'queued',
		    lease_owner = '',
		    lease_until = NULL,
		    heartbeat_at = NULL,
		    run_after = now(),
		    updated_at = now()
		FROM expired
		WHERE j.id = expired.id
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("recover expired leases: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *PostgresRepository) getByDedupe(ctx context.Context, dedupeKey string) (Job, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status,
		       priority, attempts, max_attempts, run_after, lease_owner,
		       lease_until, locked_at, heartbeat_at, deadline_at, timeout_seconds,
		       last_error, evidence_json, created_at, updated_at, completed_at
		FROM companion_jobs
		WHERE dedupe_key = $1 AND status IN ('queued','running')
		ORDER BY created_at ASC
		LIMIT 1
	`, dedupeKey)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, fmt.Errorf("get job by dedupe: %w", err)
	}
	return job, nil
}

func (r *PostgresRepository) replaceQueuedPayload(ctx context.Context, jobID uuid.UUID, payload json.RawMessage, productSurface string, runAfter time.Time, priority int) (Job, error) {
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE companion_jobs
		SET payload_json = $2,
		    product_surface = $3,
		    run_after = $4,
		    priority = $5,
		    updated_at = now()
		WHERE id = $1 AND status = 'queued'
		RETURNING id, org_id, product_surface, kind, shard_key, dedupe_key, payload_json, status,
		          priority, attempts, max_attempts, run_after, lease_owner,
		          lease_until, locked_at, heartbeat_at, deadline_at, timeout_seconds,
		          last_error, evidence_json, created_at, updated_at, completed_at
	`, jobID, payload, productSurface, runAfter, priority)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, fmt.Errorf("replace queued payload: %w", err)
	}
	return job, r.recordEvent(ctx, job.ID, "payload_replaced", payload)
}

func (r *PostgresRepository) recordEvent(ctx context.Context, jobID uuid.UUID, event string, payload json.RawMessage) error {
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_job_events (job_id, event, payload_json)
		VALUES ($1,$2,$3)
	`, jobID, event, payload)
	if err != nil {
		return fmt.Errorf("record job event: %w", err)
	}
	return nil
}

func scanJob(row pgx.Row) (Job, error) {
	var (
		job        Job
		status     string
		payload    []byte
		evidence   []byte
		leaseOwner *string
		lastError  *string
	)
	err := row.Scan(
		&job.ID, &job.OrgID, &job.ProductSurface, &job.Kind, &job.ShardKey, &job.DedupeKey, &payload, &status,
		&job.Priority, &job.Attempts, &job.MaxAttempts, &job.RunAfter, &leaseOwner,
		&job.LeaseUntil, &job.LockedAt, &job.HeartbeatAt, &job.DeadlineAt, &job.TimeoutSeconds,
		&lastError, &evidence, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	)
	if err != nil {
		return Job{}, err
	}
	job.Status = Status(status)
	job.Payload = json.RawMessage(payload)
	job.Evidence = json.RawMessage(evidence)
	if leaseOwner != nil {
		job.LeaseOwner = *leaseOwner
	}
	if lastError != nil {
		job.LastError = *lastError
	}
	return job, nil
}

func quoteJSON(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(raw)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
