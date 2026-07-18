// Package executionstats aggregates per-capability outcome metrics for a
// tenant from the run traces and execution attempts Companion already
// persists. Fase 3: the metrics INFORM (console, and later the Fase 4
// learning analyzer and Nexus risk) but never auto-escalate autonomy or
// promote procedures — every transition stays human-decided (gate G4.5).
//
// Extrapolated from v1 nexus/internal/requests/execution_stats.go: the
// aggregation pattern and the -1 "no data" sentinel are kept, but the keying
// is (tenant, capability_key) — never a bare action_type across tenants.
package executionstats

import "sort"

// NoDataRate is the sentinel returned when there are no finished executions
// to rate (v1 pattern: -1 means "no data", distinct from a real 0%).
const NoDataRate = -1.0

// CapabilityStats are the aggregated outcomes for one capability in a tenant.
type CapabilityStats struct {
	CapabilityKey       string  `json:"capability_key"`
	DryRuns             int64   `json:"dry_runs"`
	DryRunsAllowed      int64   `json:"dry_runs_allowed"`
	Gates               int64   `json:"gates"`
	GatesPassed         int64   `json:"gates_passed"`
	ExecutionsSucceeded int64   `json:"executions_succeeded"`
	ExecutionsFailed    int64   `json:"executions_failed"`
	SuccessRate         float64 `json:"success_rate"`
}

// TraceRow is one grouped row from companion_run_traces.
type TraceRow struct {
	CapabilityKey  string
	Operation      string
	DryRunDecision string
	GateDecision   string
	Count          int64
}

// ExecutionRow is one grouped row from companion_execution_attempts.
type ExecutionRow struct {
	CapabilityKey string
	Status        string
	Count         int64
}

// Merge folds grouped rows into per-capability stats, sorted by capability key.
func Merge(traces []TraceRow, executions []ExecutionRow) []CapabilityStats {
	byKey := map[string]*CapabilityStats{}
	get := func(key string) *CapabilityStats {
		if key == "" {
			return nil
		}
		if existing, ok := byKey[key]; ok {
			return existing
		}
		created := &CapabilityStats{CapabilityKey: key}
		byKey[key] = created
		return created
	}

	for _, row := range traces {
		stats := get(row.CapabilityKey)
		if stats == nil {
			continue
		}
		switch row.Operation {
		case "dry_run":
			stats.DryRuns += row.Count
			if row.DryRunDecision == "allowed" {
				stats.DryRunsAllowed += row.Count
			}
		case "execution_gate":
			stats.Gates += row.Count
			if row.GateDecision == "pass" {
				stats.GatesPassed += row.Count
			}
		}
	}

	for _, row := range executions {
		stats := get(row.CapabilityKey)
		if stats == nil {
			continue
		}
		switch row.Status {
		case "succeeded":
			stats.ExecutionsSucceeded += row.Count
		case "failed":
			stats.ExecutionsFailed += row.Count
		}
	}

	out := make([]CapabilityStats, 0, len(byKey))
	for _, stats := range byKey {
		stats.SuccessRate = successRate(stats.ExecutionsSucceeded, stats.ExecutionsFailed)
		out = append(out, *stats)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityKey < out[j].CapabilityKey })
	return out
}

func successRate(succeeded, failed int64) float64 {
	total := succeeded + failed
	if total == 0 {
		return NoDataRate
	}
	return float64(succeeded) / float64(total)
}
