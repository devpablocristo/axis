package virployees

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PreparedActionRecord struct {
	ID                uuid.UUID
	TenantID          string
	VirployeeID       uuid.UUID
	GovernanceCheckID uuid.UUID
	ApprovalID        uuid.UUID
	CapabilityKey     string
	Action            preparedactions.Action
	PayloadHash       string
	BindingHash       string
}

type ExecutionAttempt struct {
	ID                uuid.UUID
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
}

func (r *Repository) SavePreparedAction(ctx context.Context, tenantID string, virployeeID uuid.UUID, checkID, approvalID string, capabilityKey, payloadHash, bindingHash string, action preparedactions.Action) (PreparedActionRecord, error) {
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
			id, tenant_id, virployee_id, governance_check_id, approval_id,
			capability_key, action, payload, payload_hash, binding_hash, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, now())
		ON CONFLICT (tenant_id, approval_id) DO NOTHING
	`, id, tenantID, virployeeID, parsedCheckID, parsedApprovalID, capabilityKey, action.Action, payload, payloadHash, bindingHash)
	if err != nil {
		return PreparedActionRecord{}, fmt.Errorf("save prepared action: %w", err)
	}
	return r.GetPreparedActionByApproval(ctx, tenantID, virployeeID, parsedApprovalID)
}

func (r *Repository) GetPreparedActionByApproval(ctx context.Context, tenantID string, virployeeID, approvalID uuid.UUID) (PreparedActionRecord, error) {
	var out PreparedActionRecord
	var payload []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, virployee_id, governance_check_id, approval_id,
		       capability_key, action, payload, payload_hash, binding_hash
		FROM companion_prepared_actions
		WHERE tenant_id = $1 AND virployee_id = $2 AND approval_id = $3
	`, tenantID, virployeeID, approvalID).Scan(
		&out.ID, &out.TenantID, &out.VirployeeID, &out.GovernanceCheckID, &out.ApprovalID,
		&out.CapabilityKey, &out.Action.Action, &payload, &out.PayloadHash, &out.BindingHash,
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

func (r *Repository) BeginExecution(ctx context.Context, tenantID string, virployeeID uuid.UUID, preparedActionID uuid.UUID, idempotencyKey string) (ExecutionAttempt, bool, error) {
	id := uuid.New()
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO companion_execution_attempts (
			id, tenant_id, virployee_id, prepared_action_id, idempotency_key,
			status, nexus_report_status, started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'running', 'pending', now(), now())
		ON CONFLICT (tenant_id, prepared_action_id) DO NOTHING
	`, id, tenantID, virployeeID, preparedActionID, idempotencyKey)
	if err != nil {
		return ExecutionAttempt{}, false, err
	}
	attempt, err := r.GetExecutionByPreparedAction(ctx, tenantID, preparedActionID)
	return attempt, tag.RowsAffected() == 1, err
}

func (r *Repository) GetExecutionByPreparedAction(ctx context.Context, tenantID string, preparedActionID uuid.UUID) (ExecutionAttempt, error) {
	var out ExecutionAttempt
	var result []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, prepared_action_id, idempotency_key, status, resource_id, result,
		       error, duration_ms, nexus_report_status, started_at, completed_at
		FROM companion_execution_attempts
		WHERE tenant_id = $1 AND prepared_action_id = $2
	`, tenantID, preparedActionID).Scan(
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

func (r *Repository) CompleteExecution(ctx context.Context, tenantID string, id uuid.UUID, status, resourceID string, result map[string]any, executionError string, durationMS int64) (ExecutionAttempt, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	_, err = r.pool.Exec(ctx, `
		UPDATE companion_execution_attempts
		SET status = $3, resource_id = $4, result = $5::jsonb, error = $6,
		    duration_ms = $7, completed_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, status, resourceID, raw, executionError, durationMS)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	var preparedActionID uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT prepared_action_id FROM companion_execution_attempts WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&preparedActionID); err != nil {
		return ExecutionAttempt{}, err
	}
	return r.GetExecutionByPreparedAction(ctx, tenantID, preparedActionID)
}

func (r *Repository) CreateLocalCalendarEvent(ctx context.Context, tenantID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (string, error) {
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
			id, tenant_id, virployee_id, execution_attempt_id, idempotency_key,
			title, starts_at, timezone, duration_minutes, attendees, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, now())
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
	`, id, tenantID, virployeeID, attempt.ID, attempt.IdempotencyKey, action.Title, startsAt.UTC(), action.Timezone, action.DurationMinutes, attendees)
	if err != nil {
		return "", err
	}
	var existing uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT id FROM companion_local_calendar_events WHERE tenant_id = $1 AND idempotency_key = $2`, tenantID, attempt.IdempotencyKey).Scan(&existing); err != nil {
		return "", err
	}
	return existing.String(), nil
}

func (r *Repository) SetNexusReportStatus(ctx context.Context, tenantID string, id uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE companion_execution_attempts
		SET nexus_report_status = $3, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, status)
	return err
}
