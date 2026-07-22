package virployees

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/attestation"
	"github.com/devpablocristo/companion-v2/internal/outbox"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PreparedActionRecord struct {
	ID                      uuid.UUID
	OrgID                   string
	VirployeeID             uuid.UUID
	GovernanceCheckID       uuid.UUID
	ApprovalID              uuid.UUID
	CapabilityKey           string
	Action                  preparedactions.Action
	PayloadHash             string
	BindingHash             string
	AuthorityBindingHash    string
	NexusPolicySnapshotHash string
}

type ExecutionAttempt struct {
	ID                uuid.UUID
	OrgID             string
	VirployeeID       uuid.UUID
	PreparedActionID  uuid.UUID
	IdempotencyKey    string
	Status            string
	ResourceID        string
	Result            map[string]any
	Error             string
	DurationMS        int64
	NexusReportStatus string
	StartedAt         time.Time
	CompletedAt       *time.Time
	RecoveryAttempts  int
	ReportAttempts    int
}

func (r *Repository) SavePreparedAction(ctx context.Context, orgID string, virployeeID uuid.UUID, checkID, approvalID string, capabilityKey, payloadHash, bindingHash string, action preparedactions.Action) (PreparedActionRecord, error) {
	parsedCheckID, err := uuid.Parse(strings.TrimSpace(checkID))
	if err != nil {
		return PreparedActionRecord{}, domainerr.Validation("governance check id is required")
	}
	parsedApprovalID, err := uuid.Parse(strings.TrimSpace(approvalID))
	if err != nil {
		return PreparedActionRecord{}, domainerr.Validation("approval id is required")
	}
	payload, err := json.Marshal(action)
	if err != nil {
		return PreparedActionRecord{}, err
	}
	id := uuid.New()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO companion_prepared_actions (
			id, org_id, virployee_id, governance_check_id, approval_id,
			capability_key, action, payload, payload_hash, binding_hash, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, now())
		ON CONFLICT (org_id, approval_id) DO NOTHING
	`, id, orgID, virployeeID, parsedCheckID, parsedApprovalID, capabilityKey, action.Action, payload, payloadHash, bindingHash)
	if err != nil {
		return PreparedActionRecord{}, fmt.Errorf("save prepared action: %w", err)
	}
	return r.GetPreparedActionByApproval(ctx, orgID, virployeeID, parsedApprovalID)
}

func (r *Repository) GetPreparedActionByApproval(ctx context.Context, orgID string, virployeeID, approvalID uuid.UUID) (PreparedActionRecord, error) {
	var out PreparedActionRecord
	var payload []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, org_id, virployee_id, governance_check_id, approval_id,
		       capability_key, action, payload, payload_hash, binding_hash, authority_binding_hash,
		       nexus_policy_snapshot_hash
		FROM companion_prepared_actions
		WHERE org_id = $1 AND virployee_id = $2 AND approval_id = $3
	`, orgID, virployeeID, approvalID).Scan(
		&out.ID, &out.OrgID, &out.VirployeeID, &out.GovernanceCheckID, &out.ApprovalID,
		&out.CapabilityKey, &out.Action.Action, &payload, &out.PayloadHash, &out.BindingHash, &out.AuthorityBindingHash,
		&out.NexusPolicySnapshotHash,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return PreparedActionRecord{}, domainerr.NotFound("prepared action not found")
		}
		return PreparedActionRecord{}, err
	}
	if err := json.Unmarshal(payload, &out.Action); err != nil {
		return PreparedActionRecord{}, fmt.Errorf("decode prepared action: %w", err)
	}
	return out, nil
}

// BindPreparedActionAuthority makes the authority snapshot durable before an
// approval can be executed. It is immutable: retries may write the same hash,
// but can never replace it with a different policy/delegation snapshot.
func (r *Repository) BindPreparedActionAuthority(ctx context.Context, orgID string, virployeeID, approvalID uuid.UUID, snapshotHash string) error {
	snapshotHash = strings.TrimSpace(snapshotHash)
	if snapshotHash == "" {
		return domainerr.Validation("professional authority snapshot hash is required")
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE companion_prepared_actions
		SET authority_binding_hash=$4
		WHERE org_id=$1 AND virployee_id=$2 AND approval_id=$3
		  AND (authority_binding_hash='' OR authority_binding_hash=$4)
	`, orgID, virployeeID, approvalID, snapshotHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domainerr.Conflict("prepared action authority binding is missing or immutable")
	}
	return nil
}

// BindPreparedActionNexusPolicy persists the exact policy snapshot returned by
// Nexus. The value is immutable so a later approval cannot be substituted onto
// an already prepared action.
func (r *Repository) BindPreparedActionNexusPolicy(ctx context.Context, orgID string, virployeeID, approvalID uuid.UUID, snapshotHash string) error {
	snapshotHash = strings.TrimSpace(snapshotHash)
	if snapshotHash == "" {
		return domainerr.Validation("Nexus policy snapshot hash is required")
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE companion_prepared_actions
		SET nexus_policy_snapshot_hash=$4
		WHERE org_id=$1 AND virployee_id=$2 AND approval_id=$3
		  AND (nexus_policy_snapshot_hash='' OR nexus_policy_snapshot_hash=$4)
	`, orgID, virployeeID, approvalID, snapshotHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domainerr.Conflict("prepared action Nexus policy binding is missing or immutable")
	}
	return nil
}

func (r *Repository) BeginExecution(ctx context.Context, orgID string, virployeeID uuid.UUID, preparedActionID uuid.UUID, idempotencyKey string) (ExecutionAttempt, bool, error) {
	id := uuid.New()
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO companion_execution_attempts (
			id, org_id, virployee_id, prepared_action_id, idempotency_key,
			status, nexus_report_status, started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'running', 'pending', now(), now())
		ON CONFLICT (org_id, prepared_action_id) DO NOTHING
	`, id, orgID, virployeeID, preparedActionID, idempotencyKey)
	if err != nil {
		return ExecutionAttempt{}, false, err
	}
	attempt, err := r.GetExecutionByPreparedAction(ctx, orgID, preparedActionID)
	return attempt, tag.RowsAffected() == 1, err
}

func (r *Repository) GetExecutionByPreparedAction(ctx context.Context, orgID string, preparedActionID uuid.UUID) (ExecutionAttempt, error) {
	var out ExecutionAttempt
	var result []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, prepared_action_id, idempotency_key, status, resource_id, result,
		       error, duration_ms, nexus_report_status, started_at, completed_at
		FROM companion_execution_attempts
		WHERE org_id = $1 AND prepared_action_id = $2
	`, orgID, preparedActionID).Scan(
		&out.ID, &out.PreparedActionID, &out.IdempotencyKey, &out.Status, &out.ResourceID,
		&result, &out.Error, &out.DurationMS, &out.NexusReportStatus, &out.StartedAt, &out.CompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ExecutionAttempt{}, domainerr.NotFound("execution attempt not found")
		}
		return ExecutionAttempt{}, err
	}
	if err := json.Unmarshal(result, &out.Result); err != nil {
		return ExecutionAttempt{}, err
	}
	return out, nil
}

func (r *Repository) CompleteExecution(ctx context.Context, orgID string, id uuid.UUID, status, resourceID string, result map[string]any, executionError string, durationMS int64) (ExecutionAttempt, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var preparedActionID, virployeeID uuid.UUID
	var idempotencyKey string
	err = tx.QueryRow(ctx, `
		UPDATE companion_execution_attempts
		SET status = $3, resource_id = $4, result = $5::jsonb, error = $6,
		    duration_ms = $7, completed_at = now(), updated_at = now(),
		    nexus_report_status = 'pending',
		    recovery_lease_owner = '', recovery_lease_until = NULL, last_watcher_error = ''
		WHERE org_id = $1 AND id = $2
		RETURNING prepared_action_id, virployee_id, idempotency_key
	`, orgID, id, status, resourceID, raw, executionError, durationMS).Scan(&preparedActionID, &virployeeID, &idempotencyKey)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	var governanceCheckID uuid.UUID
	var bindingHash string
	if err := tx.QueryRow(ctx, `
		SELECT governance_check_id, binding_hash
		FROM companion_prepared_actions
		WHERE org_id=$1 AND id=$2
	`, orgID, preparedActionID).Scan(&governanceCheckID, &bindingHash); err != nil {
		return ExecutionAttempt{}, err
	}
	signed, err := r.attestor.Sign(attestation.Payload{
		OrgID: orgID, GovernanceCheckID: governanceCheckID.String(), BindingHash: bindingHash,
		IdempotencyKey: idempotencyKey, Status: status, DurationMS: durationMS, Result: result,
	})
	if err != nil {
		return ExecutionAttempt{}, err
	}
	payload, err := json.Marshal(outbox.NexusExecutionResult{
		VirployeeID: virployeeID.String(), GovernanceCheckID: governanceCheckID.String(),
		IdempotencyKey: idempotencyKey, BindingHash: bindingHash, Status: status,
		DurationMS: durationMS, Result: result,
		AttestationVersion: signed.Version, ExecutorVersion: signed.ExecutorVersion, Attestation: signed.Signature,
	})
	if err != nil {
		return ExecutionAttempt{}, err
	}
	message, _, err := r.outbox.EnqueueTx(ctx, tx, outbox.EnqueueInput{
		OrgID: orgID, AggregateType: "execution_attempt", AggregateID: id,
		Kind: "execution_result", DedupeKey: id.String(), Payload: payload,
	})
	if err != nil {
		return ExecutionAttempt{}, err
	}
	projection := "pending"
	if message.Status == outbox.StatusDelivered {
		projection = "reported"
	} else if message.Status == outbox.StatusDead {
		projection = "dead"
	} else if message.Attempts > 0 {
		projection = "failed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE companion_execution_attempts
		SET nexus_report_status=$3, nexus_report_attempts=$4, nexus_report_next_at=$5
		WHERE org_id=$1 AND id=$2
	`, orgID, id, projection, message.Attempts, message.AvailableAt); err != nil {
		return ExecutionAttempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExecutionAttempt{}, err
	}
	return r.GetExecutionByPreparedAction(ctx, orgID, preparedActionID)
}

func (r *Repository) CreateLocalCalendarEvent(ctx context.Context, orgID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (string, error) {
	startsAt, err := action.StartsAt()
	if err != nil {
		return "", err
	}
	attendees, err := json.Marshal(action.Attendees)
	if err != nil {
		return "", err
	}
	id := uuid.New()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO companion_local_calendar_events (
			id, org_id, virployee_id, execution_attempt_id, idempotency_key,
			title, starts_at, timezone, duration_minutes, attendees, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, now())
		ON CONFLICT (org_id, idempotency_key) DO NOTHING
	`, id, orgID, virployeeID, attempt.ID, attempt.IdempotencyKey, action.Title, startsAt.UTC(), action.Timezone, action.DurationMinutes, attendees)
	if err != nil {
		return "", err
	}
	var existing uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT id FROM companion_local_calendar_events WHERE org_id = $1 AND idempotency_key = $2`, orgID, attempt.IdempotencyKey).Scan(&existing); err != nil {
		return "", err
	}
	return existing.String(), nil
}
