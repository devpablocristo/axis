package executiongate

import (
	"errors"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type Decision string

const (
	DecisionPass    Decision = "pass"
	DecisionBlocked Decision = "blocked"
)

type CheckStatus string

const (
	CheckStatusPass    CheckStatus = "pass"
	CheckStatusBlocked CheckStatus = "blocked"
)

type Check struct {
	Key    string
	Status CheckStatus
	Reason string
}

type Gate struct {
	Decision                  Decision
	Mode                      string
	WillExecute               bool
	RequiredExecutionAutonomy virployeedomain.AutonomyLevel
	VirployeeAutonomy         virployeedomain.AutonomyLevel
	Checks                    []Check
	NextStep                  string
}

type Result struct {
	Input       string
	DryRun      dryrun.Result
	Gate        Gate
	BindingHash string
	Governance  *GovernanceCheckResult
}

type GovernanceCheckInput struct {
	TenantID             string
	ProductSurface       string
	RequesterType        string
	RequesterID          string
	SupervisorUserID     string
	ActionType           string
	TargetSystem         string
	TargetResource       string
	ResourceType         string
	Reason               string
	BindingHash          string
	AuthorityBindingHash string
	ScopeRevision        int64
	PolicyRevisionHash   string
	DelegationRequired   bool
	DelegationID         string
	DelegationRevision   int64
}

type GovernanceCheckResult struct {
	CheckID              string
	Decision             string
	RiskLevel            string
	Status               string
	DecisionReason       string
	WouldRequireApproval bool
	BindingHash          string
	ApprovalID           string
	ApprovalStatus       string
	PolicySnapshotHash   string
}

type GovernanceApproval struct {
	ID                 string
	GovernanceCheckID  string
	RequesterID        string
	BindingHash        string
	Status             string
	PolicySnapshotHash string
}

type GovernanceRevalidationInput struct {
	TenantID             string
	CheckID              string
	BindingHash          string
	PolicySnapshotHash   string
	AuthorityBindingHash string
	ScopeRevision        int64
	PolicyRevisionHash   string
	DelegationID         string
	DelegationRevision   int64
}

type GovernanceRevalidationResult struct {
	Valid              bool
	Reason             string
	PolicySnapshotHash string
}

// AuthorityCheckInput is deliberately limited to stable identifiers. Policy
// text, principal display data and user content never cross the execution gate.
type AuthorityCheckInput struct {
	TenantID       string
	VirployeeID    uuid.UUID
	JobRoleID      uuid.UUID
	CapabilityKey  string
	ProductSurface string
	ResourceType   string
	ResourceID     string
	RiskClass      string
	PrincipalType  string
	PrincipalID    string
	At             time.Time
}

// AuthorityCheckResult is the metadata-only snapshot bound to governance. A
// revision change produces a different SnapshotHash and invalidates approvals.
type AuthorityCheckResult struct {
	Allowed            bool
	Reason             string
	SnapshotHash       string
	ScopeRevision      int64
	PolicyRevisionHash string
	DelegationRequired bool
	DelegationID       string
	DelegationRevision int64
}

type ConfirmedDraft struct {
	Action string
	Kind   string
	Fields []ConfirmedDraftField
}

type ConfirmedDraftField struct {
	Key   string
	Value string
}

func Evaluate(result dryrun.Result) Result {
	requiredExecutionAutonomy := requiredExecutionAutonomyFor(result.Intent)
	checks := []Check{
		intentMatchedCheck(result),
		capabilityAssignedCheck(result),
		dryRunAllowedCheck(result),
		draftReadyCheck(result),
		executionAutonomyCheck(result, requiredExecutionAutonomy),
	}
	decision := DecisionPass
	for _, check := range checks {
		if check.Status == CheckStatusBlocked {
			decision = DecisionBlocked
			break
		}
	}
	return Result{
		Input:  result.Input,
		DryRun: result,
		Gate: Gate{
			Decision:                  decision,
			Mode:                      "simulation",
			WillExecute:               false,
			RequiredExecutionAutonomy: requiredExecutionAutonomy,
			VirployeeAutonomy:         result.VirployeeAutonomy,
			Checks:                    checks,
			NextStep:                  nextStep(decision, checks),
		},
	}
}

func ApplyGovernance(result Result, governance GovernanceCheckResult) Result {
	check := governanceCheck(governance)
	result.Gate.Checks = append(result.Gate.Checks, check)
	if check.Status == CheckStatusBlocked {
		result.Gate.Decision = DecisionBlocked
		result.Gate.NextStep = nextStep(result.Gate.Decision, result.Gate.Checks)
	}
	return result
}

func ApplyGovernanceUnavailable(result Result) Result {
	result.Gate.Checks = append(result.Gate.Checks, Check{
		Key:    "governance_check",
		Status: CheckStatusBlocked,
		Reason: "governance check is unavailable",
	})
	result.Gate.Decision = DecisionBlocked
	result.Gate.NextStep = nextStep(result.Gate.Decision, result.Gate.Checks)
	return result
}

func ApplyAuthority(result Result, authority AuthorityCheckResult) Result {
	status := CheckStatusPass
	reason := strings.TrimSpace(authority.Reason)
	if reason == "" {
		reason = "professional authority permits this capability"
	}
	if !authority.Allowed {
		status = CheckStatusBlocked
		if reason == "" {
			reason = "professional authority denied this capability"
		}
	}
	result.Gate.Checks = append(result.Gate.Checks, Check{Key: "professional_authority", Status: status, Reason: reason})
	if status == CheckStatusBlocked {
		result.Gate.Decision = DecisionBlocked
		result.Gate.NextStep = nextStep(result.Gate.Decision, result.Gate.Checks)
	}
	return result
}

func ApplyAuthorityUnavailable(result Result) Result {
	result.Gate.Checks = append(result.Gate.Checks, Check{
		Key: "professional_authority", Status: CheckStatusBlocked,
		Reason: "professional authority evaluation is unavailable",
	})
	result.Gate.Decision = DecisionBlocked
	result.Gate.NextStep = nextStep(result.Gate.Decision, result.Gate.Checks)
	return result
}

// ApplyProfessionalScope adds the topic/scope policy decision to the action
// gate. An out-of-scope or unavailable decision is always blocking; escalation
// is a conversation outcome, never permission to execute a side effect.
func ApplyProfessionalScope(result Result, scope ConversationScopeResult) Result {
	status := CheckStatusPass
	reason := strings.TrimSpace(scope.Reason)
	if reason == "" {
		reason = "action is within professional scope"
	}
	if !scope.Allowed {
		status = CheckStatusBlocked
	}
	result.Gate.Checks = append(result.Gate.Checks, Check{Key: "professional_scope", Status: status, Reason: reason})
	if status == CheckStatusBlocked {
		result.Gate.Decision = DecisionBlocked
		result.Gate.NextStep = nextStep(result.Gate.Decision, result.Gate.Checks)
	}
	return result
}

func governanceCheck(governance GovernanceCheckResult) Check {
	reason := governance.DecisionReason
	if reason == "" {
		reason = "governance decision is " + governance.Decision
	}
	if governance.RiskLevel != "" {
		reason = reason + " (risk " + governance.RiskLevel + ")"
	}
	switch governance.Decision {
	case "allow":
		return Check{Key: "governance_check", Status: CheckStatusPass, Reason: reason}
	case "deny", "require_approval":
		return Check{Key: "governance_check", Status: CheckStatusBlocked, Reason: reason}
	default:
		return Check{Key: "governance_check", Status: CheckStatusBlocked, Reason: "unknown governance decision"}
	}
}

func requiredExecutionAutonomyFor(intent dryrun.Intent) virployeedomain.AutonomyLevel {
	if !intent.Matched {
		return virployeedomain.AutonomyA0
	}
	if intent.CapabilityKey == "calendar.events.read" {
		return virployeedomain.AutonomyA1
	}
	if intent.CapabilityKey == "calendar.events.create" ||
		intent.CapabilityKey == "calendar.events.update" ||
		intent.CapabilityKey == "calendar.events.delete" {
		return virployeedomain.AutonomyA3
	}
	return virployeedomain.AutonomyA5
}

func intentMatchedCheck(result dryrun.Result) Check {
	if result.Intent.Matched {
		return Check{Key: "intent_matched", Status: CheckStatusPass, Reason: result.Intent.CapabilityKey + " detected"}
	}
	return Check{Key: "intent_matched", Status: CheckStatusBlocked, Reason: "no executable intent was detected"}
}

func capabilityAssignedCheck(result dryrun.Result) Check {
	if result.RequiredCapability != nil && result.RequiredCapability.Matched {
		return Check{Key: "capability_assigned", Status: CheckStatusPass, Reason: "capability is assigned"}
	}
	if result.RequiredCapability != nil {
		return Check{Key: "capability_assigned", Status: CheckStatusBlocked, Reason: "required capability is not assigned"}
	}
	return Check{Key: "capability_assigned", Status: CheckStatusBlocked, Reason: "no required capability because no executable intent was detected"}
}

func dryRunAllowedCheck(result dryrun.Result) Check {
	if result.Decision == dryrun.DecisionAllowed {
		return Check{Key: "dry_run_allowed", Status: CheckStatusPass, Reason: result.Reason}
	}
	return Check{Key: "dry_run_allowed", Status: CheckStatusBlocked, Reason: result.Reason}
}

func draftReadyCheck(result dryrun.Result) Check {
	if result.Draft.Status == dryrun.DraftStatusReady {
		return Check{Key: "draft_ready", Status: CheckStatusPass, Reason: "draft has required fields"}
	}
	return Check{Key: "draft_ready", Status: CheckStatusBlocked, Reason: "draft status is " + string(result.Draft.Status)}
}

func executionAutonomyCheck(result dryrun.Result, required virployeedomain.AutonomyLevel) Check {
	if result.VirployeeAutonomy.Allows(required) {
		return Check{Key: "execution_autonomy", Status: CheckStatusPass, Reason: "virployee autonomy allows execution autonomy " + string(required)}
	}
	return Check{Key: "execution_autonomy", Status: CheckStatusBlocked, Reason: "execution requires " + string(required)}
}

func nextStep(decision Decision, checks []Check) string {
	if decision == DecisionPass {
		return "would pass the execution gate, but no execution will be performed"
	}
	for _, check := range checks {
		if check.Status != CheckStatusBlocked {
			continue
		}
		switch check.Key {
		case "intent_matched":
			return "would ask for a supported action before execution"
		case "capability_assigned", "dry_run_allowed":
			return "would stop before execution"
		case "draft_ready":
			return "would ask for missing draft fields before execution"
		case "execution_autonomy":
			return "would require higher autonomy or human approval before execution"
		case "governance_check":
			return "would require governance clearance before execution"
		case "professional_authority":
			return "would stop until professional authority is valid"
		}
	}
	return "would stop before execution"
}

func ApplyConfirmedDraft(result dryrun.Result, confirmed ConfirmedDraft) (dryrun.Result, error) {
	confirmed.Action = strings.TrimSpace(confirmed.Action)
	confirmed.Kind = strings.TrimSpace(confirmed.Kind)
	if confirmed.Action == "" {
		return dryrun.Result{}, errors.New("confirmed_draft.action is required")
	}
	if !result.Intent.Matched {
		return dryrun.Result{}, errors.New("confirmed_draft cannot be used when no intent was detected")
	}
	if confirmed.Action != result.Intent.CapabilityKey {
		return dryrun.Result{}, errors.New("confirmed_draft.action must match the detected intent")
	}
	if confirmed.Action != "calendar.events.create" {
		return dryrun.Result{}, errors.New("confirmed_draft is only supported for calendar.events.create")
	}

	fields := confirmedFieldMap(confirmed.Fields)
	missing := []dryrun.DraftMissingField{}
	for _, required := range calendarEventCreateRequiredFields() {
		if strings.TrimSpace(fields[required.key]) == "" {
			missing = append(missing, dryrun.DraftMissingField{
				Key:    required.key,
				Label:  required.label,
				Reason: required.reason,
			})
		}
	}
	status := dryrun.DraftStatusReady
	if len(missing) > 0 {
		status = dryrun.DraftStatusNeedsInput
	}
	result.Draft = dryrun.Draft{
		Status:        status,
		Action:        "calendar.events.create",
		Kind:          "calendar_event",
		Summary:       "Prepare a calendar event draft",
		Fields:        confirmedDraftFields(fields),
		MissingFields: missing,
		Notes:         []string{"No external action will be executed."},
	}
	return result, nil
}

func confirmedFieldMap(fields []ConfirmedDraftField) map[string]string {
	out := map[string]string{}
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(field.Value)
	}
	return out
}

func confirmedDraftFields(fields map[string]string) []dryrun.DraftField {
	out := []dryrun.DraftField{}
	for _, required := range calendarEventCreateRequiredFields() {
		value := strings.TrimSpace(fields[required.key])
		if value == "" {
			continue
		}
		out = append(out, dryrun.DraftField{
			Key:    required.key,
			Label:  required.label,
			Value:  value,
			Source: "confirmed",
		})
	}
	return out
}

func calendarEventCreateRequiredFields() []struct {
	key    string
	label  string
	reason string
} {
	return []struct {
		key    string
		label  string
		reason string
	}{
		{key: "title", label: "Title", reason: "Title is required before preparing the event."},
		{key: "date", label: "Date", reason: "Date in YYYY-MM-DD format is required before preparing the event."},
		{key: "time", label: "Time", reason: "Time is required before preparing the event."},
		{key: "timezone", label: "Timezone", reason: "An IANA timezone is required before preparing the event."},
		{key: "duration_minutes", label: "Duration", reason: "Duration in minutes is required before preparing the event."},
		{key: "attendees", label: "Attendees", reason: "At least one attendee is required for a meeting."},
	}
}
