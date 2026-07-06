package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type VirployeeResponse struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	JobRoleID         string     `json:"job_role_id"`
	ProfileTemplateID string     `json:"profile_template_id"`
	CapabilityIDs     []string   `json:"capability_ids"`
	Description       string     `json:"description"`
	SupervisorUserID  string     `json:"supervisor_user_id"`
	Autonomy          string     `json:"autonomy"`
	State             string     `json:"state"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ArchivedAt        *time.Time `json:"archived_at"`
	TrashedAt         *time.Time `json:"trashed_at"`
	PurgeAfter        *time.Time `json:"purge_after"`
}

type ListVirployeesResponse struct {
	Data []VirployeeResponse `json:"data"`
}

type AutonomyLevelResponse struct {
	Level                    string   `json:"level"`
	Name                     string   `json:"name"`
	Description              string   `json:"description"`
	AllowsRequiredAutonomies []string `json:"allows_required_autonomies"`
}

type ListAutonomyLevelsResponse struct {
	Data []AutonomyLevelResponse `json:"data"`
}

type RuntimeContextResponse struct {
	Virployee       RuntimeContextVirployeeResponse       `json:"virployee"`
	JobRole         RuntimeContextJobRoleResponse         `json:"job_role"`
	ProfileTemplate RuntimeContextProfileTemplateResponse `json:"profile_template"`
	Capabilities    []RuntimeContextCapabilityResponse    `json:"capabilities"`
}

type RuntimeContextVirployeeResponse struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Autonomy         string `json:"autonomy"`
	State            string `json:"state"`
	SupervisorUserID string `json:"supervisor_user_id"`
}

type RuntimeContextJobRoleResponse struct {
	ID               string                                   `json:"id"`
	Name             string                                   `json:"name"`
	Mission          string                                   `json:"mission"`
	Responsibilities []RuntimeContextResponsibilityResponse   `json:"responsibilities"`
	SuccessCriteria  []RuntimeContextSuccessCriterionResponse `json:"success_criteria"`
}

type RuntimeContextResponsibilityResponse struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	ExpectedOutcome string `json:"expected_outcome"`
	Priority        int    `json:"priority"`
}

type RuntimeContextSuccessCriterionResponse struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	TargetValue string `json:"target_value"`
	Priority    int    `json:"priority"`
}

type RuntimeContextProfileTemplateResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	MaxAutonomy  string `json:"max_autonomy"`
}

type RuntimeContextCapabilityResponse struct {
	ID               string `json:"id"`
	CapabilityKey    string `json:"capability_key"`
	Name             string `json:"name"`
	RequiredAutonomy string `json:"required_autonomy"`
}

type DryRunResponse struct {
	Input              string                    `json:"input"`
	RuntimeContext     RuntimeContextResponse    `json:"runtime_context"`
	Intent             DryRunIntentResponse      `json:"intent"`
	RequiredCapability *DryRunCapabilityResponse `json:"required_capability,omitempty"`
	RequiredAutonomy   string                    `json:"required_autonomy"`
	VirployeeAutonomy  string                    `json:"virployee_autonomy"`
	Decision           string                    `json:"decision"`
	Reason             string                    `json:"reason"`
	NextStep           string                    `json:"next_step"`
	Draft              DryRunDraftResponse       `json:"draft"`
}

type DryRunCapabilityResponse struct {
	ID               string `json:"id,omitempty"`
	CapabilityKey    string `json:"capability_key"`
	Name             string `json:"name,omitempty"`
	RequiredAutonomy string `json:"required_autonomy"`
	Matched          bool   `json:"matched"`
}

type DryRunIntentResponse struct {
	Matched       bool                       `json:"matched"`
	CapabilityKey string                     `json:"capability_key"`
	Domain        string                     `json:"domain"`
	Resource      string                     `json:"resource"`
	Action        string                     `json:"action"`
	Confidence    float64                    `json:"confidence"`
	MatchedBy     []string                   `json:"matched_by"`
	Rules         []DryRunIntentRuleResponse `json:"rules"`
}

type DryRunIntentRuleResponse struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Value  string `json:"value"`
}

type DryRunDraftResponse struct {
	Status        string                       `json:"status"`
	Action        string                       `json:"action"`
	Kind          string                       `json:"kind"`
	Summary       string                       `json:"summary"`
	Fields        []DryRunDraftFieldResponse   `json:"fields"`
	MissingFields []DryRunMissingFieldResponse `json:"missing_fields"`
	Notes         []string                     `json:"notes"`
}

type DryRunDraftFieldResponse struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

type DryRunMissingFieldResponse struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

type ExecutionGateResponse struct {
	Input         string                        `json:"input"`
	DryRun        DryRunResponse                `json:"dry_run"`
	ExecutionGate ExecutionGateDecisionResponse `json:"execution_gate"`
}

type ExecutionGateDecisionResponse struct {
	Decision                  string                       `json:"decision"`
	Mode                      string                       `json:"mode"`
	WillExecute               bool                         `json:"will_execute"`
	RequiredExecutionAutonomy string                       `json:"required_execution_autonomy"`
	VirployeeAutonomy         string                       `json:"virployee_autonomy"`
	Checks                    []ExecutionGateCheckResponse `json:"checks"`
	NextStep                  string                       `json:"next_step"`
}

type ExecutionGateCheckResponse struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func VirployeeFromDomain(v domain.Virployee) VirployeeResponse {
	return VirployeeResponse{
		ID:                v.ID.String(),
		Name:              v.Name,
		JobRoleID:         v.JobRoleID.String(),
		ProfileTemplateID: v.ProfileTemplateID.String(),
		CapabilityIDs:     uuidStrings(v.CapabilityIDs),
		Description:       v.Description,
		SupervisorUserID:  v.SupervisorUserID,
		Autonomy:          string(v.Autonomy),
		State:             string(v.State()),
		CreatedAt:         v.CreatedAt,
		UpdatedAt:         v.UpdatedAt,
		ArchivedAt:        v.ArchivedAt,
		TrashedAt:         v.TrashedAt,
		PurgeAfter:        v.PurgeAfter,
	}
}

func ListVirployeesFromDomain(items []domain.Virployee) ListVirployeesResponse {
	data := make([]VirployeeResponse, 0, len(items))
	for _, item := range items {
		data = append(data, VirployeeFromDomain(item))
	}
	return ListVirployeesResponse{Data: data}
}

func RuntimeContextFromDomain(ctx runtimecontext.Context) RuntimeContextResponse {
	capabilities := make([]RuntimeContextCapabilityResponse, 0, len(ctx.Capabilities))
	for _, capability := range ctx.Capabilities {
		capabilities = append(capabilities, RuntimeContextCapabilityResponse{
			ID:               capability.ID.String(),
			CapabilityKey:    capability.CapabilityKey,
			Name:             capability.Name,
			RequiredAutonomy: string(capability.RequiredAutonomy),
		})
	}
	return RuntimeContextResponse{
		Virployee: RuntimeContextVirployeeResponse{
			ID:               ctx.Virployee.ID.String(),
			Name:             ctx.Virployee.Name,
			Description:      ctx.Virployee.Description,
			Autonomy:         string(ctx.Virployee.Autonomy),
			State:            string(ctx.Virployee.State()),
			SupervisorUserID: ctx.Virployee.SupervisorUserID,
		},
		JobRole: RuntimeContextJobRoleResponse{
			ID:               ctx.JobRole.ID.String(),
			Name:             ctx.JobRole.Name,
			Mission:          ctx.JobRole.Mission,
			Responsibilities: []RuntimeContextResponsibilityResponse{},
			SuccessCriteria:  []RuntimeContextSuccessCriterionResponse{},
		},
		ProfileTemplate: RuntimeContextProfileTemplateResponse{
			ID:           ctx.ProfileTemplate.ID.String(),
			Name:         ctx.ProfileTemplate.Name,
			SystemPrompt: ctx.ProfileTemplate.SystemPrompt,
			MaxAutonomy:  string(ctx.ProfileTemplate.MaxAutonomy),
		},
		Capabilities: capabilities,
	}
}

func DryRunFromDomain(result dryrun.Result) DryRunResponse {
	var requiredCapability *DryRunCapabilityResponse
	if result.RequiredCapability != nil {
		requiredCapability = &DryRunCapabilityResponse{
			ID:               result.RequiredCapability.ID,
			CapabilityKey:    result.RequiredCapability.CapabilityKey,
			Name:             result.RequiredCapability.Name,
			RequiredAutonomy: string(result.RequiredCapability.RequiredAutonomy),
			Matched:          result.RequiredCapability.Matched,
		}
	}
	return DryRunResponse{
		Input:              result.Input,
		RuntimeContext:     RuntimeContextFromDomain(result.RuntimeContext),
		Intent:             IntentFromDomain(result.Intent),
		RequiredCapability: requiredCapability,
		RequiredAutonomy:   string(result.RequiredAutonomy),
		VirployeeAutonomy:  string(result.VirployeeAutonomy),
		Decision:           string(result.Decision),
		Reason:             result.Reason,
		NextStep:           result.NextStep,
		Draft:              DraftFromDomain(result.Draft),
	}
}

func ExecutionGateFromDomain(result executiongate.Result) ExecutionGateResponse {
	checks := make([]ExecutionGateCheckResponse, 0, len(result.Gate.Checks))
	for _, check := range result.Gate.Checks {
		checks = append(checks, ExecutionGateCheckResponse{
			Key:    check.Key,
			Status: string(check.Status),
			Reason: check.Reason,
		})
	}
	return ExecutionGateResponse{
		Input:  result.Input,
		DryRun: DryRunFromDomain(result.DryRun),
		ExecutionGate: ExecutionGateDecisionResponse{
			Decision:                  string(result.Gate.Decision),
			Mode:                      result.Gate.Mode,
			WillExecute:               result.Gate.WillExecute,
			RequiredExecutionAutonomy: string(result.Gate.RequiredExecutionAutonomy),
			VirployeeAutonomy:         string(result.Gate.VirployeeAutonomy),
			Checks:                    checks,
			NextStep:                  result.Gate.NextStep,
		},
	}
}

func IntentFromDomain(intent dryrun.Intent) DryRunIntentResponse {
	rules := make([]DryRunIntentRuleResponse, 0, len(intent.Rules))
	for _, rule := range intent.Rules {
		rules = append(rules, DryRunIntentRuleResponse{
			Type:   rule.Type,
			Target: rule.Target,
			Value:  rule.Value,
		})
	}
	return DryRunIntentResponse{
		Matched:       intent.Matched,
		CapabilityKey: intent.CapabilityKey,
		Domain:        intent.Domain,
		Resource:      intent.Resource,
		Action:        intent.Action,
		Confidence:    intent.Confidence,
		MatchedBy:     intent.MatchedBy,
		Rules:         rules,
	}
}

func DraftFromDomain(draft dryrun.Draft) DryRunDraftResponse {
	fields := make([]DryRunDraftFieldResponse, 0, len(draft.Fields))
	for _, field := range draft.Fields {
		fields = append(fields, DryRunDraftFieldResponse{
			Key:    field.Key,
			Label:  field.Label,
			Value:  field.Value,
			Source: field.Source,
		})
	}
	missing := make([]DryRunMissingFieldResponse, 0, len(draft.MissingFields))
	for _, field := range draft.MissingFields {
		missing = append(missing, DryRunMissingFieldResponse{
			Key:    field.Key,
			Label:  field.Label,
			Reason: field.Reason,
		})
	}
	return DryRunDraftResponse{
		Status:        string(draft.Status),
		Action:        draft.Action,
		Kind:          draft.Kind,
		Summary:       draft.Summary,
		Fields:        fields,
		MissingFields: missing,
		Notes:         draft.Notes,
	}
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func ListAutonomyLevelsFromDomain(definitions []domain.AutonomyDefinition) ListAutonomyLevelsResponse {
	data := make([]AutonomyLevelResponse, 0, len(definitions))
	for _, definition := range definitions {
		data = append(data, AutonomyLevelResponse{
			Level:                    string(definition.Level),
			Name:                     definition.Name,
			Description:              definition.Description,
			AllowsRequiredAutonomies: allowedRequiredAutonomies(definition.Level, definitions),
		})
	}
	return ListAutonomyLevelsResponse{Data: data}
}

func allowedRequiredAutonomies(
	level domain.AutonomyLevel,
	definitions []domain.AutonomyDefinition,
) []string {
	out := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if level.Allows(definition.Level) {
			out = append(out, string(definition.Level))
		}
	}
	return out
}
