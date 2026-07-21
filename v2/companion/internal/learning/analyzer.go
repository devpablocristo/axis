package learning

import (
	"fmt"
	"strings"
	"time"
)

// Candidate is a (virployee, capability) pair whose successful executions
// reached the scan threshold. Extrapolated from v1's learning analyzer: group
// outcomes, apply a threshold, propose — but always scoped to ONE tenant
// (never v1's cross-org scan) and with a configurable threshold.
type Candidate struct {
	VirployeeID   string
	CapabilityKey string
	Succeeded     int64
	FirstAt       time.Time
	LastAt        time.Time
}

// ShouldPropose decides whether a qualifying candidate gets a new proposal
// given the latest existing proposal for the pair (nil = none). Pending and
// accepted block re-proposing (the unique index backs the pending case);
// after a dismissal the analyzer only insists when there is NEW evidence —
// more successful executions than the typed SucceededWatermark recorded when
// that proposal was filed (never recovered from the free-form evidence JSON,
// so a proposal ingested without analyzer evidence cannot resurrect a
// dismissal).
func ShouldPropose(latest *Proposal, succeeded int64) bool {
	if latest == nil {
		return true
	}
	switch latest.Status {
	case StatusPending, StatusAccepted:
		return false
	case StatusDismissed:
		return succeeded > latest.SucceededWatermark
	default:
		return false
	}
}

// Distill turns a candidate into proposal title + content with a
// deterministic template. It deliberately references the governed pipeline and
// the capability key only — never draft values (titles, dates, attendees), so
// no PII or payload data can leak into a proposal. The optional LLM enricher
// (PR5) may rewrite this text, but always through Ingest, never into memory.
func Distill(candidate Candidate) (title, content string) {
	title = "Learned procedure: " + candidate.CapabilityKey
	var b strings.Builder
	fmt.Fprintf(&b, "Distilled from %d successful executions of %s between %s and %s.\n\n",
		candidate.Succeeded, candidate.CapabilityKey,
		candidate.FirstAt.UTC().Format("2006-01-02"), candidate.LastAt.UTC().Format("2006-01-02"))
	b.WriteString("Observed procedure:\n")
	b.WriteString("1. Interpret the request and confirm it maps to " + candidate.CapabilityKey + " in the dry run.\n")
	b.WriteString("2. Complete any required draft fields and confirm the draft when the action prepares one.\n")
	b.WriteString("3. Pass the execution gate and whatever governance decision it requires before acting.\n")
	b.WriteString("4. Execute the action once it is cleared and verify the result is reported to Nexus.\n")
	return title, b.String()
}

// BuildEvidence snapshots what the analyzer saw at proposal time, for the
// human reviewer's display. The re-proposal watermark is NOT read from here —
// it travels typed as CreateInput.SucceededWatermark.
func BuildEvidence(candidate Candidate) map[string]any {
	return map[string]any{
		"executions_succeeded": candidate.Succeeded,
		"first_execution_at":   candidate.FirstAt.UTC().Format(time.RFC3339),
		"last_execution_at":    candidate.LastAt.UTC().Format(time.RFC3339),
	}
}
