package preparedactions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
)

const (
	SchemaVersion       = "calendar.event.create.v1"
	DeleteSchemaVersion = "calendar.event.delete.v1"
	ActionCreate        = "calendar.events.create"
	ActionDelete        = "calendar.events.delete"
)

type Action struct {
	SchemaVersion   string   `json:"schema_version"`
	Action          string   `json:"action"`
	Title           string   `json:"title"`
	Date            string   `json:"date"`
	Time            string   `json:"time"`
	Timezone        string   `json:"timezone"`
	DurationMinutes int      `json:"duration_minutes"`
	Attendees       []string `json:"attendees"`
	// Principal binds the approved action to the exact person or organization
	// on whose behalf it may run. Omitempty preserves legacy payload hashes when
	// no delegation is required.
	PrincipalType string `json:"principal_type,omitempty"`
	PrincipalID   string `json:"principal_id,omitempty"`
	// AssistContext is populated only when the execution gate was explicitly
	// tied to a completed Assist run. The server derives every field from the
	// tenant-scoped run; callers cannot provide a context hash. Because this is
	// part of the prepared-action payload, Nexus approves the exact Assist,
	// subject/case/assignment, source set, and conversation-policy snapshot.
	AssistContext *AssistContextBinding `json:"assist_context,omitempty"`
	// ProfessionalScope pins the action-shaped topic check independently from
	// the Assist conversation. It protects legacy execution-gate calls too and
	// is re-evaluated before an executor can perform side effects.
	ProfessionalScope *ProfessionalScopeBinding `json:"professional_scope,omitempty"`
	// MCPContext binds an action initiated through MCP to the exact tenant,
	// subject/case assignment, capability manifest, policy revision and caller
	// idempotency key. Only hashes and stable identifiers are stored here.
	MCPContext *MCPContextBinding `json:"mcp_context,omitempty"`
	// EventID identifies the target of a delete (compensation) action. It is
	// omitempty so create actions serialize exactly as before — their payload
	// hash, and therefore their binding hash, is unchanged.
	EventID string `json:"event_id,omitempty"`
}

// AssistContextBinding contains metadata-only provenance. Source bodies,
// prompts, signed URLs, and patient display data must never be stored here.
type AssistContextBinding struct {
	RunID                   string `json:"run_id"`
	ContextHash             string `json:"context_hash"`
	SubjectID               string `json:"subject_id,omitempty"`
	CaseID                  string `json:"case_id,omitempty"`
	AssignmentID            string `json:"assignment_id,omitempty"`
	AssignmentVersion       int64  `json:"assignment_version,omitempty"`
	GroundingMode           string `json:"grounding_mode"`
	SourcesHash             string `json:"sources_hash"`
	MemoryContextHash       string `json:"memory_context_hash,omitempty"`
	JobRoleSnapshotHash     string `json:"job_role_snapshot_hash"`
	PolicySnapshotHash      string `json:"policy_snapshot_hash,omitempty"`
	SourceAuthorizationHash string `json:"source_authorization_hash,omitempty"`
}

type ProfessionalScopeBinding struct {
	QueryHash    string `json:"query_hash"`
	SnapshotHash string `json:"snapshot_hash"`
}

type MCPContextBinding struct {
	TenantID          string `json:"tenant_id"`
	ActorID           string `json:"actor_id"`
	VirployeeID       string `json:"virployee_id"`
	SubjectID         string `json:"subject_id"`
	CaseID            string `json:"case_id,omitempty"`
	AssignmentID      string `json:"assignment_id"`
	AssignmentVersion int64  `json:"assignment_version"`
	CapabilityKey     string `json:"capability_key"`
	CapabilityVersion string `json:"capability_version"`
	ManifestHash      string `json:"manifest_hash"`
	PolicyVersion     int64  `json:"policy_version"`
	AuthorityHash     string `json:"authority_hash"`
	ContextHash       string `json:"context_hash"`
	PayloadHash       string `json:"payload_hash"`
	IdempotencyHash   string `json:"idempotency_hash"`
}

// FromReadyDraft builds a prepared action for the executable actions (create and
// delete). Non-executable or not-yet-supported actions (read, update, generic)
// return (nil, nil): they never produce a prepared action, a payload, or a
// binding. A delete carries a different payload than a create, so it necessarily
// gets its own binding hash (G3.5) — a create's approval can never authorize it.
func FromReadyDraft(draft dryrun.Draft) (*Action, error) {
	switch strings.TrimSpace(draft.Action) {
	case ActionCreate:
		action, err := FromDraft(draft)
		if err != nil {
			return nil, err
		}
		return &action, nil
	case ActionDelete:
		action, err := FromDeleteDraft(draft)
		if err != nil {
			return nil, err
		}
		return &action, nil
	default:
		return nil, nil
	}
}

// FromDeleteDraft builds a compensating delete action from a ready delete draft.
// The event to delete is identified by the confirmed "event_reference" field.
func FromDeleteDraft(draft dryrun.Draft) (Action, error) {
	if strings.TrimSpace(draft.Action) != ActionDelete {
		return Action{}, fmt.Errorf("delete prepared action is only supported for %s", ActionDelete)
	}
	fields := draftFieldMap(draft)
	eventID := fields["event_reference"]
	if eventID == "" {
		return Action{}, fmt.Errorf("event_reference is required")
	}
	return Action{
		SchemaVersion: DeleteSchemaVersion,
		Action:        ActionDelete,
		EventID:       eventID,
	}, nil
}

func draftFieldMap(draft dryrun.Draft) map[string]string {
	fields := make(map[string]string, len(draft.Fields))
	for _, field := range draft.Fields {
		fields[strings.TrimSpace(field.Key)] = strings.TrimSpace(field.Value)
	}
	return fields
}

func FromDraft(draft dryrun.Draft) (Action, error) {
	if strings.TrimSpace(draft.Action) != ActionCreate {
		return Action{}, fmt.Errorf("prepared action is only supported for %s", ActionCreate)
	}
	fields := draftFieldMap(draft)
	duration := 60
	if value := fields["duration_minutes"]; value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return Action{}, fmt.Errorf("duration_minutes must be a number")
		}
		duration = parsed
	}
	action := Action{
		SchemaVersion:   SchemaVersion,
		Action:          ActionCreate,
		Title:           fields["title"],
		Date:            fields["date"],
		Time:            fields["time"],
		Timezone:        fields["timezone"],
		DurationMinutes: duration,
		Attendees:       normalizeAttendees(fields["attendees"]),
	}
	if action.Title == "" {
		return Action{}, fmt.Errorf("title is required")
	}
	if _, err := time.Parse("2006-01-02", action.Date); err != nil {
		return Action{}, fmt.Errorf("date must use YYYY-MM-DD")
	}
	if _, err := time.Parse("15:04", action.Time); err != nil {
		return Action{}, fmt.Errorf("time must use HH:MM in 24-hour format")
	}
	if action.Timezone == "" {
		return Action{}, fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(action.Timezone); err != nil {
		return Action{}, fmt.Errorf("timezone must be a valid IANA timezone")
	}
	if action.DurationMinutes < 5 || action.DurationMinutes > 1440 {
		return Action{}, fmt.Errorf("duration_minutes must be between 5 and 1440")
	}
	if len(action.Attendees) == 0 {
		return Action{}, fmt.Errorf("at least one valid attendee is required")
	}
	return action, nil
}

func (a Action) StartsAt() (time.Time, error) {
	location, err := time.LoadLocation(a.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation("2006-01-02 15:04", a.Date+" "+a.Time, location)
}

func (a Action) PayloadHash() (string, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeAttendees(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		email := strings.ToLower(strings.TrimSpace(part))
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email {
			continue
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	sort.Strings(out)
	return out
}
