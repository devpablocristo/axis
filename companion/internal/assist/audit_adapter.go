package assist

import (
	"context"
	"log/slog"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
)

// PostgresLifecycleAudit persists assist-pack lifecycle transitions
// (archive/restore/hard-delete) into the lifecycle_audit table. It implements
// lifecycle.AuditPort.
//
// Append is best-effort: a failed insert is logged at WARN and swallowed so the
// lifecycle mutation (which already succeeded in the repo) is not left half-done
// without a rollback. This preserves the prior no-op semantics while giving us a
// real audit trail on the happy path. A shared transaction is deferred (see plan).
type PostgresLifecycleAudit struct {
	db  *sharedpostgres.DB
	log *slog.Logger
}

func NewPostgresLifecycleAudit(db *sharedpostgres.DB) *PostgresLifecycleAudit {
	return &PostgresLifecycleAudit{db: db, log: slog.Default()}
}

func (a *PostgresLifecycleAudit) Append(ctx context.Context, entry lifecycle.ArchiveAudit) error {
	_, err := a.db.Pool().Exec(ctx, `
		INSERT INTO lifecycle_audit (
			id, tenant_id, resource_type, resource_id, action, occurred_at,
			actor, reason, batch_id, retention_expires
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, entry.ID, entry.TenantID, entry.ResourceType, entry.ResourceID, string(entry.Action),
		entry.OccurredAt, entry.Actor, entry.Reason, entry.BatchID, entry.RetentionExpires)
	if err != nil {
		a.log.WarnContext(ctx, "lifecycle audit insert failed",
			"resource_type", entry.ResourceType, "resource_id", entry.ResourceID,
			"action", string(entry.Action), "error", err)
		return nil
	}
	return nil
}
