package mcpgovernance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	GetPolicy(context.Context, string) (Policy, error)
	PutPolicy(context.Context, string, string, PutPolicyInput) (Policy, error)
	ListPolicyAudit(context.Context, string, int) ([]PolicyAudit, error)
	ResolveContext(context.Context, ContextRequest) (InvocationContext, error)
	ReserveInvocation(context.Context, InvocationAudit, int, int) error
	CompleteInvocation(context.Context, string, uuid.UUID, string, string, string, string, int64) error
	ListInvocations(context.Context, string, uuid.UUID, int) ([]InvocationAudit, error)
}

type InvocationOutcomeRepositoryPort interface {
	SaveInvocationOutcome(context.Context, string, uuid.UUID, string, string, string) error
}

type CapabilityCatalogPort interface {
	ListActive(context.Context, string) ([]capabilitydomain.Capability, error)
}

type VirployeeReaderPort interface {
	Get(context.Context, string, uuid.UUID) (virployeedomain.Virployee, error)
}

type AuthorityEvaluatorPort interface {
	EvaluateAuthority(context.Context, executiongate.AuthorityCheckInput) (executiongate.AuthorityCheckResult, error)
}

type ReadExecutorPort interface {
	Execute(context.Context, InvocationContext, capabilitydomain.Capability, map[string]any) (map[string]any, error)
}

type WriteGateInput struct {
	Context        InvocationContext
	Capability     capabilitydomain.Capability
	Arguments      map[string]any
	IdempotencyKey string
	PayloadHash    string
	ContextHash    string
	AuthorityHash  string
	PolicyVersion  int64
}

type WriteGateResult struct {
	Status         string
	ApprovalID     string
	BindingHash    string
	DecisionReason string
}

type WriteGatePort interface {
	SupportsMCPAction(string) bool
	PrepareMCPAction(context.Context, WriteGateInput) (WriteGateResult, error)
}

type UseCases struct {
	repo       RepositoryPort
	catalog    CapabilityCatalogPort
	virployees VirployeeReaderPort
	authority  AuthorityEvaluatorPort
	writeGate  WriteGatePort
	readers    map[string]ReadExecutorPort
	now        func() time.Time
}

// ToolInvocationGate is the common governed invocation service. The alias
// keeps the package's existing use-case naming while making the architectural
// boundary explicit to internal callers.
type ToolInvocationGate = UseCases

func NewToolInvocationGate(repo RepositoryPort, catalog CapabilityCatalogPort, virployees VirployeeReaderPort, authority AuthorityEvaluatorPort, writeGate WriteGatePort) *ToolInvocationGate {
	return NewUseCases(repo, catalog, virployees, authority, writeGate)
}

func NewUseCases(repo RepositoryPort, catalog CapabilityCatalogPort, virployees VirployeeReaderPort, authority AuthorityEvaluatorPort, writeGate WriteGatePort) *UseCases {
	return &UseCases{repo: repo, catalog: catalog, virployees: virployees, authority: authority, writeGate: writeGate, readers: map[string]ReadExecutorPort{}, now: func() time.Time { return time.Now().UTC() }}
}

func (u *UseCases) RegisterReadExecutor(capabilityKey string, executor ReadExecutorPort) {
	capabilityKey = strings.ToLower(strings.TrimSpace(capabilityKey))
	if capabilityKey != "" && executor != nil {
		u.readers[capabilityKey] = executor
	}
}

func (u *UseCases) HasReadExecutor(capabilityKey string) bool {
	return u.readers[strings.ToLower(strings.TrimSpace(capabilityKey))] != nil
}

func (u *UseCases) GetPolicy(ctx context.Context, orgID string) (Policy, error) {
	return u.repo.GetPolicy(ctx, strings.TrimSpace(orgID))
}

func (u *UseCases) PutPolicy(ctx context.Context, orgID, actorID, actorRole string, input PutPolicyInput) (Policy, error) {
	if !ownerOrAdmin(actorRole) {
		return Policy{}, domainerr.Forbidden("MCP policy changes require an owner or admin")
	}
	normalized, err := NormalizePolicyInput(input)
	if err != nil {
		return Policy{}, err
	}
	return u.repo.PutPolicy(ctx, strings.TrimSpace(orgID), strings.TrimSpace(actorID), normalized)
}

func (u *UseCases) ListPolicyAudit(ctx context.Context, orgID, actorRole string, limit int) ([]PolicyAudit, error) {
	if !ownerOrAdmin(actorRole) {
		return nil, domainerr.Forbidden("MCP policy audit requires an owner or admin")
	}
	return u.repo.ListPolicyAudit(ctx, strings.TrimSpace(orgID), limit)
}

func (u *UseCases) ListInvocations(ctx context.Context, orgID, actorRole string, virployeeID uuid.UUID, limit int) ([]InvocationAudit, error) {
	if !ownerOrAdmin(actorRole) {
		return nil, domainerr.Forbidden("MCP invocation audit requires an owner or admin")
	}
	return u.repo.ListInvocations(ctx, strings.TrimSpace(orgID), virployeeID, limit)
}

func (u *UseCases) ResolveContext(ctx context.Context, request ContextRequest) (InvocationContext, error) {
	if strings.TrimSpace(request.OrgID) == "" || strings.TrimSpace(request.ActorID) == "" || request.VirployeeID == uuid.Nil || request.SubjectID == uuid.Nil {
		return InvocationContext{}, domainerr.Validation("organization, actor, virployee_id and subject_id are required")
	}
	return u.repo.ResolveContext(ctx, request)
}

// ValidateMCPExecutionContext implements virployees.MCPExecutionContextValidatorPort.
// It reloads every mutable authorization input and requires the exact approved
// context hash; payload and idempotency hashes remain protected by the prepared
// action's own immutable payload hash.
func (u *UseCases) ValidateMCPExecutionContext(ctx context.Context, binding preparedactions.MCPContextBinding) error {
	virployeeID, err := uuid.Parse(binding.VirployeeID)
	if err != nil {
		return domainerr.Conflict("prepared MCP virployee is invalid")
	}
	subjectID, err := uuid.Parse(binding.SubjectID)
	if err != nil {
		return domainerr.Conflict("prepared MCP subject is invalid")
	}
	assignmentID, err := uuid.Parse(binding.AssignmentID)
	if err != nil {
		return domainerr.Conflict("prepared MCP assignment is invalid")
	}
	var caseID uuid.UUID
	if strings.TrimSpace(binding.CaseID) != "" {
		caseID, err = uuid.Parse(binding.CaseID)
		if err != nil {
			return domainerr.Conflict("prepared MCP case is invalid")
		}
	}
	resolved, err := u.repo.ResolveContext(ctx, ContextRequest{
		OrgID: binding.OrgID, ActorID: binding.ActorID, VirployeeID: virployeeID, SubjectID: subjectID, CaseID: caseID,
	})
	if err != nil || resolved.AssignmentID != assignmentID || resolved.AssignmentVersion != binding.AssignmentVersion {
		return domainerr.Conflict("prepared MCP assignment changed after approval")
	}
	policy, err := u.repo.GetPolicy(ctx, binding.OrgID)
	if err != nil || policy.Version != binding.PolicyVersion {
		return domainerr.Conflict("MCP policy changed after approval")
	}
	virployee, err := u.virployees.Get(ctx, binding.OrgID, virployeeID)
	if err != nil {
		return domainerr.Conflict("MCP Virployee could not be revalidated")
	}
	tool, blockedBy, err := u.resolveTool(ctx, policy, resolved, virployee, binding.CapabilityKey)
	if err != nil || blockedBy != "" {
		return domainerr.Conflict("MCP capability is no longer authorized")
	}
	if tool.Meta.CapabilityVersion != binding.CapabilityVersion || tool.Meta.ManifestHash != binding.ManifestHash || tool.AuthorityHash != binding.AuthorityHash {
		return domainerr.Conflict("MCP capability or authority changed after approval")
	}
	hash, err := toolContextHash(resolved, virployee, policy, tool)
	if err != nil || hash != binding.ContextHash {
		return domainerr.Conflict("MCP context changed after approval")
	}
	return nil
}

func (u *UseCases) ListTools(ctx context.Context, request ContextRequest) ([]Tool, error) {
	started := u.now()
	invocationContext, err := u.ResolveContext(ctx, request)
	if err != nil {
		return nil, err
	}
	policy, err := u.repo.GetPolicy(ctx, request.OrgID)
	if err != nil {
		return nil, err
	}
	virployee, err := u.virployees.Get(ctx, request.OrgID, request.VirployeeID)
	if err != nil {
		return nil, err
	}
	tools, err := u.resolveTools(ctx, policy, invocationContext, virployee)
	if err != nil {
		return nil, err
	}
	contextHash, err := listContextHash(invocationContext, virployee, policy, tools)
	if err != nil {
		return nil, err
	}
	audit := InvocationAudit{ID: uuid.New(), Context: invocationContext, Method: "tools/list", PolicyVersion: policy.Version, ContextHash: contextHash, CreatedAt: started}
	if err := u.repo.ReserveInvocation(ctx, audit, policy.MaxCallsPerMinute, policy.MaxConcurrency); err != nil {
		return nil, err
	}
	resultHash, _ := Hash(toolNames(tools))
	if err := u.repo.CompleteInvocation(ctx, request.OrgID, audit.ID, "succeeded", "", "", resultHash, time.Since(started).Milliseconds()); err != nil {
		return nil, err
	}
	return tools, nil
}

func (u *UseCases) CallTool(ctx context.Context, invocation Invocation) (out InvocationResult, err error) {
	started := u.now()
	policy, err := u.repo.GetPolicy(ctx, invocation.Context.OrgID)
	if err != nil {
		return InvocationResult{}, err
	}
	virployee, err := u.virployees.Get(ctx, invocation.Context.OrgID, invocation.Context.VirployeeID)
	if err != nil {
		return InvocationResult{}, err
	}
	tool, blockedBy, err := u.resolveTool(ctx, policy, invocation.Context, virployee, invocation.ToolName)
	if err != nil {
		return InvocationResult{}, err
	}
	if expected := strings.TrimSpace(invocation.ExpectedManifestHash); expected != "" && tool.Meta.ManifestHash != expected {
		return InvocationResult{}, domainerr.Conflict("capability manifest changed after Assist acceptance")
	}
	payloadHash, err := Hash(invocation.Arguments)
	if err != nil {
		return InvocationResult{}, domainerr.Validation("tool arguments are invalid")
	}
	contextHash, err := toolContextHash(invocation.Context, virployee, policy, tool)
	if err != nil {
		return InvocationResult{}, err
	}
	audit := InvocationAudit{
		ID: uuid.New(), Context: invocation.Context, Method: "tools/call", CapabilityKey: tool.Name,
		CapabilityVersion: tool.Meta.CapabilityVersion, ManifestHash: tool.Meta.ManifestHash,
		PolicyVersion: policy.Version, ContextHash: contextHash, PayloadHash: payloadHash,
		CreatedAt: started,
	}
	if tool.Capability.SideEffectClass == "write" && invocation.IdempotencyKey != "" {
		audit.IdempotencyHash = HashString(invocation.IdempotencyKey)
	}
	if err := u.repo.ReserveInvocation(ctx, audit, policy.MaxCallsPerMinute, policy.MaxConcurrency); err != nil {
		var replay *IdempotentReplayError
		if errors.As(err, &replay) {
			switch replay.Prior.Status {
			case "pending_approval":
				return InvocationResult{Status: replay.Prior.Status, ApprovalID: replay.Prior.ApprovalID, BindingHash: replay.Prior.BindingHash, DecisionReason: replay.Prior.DecisionReason}, nil
			case "running":
				return InvocationResult{}, domainerr.Conflict("MCP write with this idempotency key is already in progress")
			default:
				return InvocationResult{}, domainerr.Conflict("MCP write with this idempotency key was already completed")
			}
		}
		return InvocationResult{}, err
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		status, code := "failed", "execution_failed"
		if err != nil && blockedBy != "" {
			status, code = "blocked", "governance_blocked"
		}
		_ = u.repo.CompleteInvocation(context.WithoutCancel(ctx), invocation.Context.OrgID, audit.ID, status, blockedBy, code, "", time.Since(started).Milliseconds())
	}()
	if blockedBy != "" {
		return InvocationResult{}, domainerr.Forbidden("tool is blocked by " + blockedBy)
	}
	if err := ValidateJSONSchema(tool.InputSchema, invocation.Arguments); err != nil {
		blockedBy = "input_schema"
		return InvocationResult{}, domainerr.Validation(err.Error())
	}
	if tool.Capability.SideEffectClass == "write" && strings.TrimSpace(invocation.IdempotencyKey) == "" {
		blockedBy = "idempotency"
		return InvocationResult{}, domainerr.Validation("idempotency key is required for write capabilities")
	}

	if tool.Capability.SideEffectClass == "write" || tool.Capability.RequiresGovernanceApproval {
		if u.writeGate == nil {
			blockedBy = "execution_gate"
			return InvocationResult{}, domainerr.Conflict("execution gate is not configured")
		}
		gate, gateErr := u.writeGate.PrepareMCPAction(ctx, WriteGateInput{
			Context: invocation.Context, Capability: tool.Capability, Arguments: invocation.Arguments,
			IdempotencyKey: invocation.IdempotencyKey, PayloadHash: payloadHash, ContextHash: contextHash,
			AuthorityHash: tool.AuthorityHash, PolicyVersion: policy.Version,
		})
		if gateErr != nil {
			blockedBy = "execution_gate"
			return InvocationResult{}, gateErr
		}
		out = InvocationResult{Status: gate.Status, ApprovalID: gate.ApprovalID, BindingHash: gate.BindingHash, DecisionReason: gate.DecisionReason}
		if outcomes, ok := u.repo.(InvocationOutcomeRepositoryPort); ok {
			if err := outcomes.SaveInvocationOutcome(ctx, invocation.Context.OrgID, audit.ID, out.ApprovalID, out.BindingHash, out.DecisionReason); err != nil {
				return InvocationResult{}, err
			}
		}
		status := "blocked"
		if gate.Status == "pending_approval" {
			status = "pending_approval"
		}
		resultHash, _ := Hash(out)
		if err := u.repo.CompleteInvocation(ctx, invocation.Context.OrgID, audit.ID, status, "execution_gate", "", resultHash, time.Since(started).Milliseconds()); err != nil {
			return InvocationResult{}, err
		}
		completed = true
		return out, nil
	}

	executor := u.readers[tool.Name]
	if executor == nil {
		blockedBy = "executor"
		return InvocationResult{}, domainerr.Conflict("executor is not configured for tool")
	}
	// Re-resolve immediately before execution so policy, assignment, capability
	// promotion and delegation changes invalidate the earlier decision.
	currentContext, err := u.repo.ResolveContext(ctx, ContextRequest{
		OrgID: invocation.Context.OrgID, ActorID: invocation.Context.ActorID, ActorRole: invocation.Context.ActorRole,
		VirployeeID: invocation.Context.VirployeeID, SubjectID: invocation.Context.SubjectID, CaseID: invocation.Context.CaseID,
		ProductSurface: invocation.Context.ProductSurface, RepositoryGeneration: invocation.Context.RepositoryGeneration,
	})
	if err != nil {
		blockedBy = "context_revalidation"
		return InvocationResult{}, err
	}
	currentPolicy, err := u.repo.GetPolicy(ctx, invocation.Context.OrgID)
	if err != nil {
		blockedBy = "policy_revalidation"
		return InvocationResult{}, err
	}
	currentTool, currentBlockedBy, err := u.resolveTool(ctx, currentPolicy, currentContext, virployee, tool.Name)
	if err != nil || currentBlockedBy != "" {
		blockedBy = "authority_revalidation"
		if err != nil {
			return InvocationResult{}, err
		}
		return InvocationResult{}, domainerr.Forbidden("tool authorization changed")
	}
	currentHash, _ := toolContextHash(currentContext, virployee, currentPolicy, currentTool)
	if currentHash != contextHash {
		blockedBy = "context_revalidation"
		return InvocationResult{}, domainerr.Conflict("tool context changed before execution")
	}
	result, err := executor.Execute(ctx, currentContext, currentTool.Capability, invocation.Arguments)
	if err != nil {
		blockedBy = "executor"
		return InvocationResult{}, err
	}
	if err := ValidateJSONSchema(currentTool.OutputSchema, result); err != nil {
		blockedBy = "output_schema"
		return InvocationResult{}, domainerr.Conflict("executor result does not satisfy output schema")
	}
	out = InvocationResult{Status: "succeeded", Result: result}
	resultHash, _ := Hash(result)
	if err := u.repo.CompleteInvocation(ctx, invocation.Context.OrgID, audit.ID, "succeeded", "", "", resultHash, time.Since(started).Milliseconds()); err != nil {
		return InvocationResult{}, err
	}
	completed = true
	return out, nil
}

func (u *UseCases) resolveTools(ctx context.Context, policy Policy, invocation InvocationContext, virployee virployeedomain.Virployee) ([]Tool, error) {
	if !policy.Enabled || policy.KillSwitch {
		return []Tool{}, nil
	}
	if virployee.State() != virployeedomain.StateActive {
		return []Tool{}, nil
	}
	capabilities, err := u.catalog.ListActive(ctx, invocation.OrgID)
	if err != nil {
		return nil, err
	}
	assigned := make(map[uuid.UUID]struct{}, len(virployee.CapabilityIDs))
	for _, id := range virployee.CapabilityIDs {
		assigned[id] = struct{}{}
	}
	tools := make([]Tool, 0, len(assigned))
	for _, capability := range capabilities {
		if _, ok := assigned[capability.ID]; !ok || capability.PromotionState != capabilitydomain.PromotionActive || capability.ManifestHash == "" || capability.ConformedHash != capability.ManifestHash {
			continue
		}
		if invocation.ProductSurface != "" && capability.Manifest.ProductSurface != invocation.ProductSurface {
			continue
		}
		if capability.SideEffectClass == "read" {
			if u.readers[capability.CapabilityKey] == nil {
				continue
			}
		} else if u.writeGate == nil || !u.writeGate.SupportsMCPAction(capability.CapabilityKey) {
			continue
		}
		if !virployee.Autonomy.Allows(capability.RequiredAutonomy) {
			continue
		}
		if allowed, _ := AllowsPolicy(policy, capability, virployee.JobRoleID); !allowed {
			continue
		}
		if u.authority == nil {
			continue
		}
		authority, err := u.authority.EvaluateAuthority(ctx, executiongate.AuthorityCheckInput{
			OrgID: invocation.OrgID, VirployeeID: virployee.ID, JobRoleID: virployee.JobRoleID,
			CapabilityKey: capability.CapabilityKey, PrincipalType: invocation.PrincipalType,
			PrincipalID: invocation.PrincipalID, ProductSurface: capability.Manifest.ProductSurface,
			ResourceType: mcpResourceType(invocation), ResourceID: mcpResourceID(invocation),
			RiskClass: capability.RiskClass, At: u.now(),
		})
		if err != nil || !authority.Allowed || strings.TrimSpace(authority.SnapshotHash) == "" {
			continue
		}
		tools = append(tools, toolFromCapability(capability, authority.SnapshotHash))
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func mcpResourceType(invocation InvocationContext) string {
	if invocation.CaseID != uuid.Nil {
		return "case"
	}
	return "work_subject"
}

func mcpResourceID(invocation InvocationContext) string {
	if invocation.CaseID != uuid.Nil {
		return invocation.CaseID.String()
	}
	return invocation.SubjectID.String()
}

func (u *UseCases) resolveTool(ctx context.Context, policy Policy, invocation InvocationContext, virployee virployeedomain.Virployee, name string) (Tool, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Tool{}, "tool_name", domainerr.Validation("tool name is required")
	}
	tools, err := u.resolveTools(ctx, policy, invocation, virployee)
	if err != nil {
		return Tool{}, "catalog", err
	}
	for _, tool := range tools {
		if tool.Name == name {
			return tool, "", nil
		}
	}
	return Tool{}, "effective_policy", domainerr.Forbidden("tool is not available in the effective context")
}

func toolFromCapability(capability capabilitydomain.Capability, authorityHash string) Tool {
	return Tool{
		Name: capability.CapabilityKey, Description: capability.Description,
		InputSchema: capability.Manifest.InputSchema, OutputSchema: capability.Manifest.OutputSchema,
		Annotations: ToolAnnotations{
			ReadOnlyHint:    capability.SideEffectClass == "read",
			DestructiveHint: capability.SideEffectClass == "write" && (strings.HasSuffix(capability.CapabilityKey, ".delete") || strings.HasSuffix(capability.CapabilityKey, ".remove")),
			IdempotentHint:  capability.Manifest.Idempotency.Mode == "required", OpenWorldHint: capability.SideEffectClass == "write",
		},
		Meta: ToolMeta{CapabilityVersion: capability.Manifest.Version, ManifestHash: capability.ManifestHash,
			RiskClass: capability.RiskClass, RequiresApproval: capability.RequiresGovernanceApproval, RollbackMode: capability.Manifest.RollbackMode},
		Capability: capability, AuthorityHash: authorityHash,
	}
}

func toolContextHash(context InvocationContext, virployee virployeedomain.Virployee, policy Policy, tool Tool) (string, error) {
	return Hash(map[string]any{
		"schema_version": "axis.mcp.context.v1", "org_id": context.OrgID,
		"actor_id": context.ActorID, "virployee_id": context.VirployeeID.String(), "job_role_id": virployee.JobRoleID.String(),
		"subject_id": context.SubjectID.String(), "case_id": optionalUUID(context.CaseID),
		"assignment_id": context.AssignmentID.String(), "assignment_version": context.AssignmentVersion,
		"product_surface": context.ProductSurface, "repository_generation": context.RepositoryGeneration,
		"principal_type": context.PrincipalType, "principal_id": context.PrincipalID,
		"capability_key": tool.Name, "capability_version": tool.Meta.CapabilityVersion,
		"manifest_hash": tool.Meta.ManifestHash, "authority_hash": tool.AuthorityHash, "mcp_policy_version": policy.Version,
	})
}

func listContextHash(context InvocationContext, virployee virployeedomain.Virployee, policy Policy, tools []Tool) (string, error) {
	items := make([]map[string]string, 0, len(tools))
	for _, tool := range tools {
		items = append(items, map[string]string{"key": tool.Name, "manifest_hash": tool.Meta.ManifestHash, "authority_hash": tool.AuthorityHash})
	}
	return Hash(map[string]any{
		"schema_version": "axis.mcp.list-context.v1", "org_id": context.OrgID,
		"virployee_id": context.VirployeeID.String(), "job_role_id": virployee.JobRoleID.String(),
		"subject_id": context.SubjectID.String(), "case_id": optionalUUID(context.CaseID),
		"assignment_id": context.AssignmentID.String(), "assignment_version": context.AssignmentVersion,
		"product_surface": context.ProductSurface, "repository_generation": context.RepositoryGeneration,
		"mcp_policy_version": policy.Version, "tools": items,
	})
}

func toolNames(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func optionalUUID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func ownerOrAdmin(role string) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	return role == "owner" || role == "admin"
}

func BuildDeterministicActionInput(capabilityKey string) string {
	parts := strings.Split(capabilityKey, ".")
	if len(parts) < 3 {
		return capabilityKey
	}
	return fmt.Sprintf("%s %s %s", parts[len(parts)-1], parts[0], parts[len(parts)-2])
}
