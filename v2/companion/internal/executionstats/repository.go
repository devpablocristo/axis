package executionstats

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository aggregates stats on read from the tables Companion already
// writes (run traces and execution attempts). No counters table: the source
// of truth cannot drift, and volumes are small at this stage. Every query is
// tenant-scoped — stats never cross tenants.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) TraceRows(ctx context.Context, tenantID string) ([]TraceRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT capability_key, operation, dry_run_decision, COALESCE(gate_decision, ''), count(*)
		FROM companion_run_traces
		WHERE tenant_id = $1 AND capability_key <> ''
		GROUP BY 1, 2, 3, 4
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TraceRow{}
	for rows.Next() {
		var row TraceRow
		if err := rows.Scan(&row.CapabilityKey, &row.Operation, &row.DryRunDecision, &row.GateDecision, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) ExecutionRows(ctx context.Context, tenantID string) ([]ExecutionRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT pa.capability_key, ea.status, count(*)
		FROM companion_execution_attempts ea
		JOIN companion_prepared_actions pa
			ON pa.id = ea.prepared_action_id AND pa.tenant_id = ea.tenant_id
		WHERE ea.tenant_id = $1
		GROUP BY 1, 2
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ExecutionRow{}
	for rows.Next() {
		var row ExecutionRow
		if err := rows.Scan(&row.CapabilityKey, &row.Status, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
