package virployees

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
)

type TimedOutAssist struct {
	ID          uuid.UUID
	OrgID       string
	VirployeeID uuid.UUID
	InputHash   string
	StartedAt   time.Time
	Outcome     string
}

type ExecutionWork struct {
	Attempt ExecutionAttempt
	Action  PreparedActionRecord
}

type OperationalRepositoryPort interface {
	ReconcileStaleAssistRuns(context.Context, time.Time, int, int) ([]TimedOutAssist, error)
	ClaimStaleExecutions(context.Context, time.Time, int, string, time.Duration, int) ([]ExecutionWork, error)
	ReleaseExecutionRecovery(context.Context, string, uuid.UUID, string) error
}

func (r *Repository) ReconcileStaleAssistRuns(ctx context.Context, cutoff time.Time, limit, maxAttempts int) ([]TimedOutAssist, error) {
	rows, err := r.pool.Query(ctx, `
		WITH stale AS (
			SELECT id, status, recovery_attempts FROM companion_assist_runs
			WHERE status IN ('staging', 'extracting', 'indexing', 'answering') AND updated_at <= $1
			ORDER BY updated_at, id LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		UPDATE companion_assist_runs a
		SET status = CASE
				WHEN stale.status = 'answering' OR stale.recovery_attempts + 1 >= $3 THEN 'failed'
				ELSE 'received'
			END,
			error = CASE
				WHEN stale.status = 'answering' THEN 'assist answer state became stale'
				WHEN stale.recovery_attempts + 1 >= $3 THEN 'assist recovery exhausted'
				ELSE ''
			END,
			recovery_attempts = stale.recovery_attempts + 1,
			completed_at = CASE
				WHEN stale.status = 'answering' OR stale.recovery_attempts + 1 >= $3 THEN now()
				ELSE NULL
			END,
			updated_at = now(),
			duration_ms = CASE
				WHEN stale.status = 'answering' OR stale.recovery_attempts + 1 >= $3
				THEN GREATEST(0, EXTRACT(EPOCH FROM (now() - started_at)) * 1000)::bigint
				ELSE duration_ms
			END
		FROM stale WHERE a.id = stale.id
		RETURNING a.id, a.org_id, a.virployee_id, a.input_hash, a.started_at,
			CASE WHEN a.status = 'received' THEN 'recovered' ELSE 'timed_out' END
	`, cutoff.UTC(), limit, maxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimedOutAssist
	for rows.Next() {
		var item TimedOutAssist
		if err := rows.Scan(&item.ID, &item.OrgID, &item.VirployeeID, &item.InputHash, &item.StartedAt, &item.Outcome); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ClaimStaleExecutions(ctx context.Context, cutoff time.Time, limit int, owner string, lease time.Duration, maxAttempts int) ([]ExecutionWork, error) {
	return r.claimExecutionWork(ctx, `
		e.status = 'running' AND e.updated_at <= $1
		AND e.recovery_attempts < $4
		AND (e.recovery_lease_until IS NULL OR e.recovery_lease_until <= $2)
	`, cutoff, limit, owner, lease, maxAttempts)
}

func (r *Repository) claimExecutionWork(ctx context.Context, predicate string, cutoff time.Time, limit int, owner string, lease time.Duration, maxAttempts int) ([]ExecutionWork, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now().UTC()
	rows, err := tx.Query(ctx, `
		SELECT e.id, e.org_id, e.virployee_id, e.prepared_action_id, e.idempotency_key,
			e.status, e.resource_id, e.result, e.error, e.duration_ms,
			e.governance_report_status, e.nexus_report_status,
			e.started_at, e.completed_at, e.recovery_attempts, e.nexus_report_attempts,
			p.governance_check_id, p.approval_id,
			COALESCE(p.capability_id,'00000000-0000-0000-0000-000000000000'::uuid),
			p.capability_key, p.payload_version, p.executor_binding_id, p.operation, p.payload,
			p.payload_hash, p.binding_hash, p.authority_binding_hash,
			p.governance_policy_snapshot_hash, p.nexus_policy_snapshot_hash
		FROM companion_execution_attempts e
		JOIN companion_prepared_actions p ON p.id = e.prepared_action_id
		WHERE `+predicate+`
		ORDER BY e.updated_at, e.id LIMIT $3 FOR UPDATE OF e SKIP LOCKED
	`, cutoff.UTC(), now, limit, maxAttempts)
	if err != nil {
		return nil, err
	}
	var out []ExecutionWork
	var ids []uuid.UUID
	for rows.Next() {
		var work ExecutionWork
		var payload, result []byte
		if err := rows.Scan(&work.Attempt.ID, &work.Attempt.OrgID, &work.Attempt.VirployeeID,
			&work.Attempt.PreparedActionID, &work.Attempt.IdempotencyKey, &work.Attempt.Status,
			&work.Attempt.ResourceID, &result, &work.Attempt.Error, &work.Attempt.DurationMS,
			&work.Attempt.GovernanceReportStatus, &work.Attempt.NexusReportStatus,
			&work.Attempt.StartedAt, &work.Attempt.CompletedAt,
			&work.Attempt.RecoveryAttempts, &work.Attempt.ReportAttempts,
			&work.Action.GovernanceCheckID, &work.Action.ApprovalID, &work.Action.CapabilityID,
			&work.Action.CapabilityKey, &work.Action.PayloadVersion, &work.Action.ExecutorBindingID,
			&work.Action.Operation, &payload, &work.Action.PayloadHash, &work.Action.BindingHash,
			&work.Action.AuthorityBindingHash, &work.Action.GovernancePolicySnapshotHash,
			&work.Action.NexusPolicySnapshotHash); err != nil {
			rows.Close()
			return nil, err
		}
		work.Action.ID, work.Action.OrgID, work.Action.VirployeeID = work.Attempt.PreparedActionID, work.Attempt.OrgID, work.Attempt.VirployeeID
		if work.Attempt.GovernanceReportStatus == "" {
			work.Attempt.GovernanceReportStatus = work.Attempt.NexusReportStatus
		}
		if work.Attempt.NexusReportStatus == "" {
			work.Attempt.NexusReportStatus = work.Attempt.GovernanceReportStatus
		}
		if work.Action.GovernancePolicySnapshotHash == "" {
			work.Action.GovernancePolicySnapshotHash = work.Action.NexusPolicySnapshotHash
		}
		if work.Action.NexusPolicySnapshotHash == "" {
			work.Action.NexusPolicySnapshotHash = work.Action.GovernancePolicySnapshotHash
		}
		if work.Action.PayloadVersion == preparedactions.V2SchemaVersion {
			var action preparedactions.PreparedActionV2
			if err := json.Unmarshal(payload, &action); err != nil {
				rows.Close()
				return nil, fmt.Errorf("decode watcher prepared action v2: %w", err)
			}
			if err := action.Validate(); err != nil {
				rows.Close()
				return nil, fmt.Errorf("validate watcher prepared action v2: %w", err)
			}
			work.Action.ActionV2 = &action
		} else if err := json.Unmarshal(payload, &work.Action.Action); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode watcher prepared action: %w", err)
		}
		if len(result) > 0 {
			_ = json.Unmarshal(result, &work.Attempt.Result)
		}
		out, ids = append(out, work), append(ids, work.Attempt.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_, err = tx.Exec(ctx, `UPDATE companion_execution_attempts SET recovery_lease_owner=$2, recovery_lease_until=$3 WHERE id=ANY($1)`, ids, owner, now.Add(lease))
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ReleaseExecutionRecovery(ctx context.Context, orgID string, id uuid.UUID, watcherError string) error {
	_, err := r.pool.Exec(ctx, `UPDATE companion_execution_attempts SET recovery_attempts=recovery_attempts+1, recovery_lease_owner='', recovery_lease_until=NULL, last_watcher_error=$3 WHERE org_id=$1 AND id=$2`, orgID, id, watcherError)
	return err
}

var _ OperationalRepositoryPort = (*Repository)(nil)
