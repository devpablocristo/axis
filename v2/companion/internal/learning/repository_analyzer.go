package learning

import (
	"context"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// Candidates returns the (virployee, capability) pairs of this tenant whose
// SUCCESSFUL executions reached the threshold. Counting uses the same
// authoritative source as the F3 stats: execution attempts joined to their
// prepared actions, always tenant-scoped.
//
// KEEP IN SYNC: the success predicate (attempts JOIN prepared_actions,
// status='succeeded') must match executionstats.Repository.ExecutionRows —
// if success semantics change there, change them here too or the analyzer's
// counts will silently disagree with the stats console.
func (r *Repository) Candidates(ctx context.Context, tenantID string, minExecutions int) ([]Candidate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT pa.virployee_id::text, pa.capability_key, count(*),
			min(COALESCE(ea.completed_at, ea.started_at)), max(COALESCE(ea.completed_at, ea.started_at))
		FROM companion_execution_attempts ea
		JOIN companion_prepared_actions pa
			ON pa.id = ea.prepared_action_id AND pa.tenant_id = ea.tenant_id
		WHERE ea.tenant_id = $1 AND ea.status = 'succeeded'
		GROUP BY 1, 2
		HAVING count(*) >= $2
		ORDER BY 2, 1
	`, tenantID, minExecutions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Candidate{}
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.VirployeeID, &c.CapabilityKey, &c.Succeeded, &c.FirstAt, &c.LastAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LatestForPair returns the most recent proposal for the pair, or nil.
func (r *Repository) LatestForPair(ctx context.Context, tenantID string, virployeeID uuid.UUID, capabilityKey string) (*Proposal, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+proposalColumns+`
		FROM companion_learning_proposals
		WHERE tenant_id = $1 AND virployee_id = $2::uuid AND capability_key = $3
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID, virployeeID.String(), capabilityKey)
	proposal, err := scanProposal(row)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &proposal, nil
}

// SuccessfulExecutionTraceIDs returns provenance pointers (run trace ids of
// operation=execution with a succeeded result) for the pair, newest first.
func (r *Repository) SuccessfulExecutionTraceIDs(ctx context.Context, tenantID string, virployeeID uuid.UUID, capabilityKey string, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text
		FROM companion_run_traces
		WHERE tenant_id = $1 AND virployee_id = $2::uuid AND capability_key = $3
			AND operation = 'execution' AND execution_result->>'status' = 'succeeded'
		ORDER BY created_at DESC
		LIMIT $4
	`, tenantID, virployeeID.String(), capabilityKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
