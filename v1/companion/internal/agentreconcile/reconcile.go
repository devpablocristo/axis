package agentreconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/devpablocristo/companion/internal/agentprofiles"
)

type RuntimeReference struct {
	OrgID          string `json:"org_id"`
	ProductSurface string `json:"product_surface"`
	AgentID        string `json:"agent_id"`
	Runs           int64  `json:"runs"`
	Observability  int64  `json:"observability"`
	Costs          int64  `json:"costs"`
}

type NoAgentRunContext struct {
	OrgID          string `json:"org_id"`
	ProductSurface string `json:"product_surface"`
	Runs           int64  `json:"runs"`
}

type Report struct {
	Apply                  bool                `json:"apply"`
	CompanionAgents        int                 `json:"companion_agents"`
	RuntimeAgentReferences int                 `json:"runtime_agent_references"`
	InferredAgents         int                 `json:"inferred_agents"`
	Created                int                 `json:"created"`
	NoAgentRuns            []NoAgentRunContext `json:"no_agent_runs,omitempty"`
	Rows                   []RuntimeReference  `json:"rows,omitempty"`
}

type sourceKey struct {
	OrgID          string
	ProductSurface string
	AgentID        string
}

func Run(ctx context.Context, db *sql.DB, apply bool) (Report, error) {
	existing, err := loadExistingAgents(ctx, db)
	if err != nil {
		return Report{}, err
	}
	refs, err := loadRuntimeAgentReferences(ctx, db)
	if err != nil {
		return Report{}, err
	}
	noAgentRuns, err := loadNoAgentRuns(ctx, db)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Apply:                  apply,
		CompanionAgents:        len(existing),
		RuntimeAgentReferences: len(refs),
		NoAgentRuns:            noAgentRuns,
		Rows:                   []RuntimeReference{},
	}
	for _, ref := range refs {
		if _, ok := existing[sourceKey{OrgID: ref.OrgID, ProductSurface: ref.ProductSurface, AgentID: ref.AgentID}]; ok {
			continue
		}
		report.Rows = append(report.Rows, ref)
		report.InferredAgents++
		if !apply {
			continue
		}
		if err := insertInferredAgent(ctx, db, ref); err != nil {
			return report, err
		}
		report.Created++
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		a, b := report.Rows[i], report.Rows[j]
		return a.ProductSurface+"/"+a.OrgID+"/"+a.AgentID < b.ProductSurface+"/"+b.OrgID+"/"+b.AgentID
	})
	return report, nil
}

func loadExistingAgents(ctx context.Context, db *sql.DB) (map[sourceKey]struct{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT org_id, product_surface, agent_id
		FROM companion_agents
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[sourceKey]struct{}{}
	for rows.Next() {
		var key sourceKey
		if err := rows.Scan(&key.OrgID, &key.ProductSurface, &key.AgentID); err != nil {
			return nil, err
		}
		out[key] = struct{}{}
	}
	return out, rows.Err()
}

func loadRuntimeAgentReferences(ctx context.Context, db *sql.DB) ([]RuntimeReference, error) {
	rows, err := db.QueryContext(ctx, `
		WITH used_agents AS (
			SELECT org_id, product_surface, identity_chain_json->>'agent_id' AS agent_id,
			       count(*)::bigint AS runs, 0::bigint AS observability, 0::bigint AS costs
			FROM companion_run_traces
			WHERE COALESCE(identity_chain_json->>'agent_id', '') <> ''
			GROUP BY org_id, product_surface, identity_chain_json->>'agent_id'
			UNION ALL
			SELECT org_id, product_surface, agent_id,
			       0::bigint AS runs, count(*)::bigint AS observability, 0::bigint AS costs
			FROM companion_observability_events
			WHERE COALESCE(agent_id, '') <> ''
			GROUP BY org_id, product_surface, agent_id
			UNION ALL
			SELECT org_id, product_surface, agent_id,
			       0::bigint AS runs, 0::bigint AS observability, count(*)::bigint AS costs
			FROM companion_cost_events
			WHERE COALESCE(agent_id, '') <> ''
			GROUP BY org_id, product_surface, agent_id
		)
		SELECT org_id, product_surface, agent_id,
		       sum(runs)::bigint, sum(observability)::bigint, sum(costs)::bigint
		FROM used_agents
		GROUP BY org_id, product_surface, agent_id
		ORDER BY product_surface, org_id, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuntimeReference{}
	for rows.Next() {
		var ref RuntimeReference
		if err := rows.Scan(&ref.OrgID, &ref.ProductSurface, &ref.AgentID, &ref.Runs, &ref.Observability, &ref.Costs); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func loadNoAgentRuns(ctx context.Context, db *sql.DB) ([]NoAgentRunContext, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT org_id, product_surface, count(*)::bigint
		FROM companion_run_traces
		WHERE COALESCE(identity_chain_json->>'agent_id', '') = ''
		GROUP BY org_id, product_surface
		ORDER BY product_surface, org_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NoAgentRunContext{}
	for rows.Next() {
		var row NoAgentRunContext
		if err := rows.Scan(&row.OrgID, &row.ProductSurface, &row.Runs); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func insertInferredAgent(ctx context.Context, db *sql.DB, ref RuntimeReference) error {
	metadata, err := json.Marshal(map[string]any{
		"profile_missing": true,
		"source":          "runtime_reconcile",
		"usage": map[string]any{
			"runs":          ref.Runs,
			"observability": ref.Observability,
			"costs":         ref.Costs,
		},
	})
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO companion_agents (
			org_id, product_surface, agent_id, display_name, role, profile_id, status,
			lifecycle_status, origin_kind, review_status, max_autonomy, metadata_json,
			created_by, created_at, updated_at
		)
		VALUES ($1, $2, $3, $3, $4, $5, 'disabled',
		        'archived', 'runtime_inferred', 'needs_review', 'A1', $6::jsonb,
		        'agent-reconcile', now(), now())
		ON CONFLICT (org_id, product_surface, agent_id) DO NOTHING
	`, strings.TrimSpace(ref.OrgID), defaultSurface(ref.ProductSurface), strings.TrimSpace(ref.AgentID),
		"Agente inferido desde runtime histórico.", agentprofiles.UnprofiledProfileID, string(metadata))
	if err != nil {
		return fmt.Errorf("insert inferred agent: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read inferred agent insert count: %w", err)
	}
	if affected > 1 {
		return fmt.Errorf("unexpected inferred agent insert count")
	}
	return nil
}

func defaultSurface(surface string) string {
	surface = strings.TrimSpace(surface)
	if surface == "" {
		return "companion"
	}
	return surface
}
