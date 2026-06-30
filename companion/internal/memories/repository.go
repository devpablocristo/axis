package memories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	commonaudit "github.com/devpablocristo/companion/internal/audit"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type PostgresRepository struct {
	db    *sharedpostgres.DB
	audit commonaudit.Recorder
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) SetAuditRecorder(recorder commonaudit.Recorder) {
	r.audit = recorder
}

const memoryColumnsSQL = `
	id, tenant_id, org_id, product_surface, owner_employee_id, policy_json,
	status, created_at, updated_at, archived_at, version`

const selectMemorySQL = `
	SELECT ` + memoryColumnsSQL + `
	FROM companion_memories`

func (r *PostgresRepository) ListMemories(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]Memory, error) {
	query := selectMemorySQL + ` WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3`
	switch lifecycle {
	case "archived":
		query += ` AND status = 'archived'`
	case "all":
	default:
		query += ` AND status <> 'archived'`
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := r.db.Pool().Query(ctx, query, tenantID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface))
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()
	out := make([]Memory, 0)
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, memory)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetMemory(ctx context.Context, tenantID, orgID, productSurface, memoryID string) (Memory, error) {
	row := r.db.Pool().QueryRow(ctx, selectMemorySQL+`
		WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4
	`, tenantID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(memoryID))
	memory, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, ErrNotFound
		}
		return Memory{}, fmt.Errorf("get memory: %w", err)
	}
	return memory, nil
}

func (r *PostgresRepository) CreateMemory(ctx context.Context, memory Memory, actorID string) (Memory, error) {
	policy, err := json.Marshal(memory.Policy)
	if err != nil {
		return Memory{}, fmt.Errorf("marshal memory policy: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO companion_memories
				(tenant_id, org_id, product_surface, owner_employee_id, policy_json, status)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING `+memoryColumnsSQL+`
		)
		SELECT * FROM inserted
	`, memory.TenantID, memory.OrgID, memory.ProductSurface, memory.OwnerEmployeeID, policy, memory.Status)
	created, err := scanMemory(row)
	if err != nil {
		return Memory{}, fmt.Errorf("create memory: %w", err)
	}
	if err := r.recordAudit(ctx, created, actorID, "created"); err != nil {
		return Memory{}, err
	}
	return created, nil
}

func (r *PostgresRepository) UpdateMemory(ctx context.Context, memory Memory, actorID string) (Memory, error) {
	policy, err := json.Marshal(memory.Policy)
	if err != nil {
		return Memory{}, fmt.Errorf("marshal memory policy: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		WITH updated AS (
			UPDATE companion_memories
			SET owner_employee_id = $5,
			    policy_json = $6,
			    status = $7,
			    updated_at = now(),
			    version = version + 1
			WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4 AND status <> 'archived'
			RETURNING `+memoryColumnsSQL+`
		)
		SELECT * FROM updated
	`, memory.TenantID, memory.OrgID, memory.ProductSurface, memory.MemoryID, memory.OwnerEmployeeID, policy, memory.Status)
	updated, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := r.GetMemory(ctx, memory.TenantID.String(), memory.OrgID, memory.ProductSurface, memory.MemoryID.String())
			if getErr == nil && existing.Status == MemoryStatusArchived {
				return Memory{}, fmt.Errorf("%w: memory is archived; restore it before updating", ErrConflict)
			}
			if errors.Is(getErr, ErrNotFound) {
				return Memory{}, ErrNotFound
			}
			if getErr != nil {
				return Memory{}, getErr
			}
		}
		return Memory{}, fmt.Errorf("update memory: %w", err)
	}
	if err := r.recordAudit(ctx, updated, actorID, "updated"); err != nil {
		return Memory{}, err
	}
	return updated, nil
}

func (r *PostgresRepository) SetMemoryStatus(ctx context.Context, tenantID, orgID, productSurface, memoryID string, status MemoryStatus, actorID string) (Memory, error) {
	current, err := r.GetMemory(ctx, tenantID, orgID, productSurface, memoryID)
	if err != nil {
		return Memory{}, err
	}
	if current.Status == status {
		return current, nil
	}
	archivedExpr := "NULL"
	if status == MemoryStatusArchived {
		archivedExpr = "now()"
	}
	row := r.db.Pool().QueryRow(ctx, `
		WITH updated AS (
			UPDATE companion_memories
			SET status = $5,
			    archived_at = `+archivedExpr+`,
			    updated_at = now(),
			    version = version + 1
			WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4
			RETURNING `+memoryColumnsSQL+`
		)
		SELECT * FROM updated
	`, tenantID, orgID, productSurface, memoryID, status)
	updated, err := scanMemory(row)
	if err != nil {
		return Memory{}, fmt.Errorf("set memory status: %w", err)
	}
	if err := r.recordAudit(ctx, updated, actorID, "status."+string(status)); err != nil {
		return Memory{}, err
	}
	return updated, nil
}

func (r *PostgresRepository) ListEntries(ctx context.Context, tenantID, orgID, productSurface, memoryID string) ([]MemoryEntry, error) {
	if _, err := r.GetMemory(ctx, tenantID, orgID, productSurface, memoryID); err != nil {
		return nil, err
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, memory_id, kind, content_text, confidence, status, created_at, updated_at
		FROM companion_memory_container_entries
		WHERE memory_id = $1
		ORDER BY updated_at DESC
	`, memoryID)
	if err != nil {
		return nil, fmt.Errorf("list memory entries: %w", err)
	}
	defer rows.Close()
	out := make([]MemoryEntry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) CreateEntry(ctx context.Context, tenantID, orgID, productSurface, memoryID string, entry MemoryEntry, actorID string) (MemoryEntry, error) {
	memory, err := r.GetMemory(ctx, tenantID, orgID, productSurface, memoryID)
	if err != nil {
		return MemoryEntry{}, err
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_memory_container_entries
			(memory_id, kind, content_text, confidence, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, memory_id, kind, content_text, confidence, status, created_at, updated_at
	`, memoryID, entry.Kind, entry.ContentText, entry.Confidence, entry.Status)
	created, err := scanEntry(row)
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("create memory entry: %w", err)
	}
	if err := r.recordAudit(ctx, memory, actorID, "entry.created"); err != nil {
		return MemoryEntry{}, err
	}
	return created, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMemory(row scanner) (Memory, error) {
	var memory Memory
	var policy []byte
	if err := row.Scan(
		&memory.MemoryID,
		&memory.TenantID,
		&memory.OrgID,
		&memory.ProductSurface,
		&memory.OwnerEmployeeID,
		&policy,
		&memory.Status,
		&memory.CreatedAt,
		&memory.UpdatedAt,
		&memory.ArchivedAt,
		&memory.Version,
	); err != nil {
		return Memory{}, err
	}
	if len(policy) > 0 {
		if err := json.Unmarshal(policy, &memory.Policy); err != nil {
			return Memory{}, fmt.Errorf("decode memory policy: %w", err)
		}
	}
	return memory, nil
}

func scanEntry(row scanner) (MemoryEntry, error) {
	var entry MemoryEntry
	if err := row.Scan(
		&entry.MemoryEntryID,
		&entry.MemoryID,
		&entry.Kind,
		&entry.ContentText,
		&entry.Confidence,
		&entry.Status,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	); err != nil {
		return MemoryEntry{}, err
	}
	return entry, nil
}

func (r *PostgresRepository) recordAudit(ctx context.Context, memory Memory, actorID string, action string) error {
	snapshot, err := json.Marshal(memory)
	if err != nil {
		return fmt.Errorf("marshal memory audit snapshot: %w", err)
	}
	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO companion_memory_container_audit
			(memory_id, tenant_id, actor_id, action, status, snapshot)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, memory.MemoryID, memory.TenantID, strings.TrimSpace(actorID), action, memory.Status, snapshot)
	if err != nil {
		return fmt.Errorf("record memory audit: %w", err)
	}
	if r.audit != nil {
		if err := r.audit.Record(ctx, commonaudit.Event{
			TenantID:     memory.TenantID.String(),
			ResourceType: "memory",
			ResourceID:   memory.MemoryID,
			Action:       action,
			ActorUserID:  strings.TrimSpace(actorID),
		}); err != nil {
			return fmt.Errorf("record memory lifecycle audit: %w", err)
		}
	}
	return nil
}

var _ Repository = (*PostgresRepository)(nil)
