package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	actiondomain "github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/attestation"
	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/authorization"
	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/governancepolicies"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type ActionTypeReaderPort interface {
	GetByKey(ctx context.Context, tenantID string, key string) (actiondomain.ActionType, error)
}

type CheckRecorderPort interface {
	RecordCheck(ctx context.Context, tenantID string, input domain.NormalizedCheckInput, result domain.CheckResult) (domain.RecordedCheck, error)
}

type ExecutionResultRecorderPort interface {
	RecordExecutionResult(ctx context.Context, tenantID, checkID string, input domain.ExecutionResultInput) (domain.ExecutionResult, error)
}

type RevalidationReaderPort interface {
	GetCheckForRevalidation(context.Context, string, string) (domain.RevalidationRecord, error)
}

type PolicyEvaluatorPort interface {
	Evaluate(context.Context, string, governancepolicies.SafeInput) (governancepolicies.EvaluationResult, error)
}

type FunctionalRoleResolverPort interface {
	EffectiveGrants(context.Context, string, string) ([]authorization.Grant, error)
}

type AuditEmitterPort interface {
	Append(ctx context.Context, tenantID string, in auditdomain.AppendInput) (auditdomain.AuditEvent, error)
}

type UseCases struct {
	actionTypes     ActionTypeReaderPort
	checks          CheckRecorderPort
	results         ExecutionResultRecorderPort
	attestations    *attestation.Verifier
	audit           AuditEmitterPort
	policies        PolicyEvaluatorPort
	revalidation    RevalidationReaderPort
	functionalRoles FunctionalRoleResolverPort
}

func (u *UseCases) SetAttestationVerifier(verifier *attestation.Verifier) { u.attestations = verifier }
func (u *UseCases) SetAuditEmitter(emitter AuditEmitterPort)              { u.audit = emitter }
func (u *UseCases) SetPolicyEvaluator(evaluator PolicyEvaluatorPort)      { u.policies = evaluator }
func (u *UseCases) SetFunctionalRoleResolver(resolver FunctionalRoleResolverPort) {
	u.functionalRoles = resolver
}

func NewUseCases(actionTypes ActionTypeReaderPort, checks ...CheckRecorderPort) *UseCases {
	var recorder CheckRecorderPort
	if len(checks) > 0 {
		recorder = checks[0]
	}
	var results ExecutionResultRecorderPort
	if typed, ok := recorder.(ExecutionResultRecorderPort); ok {
		results = typed
	}
	var revalidation RevalidationReaderPort
	if typed, ok := recorder.(RevalidationReaderPort); ok {
		revalidation = typed
	}
	return &UseCases{actionTypes: actionTypes, checks: recorder, results: results, revalidation: revalidation}
}

func (u *UseCases) Check(ctx context.Context, tenantID string, input domain.CheckInput) (domain.CheckResult, error) {
	normalized, err := domain.NormalizeCheckInput(input)
	if err != nil {
		return domain.CheckResult{}, err
	}
	actionType, err := u.actionTypes.GetByKey(ctx, tenantID, normalized.ActionType)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domain.CheckResult{}, domainerr.Validation("unknown action_type")
		}
		return domain.CheckResult{}, err
	}
	if actionType.RiskClass == actiondomain.RiskClassCritical && normalized.Reason == "" {
		return domain.CheckResult{}, domainerr.Validation("critical actions require break-glass justification")
	}
	if !actionType.Enabled {
		result := denyForDisabledActionType(actionType.RiskClass)
		result.RoleSnapshot = roleSnapshot(normalized)
		result.BindingHash = normalized.BindingHash
		if u.checks != nil {
			recorded, err := u.checks.RecordCheck(ctx, tenantID, normalized, result)
			if err != nil {
				return domain.CheckResult{}, err
			}
			result.CheckID = recorded.CheckID
		}
		return result, nil
	}
	if err := u.resolveFunctionalAuthority(ctx, tenantID, &normalized); err != nil {
		return domain.CheckResult{}, err
	}
	result := decisionForRisk(actionType.RiskClass)
	if u.policies != nil {
		policyResult, err := u.policies.Evaluate(ctx, tenantID, safePolicyInput(normalized, string(actionType.RiskClass)))
		if err != nil {
			return domain.CheckResult{}, err
		}
		result = resultFromPolicies(policyResult)
	}
	result.RoleSnapshot = roleSnapshot(normalized)
	result.BindingHash = normalized.BindingHash
	if u.checks != nil {
		recorded, err := u.checks.RecordCheck(ctx, tenantID, normalized, result)
		if err != nil {
			return domain.CheckResult{}, err
		}
		result.ApprovalID = recorded.ApprovalID
		result.ApprovalStatus = recorded.ApprovalStatus
		result.CheckID = recorded.CheckID
	}
	return result, nil
}

func (u *UseCases) Revalidate(ctx context.Context, tenantID, checkID string, input domain.RevalidationInput) (domain.RevalidationResult, error) {
	if u.revalidation == nil || u.policies == nil {
		return domain.RevalidationResult{}, domainerr.Conflict("governance revalidation is unavailable")
	}
	input.BindingHash = strings.TrimSpace(input.BindingHash)
	if input.BindingHash == "" {
		return domain.RevalidationResult{}, domainerr.Validation("binding_hash is required")
	}
	record, err := u.revalidation.GetCheckForRevalidation(ctx, tenantID, strings.TrimSpace(checkID))
	if err != nil {
		return domain.RevalidationResult{}, err
	}
	if input.BindingHash != record.Input.BindingHash || strings.TrimSpace(input.AuthorityBindingHash) != record.Input.AuthorityBindingHash ||
		input.ScopeRevision != record.Input.ScopeRevision || strings.TrimSpace(input.PolicyRevisionHash) != record.Input.PolicyRevisionHash ||
		strings.TrimSpace(input.DelegationID) != record.Input.DelegationID || input.DelegationRevision != record.Input.DelegationRevision {
		return domain.RevalidationResult{Valid: false, Reason: "authority or binding changed", PolicySnapshotHash: record.PolicySnapshotHash}, nil
	}
	if expected := strings.TrimSpace(input.PolicySnapshotHash); expected != "" && expected != record.PolicySnapshotHash {
		return domain.RevalidationResult{Valid: false, Reason: "approval policy snapshot does not match the check", PolicySnapshotHash: record.PolicySnapshotHash}, nil
	}
	actionType, err := u.actionTypes.GetByKey(ctx, tenantID, record.Input.ActionType)
	if err != nil {
		return domain.RevalidationResult{}, err
	}
	if !actionType.Enabled {
		return domain.RevalidationResult{Valid: false, Reason: "action type is no longer enabled", PolicySnapshotHash: record.PolicySnapshotHash}, nil
	}
	storedRoles := append([]string(nil), record.Input.FunctionalRoles...)
	storedScopes := append([]string(nil), record.Input.FunctionalScopes...)
	if err := u.resolveFunctionalAuthority(ctx, tenantID, &record.Input); err != nil {
		return domain.RevalidationResult{}, err
	}
	if !slices.Equal(storedRoles, record.Input.FunctionalRoles) || !slices.Equal(storedScopes, record.Input.FunctionalScopes) {
		return domain.RevalidationResult{Valid: false, Reason: "functional role authority changed", PolicySnapshotHash: record.PolicySnapshotHash}, nil
	}
	policyResult, err := u.policies.Evaluate(ctx, tenantID, safePolicyInput(record.Input, string(actionType.RiskClass)))
	if err != nil {
		return domain.RevalidationResult{}, err
	}
	current := resultFromPolicies(policyResult)
	storedSnapshot := record.PolicySnapshotHash
	legacyEmptySnapshot := storedSnapshot == "" && policyResult.PolicySnapshotHash == governancepolicies.Hash([]map[string]any{})
	if !legacyEmptySnapshot && storedSnapshot != policyResult.PolicySnapshotHash {
		return domain.RevalidationResult{Valid: false, Reason: "active policy snapshot changed", PolicySnapshotHash: policyResult.PolicySnapshotHash}, nil
	}
	if current.Decision != record.Decision {
		return domain.RevalidationResult{Valid: false, Reason: "governance decision changed", PolicySnapshotHash: policyResult.PolicySnapshotHash}, nil
	}
	return domain.RevalidationResult{Valid: true, Reason: "binding and policy authority remain valid", PolicySnapshotHash: policyResult.PolicySnapshotHash}, nil
}

func (u *UseCases) ReportExecutionResult(ctx context.Context, tenantID, checkID string, input domain.ExecutionResultInput) (domain.ExecutionResult, error) {
	if u.results == nil {
		return domain.ExecutionResult{}, domainerr.Conflict("governance result recorder is not configured")
	}
	attestedResult := input.Result
	if attestedResult == nil {
		attestedResult = map[string]any{}
	}
	normalized, err := domain.NormalizeExecutionResultInput(input)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	if u.attestations == nil {
		return domain.ExecutionResult{}, domainerr.Conflict("executor attestation verifier is not configured")
	}
	if err := u.attestations.Verify(attestation.Payload{
		Version: normalized.AttestationVersion, ExecutorVersion: normalized.ExecutorVersion,
		TenantID: tenantID, GovernanceCheckID: checkID, BindingHash: normalized.BindingHash,
		IdempotencyKey: normalized.IdempotencyKey, Status: normalized.Status,
		DurationMS: normalized.DurationMS, Result: attestedResult,
	}, normalized.Attestation); err != nil {
		return domain.ExecutionResult{}, domainerr.Conflict("executor attestation is invalid")
	}
	result, err := u.results.RecordExecutionResult(ctx, tenantID, checkID, normalized)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	if result.Created {
		u.emitAttestationVerified(ctx, tenantID, normalized, result)
	}
	return result, nil
}

func (u *UseCases) emitAttestationVerified(ctx context.Context, tenantID string, input domain.ExecutionResultInput, result domain.ExecutionResult) {
	if u.audit == nil || result.RequesterID == "" {
		return
	}
	resultHash := result.ResultHash
	if resultHash == "" {
		resultHash, _ = attestation.ResultHash(result.Result)
	}
	attestationSum := sha256.Sum256([]byte(input.Attestation))
	idempotencySum := sha256.Sum256([]byte(input.IdempotencyKey))
	_, err := u.audit.Append(ctx, tenantID, auditdomain.AppendInput{
		VirployeeID: result.RequesterID, SubjectType: "governance_execution", SubjectID: result.ID,
		EventType: auditdomain.EventAttestationVerified, ActorType: "service", ActorID: input.ExecutorVersion,
		Summary: "executor attestation verified",
		Data:    map[string]any{"governance_check_id": result.GovernanceCheckID, "binding_hash": result.BindingHash, "status": result.Status, "duration_ms": result.DurationMS, "attestation_version": input.AttestationVersion, "executor_version": input.ExecutorVersion, "attestation": input.Attestation, "attestation_hash": hex.EncodeToString(attestationSum[:]), "idempotency_key": input.IdempotencyKey, "idempotency_hash": hex.EncodeToString(idempotencySum[:]), "output_hash": resultHash},
	})
	if err != nil {
		slog.ErrorContext(ctx, "append executor attestation audit event", "governance_check_id", result.GovernanceCheckID, "error", err)
	}
}

func decisionForRisk(risk actiondomain.RiskClass) domain.CheckResult {
	result := domain.CheckResult{
		RiskLevel: string(risk),
		Mode:      "simulation",
	}
	switch risk {
	case actiondomain.RiskClassHigh, actiondomain.RiskClassCritical:
		result.Decision = domain.DecisionRequireApproval
		result.Status = domain.StatusPendingApproval
		result.WouldRequireApproval = true
	default:
		result.Decision = domain.DecisionAllow
		result.Status = domain.StatusAllowed
		result.WouldRequireApproval = false
	}
	result.DecisionReason = "No policy matched; default for risk " + result.RiskLevel
	return result
}

func denyForDisabledActionType(risk actiondomain.RiskClass) domain.CheckResult {
	return domain.CheckResult{
		Decision:             domain.DecisionDeny,
		RiskLevel:            string(risk),
		Status:               domain.StatusDenied,
		DecisionReason:       "Action type is disabled",
		WouldRequireApproval: false,
		Mode:                 "simulation",
	}
}

func safePolicyInput(input domain.NormalizedCheckInput, risk string) governancepolicies.SafeInput {
	return governancepolicies.SafeInput{ProductSurface: input.ProductSurface, ActionType: input.ActionType, TargetSystem: input.TargetSystem,
		ResourceType: input.ResourceType, ResourceReference: input.TargetResource, RiskClass: risk, RequesterType: input.RequesterType,
		RequesterID: input.RequesterID, MembershipRole: input.MembershipRole, FunctionalRoles: input.FunctionalRoles,
		FunctionalScopes: input.FunctionalScopes, AuthorityHashes: map[string]string{"authority_binding_hash": input.AuthorityBindingHash,
			"professional_policy_hash": input.PolicyRevisionHash, "binding_hash": input.BindingHash}}
}

func resultFromPolicies(input governancepolicies.EvaluationResult) domain.CheckResult {
	result := decisionForRisk(actiondomain.RiskClass(input.EffectiveRisk))
	if input.Matched {
		switch input.Decision {
		case governancepolicies.EffectDeny:
			result.Decision, result.Status, result.WouldRequireApproval = domain.DecisionDeny, domain.StatusDenied, false
		case governancepolicies.EffectRequireApproval:
			result.Decision, result.Status, result.WouldRequireApproval = domain.DecisionRequireApproval, domain.StatusPendingApproval, true
		case governancepolicies.EffectAllow:
			result.Decision, result.Status, result.WouldRequireApproval = domain.DecisionAllow, domain.StatusAllowed, false
		}
		result.DecisionReason = input.Reason
		result.Mode = "enforced"
	}
	result.RiskLevel = input.EffectiveRisk
	result.PolicySnapshotHash = input.PolicySnapshotHash
	result.PolicyInputHash = input.InputHash
	result.PolicyMatches = make([]map[string]any, 0, len(input.Matches))
	for _, match := range input.Matches {
		raw, _ := json.Marshal(match)
		var value map[string]any
		_ = json.Unmarshal(raw, &value)
		result.PolicyMatches = append(result.PolicyMatches, value)
	}
	return result
}

func roleSnapshot(input domain.NormalizedCheckInput) map[string]any {
	return map[string]any{"membership_role": input.MembershipRole, "functional_roles": input.FunctionalRoles, "functional_scopes": input.FunctionalScopes}
}

func (u *UseCases) resolveFunctionalAuthority(ctx context.Context, tenantID string, input *domain.NormalizedCheckInput) error {
	if input.RequesterType != "human" {
		input.FunctionalRoles = nil
		input.FunctionalScopes = nil
		return nil
	}
	if u.functionalRoles == nil {
		return nil
	}
	grants, err := u.functionalRoles.EffectiveGrants(ctx, tenantID, input.RequesterID)
	if err != nil {
		return fmt.Errorf("resolve functional role authority: %w", err)
	}
	roles := make([]string, 0, len(grants))
	scopes := make([]string, 0, len(grants))
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if _, ok := seen[grant.RoleKey]; !ok {
			seen[grant.RoleKey] = struct{}{}
			roles = append(roles, grant.RoleKey)
		}
		scopes = append(scopes, fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%s", grant.RoleKey, grant.ProductSurface,
			grant.ActionTypePattern, grant.ResourceType, grant.ResourceID, grant.MaxRiskClass, grant.Revision, grant.ID))
	}
	sort.Strings(roles)
	sort.Strings(scopes)
	input.FunctionalRoles = roles
	input.FunctionalScopes = scopes
	return nil
}
