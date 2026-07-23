package executiongate

import (
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
	OrgID                string
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
	OrgID                string
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
	OrgID          string
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
	requiredExecutionAutonomy := requiredExecutionAutonomyFor(result)
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

func requiredExecutionAutonomyFor(result dryrun.Result) virployeedomain.AutonomyLevel {
	intent := result.Intent
	if !intent.Matched {
		return virployeedomain.AutonomyA0
	}
	for _, capability := range result.RuntimeContext.Capabilities {
		if (intent.CapabilityID != "" && capability.ID.String() == intent.CapabilityID) ||
			(intent.CapabilityID == "" && capability.CapabilityKey == intent.CapabilityKey) {
			if capability.Manifest.ExecutorBindingID != "" {
				return capability.RequiredAutonomy
			}
			if required, ok := legacyRequiredExecutionAutonomy(intent.CapabilityKey); ok {
				return required
			}
			return capability.RequiredAutonomy
		}
	}
	if required, ok := legacyRequiredExecutionAutonomy(intent.CapabilityKey); ok {
		return required
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
	return applyLegacyCalendarConfirmedDraft(result, confirmed)
}
