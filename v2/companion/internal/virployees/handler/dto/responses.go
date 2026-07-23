package dto

import (
	"encoding/json"
	"time"

	"github.com/devpablocristo/companion-v2/internal/invocation"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

// AssistRunResponse is the generic result of a process-and-respond run. Status is
// received|answering|done|failed; Output is the virployee's structured answer; Degraded is
// true when no real model answered (Echo / no credentials). The product-facing
// edge (BFF) maps this to the product's own contract.
type AssistRunResponse struct {
	ID                     string             `json:"id"`
	SubjectID              string             `json:"subject_id,omitempty"`
	CaseID                 string             `json:"case_id,omitempty"`
	AssignmentID           string             `json:"assignment_id,omitempty"`
	AssignmentVersion      int64              `json:"assignment_version,omitempty"`
	ResponsibleVirployeeID string             `json:"responsible_virployee_id,omitempty"`
	ProductID              string             `json:"product_id,omitempty"`
	ProductSurface         string             `json:"product_surface,omitempty"`
	InvocationContext      invocation.Context `json:"invocation_context"`
	CapabilityID           string             `json:"capability_id,omitempty"`
	CapabilityKey          string             `json:"capability_key,omitempty"`
	CapabilityManifestHash string             `json:"capability_manifest_hash,omitempty"`
	Status                 string             `json:"status"`
	GroundingMode          string             `json:"grounding_mode"`
	ContextHash            string             `json:"context_hash,omitempty"`
	AnswerStatus           string             `json:"answer_status,omitempty"`
	Citations              []CitationResponse `json:"citations"`
	Output                 json.RawMessage    `json:"output,omitempty"`
	OutputText             string             `json:"output_text,omitempty"`
	Answered               bool               `json:"answered"`
	Degraded               bool               `json:"degraded"`
	Model                  string             `json:"model,omitempty"`
	PromptVersion          string             `json:"prompt_version,omitempty"`
	Error                  string             `json:"error_message,omitempty"`
	DurationMS             int64              `json:"duration_ms"`
	Orchestration          any                `json:"orchestration,omitempty"`
}

type CitationResponse struct {
	KnowledgeBaseID string          `json:"knowledge_base_id,omitempty"`
	DocumentID      string          `json:"document_id"`
	SourceVersion   string          `json:"source_version,omitempty"`
	SHA256          string          `json:"sha256,omitempty"`
	Locator         json.RawMessage `json:"locator,omitempty"`
}

type VirployeeResponse struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	JobRoleID         string     `json:"job_role_id"`
	ProfileTemplateID string     `json:"profile_template_id"`
	CapabilityIDs     []string   `json:"capability_ids"`
	Description       string     `json:"description"`
	SupervisorUserID  string     `json:"supervisor_user_id"`
	Autonomy          string     `json:"autonomy"`
	GroundingMode     string     `json:"grounding_mode"`
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
	Virployee         RuntimeContextVirployeeResponse       `json:"virployee"`
	JobRole           RuntimeContextJobRoleResponse         `json:"job_role"`
	ProfileTemplate   RuntimeContextProfileTemplateResponse `json:"profile_template"`
	Capabilities      []RuntimeContextCapabilityResponse    `json:"capabilities"`
	MemoryReferences  []MemoryReferenceResponse             `json:"memory_references"`
	MemoryContextHash string                                `json:"memory_context_hash"`
}

type MemoryReferenceResponse struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Type        string  `json:"type"`
	Version     int     `json:"version"`
	Hash        string  `json:"hash"`
	Sensitivity string  `json:"sensitivity"`
	Score       float64 `json:"score"`
}

type RuntimeContextVirployeeResponse struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Autonomy         string `json:"autonomy"`
	GroundingMode    string `json:"grounding_mode"`
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
	Input              string                         `json:"input"`
	RuntimeContext     RuntimeContextResponse         `json:"runtime_context"`
	Intent             DryRunIntentResponse           `json:"intent"`
	RequiredCapability *DryRunCapabilityResponse      `json:"required_capability,omitempty"`
	RequiredAutonomy   string                         `json:"required_autonomy"`
	VirployeeAutonomy  string                         `json:"virployee_autonomy"`
	Decision           string                         `json:"decision"`
	Reason             string                         `json:"reason"`
	NextStep           string                         `json:"next_step"`
	Draft              DryRunDraftResponse            `json:"draft"`
	PreparedAction     *dryrun.PreparedActionProposal `json:"prepared_action,omitempty"`
}

type DryRunCapabilityResponse struct {
	ID               string         `json:"id,omitempty"`
	ManifestHash     string         `json:"manifest_hash,omitempty"`
	CapabilityKey    string         `json:"capability_key"`
	Name             string         `json:"name,omitempty"`
	RequiredAutonomy string         `json:"required_autonomy"`
	Matched          bool           `json:"matched"`
	InputSchema      map[string]any `json:"input_schema,omitempty"`
}

type DryRunIntentResponse struct {
	Matched       bool                       `json:"matched"`
	CapabilityID  string                     `json:"capability_id,omitempty"`
	CapabilityKey string                     `json:"capability_key"`
	Domain        string                     `json:"domain"`
	Resource      string                     `json:"resource"`
	Action        string                     `json:"action"`
	Confidence    float64                    `json:"confidence"`
	MatchedBy     []string                   `json:"matched_by"`
	Rules         []DryRunIntentRuleResponse `json:"rules"`
	ProposedBy    string                     `json:"proposed_by"`
	ModelID       string                     `json:"model_id"`
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

type RunTraceResponse struct {
	ID                string                            `json:"id"`
	VirployeeID       string                            `json:"virployee_id"`
	Operation         string                            `json:"operation"`
	InputHash         string                            `json:"input_hash"`
	InputPreview      string                            `json:"input_preview"`
	Intent            map[string]any                    `json:"intent"`
	CapabilityID      string                            `json:"capability_id,omitempty"`
	CapabilityKey     string                            `json:"capability_key"`
	DryRunDecision    string                            `json:"dry_run_decision"`
	GateDecision      string                            `json:"gate_decision,omitempty"`
	GateChecks        []RunTraceGateCheckResponse       `json:"gate_checks"`
	GovernanceResult  *RunTraceGovernanceResultResponse `json:"governance_result,omitempty"`
	NexusResult       *RunTraceNexusResultResponse      `json:"nexus_result,omitempty"`
	ExecutionResult   *RunTraceExecutionResultResponse  `json:"execution_result,omitempty"`
	BindingHash       string                            `json:"binding_hash,omitempty"`
	MemoryReferences  []MemoryReferenceResponse         `json:"memory_references"`
	MemoryContextHash string                            `json:"memory_context_hash"`
	CreatedAt         time.Time                         `json:"created_at"`
}

type RunTraceGateCheckResponse struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type RunTraceGovernanceResultResponse struct {
	CheckID              string `json:"check_id,omitempty"`
	Available            bool   `json:"available"`
	Decision             string `json:"decision,omitempty"`
	RiskLevel            string `json:"risk_level,omitempty"`
	Status               string `json:"status,omitempty"`
	DecisionReason       string `json:"decision_reason,omitempty"`
	WouldRequireApproval bool   `json:"would_require_approval,omitempty"`
	BindingHash          string `json:"binding_hash,omitempty"`
	ApprovalID           string `json:"approval_id,omitempty"`
	ApprovalStatus       string `json:"approval_status,omitempty"`
	Error                string `json:"error,omitempty"`
}

type RunTraceNexusResultResponse = RunTraceGovernanceResultResponse

type RunTraceExecutionResultResponse struct {
	Status                 string `json:"status,omitempty"`
	Mode                   string `json:"mode,omitempty"`
	ApprovalID             string `json:"approval_id,omitempty"`
	ApprovalStatus         string `json:"approval_status,omitempty"`
	BindingHash            string `json:"binding_hash,omitempty"`
	Message                string `json:"message,omitempty"`
	ExternalEffects        bool   `json:"external_effects"`
	ResourceID             string `json:"resource_id,omitempty"`
	DurationMS             int64  `json:"duration_ms,omitempty"`
	GovernanceReportStatus string `json:"governance_report_status,omitempty"`
	NexusReportStatus      string `json:"nexus_report_status,omitempty"`
}

type ListRunTracesResponse struct {
	Data []RunTraceResponse `json:"data"`
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
		GroundingMode:     string(v.GroundingMode),
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

func ListRunTracesFromDomain(items []runtraces.Trace) ListRunTracesResponse {
	data := make([]RunTraceResponse, 0, len(items))
	for _, item := range items {
		data = append(data, RunTraceFromDomain(item))
	}
	return ListRunTracesResponse{Data: data}
}

func RunTraceFromDomain(trace runtraces.Trace) RunTraceResponse {
	checks := make([]RunTraceGateCheckResponse, 0, len(trace.GateChecks))
	for _, check := range trace.GateChecks {
		checks = append(checks, RunTraceGateCheckResponse{
			Key:    check.Key,
			Status: check.Status,
			Reason: check.Reason,
		})
	}
	memoryReferences := make([]MemoryReferenceResponse, 0, len(trace.MemoryReferences))
	for _, ref := range trace.MemoryReferences {
		memoryReferences = append(memoryReferences, MemoryReferenceResponse{ID: ref.ID.String(), Title: ref.Title, Type: ref.Type, Version: ref.Version, Hash: ref.Hash, Sensitivity: ref.Sensitivity, Score: ref.Score})
	}
	governanceResult := trace.GovernanceResult
	if governanceResult == nil {
		governanceResult = trace.NexusResult
	}
	governanceResponse := runTraceGovernanceResultFromDomain(governanceResult)
	return RunTraceResponse{
		ID:                trace.ID.String(),
		VirployeeID:       trace.VirployeeID.String(),
		Operation:         string(trace.Operation),
		InputHash:         trace.InputHash,
		InputPreview:      trace.InputPreview,
		Intent:            trace.Intent,
		CapabilityID:      trace.CapabilityID,
		CapabilityKey:     trace.CapabilityKey,
		DryRunDecision:    trace.DryRunDecision,
		GateDecision:      trace.GateDecision,
		GateChecks:        checks,
		GovernanceResult:  governanceResponse,
		NexusResult:       governanceResponse,
		ExecutionResult:   runTraceExecutionResultFromDomain(trace.ExecutionResult),
		BindingHash:       trace.BindingHash,
		MemoryReferences:  memoryReferences,
		MemoryContextHash: trace.MemoryContextHash,
		CreatedAt:         trace.CreatedAt,
	}
}

func runTraceGovernanceResultFromDomain(result *runtraces.GovernanceResult) *RunTraceGovernanceResultResponse {
	if result == nil {
		return nil
	}
	return &RunTraceGovernanceResultResponse{
		CheckID:              result.CheckID,
		Available:            result.Available,
		Decision:             result.Decision,
		RiskLevel:            result.RiskLevel,
		Status:               result.Status,
		DecisionReason:       result.DecisionReason,
		WouldRequireApproval: result.WouldRequireApproval,
		BindingHash:          result.BindingHash,
		ApprovalID:           result.ApprovalID,
		ApprovalStatus:       result.ApprovalStatus,
		Error:                result.Error,
	}
}

func runTraceExecutionResultFromDomain(result *runtraces.ExecutionResult) *RunTraceExecutionResultResponse {
	if result == nil {
		return nil
	}
	governanceReportStatus := result.GovernanceReportStatus
	if governanceReportStatus == "" {
		governanceReportStatus = result.NexusReportStatus
	}
	return &RunTraceExecutionResultResponse{
		Status:                 result.Status,
		Mode:                   result.Mode,
		ApprovalID:             result.ApprovalID,
		ApprovalStatus:         result.ApprovalStatus,
		BindingHash:            result.BindingHash,
		Message:                result.Message,
		ExternalEffects:        result.ExternalEffects,
		ResourceID:             result.ResourceID,
		DurationMS:             result.DurationMS,
		GovernanceReportStatus: governanceReportStatus,
		NexusReportStatus:      governanceReportStatus,
	}
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
	memoryReferences := make([]MemoryReferenceResponse, 0, len(ctx.MemoryReferences))
	for _, ref := range ctx.MemoryReferences {
		memoryReferences = append(memoryReferences, MemoryReferenceResponse{ID: ref.ID.String(), Title: ref.Title, Type: ref.Type, Version: ref.Version, Hash: ref.Hash, Sensitivity: ref.Sensitivity, Score: ref.Score})
	}
	responsibilities := make([]RuntimeContextResponsibilityResponse, 0, len(ctx.JobRole.Responsibilities))
	for _, item := range ctx.JobRole.Responsibilities {
		responsibilities = append(responsibilities, RuntimeContextResponsibilityResponse{Title: item.Title, Description: item.Description, ExpectedOutcome: item.ExpectedOutcome, Priority: item.Priority})
	}
	successCriteria := make([]RuntimeContextSuccessCriterionResponse, 0, len(ctx.JobRole.SuccessCriteria))
	for _, item := range ctx.JobRole.SuccessCriteria {
		successCriteria = append(successCriteria, RuntimeContextSuccessCriterionResponse{Title: item.Title, Description: item.Description, TargetValue: item.TargetValue, Priority: item.Priority})
	}
	return RuntimeContextResponse{
		Virployee: RuntimeContextVirployeeResponse{
			ID:               ctx.Virployee.ID.String(),
			Name:             ctx.Virployee.Name,
			Description:      ctx.Virployee.Description,
			Autonomy:         string(ctx.Virployee.Autonomy),
			GroundingMode:    string(ctx.Virployee.GroundingMode),
			State:            string(ctx.Virployee.State()),
			SupervisorUserID: ctx.Virployee.SupervisorUserID,
		},
		JobRole: RuntimeContextJobRoleResponse{
			ID:               ctx.JobRole.ID.String(),
			Name:             ctx.JobRole.Name,
			Mission:          ctx.JobRole.Mission,
			Responsibilities: responsibilities,
			SuccessCriteria:  successCriteria,
		},
		ProfileTemplate: RuntimeContextProfileTemplateResponse{
			ID:           ctx.ProfileTemplate.ID.String(),
			Name:         ctx.ProfileTemplate.Name,
			SystemPrompt: ctx.ProfileTemplate.SystemPrompt,
			MaxAutonomy:  string(ctx.ProfileTemplate.MaxAutonomy),
		},
		Capabilities:      capabilities,
		MemoryReferences:  memoryReferences,
		MemoryContextHash: ctx.MemoryContextHash,
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
		for _, capability := range result.RuntimeContext.Capabilities {
			if capability.ID.String() == result.RequiredCapability.ID {
				requiredCapability.ManifestHash = capability.ManifestHash
				requiredCapability.InputSchema = capability.Manifest.InputSchema
				break
			}
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
		PreparedAction:     result.PreparedAction,
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
		CapabilityID:  intent.CapabilityID,
		CapabilityKey: intent.CapabilityKey,
		Domain:        intent.Domain,
		Resource:      intent.Resource,
		Action:        intent.Action,
		Confidence:    intent.Confidence,
		MatchedBy:     intent.MatchedBy,
		Rules:         rules,
		ProposedBy:    intent.ProposedBy,
		ModelID:       intent.ModelID,
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
