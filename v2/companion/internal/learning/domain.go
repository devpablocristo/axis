// Package learning implements Fase 4's procedural-learning loop: successful
// executions are distilled into procedure PROPOSALS that wait in an inbox for
// human review. A proposal only becomes memory through an explicit human
// Accept (never automatically) — the same analyzer→proposer→human-accept
// pattern v1 proved for governance policies, re-keyed per tenant.
package learning

import (
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	StatusPending   = "pending"
	StatusAccepted  = "accepted"
	StatusDismissed = "dismissed"

	ProposedByAnalyzer = "analyzer"
	ProposedByLLM      = "llm"
)

// Proposal is a distilled procedure candidate awaiting human review.
type Proposal struct {
	ID             uuid.UUID      `json:"id"`
	TenantID       string         `json:"tenant_id"`
	VirployeeID    uuid.UUID      `json:"virployee_id"`
	CapabilityKey  string         `json:"capability_key"`
	Title          string         `json:"title"`
	Content        string         `json:"content"`
	ContentHash    string         `json:"content_hash"`
	Evidence       map[string]any `json:"evidence"`
	SourceTraceIDs []string       `json:"source_trace_ids"`
	Status         string         `json:"status"`
	ProposedBy     string         `json:"proposed_by"`
	// SucceededWatermark is the successful-execution count observed when the
	// proposal was filed; the analyzer's dismissed-re-proposal rule compares
	// against it (typed here, never recovered from the evidence JSON).
	SucceededWatermark int64 `json:"succeeded_watermark"`
	// DecidedBy/DecidedAt/MemoryID record the human decision (PR3). MemoryID is
	// the procedure memory installed on accept (nil for pending/dismissed, or
	// when an equivalent memory already existed).
	DecidedBy string     `json:"decided_by,omitempty"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	MemoryID  *uuid.UUID `json:"memory_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CreateInput struct {
	VirployeeID        uuid.UUID
	CapabilityKey      string
	Title              string
	Content            string
	Evidence           map[string]any
	SourceTraceIDs     []string
	ProposedBy         string
	SucceededWatermark int64
}

type NormalizedCreateInput struct {
	VirployeeID        uuid.UUID
	CapabilityKey      string
	Title              string
	Content            string
	ContentHash        string
	Evidence           map[string]any
	SourceTraceIDs     []string
	ProposedBy         string
	SucceededWatermark int64
}

var capabilityKeyPattern = regexp.MustCompile(`^[a-zñ]+\.[a-zñ]+\.[a-zñ]+$`)

// NormalizeCreateInput applies the same size and shape limits the memories
// module enforces, so an accepted proposal is guaranteed to be installable as
// a procedure memory later.
func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	title := strings.TrimSpace(in.Title)
	content := strings.TrimSpace(in.Content)
	key := strings.TrimSpace(strings.ToLower(in.CapabilityKey))
	proposedBy := strings.TrimSpace(strings.ToLower(in.ProposedBy))
	if proposedBy == "" {
		proposedBy = ProposedByAnalyzer
	}
	if in.VirployeeID == uuid.Nil {
		return NormalizedCreateInput{}, domainerr.Validation("virployee_id is required")
	}
	if !capabilityKeyPattern.MatchString(key) {
		return NormalizedCreateInput{}, domainerr.Validation("capability_key must use domain.resource.action with lowercase letters only")
	}
	if title == "" || len([]rune(title)) > 200 {
		return NormalizedCreateInput{}, domainerr.Validation("title is required and must not exceed 200 characters")
	}
	if content == "" || len([]rune(content)) > 20000 {
		return NormalizedCreateInput{}, domainerr.Validation("content is required and must not exceed 20000 characters")
	}
	if proposedBy != ProposedByAnalyzer && proposedBy != ProposedByLLM {
		return NormalizedCreateInput{}, domainerr.Validation("proposed_by must be analyzer or llm")
	}
	evidence := in.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	sources := make([]string, 0, len(in.SourceTraceIDs))
	for _, id := range in.SourceTraceIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			sources = append(sources, trimmed)
		}
	}
	watermark := in.SucceededWatermark
	if watermark < 0 {
		watermark = 0
	}
	return NormalizedCreateInput{
		VirployeeID:        in.VirployeeID,
		CapabilityKey:      key,
		Title:              title,
		Content:            content,
		ContentHash:        memories.ContentHash(content),
		Evidence:           evidence,
		SourceTraceIDs:     sources,
		ProposedBy:         proposedBy,
		SucceededWatermark: watermark,
	}, nil
}

// NormalizeStatusFilter validates a list filter; empty defaults to pending —
// the inbox view is the primary consumer.
func NormalizeStatusFilter(raw string) (string, error) {
	status := strings.TrimSpace(strings.ToLower(raw))
	if status == "" {
		return StatusPending, nil
	}
	switch status {
	case StatusPending, StatusAccepted, StatusDismissed:
		return status, nil
	default:
		return "", domainerr.Validation("status must be pending, accepted, or dismissed")
	}
}
