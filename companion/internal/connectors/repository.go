package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

// PostgresRepository implementación PostgreSQL del repositorio de conectores.
type PostgresRepository struct {
	db *sharedpostgres.DB
}

// NewPostgresRepository crea un nuevo repositorio de conectores.
func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// SaveConnector crea un nuevo conector.
func (r *PostgresRepository) SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	now := time.Now().UTC()
	c.ID = uuid.New()
	c.CreatedAt = now
	c.UpdatedAt = now
	if len(c.ConfigJSON) == 0 {
		c.ConfigJSON = json.RawMessage(`{}`)
	}

	if c.Status == "" {
		if c.Enabled {
			c.Status = ConnectorStatusActive
		} else {
			c.Status = ConnectorStatusDisabled
		}
	}
	c.Version = 1

	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_connectors (id, org_id, name, kind, enabled, status, config_json, created_at, updated_at, version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, c.ID, c.OrgID, c.Name, c.Kind, c.Enabled, c.Status, c.ConfigJSON, c.CreatedAt, c.UpdatedAt, c.Version)
	if err != nil {
		return domain.Connector{}, fmt.Errorf("insert connector: %w", err)
	}
	return c, nil
}

// GetConnector obtiene un conector por ID.
func (r *PostgresRepository) GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT id, org_id, name, kind, enabled, status, config_json, created_at, updated_at, archived_at, trashed_at, version
		FROM companion_connectors WHERE id = $1
	`, id)
	c, err := scanConnector(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Connector{}, ErrNotFound
		}
		return domain.Connector{}, fmt.Errorf("get connector: %w", err)
	}
	return c, nil
}

func (r *PostgresRepository) ListConnectors(ctx context.Context) ([]domain.Connector, error) {
	return r.ListConnectorsByLifecycle(ctx, ConnectorStatusActive)
}

// ListConnectorsByLifecycle lista todos los conectores filtrados por lifecycle.
func (r *PostgresRepository) ListConnectorsByLifecycle(ctx context.Context, lifecycle string) ([]domain.Connector, error) {
	query := `
		SELECT id, org_id, name, kind, enabled, status, config_json, created_at, updated_at, archived_at, trashed_at, version
		FROM companion_connectors`
	switch lifecycle {
	case ConnectorStatusArchived:
		query += ` WHERE status = 'archived'`
	case ConnectorStatusTrash:
		query += ` WHERE status = 'trash'`
	case "all":
	default:
		query += ` WHERE status NOT IN ('archived', 'trash')`
	}
	query += ` ORDER BY created_at ASC`
	rows, err := r.db.Pool().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	defer rows.Close()

	var out []domain.Connector
	for rows.Next() {
		c, err := scanConnector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateConnector actualiza un conector.
func (r *PostgresRepository) UpdateConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	c.UpdatedAt = time.Now().UTC()
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE companion_connectors
		SET name = $2,
		    enabled = $3,
		    status = $4,
		    config_json = $5,
		    updated_at = $6,
		    version = version + 1
		WHERE id = $1 AND status NOT IN ('archived', 'trash')
		RETURNING id, org_id, name, kind, enabled, status, config_json, created_at, updated_at, archived_at, trashed_at, version
	`, c.ID, c.Name, c.Enabled, c.Status, c.ConfigJSON, c.UpdatedAt)
	updated, err := scanConnector(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, getErr := r.GetConnector(ctx, c.ID); getErr == nil {
				return domain.Connector{}, ErrConflict
			}
			return domain.Connector{}, ErrNotFound
		}
		return domain.Connector{}, fmt.Errorf("update connector: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) SetConnectorStatus(ctx context.Context, id uuid.UUID, status string) (domain.Connector, error) {
	current, err := r.GetConnector(ctx, id)
	if err != nil {
		return domain.Connector{}, err
	}
	if current.Status == status {
		return current, nil
	}
	enabled := status == ConnectorStatusActive
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE companion_connectors
		SET status = $2,
		    enabled = $3,
		    archived_at = CASE WHEN $2 = 'archived' THEN now() WHEN $2 = 'trash' THEN archived_at ELSE NULL END,
		    trashed_at = CASE WHEN $2 = 'trash' THEN now() ELSE NULL END,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $1
		RETURNING id, org_id, name, kind, enabled, status, config_json, created_at, updated_at, archived_at, trashed_at, version
	`, id, status, enabled)
	updated, err := scanConnector(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Connector{}, ErrNotFound
		}
		return domain.Connector{}, fmt.Errorf("set connector status: %w", err)
	}
	return updated, nil
}

// DeleteConnector elimina un conector.
func (r *PostgresRepository) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM companion_connectors WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete connector: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveExecution persiste un resultado de ejecución.
func (r *PostgresRepository) SaveExecution(ctx context.Context, e domain.ExecutionResult) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if len(e.ResultJSON) == 0 {
		e.ResultJSON = json.RawMessage(`{}`)
	}
	if len(e.EvidenceJSON) == 0 {
		e.EvidenceJSON = json.RawMessage(`{}`)
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage(`{}`)
	}

	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_connector_executions
			(id, connector_id, org_id, actor_id, operation, status, external_ref, payload, result_json,
			 evidence_json, error_message, retryable, duration_ms, idempotency_key, task_id, nexus_request_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	`, e.ID, e.ConnectorID, e.OrgID, e.ActorID, e.Operation, e.Status, e.ExternalRef,
		e.Payload, e.ResultJSON, e.EvidenceJSON, nullIfEmpty(e.ErrorMessage),
		e.Retryable, e.DurationMS, e.IdempotencyKey, e.TaskID, e.NexusRequestID, e.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("save execution: %w", err)
	}
	return nil
}

func (r *PostgresRepository) AcquireExecutionLock(ctx context.Context, lockKey string) (bool, error) {
	if lockKey == "" {
		return true, nil
	}
	tag, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_connector_execution_locks (lock_key, created_at)
		VALUES ($1, now())
		ON CONFLICT (lock_key) DO NOTHING
	`, lockKey)
	if err != nil {
		return false, fmt.Errorf("acquire execution lock: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PostgresRepository) ReleaseExecutionLock(ctx context.Context, lockKey string) error {
	if lockKey == "" {
		return nil
	}
	_, err := r.db.Pool().Exec(ctx, `DELETE FROM companion_connector_execution_locks WHERE lock_key = $1`, lockKey)
	if err != nil {
		return fmt.Errorf("release execution lock: %w", err)
	}
	return nil
}

// GetExecutionByIdempotency devuelve una ejecución ya registrada para una key de idempotencia.
func (r *PostgresRepository) GetExecutionByIdempotency(ctx context.Context, taskID uuid.UUID, operation string, nexusRequestID *uuid.UUID, idempotencyKey string) (domain.ExecutionResult, error) {
	if taskID == uuid.Nil || idempotencyKey == "" {
		return domain.ExecutionResult{}, ErrNotFound
	}
	var nexusID any
	if nexusRequestID != nil {
		nexusID = *nexusRequestID
	}
	row := r.db.Pool().QueryRow(ctx, `
		SELECT id, connector_id, org_id, actor_id, operation, status, external_ref, payload, result_json,
		       evidence_json, error_message, retryable, duration_ms, idempotency_key, task_id, nexus_request_id, created_at
		FROM companion_connector_executions
		WHERE task_id = $1
		  AND operation = $2
		  AND idempotency_key = $3
		  AND (($4::uuid IS NULL AND nexus_request_id IS NULL) OR nexus_request_id = $4::uuid)
		ORDER BY created_at DESC
		LIMIT 1
	`, taskID, operation, idempotencyKey, nexusID)
	execution, err := scanExecution(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ExecutionResult{}, ErrNotFound
		}
		return domain.ExecutionResult{}, fmt.Errorf("get execution by idempotency: %w", err)
	}
	return execution, nil
}

// ListExecutions lista resultados de ejecución de un conector.
func (r *PostgresRepository) ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, connector_id, org_id, actor_id, operation, status, external_ref, payload, result_json,
		       evidence_json, error_message, retryable, duration_ms, idempotency_key, task_id, nexus_request_id, created_at
		FROM companion_connector_executions
		WHERE connector_id = $1
		ORDER BY created_at DESC LIMIT $2
	`, connectorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()

	var out []domain.ExecutionResult
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanConnector(row rowScanner) (domain.Connector, error) {
	var c domain.Connector
	var configRaw []byte
	err := row.Scan(&c.ID, &c.OrgID, &c.Name, &c.Kind, &c.Enabled, &c.Status, &configRaw, &c.CreatedAt, &c.UpdatedAt, &c.ArchivedAt, &c.TrashedAt, &c.Version)
	if err != nil {
		return domain.Connector{}, err
	}
	if configRaw != nil {
		c.ConfigJSON = json.RawMessage(configRaw)
	}
	return c, nil
}

func scanExecution(row rowScanner) (domain.ExecutionResult, error) {
	var e domain.ExecutionResult
	var payloadRaw, resultRaw, evidenceRaw []byte
	var errMsg *string

	err := row.Scan(
		&e.ID, &e.ConnectorID, &e.OrgID, &e.ActorID, &e.Operation, &e.Status, &e.ExternalRef,
		&payloadRaw, &resultRaw, &evidenceRaw, &errMsg, &e.Retryable, &e.DurationMS,
		&e.IdempotencyKey, &e.TaskID, &e.NexusRequestID, &e.CreatedAt,
	)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	if payloadRaw != nil {
		e.Payload = json.RawMessage(payloadRaw)
	}
	if resultRaw != nil {
		e.ResultJSON = json.RawMessage(resultRaw)
	}
	if evidenceRaw != nil {
		e.EvidenceJSON = json.RawMessage(evidenceRaw)
	}
	if errMsg != nil {
		e.ErrorMessage = *errMsg
	}
	return e, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
