package memories

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Memory struct {
	ID               uuid.UUID  `json:"id"`
	VirployeeID      uuid.UUID  `json:"virployee_id"`
	ScopeType        string     `json:"scope_type"`
	SubjectID        string     `json:"subject_id,omitempty"`
	CaseID           *uuid.UUID `json:"case_id,omitempty"`
	Title            string     `json:"title"`
	Type             string     `json:"type"`
	Content          string     `json:"content,omitempty"`
	Preview          string     `json:"preview,omitempty"`
	Sensitivity      string     `json:"sensitivity"`
	Provenance       string     `json:"provenance"`
	ActorID          string     `json:"actor_id"`
	SourceReference  string     `json:"source_reference,omitempty"`
	ContentHash      string     `json:"content_hash"`
	Version          int        `json:"version"`
	State            string     `json:"state"`
	TrustScore       float64    `json:"trust_score"`
	ReviewState      string     `json:"review_state"`
	ReviewReason     string     `json:"review_reason,omitempty"`
	PoisoningFlags   []string   `json:"poisoning_flags,omitempty"`
	PIIFlags         []string   `json:"pii_flags,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	DecayAt          *time.Time `json:"decay_at,omitempty"`
	LastRecalledAt   *time.Time `json:"last_recalled_at,omitempty"`
	RecallCount      int64      `json:"recall_count"`
	ReviewedBy       string     `json:"reviewed_by,omitempty"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	EmbeddingModel   string     `json:"embedding_model,omitempty"`
	EmbeddingVersion string     `json:"embedding_version,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Scope struct {
	Type      string     `json:"type"`
	SubjectID string     `json:"subject_id,omitempty"`
	CaseID    *uuid.UUID `json:"case_id,omitempty"`
}

type CreateInput struct {
	Title, Type, Content, Sensitivity, Provenance, ActorID, SourceReference string
	Scope                                                                   Scope
}
type UpdateInput struct {
	Title, Type, Content, Sensitivity, ActorID string
	ExpectedVersion                            int
}

type CuratedInput struct {
	CreateInput
	TrustScore     float64
	ReviewState    string
	ReviewReason   string
	PoisoningFlags []string
	PIIFlags       []string
	ExpiresAt      *time.Time
	DecayAt        *time.Time
}
type ListInput struct {
	State, Query, Cursor string
	Limit                int
	Scope                Scope
}
type Page struct {
	Items      []Memory `json:"items"`
	NextCursor string   `json:"next_cursor,omitempty"`
}
type Reference struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	Version     int        `json:"version"`
	Hash        string     `json:"hash"`
	Sensitivity string     `json:"sensitivity"`
	Score       float64    `json:"score"`
	ScopeType   string     `json:"scope_type"`
	SubjectID   string     `json:"subject_id,omitempty"`
	CaseID      *uuid.UUID `json:"case_id,omitempty"`
}
type Recalled struct {
	Memory    Memory
	Reference Reference
}

// ContextItem is the safe, approved content supplied to Runtime. It is kept
// separate from Reference so traces and evidence continue to store hashes and
// metadata only, never the memory body.
type ContextItem struct {
	Title   string `json:"title,omitempty"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content,omitempty"`
}

type IndexJobPayload struct {
	MemoryID string `json:"memory_id"`
	Version  int    `json:"version"`
}

const (
	ReviewApproved      = "approved"
	ReviewPending       = "pending"
	ReviewQuarantined   = "quarantined"
	ReviewRejected      = "rejected"
	RecallTrustFloor    = 0.60
	EmbeddingDimensions = 768
	EmbeddingVersion    = "memory-embed.v1"
	ScopeVirployee      = "virployee"
	ScopeSubject        = "subject"
	ScopeCase           = "case"
)

func normalizeCreate(in CreateInput) (CreateInput, error) {
	in.Title, in.Type, in.Content = strings.TrimSpace(in.Title), strings.TrimSpace(strings.ToLower(in.Type)), strings.TrimSpace(in.Content)
	in.Sensitivity, in.Provenance, in.ActorID = strings.TrimSpace(strings.ToLower(in.Sensitivity)), strings.TrimSpace(strings.ToLower(in.Provenance)), strings.TrimSpace(in.ActorID)
	if in.Sensitivity == "" {
		in.Sensitivity = "normal"
	}
	if in.Provenance == "" {
		in.Provenance = "human"
	}
	if in.Title == "" || len([]rune(in.Title)) > 200 {
		return in, domainerr.Validation("title is required and must not exceed 200 characters")
	}
	if in.Content == "" || len([]rune(in.Content)) > 20000 {
		return in, domainerr.Validation("content is required and must not exceed 20000 characters")
	}
	if !oneOf(in.Type, "fact", "preference", "procedure", "note") {
		return in, domainerr.Validation("type must be fact, preference, procedure, or note")
	}
	if !oneOf(in.Sensitivity, "normal", "sensitive") {
		return in, domainerr.Validation("sensitivity must be normal or sensitive")
	}
	if !oneOf(in.Provenance, "human", "system") {
		return in, domainerr.Validation("provenance must be human or system")
	}
	if in.ActorID == "" {
		return in, domainerr.Validation("actor_id is required")
	}
	scope, err := NormalizeScope(in.Scope)
	if err != nil {
		return in, err
	}
	if scope.Type == ScopeVirployee && in.Type != "procedure" {
		return in, domainerr.Validation("virployee-global memory is reserved for non-personal procedures; facts, preferences, and notes require a subject or case scope")
	}
	in.Scope = scope
	return in, nil
}

func NormalizeScope(in Scope) (Scope, error) {
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.SubjectID = strings.TrimSpace(in.SubjectID)
	if in.Type == "" {
		in.Type = ScopeVirployee
	}
	switch in.Type {
	case ScopeVirployee:
		if in.SubjectID != "" || in.CaseID != nil {
			return Scope{}, domainerr.Validation("virployee memory scope cannot include subject_id or case_id")
		}
	case ScopeSubject:
		if in.SubjectID == "" || in.CaseID != nil {
			return Scope{}, domainerr.Validation("subject memory scope requires subject_id and no case_id")
		}
		subjectID, err := uuid.Parse(in.SubjectID)
		if err != nil || subjectID == uuid.Nil {
			return Scope{}, domainerr.Validation("subject memory scope requires a valid subject_id")
		}
		in.SubjectID = subjectID.String()
	case ScopeCase:
		if in.SubjectID == "" || in.CaseID == nil || *in.CaseID == uuid.Nil {
			return Scope{}, domainerr.Validation("case memory scope requires subject_id and a valid case_id")
		}
		subjectID, err := uuid.Parse(in.SubjectID)
		if err != nil || subjectID == uuid.Nil {
			return Scope{}, domainerr.Validation("case memory scope requires a valid subject_id and case_id")
		}
		in.SubjectID = subjectID.String()
	default:
		return Scope{}, domainerr.Validation("memory scope type must be virployee, subject, or case")
	}
	return in, nil
}

func (m Memory) Scope() Scope {
	return Scope{Type: m.ScopeType, SubjectID: m.SubjectID, CaseID: m.CaseID}
}

func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}
func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}

func safeForPrompt(memory Memory) bool {
	return memory.State == "active" && memory.ReviewState == ReviewApproved &&
		memory.TrustScore >= RecallTrustFloor && memory.Sensitivity == "normal" &&
		len(memory.PoisoningFlags) == 0 && memory.ReviewReason != "conflicting_memory_requires_review" &&
		(memory.ExpiresAt == nil || memory.ExpiresAt.After(time.Now().UTC()))
}
func redact(m Memory, detail bool) Memory {
	if detail {
		m.Preview = ""
		return m
	}
	if m.Sensitivity == "sensitive" {
		m.Content = ""
		m.Preview = "[REDACTED]"
		return m
	}
	r := []rune(m.Content)
	if len(r) > 240 {
		r = r[:240]
	}
	m.Preview, m.Content = string(r), ""
	return m
}
