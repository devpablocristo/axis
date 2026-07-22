package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	actiondomain "github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/attestation"
	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
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

type AuditEmitterPort interface {
	Append(ctx context.Context, tenantID string, in auditdomain.AppendInput) (auditdomain.AuditEvent, error)
}

type UseCases struct {
	actionTypes  ActionTypeReaderPort
	checks       CheckRecorderPort
	results      ExecutionResultRecorderPort
	attestations *attestation.Verifier
	audit        AuditEmitterPort
}

func (u *UseCases) SetAttestationVerifier(verifier *attestation.Verifier) { u.attestations = verifier }
func (u *UseCases) SetAuditEmitter(emitter AuditEmitterPort)              { u.audit = emitter }

func NewUseCases(actionTypes ActionTypeReaderPort, checks ...CheckRecorderPort) *UseCases {
	var recorder CheckRecorderPort
	if len(checks) > 0 {
		recorder = checks[0]
	}
	var results ExecutionResultRecorderPort
	if typed, ok := recorder.(ExecutionResultRecorderPort); ok {
		results = typed
	}
	return &UseCases{actionTypes: actionTypes, checks: recorder, results: results}
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
	result := decisionForRisk(actionType.RiskClass)
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
