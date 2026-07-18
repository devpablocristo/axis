package executionstats

import "testing"

func TestMergeFoldsTracesAndExecutions(t *testing.T) {
	stats := Merge(
		[]TraceRow{
			{CapabilityKey: "calendar.events.create", Operation: "dry_run", DryRunDecision: "allowed", Count: 3},
			{CapabilityKey: "calendar.events.create", Operation: "dry_run", DryRunDecision: "blocked", Count: 1},
			{CapabilityKey: "calendar.events.create", Operation: "execution_gate", GateDecision: "pass", Count: 2},
			{CapabilityKey: "calendar.events.create", Operation: "execution_gate", GateDecision: "blocked", Count: 2},
			// Execution-type trace operations do not double-count executions.
			{CapabilityKey: "calendar.events.create", Operation: "execution", Count: 5},
			{CapabilityKey: "calendar.events.read", Operation: "dry_run", DryRunDecision: "allowed", Count: 7},
		},
		[]ExecutionRow{
			{CapabilityKey: "calendar.events.create", Status: "succeeded", Count: 3},
			{CapabilityKey: "calendar.events.create", Status: "failed", Count: 1},
			{CapabilityKey: "calendar.events.create", Status: "running", Count: 1}, // in-flight: ignored
		},
	)

	if len(stats) != 2 {
		t.Fatalf("expected 2 capabilities, got %+v", stats)
	}
	create := stats[0]
	if create.CapabilityKey != "calendar.events.create" {
		t.Fatalf("expected sorted keys with create first, got %+v", stats)
	}
	if create.DryRuns != 4 || create.DryRunsAllowed != 3 {
		t.Fatalf("unexpected dry-run counts: %+v", create)
	}
	if create.Gates != 4 || create.GatesPassed != 2 {
		t.Fatalf("unexpected gate counts: %+v", create)
	}
	if create.ExecutionsSucceeded != 3 || create.ExecutionsFailed != 1 {
		t.Fatalf("unexpected execution counts: %+v", create)
	}
	if create.SuccessRate != 0.75 {
		t.Fatalf("expected success rate 0.75, got %v", create.SuccessRate)
	}

	read := stats[1]
	if read.CapabilityKey != "calendar.events.read" || read.DryRuns != 7 {
		t.Fatalf("unexpected read stats: %+v", read)
	}
	if read.SuccessRate != NoDataRate {
		t.Fatalf("no executions must yield the no-data sentinel, got %v", read.SuccessRate)
	}
}

func TestMergeIgnoresEmptyCapabilityKey(t *testing.T) {
	stats := Merge(
		[]TraceRow{{CapabilityKey: "", Operation: "dry_run", DryRunDecision: "allowed", Count: 9}},
		[]ExecutionRow{{CapabilityKey: "", Status: "succeeded", Count: 9}},
	)
	if len(stats) != 0 {
		t.Fatalf("rows without a capability key must be ignored, got %+v", stats)
	}
}

func TestSuccessRateSentinelDistinguishesNoDataFromZero(t *testing.T) {
	if got := successRate(0, 0); got != NoDataRate {
		t.Fatalf("expected sentinel for no data, got %v", got)
	}
	if got := successRate(0, 4); got != 0 {
		t.Fatalf("expected real 0%% for all-failed, got %v", got)
	}
}
