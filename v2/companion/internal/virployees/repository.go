package virployees

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/companion-v2/internal/attestation"
	"github.com/devpablocristo/companion-v2/internal/outbox"
	"github.com/devpablocristo/companion-v2/internal/virployees/repository/models"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

type Repository struct {
	pool     *pgxpool.Pool
	outbox   *outbox.Repository
	attestor *attestation.Signer
}

func (r *Repository) SetExecutionAttestor(signer *attestation.Signer) { r.attestor = signer }

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, outbox: outbox.NewRepository(pool)}
}

func (r *Repository) Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.Virployee, error) {
	id := uuid.New()
	now := time.Now().UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Virployee{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		INSERT INTO virployees (id, tenant_id, name, job_role_id, profile_template_id, description, supervisor_user_id, autonomy, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4::uuid, $5::uuid, $6, $7, $8, $9, $9)
		RETURNING id::text, name, job_role_id::text, profile_template_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
	`, id.String(), tenantID, input.Name, input.JobRoleID.String(), input.ProfileTemplateID.String(), input.Description, input.SupervisorUserID, string(input.Autonomy), now)
	item, err := scanVirployee(row)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := replaceVirployeeCapabilities(ctx, tx, tenantID, id, input.CapabilityIDs); err != nil {
		return domain.Virployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Virployee{}, err
	}
	item.CapabilityIDs = input.CapabilityIDs
	return item, nil
}

func (r *Repository) List(ctx context.Context, tenantID string, state domain.State) ([]domain.Virployee, error) {
	var where string
	switch state {
	case domain.StateActive, "":
		where = "tenant_id = $1 AND archived_at IS NULL AND trashed_at IS NULL"
	case domain.StateArchived:
		where = "tenant_id = $1 AND archived_at IS NOT NULL AND trashed_at IS NULL"
	case domain.StateTrashed:
		where = "tenant_id = $1 AND trashed_at IS NOT NULL"
	default:
		return nil, domainerr.Validation("invalid lifecycle state")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, job_role_id::text, profile_template_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM virployees
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Virployee{}
	for rows.Next() {
		item, err := scanVirployee(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return r.attachRelated(ctx, tenantID, out)
}

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Virployee, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, name, job_role_id::text, profile_template_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM virployees
		WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, id.String())
	item, err := scanVirployee(row)
	if err != nil {
		return domain.Virployee{}, err
	}
	ids, err := r.capabilityIDs(ctx, tenantID, id)
	if err != nil {
		return domain.Virployee{}, err
	}
	item.CapabilityIDs = ids
	return item, nil
}

func (r *Repository) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Virployee{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		UPDATE virployees
		SET name = $3,
			job_role_id = $4::uuid,
			profile_template_id = $5::uuid,
			description = $6,
			supervisor_user_id = $7,
			autonomy = $8,
			updated_at = $9
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
		RETURNING id::text, name, job_role_id::text, profile_template_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
	`, tenantID, id.String(), input.Name, input.JobRoleID.String(), input.ProfileTemplateID.String(), input.Description, input.SupervisorUserID, string(input.Autonomy), time.Now().UTC())
	item, err := scanVirployee(row)
	if err == nil {
		if err := replaceVirployeeCapabilities(ctx, tx, tenantID, id, input.CapabilityIDs); err != nil {
			return domain.Virployee{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Virployee{}, err
		}
		item.CapabilityIDs = input.CapabilityIDs
		return item, nil
	}
	if !domainerr.IsNotFound(err) {
		return domain.Virployee{}, err
	}
	state, stateErr := r.State(ctx, tenantID, id)
	if stateErr != nil {
		return domain.Virployee{}, stateErr
	}
	if state != lifecycle.StateActive {
		return domain.Virployee{}, domainerr.Conflict("virployee is not active")
	}
	return domain.Virployee{}, err
}

func (r *Repository) Archive(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET archived_at = $3, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), at.UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) Unarchive(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET archived_at = NULL, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) Purge(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM virployees
		WHERE tenant_id = $1 AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, tenantID, resourceID.String())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) IsArchived(ctx context.Context, tenantID string, resourceID uuid.UUID) (bool, error) {
	state, err := r.State(ctx, tenantID, resourceID)
	if err != nil {
		return false, err
	}
	return state == lifecycle.StateArchived, nil
}

func (r *Repository) Trash(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET archived_at = NULL, trashed_at = $3, purge_after = $4, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), at.UTC(), nullableTime(purgeAfter))
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET trashed_at = NULL, purge_after = NULL, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, tenantID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) State(ctx context.Context, tenantID string, resourceID uuid.UUID) (lifecycle.LifecycleState, error) {
	var archivedAt sql.NullTime
	var trashedAt sql.NullTime
	err := r.pool.QueryRow(ctx, `
		SELECT archived_at, trashed_at
		FROM virployees
		WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, resourceID.String()).Scan(&archivedAt, &trashedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domainerr.NotFoundf("virployee", resourceID.String())
	}
	if err != nil {
		return "", err
	}
	switch {
	case trashedAt.Valid:
		return lifecycle.StateTrashed, nil
	case archivedAt.Valid:
		return lifecycle.StateArchived, nil
	default:
		return lifecycle.StateActive, nil
	}
}

func (r *Repository) CreateRunTrace(ctx context.Context, tenantID string, input runtraces.CreateInput) (runtraces.Trace, error) {
	id := uuid.New()
	now := time.Now().UTC()
	intent, err := json.Marshal(runtraces.RedactValue(input.Intent))
	if err != nil {
		return runtraces.Trace{}, fmt.Errorf("marshal intent trace: %w", err)
	}
	checks, err := json.Marshal(input.GateChecks)
	if err != nil {
		return runtraces.Trace{}, fmt.Errorf("marshal gate checks trace: %w", err)
	}
	memoryReferences, err := json.Marshal(input.MemoryReferences)
	if err != nil {
		return runtraces.Trace{}, fmt.Errorf("marshal memory references: %w", err)
	}
	var nexusResult any
	if input.NexusResult != nil {
		redacted := runtraces.RedactValue(map[string]any{
			"check_id":               input.NexusResult.CheckID,
			"available":              input.NexusResult.Available,
			"decision":               input.NexusResult.Decision,
			"risk_level":             input.NexusResult.RiskLevel,
			"status":                 input.NexusResult.Status,
			"decision_reason":        input.NexusResult.DecisionReason,
			"would_require_approval": input.NexusResult.WouldRequireApproval,
			"binding_hash":           input.NexusResult.BindingHash,
			"approval_id":            input.NexusResult.ApprovalID,
			"approval_status":        input.NexusResult.ApprovalStatus,
			"error":                  input.NexusResult.Error,
		})
		raw, err := json.Marshal(redacted)
		if err != nil {
			return runtraces.Trace{}, fmt.Errorf("marshal nexus trace: %w", err)
		}
		nexusResult = string(raw)
	}
	var executionResult any
	if input.ExecutionResult != nil {
		redacted := runtraces.RedactValue(map[string]any{
			"status":              input.ExecutionResult.Status,
			"mode":                input.ExecutionResult.Mode,
			"approval_id":         input.ExecutionResult.ApprovalID,
			"approval_status":     input.ExecutionResult.ApprovalStatus,
			"binding_hash":        input.ExecutionResult.BindingHash,
			"message":             input.ExecutionResult.Message,
			"external_effects":    input.ExecutionResult.ExternalEffects,
			"resource_id":         input.ExecutionResult.ResourceID,
			"duration_ms":         input.ExecutionResult.DurationMS,
			"nexus_report_status": input.ExecutionResult.NexusReportStatus,
		})
		raw, err := json.Marshal(redacted)
		if err != nil {
			return runtraces.Trace{}, fmt.Errorf("marshal execution trace: %w", err)
		}
		executionResult = string(raw)
	}
	inputHash := strings.TrimSpace(input.InputHash)
	if inputHash == "" {
		inputHash = runtraces.HashString(input.Input)
	}
	inputPreview := strings.TrimSpace(input.InputPreview)
	if inputPreview == "" {
		inputPreview = runtraces.InputPreview(input.Input)
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO companion_run_traces (
			id,
			tenant_id,
			virployee_id,
			operation,
			input_hash,
			input_preview,
			intent,
			capability_id,
			capability_key,
			dry_run_decision,
			gate_decision,
			gate_checks,
			nexus_result,
			execution_result,
			binding_hash,
			memory_references,
			memory_context_hash,
			created_at
		)
		VALUES (
			$1::uuid,
			$2,
			$3::uuid,
			$4,
			$5,
			$6,
			$7::jsonb,
			$8::uuid,
			$9,
			$10,
			$11,
			$12::jsonb,
			$13::jsonb,
			$14::jsonb,
			$15,
			$16::jsonb,
			$17,
			$18
		)
		RETURNING
			id::text,
			tenant_id,
			virployee_id::text,
			operation,
			input_hash,
			input_preview,
			intent,
			capability_id::text,
			capability_key,
			dry_run_decision,
			gate_decision,
			gate_checks,
			nexus_result,
			execution_result,
			binding_hash,
			memory_references,
			memory_context_hash,
			created_at
	`, id.String(), tenantID, input.VirployeeID.String(), string(input.Operation), inputHash, inputPreview, string(intent), nullableUUID(input.CapabilityID), input.CapabilityKey, input.DryRunDecision, input.GateDecision, string(checks), nexusResult, executionResult, input.BindingHash, string(memoryReferences), input.MemoryContextHash, now)
	return scanRunTrace(row)
}

func (r *Repository) ListRunTraces(ctx context.Context, tenantID string, virployeeID uuid.UUID, limit int) ([]runtraces.Trace, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			tenant_id,
			virployee_id::text,
			operation,
			input_hash,
			input_preview,
			intent,
			capability_id::text,
			capability_key,
			dry_run_decision,
			gate_decision,
			gate_checks,
			nexus_result,
			execution_result,
			binding_hash,
			memory_references,
			memory_context_hash,
			created_at
		FROM companion_run_traces
		WHERE tenant_id = $1
			AND virployee_id = $2::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, tenantID, virployeeID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []runtraces.Trace{}
	for rows.Next() {
		item, err := scanRunTrace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) FindExecutionGateTraceByApproval(
	ctx context.Context,
	tenantID string,
	virployeeID uuid.UUID,
	approvalID string,
) (runtraces.Trace, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			tenant_id,
			virployee_id::text,
			operation,
			input_hash,
			input_preview,
			intent,
			capability_id::text,
			capability_key,
			dry_run_decision,
			gate_decision,
			gate_checks,
			nexus_result,
			execution_result,
			binding_hash,
			memory_references,
			memory_context_hash,
			created_at
		FROM companion_run_traces
		WHERE tenant_id = $1
			AND virployee_id = $2::uuid
			AND operation = 'execution_gate'
			AND nexus_result->>'approval_id' = $3
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID, virployeeID.String(), strings.TrimSpace(approvalID))
	return scanRunTrace(row)
}

func (r *Repository) FindSimulatedExecutionTraceByApproval(
	ctx context.Context,
	tenantID string,
	virployeeID uuid.UUID,
	approvalID string,
) (runtraces.Trace, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			tenant_id,
			virployee_id::text,
			operation,
			input_hash,
			input_preview,
			intent,
			capability_id::text,
			capability_key,
			dry_run_decision,
			gate_decision,
			gate_checks,
			nexus_result,
			execution_result,
			binding_hash,
			memory_references,
			memory_context_hash,
			created_at
		FROM companion_run_traces
		WHERE tenant_id = $1
			AND virployee_id = $2::uuid
			AND operation = 'simulated_execution'
			AND execution_result->>'approval_id' = $3
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID, virployeeID.String(), strings.TrimSpace(approvalID))
	return scanRunTrace(row)
}

func (r *Repository) FindExecutionTraceByApproval(
	ctx context.Context,
	tenantID string,
	virployeeID uuid.UUID,
	approvalID string,
) (runtraces.Trace, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text, tenant_id, virployee_id::text, operation, input_hash, input_preview,
			intent, capability_id::text, capability_key, dry_run_decision, gate_decision,
			gate_checks, nexus_result, execution_result, binding_hash, memory_references, memory_context_hash, created_at
		FROM companion_run_traces
		WHERE tenant_id = $1 AND virployee_id = $2::uuid AND operation = 'execution'
			AND execution_result->>'approval_id' = $3
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID, virployeeID.String(), strings.TrimSpace(approvalID))
	return scanRunTrace(row)
}

func (r *Repository) lifecycleResult(ctx context.Context, tenantID string, id uuid.UUID, tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, stateErr := r.State(ctx, tenantID, id); stateErr != nil {
		return stateErr
	}
	return domainerr.Conflict("invalid lifecycle transition")
}

func (r *Repository) attachRelated(ctx context.Context, tenantID string, items []domain.Virployee) ([]domain.Virployee, error) {
	for i := range items {
		ids, err := r.capabilityIDs(ctx, tenantID, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].CapabilityIDs = ids
	}
	return items, nil
}

func (r *Repository) capabilityIDs(ctx context.Context, tenantID string, virployeeID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT capability_id::text
		FROM virployee_capabilities
		WHERE tenant_id = $1
			AND virployee_id = $2::uuid
		ORDER BY capability_id::text ASC
	`, tenantID, virployeeID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []uuid.UUID{}
	for rows.Next() {
		var idText string
		if err := rows.Scan(&idText); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(idText)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func replaceVirployeeCapabilities(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	virployeeID uuid.UUID,
	capabilityIDs []uuid.UUID,
) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM virployee_capabilities
		WHERE tenant_id = $1
			AND virployee_id = $2::uuid
	`, tenantID, virployeeID.String()); err != nil {
		return err
	}
	for _, capabilityID := range capabilityIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO virployee_capabilities (tenant_id, virployee_id, capability_id)
			VALUES ($1, $2::uuid, $3::uuid)
		`, tenantID, virployeeID.String(), capabilityID.String()); err != nil {
			return mapVirployeeStorageError(err)
		}
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanVirployee(row scanner) (domain.Virployee, error) {
	var idText string
	var jobRoleIDText string
	var profileTemplateIDText string
	var supervisorUserIDText string
	var autonomyText string
	var model models.Virployee
	err := row.Scan(
		&idText,
		&model.Name,
		&jobRoleIDText,
		&profileTemplateIDText,
		&model.Description,
		&supervisorUserIDText,
		&autonomyText,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Virployee{}, domainerr.NotFound("virployee not found")
	}
	if err != nil {
		return domain.Virployee{}, mapVirployeeStorageError(err)
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.Virployee{}, err
	}
	jobRoleID, err := uuid.Parse(jobRoleIDText)
	if err != nil {
		return domain.Virployee{}, err
	}
	profileTemplateID, err := uuid.Parse(profileTemplateIDText)
	if err != nil {
		return domain.Virployee{}, err
	}
	model.ID = id
	model.JobRoleID = jobRoleID
	model.ProfileTemplateID = profileTemplateID
	model.SupervisorUserID = supervisorUserIDText
	model.Autonomy = domain.AutonomyLevel(autonomyText)
	return model.ToDomain(), nil
}

func scanRunTrace(row scanner) (runtraces.Trace, error) {
	var idText string
	var virployeeIDText string
	var operation string
	var capabilityID sql.NullString
	var gateDecision sql.NullString
	var intentRaw []byte
	var checksRaw []byte
	var nexusRaw []byte
	var executionRaw []byte
	var memoryReferencesRaw []byte
	var item runtraces.Trace
	err := row.Scan(
		&idText,
		&item.TenantID,
		&virployeeIDText,
		&operation,
		&item.InputHash,
		&item.InputPreview,
		&intentRaw,
		&capabilityID,
		&item.CapabilityKey,
		&item.DryRunDecision,
		&gateDecision,
		&checksRaw,
		&nexusRaw,
		&executionRaw,
		&item.BindingHash,
		&memoryReferencesRaw,
		&item.MemoryContextHash,
		&item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtraces.Trace{}, domainerr.NotFound("run trace not found")
	}
	if err != nil {
		return runtraces.Trace{}, err
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return runtraces.Trace{}, err
	}
	virployeeID, err := uuid.Parse(virployeeIDText)
	if err != nil {
		return runtraces.Trace{}, err
	}
	item.ID = id
	item.VirployeeID = virployeeID
	item.Operation = runtraces.Operation(operation)
	if len(intentRaw) > 0 {
		if err := json.Unmarshal(intentRaw, &item.Intent); err != nil {
			return runtraces.Trace{}, err
		}
	}
	if len(checksRaw) > 0 {
		if err := json.Unmarshal(checksRaw, &item.GateChecks); err != nil {
			return runtraces.Trace{}, err
		}
	}
	if capabilityID.Valid {
		item.CapabilityID = capabilityID.String
	}
	if gateDecision.Valid {
		item.GateDecision = gateDecision.String
	}
	if len(nexusRaw) > 0 {
		var nexus runtraces.NexusResult
		if err := json.Unmarshal(nexusRaw, &nexus); err != nil {
			return runtraces.Trace{}, err
		}
		item.NexusResult = &nexus
	}
	if len(executionRaw) > 0 {
		var execution runtraces.ExecutionResult
		if err := json.Unmarshal(executionRaw, &execution); err != nil {
			return runtraces.Trace{}, err
		}
		item.ExecutionResult = &execution
	}
	if len(memoryReferencesRaw) > 0 {
		if err := json.Unmarshal(memoryReferencesRaw, &item.MemoryReferences); err != nil {
			return runtraces.Trace{}, err
		}
	}
	return item, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullableUUID(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func mapVirployeeStorageError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		switch pgErr.ConstraintName {
		case "virployees_job_role_id_fkey":
			return domainerr.Validation("job_role_id must reference an existing job role")
		case "virployees_profile_template_id_fkey":
			return domainerr.Validation("profile_template_id must reference an existing profile template")
		case "virployee_capabilities_capability_id_fkey":
			return domainerr.Validation("capability_ids must reference existing capabilities")
		default:
			return domainerr.Validation("related resource does not exist")
		}
	}
	return err
}

var _ RepositoryPort = (*Repository)(nil)
