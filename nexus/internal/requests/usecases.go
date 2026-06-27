package requests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	approvaldomain "github.com/devpablocristo/nexus/internal/approvals/usecases/domain"
	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	"github.com/devpablocristo/nexus/internal/callbacks"
	"github.com/devpablocristo/nexus/internal/contracts"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const DefaultApprovalTTL = time.Hour
const ToolIntentSchemaVersion = "tool_intent.v1"

// Ports definidos en el consumidor (usecases)
type approvalCreator interface {
	Create(ctx context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error)
}

type pendingApprovalCreator interface {
	CreateWithApproval(ctx context.Context, req requestdomain.Request, approval approvaldomain.Approval) (requestdomain.Request, approvaldomain.Approval, error)
}

// PolicyLister es el port mínimo que requests necesita de policies.
type PolicyLister interface {
	ListActive(ctx context.Context, orgID *string) ([]PolicyForEval, error)
}

// ShadowHitRecorder registra hits de shadow policies (best-effort)
type ShadowHitRecorder interface {
	IncrementShadowHits(ctx context.Context, policyID uuid.UUID) error
}

// ActionTypeChecker verifica que un action_type existe y está habilitado
type ActionTypeChecker interface {
	GetByName(ctx context.Context, name string, orgID *string) (ActionTypeInfo, error)
}

// ActionTypeInfo contiene lo que Submit necesita de un action_type
type ActionTypeInfo struct {
	Name               string
	RiskClass          string
	Schema             map[string]any
	RequiresBreakGlass bool
	Enabled            bool
}

// DelegationChecker verifica si un agente tiene delegación para una acción
type DelegationChecker interface {
	CheckDelegation(ctx context.Context, agentID, actionType string, orgID *string) (bool, error)
}

// ApprovalGetter obtiene una approval por ID (para simular decisiones).
type ApprovalGetter interface {
	GetByID(ctx context.Context, id uuid.UUID) (approvaldomain.Approval, error)
}

type ContractValidator interface {
	Validate(ctx context.Context, in contracts.ValidateInput) (contracts.ValidateOutput, error)
}

// PolicyForEval contiene solo los campos necesarios para evaluación.
type PolicyForEval struct {
	ID           uuid.UUID
	Name         string
	ActionType   *string
	TargetSystem *string
	Expression   string
	Effect       string
	RiskOverride *string
	Mode         string // "enforced" o "shadow"
}

// BreakGlassRule define cuándo se activa break-glass (copiado de config domain)
type BreakGlassRule struct {
	ActionTypes       []string
	RiskLevel         string
	RequiredApprovals int
}

// BreakGlassConfig provee las reglas de break-glass
type BreakGlassConfig struct {
	Rules            []BreakGlassRule
	DefaultApprovals int
}

type Usecases struct {
	reqRepo        Repository
	policyRepo     PolicyLister
	approvalRepo   approvalCreator
	pendingCreator pendingApprovalCreator
	idemStore      IdempotencyStore
	audit          AuditSink
	evaluator      *PolicyEvaluator
	riskConfig     RiskConfig
	approvalTTL    time.Duration
	shadowHits     ShadowHitRecorder
	execStats      ExecutionStatsStore
	breakGlassCfg  BreakGlassConfig
	actionTypes    ActionTypeChecker
	delegations    DelegationChecker
	attestations   AttestationStore
	attestVerifier AttestationVerifier
	approvalGetter ApprovalGetter
	approvalEvents callbacks.ApprovalPublisher
	resultReports  ResultReportStore
	contracts      ContractValidator
}

// AttestationVerifier valida criptográficamente la firma + atester antes de
// persistir una attestation. Si no se inyecta, Attest exige al menos
// signature y attester no vacíos pero no verifica criptografía — el caller
// debe decidir si eso es aceptable para su modelo de amenaza.
type AttestationVerifier interface {
	Verify(ctx context.Context, a requestdomain.Attestation) error
}

// Option configura el constructor de Usecases (functional options pattern — Uber).
type Option func(*Usecases)

func WithIdempotencyStore(s IdempotencyStore) Option {
	return func(u *Usecases) { u.idemStore = s }
}

func WithAuditSink(s AuditSink) Option {
	return func(u *Usecases) { u.audit = s }
}

func WithApprovalTTL(d time.Duration) Option {
	return func(u *Usecases) { u.approvalTTL = d }
}

func WithRiskConfig(cfg RiskConfig) Option {
	return func(u *Usecases) { u.riskConfig = cfg }
}

func WithShadowHitRecorder(r ShadowHitRecorder) Option {
	return func(u *Usecases) { u.shadowHits = r }
}

func WithExecutionStats(s ExecutionStatsStore) Option {
	return func(u *Usecases) { u.execStats = s }
}

func WithBreakGlassConfig(cfg BreakGlassConfig) Option {
	return func(u *Usecases) { u.breakGlassCfg = cfg }
}

func WithActionTypeChecker(c ActionTypeChecker) Option {
	return func(u *Usecases) { u.actionTypes = c }
}

func WithDelegationChecker(c DelegationChecker) Option {
	return func(u *Usecases) { u.delegations = c }
}

func WithAttestationStore(s AttestationStore) Option {
	return func(u *Usecases) { u.attestations = s }
}

func WithAttestationVerifier(v AttestationVerifier) Option {
	return func(u *Usecases) { u.attestVerifier = v }
}

func WithApprovalGetter(g ApprovalGetter) Option {
	return func(u *Usecases) { u.approvalGetter = g }
}

func WithApprovalCallbacks(p callbacks.ApprovalPublisher) Option {
	return func(u *Usecases) { u.approvalEvents = p }
}

func WithResultReportStore(s ResultReportStore) Option {
	return func(u *Usecases) { u.resultReports = s }
}

func WithContractValidator(v ContractValidator) Option {
	return func(u *Usecases) { u.contracts = v }
}

// NewUsecases crea Usecases con los 3 repos obligatorios + evaluator, y opciones para el resto.
func NewUsecases(
	reqRepo Repository,
	policyRepo PolicyLister,
	approvalRepo approvalCreator,
	evaluator *PolicyEvaluator,
	opts ...Option,
) *Usecases {
	u := &Usecases{
		reqRepo:      reqRepo,
		policyRepo:   policyRepo,
		approvalRepo: approvalRepo,
		evaluator:    evaluator,
		riskConfig:   DefaultRiskConfig(),
		approvalTTL:  DefaultApprovalTTL,
	}
	if c, ok := reqRepo.(pendingApprovalCreator); ok {
		u.pendingCreator = c
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

type SubmitInput struct {
	IdempotencyKey *string
	RequesterType  string
	RequesterID    string
	RequesterName  string
	ActionType     string
	TargetSystem   string
	TargetResource string
	ActionBinding  map[string]any
	Params         map[string]any
	Reason         string
	Context        string
	// ProductSurface is the product half of the tenant (org x product), taken
	// from the caller's verified JWT claim by the handler. "" => unscoped.
	ProductSurface string
}

type SubmitOutput struct {
	RequestID      uuid.UUID
	Decision       string
	RiskLevel      string
	DecisionReason string
	Status         string
	BindingHash    string
	Approval       *struct {
		ID        uuid.UUID
		ExpiresAt time.Time
	}
	AISummary  string
	AIDegraded bool
}

func (u *Usecases) Submit(ctx context.Context, in SubmitInput) (SubmitOutput, error) {
	now := time.Now().UTC()
	req := requestdomain.Request{
		ID:             uuid.New(),
		OrgID:          orgIDFromParams(in.Params),
		ProductSurface: strings.TrimSpace(in.ProductSurface),
		IdempotencyKey: in.IdempotencyKey,
		RequesterType:  requestdomain.RequesterType(in.RequesterType),
		RequesterID:    in.RequesterID,
		RequesterName:  in.RequesterName,
		ActionType:     in.ActionType,
		TargetSystem:   in.TargetSystem,
		TargetResource: in.TargetResource,
		ActionBinding:  normalizeActionBinding(in.ActionBinding, in.Params),
		Params:         in.Params,
		Reason:         in.Reason,
		Context:        in.Context,
		Status:         requestdomain.StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if req.RequesterType == "" {
		req.RequesterType = requestdomain.RequesterTypeAgent
	}
	if req.Params == nil {
		req.Params = make(map[string]any)
	}
	bindingHash, err := actionBindingHash(req.ActionBinding)
	if err != nil {
		return SubmitOutput{}, err
	}
	req.BindingHash = bindingHash
	if err := validateActionBinding(req); err != nil {
		return SubmitOutput{}, err
	}
	if err := u.validateContract(ctx, "tool_intent", "action_binding", req.ID.String(), req.OrgID, req.ActionBinding); err != nil {
		return SubmitOutput{}, err
	}
	fingerprint := requestFingerprint(req)
	// Idempotencia: si ya existe, retornar respuesta cacheada solo cuando el
	// payload coincide. Reusar una key con otro payload es replay conflictivo.
	if u.idemStore != nil && in.IdempotencyKey != nil && *in.IdempotencyKey != "" {
		if reqID, resp, ok := u.idemStore.Get(ctx, *in.IdempotencyKey); ok {
			if cachedFingerprint := strings.TrimSpace(fmt.Sprint(resp["_request_fingerprint"])); cachedFingerprint != "" && cachedFingerprint != fingerprint {
				return SubmitOutput{}, ErrIdempotencyConflict
			}
			return rebuildOutputFromCache(reqID, resp), nil
		}
	}

	var actionTypeRiskOverride *string
	forceBreakGlass := false
	// Validar action_type si el checker está configurado
	if u.actionTypes != nil {
		atInfo, atErr := u.actionTypes.GetByName(ctx, req.ActionType, req.OrgID)
		if atErr != nil {
			return SubmitOutput{}, fmt.Errorf("unknown action_type: %s", req.ActionType)
		}
		if !atInfo.Enabled {
			return SubmitOutput{}, fmt.Errorf("action_type %s is disabled", req.ActionType)
		}
		if err := validateParamsSchema(req.Params, atInfo.Schema); err != nil {
			return SubmitOutput{}, err
		}
		actionTypeRiskOverride = riskOverrideFromActionType(atInfo.RiskClass)
		forceBreakGlass = atInfo.RequiresBreakGlass
	}

	// Validar delegación si el checker está configurado
	if u.delegations != nil {
		allowed, delErr := u.delegations.CheckDelegation(ctx, req.RequesterID, req.ActionType, req.OrgID)
		if delErr != nil {
			slog.Error("delegation check failed", "error", delErr, "requester_id", req.RequesterID)
		} else if !allowed {
			return SubmitOutput{}, fmt.Errorf("requester %s is not delegated for action %s", req.RequesterID, req.ActionType)
		}
	}

	// Audit: best-effort, nunca falla la request
	logAuditError(
		u.audit.AppendEvent(ctx, req.ID, auditdomain.EventReceived, "requester", req.RequesterID,
			"Request received: "+req.ActionType+" "+req.TargetResource, nil),
		req.ID, auditdomain.EventReceived,
	)

	// Evaluar políticas (filtradas por org del request + globales)
	policyList, err := u.policyRepo.ListActive(ctx, req.OrgID)
	if err != nil {
		return SubmitOutput{}, fmt.Errorf("list active policies: %w", err)
	}

	requestMap := requestToMap(req)
	matched := false
	for _, p := range policyList {
		if (p.ActionType != nil && *p.ActionType != "" && *p.ActionType != req.ActionType) ||
			(p.TargetSystem != nil && *p.TargetSystem != "" && *p.TargetSystem != req.TargetSystem) {
			continue
		}
		match, evalErr := u.evaluator.Matches(p.Expression, requestMap, now)
		if evalErr != nil {
			slog.Error("policy evaluation error", "error", evalErr, "policy_id", p.ID, "expression", p.Expression)
			continue
		}
		if !match {
			continue
		}

		// Shadow mode: loguear + incrementar counter, pero NO actuar
		if p.Mode == "shadow" {
			slog.Info("shadow policy matched",
				"policy_id", p.ID, "policy_name", p.Name,
				"effect", p.Effect, "request_action", req.ActionType,
				"request_id", req.ID)
			if u.shadowHits != nil {
				if err := u.shadowHits.IncrementShadowHits(ctx, p.ID); err != nil {
					slog.Error("increment shadow hits failed", "error", err, "policy_id", p.ID)
				}
			}
			continue // no actúa, sigue buscando policies enforced
		}

		matched = true
		req.RiskLevel = TierRisk(req.ActionType, firstRiskOverride(p.RiskOverride, actionTypeRiskOverride), u.riskConfig)
		dec, ok := DecideFromPolicy(p.Effect, req.RiskLevel)
		if !ok {
			continue
		}
		req.Decision = dec
		req.DecisionReason = "Policy '" + p.Name + "'"
		req.PolicyID = &p.ID
		break
	}
	if !matched {
		req.RiskLevel = TierRisk(req.ActionType, actionTypeRiskOverride, u.riskConfig)
		req.Decision = DefaultDecision(req.RiskLevel)
		req.DecisionReason = "No policy matched; default for risk " + string(req.RiskLevel)
	}

	req.EvaluatedAt = &now
	req.Status = requestdomain.StatusEvaluated

	logAuditError(
		u.audit.AppendEvent(ctx, req.ID, auditdomain.EventEvaluated, "system", "nexus",
			"Risk: "+string(req.RiskLevel)+". "+req.DecisionReason+". Decision: "+string(req.Decision), nil),
		req.ID, auditdomain.EventEvaluated,
	)

	switch req.Decision {
	case requestdomain.DecisionAllow:
		return u.finalizeDecision(ctx, req, in.IdempotencyKey, now, auditdomain.EventAllowed, requestdomain.StatusAllowed, fingerprint)
	case requestdomain.DecisionDeny:
		return u.finalizeDecision(ctx, req, in.IdempotencyKey, now, auditdomain.EventDenied, requestdomain.StatusDenied, fingerprint)
	}

	// Require approval
	return u.handleRequireApproval(ctx, req, in, now, forceBreakGlass, fingerprint)
}

func (u *Usecases) finalizeDecision(ctx context.Context, req requestdomain.Request, idemKey *string, now time.Time, auditEvent string, status requestdomain.RequestStatus, fingerprint string) (SubmitOutput, error) {
	req.Status = status
	req.DecidedAt = &now
	req.UpdatedAt = now

	if _, err := u.reqRepo.Create(ctx, req); err != nil {
		if domainerr.IsConflict(err) {
			return u.rebuildSubmitOutputByIdempotency(ctx, idemKey)
		}
		return SubmitOutput{}, fmt.Errorf("create request: %w", err)
	}

	logAuditError(
		u.audit.AppendEvent(ctx, req.ID, auditEvent, "system", "nexus", "Request "+string(status), nil),
		req.ID, auditEvent,
	)

	out := SubmitOutput{
		RequestID:      req.ID,
		Decision:       string(req.Decision),
		RiskLevel:      string(req.RiskLevel),
		DecisionReason: req.DecisionReason,
		Status:         string(req.Status),
		BindingHash:    req.BindingHash,
	}
	u.cacheIdempotency(ctx, idemKey, req.ID, out, now.Add(24*time.Hour), fingerprint)
	return out, nil
}

func (u *Usecases) handleRequireApproval(ctx context.Context, req requestdomain.Request, in SubmitInput, now time.Time, forceBreakGlass bool, fingerprint string) (SubmitOutput, error) {
	expiresAt := now.Add(u.approvalTTL)

	// AISummary / AIDegraded quedan vacíos: Nexus es AI-independent. Quien necesite
	// el resumen para humanos llama a Companion (nexus-assist/explain) como
	// secondary call al renderizar la card en la console.
	req.Status = requestdomain.StatusPendingApproval
	req.ExpiresAt = &expiresAt
	req.UpdatedAt = now

	// Determinar si es break-glass
	breakGlass, requiredApprovals := u.checkBreakGlass(req.ActionType, string(req.RiskLevel))
	if forceBreakGlass && !breakGlass {
		breakGlass = true
		requiredApprovals = u.breakGlassCfg.DefaultApprovals
		if requiredApprovals <= 1 {
			requiredApprovals = 2
		}
	}

	// Crear approval después (referencia a request existente)
	approval := approvaldomain.Approval{
		ID:                uuid.New(),
		OrgID:             req.OrgID,
		RequestID:         req.ID,
		Status:            approvaldomain.ApprovalStatusPending,
		ExpiresAt:         expiresAt,
		CreatedAt:         now,
		BreakGlass:        breakGlass,
		RequiredApprovals: requiredApprovals,
	}
	if u.pendingCreator != nil {
		createdReq, createdApproval, err := u.pendingCreator.CreateWithApproval(ctx, req, approval)
		if err != nil {
			if domainerr.IsConflict(err) {
				return u.rebuildSubmitOutputByIdempotency(ctx, in.IdempotencyKey)
			}
			return SubmitOutput{}, fmt.Errorf("create request approval: %w", err)
		}
		req = createdReq
		approval = createdApproval
	} else {
		// Fallback para fakes/tests: producción inyecta CreateWithApproval.
		if _, err := u.reqRepo.Create(ctx, req); err != nil {
			if domainerr.IsConflict(err) {
				return u.rebuildSubmitOutputByIdempotency(ctx, in.IdempotencyKey)
			}
			return SubmitOutput{}, fmt.Errorf("create request: %w", err)
		}
		createdApproval, err := u.approvalRepo.Create(ctx, approval)
		if err != nil {
			return SubmitOutput{}, fmt.Errorf("create approval: %w", err)
		}
		approval = createdApproval
		req.ApprovalID = &approval.ID
		if _, err := u.reqRepo.Update(ctx, req); err != nil {
			slog.Error("update request with approval_id failed", "error", err, "request_id", req.ID)
		}
	}

	logAuditError(
		u.audit.AppendEvent(ctx, req.ID, auditdomain.EventSentToApproval, "system", "nexus",
			"Sent to approval. Expires at "+expiresAt.Format(time.RFC3339), nil),
		req.ID, auditdomain.EventSentToApproval,
	)
	u.emitApprovalCallback(ctx, callbacks.ApprovalEvent{
		Event:          callbacks.EventApprovalPending,
		ApprovalID:     approval.ID.String(),
		OrgID:          stringOrEmpty(req.OrgID),
		RequestID:      req.ID.String(),
		Decision:       string(approval.Status),
		ActionType:     req.ActionType,
		TargetResource: req.TargetResource,
		Reason:         req.Reason,
		RiskLevel:      string(req.RiskLevel),
		AISummary:      stringPtrOrNil(strings.TrimSpace(req.AISummary)),
		CreatedAt:      approval.CreatedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:      timePtrRFC3339(&approval.ExpiresAt),
	})

	out := SubmitOutput{
		RequestID:      req.ID,
		Decision:       string(req.Decision),
		RiskLevel:      string(req.RiskLevel),
		DecisionReason: req.DecisionReason,
		Status:         string(req.Status),
		BindingHash:    req.BindingHash,
		Approval: &struct {
			ID        uuid.UUID
			ExpiresAt time.Time
		}{ID: approval.ID, ExpiresAt: approval.ExpiresAt},
		AISummary:  req.AISummary,
		AIDegraded: req.AIDegraded,
	}
	u.cacheIdempotency(ctx, in.IdempotencyKey, req.ID, out, expiresAt, fingerprint)
	return out, nil
}

func (u *Usecases) emitApprovalCallback(ctx context.Context, event callbacks.ApprovalEvent) {
	if u.approvalEvents == nil {
		return
	}
	if err := u.approvalEvents.Publish(ctx, event); err != nil {
		slog.Error("approval callback publish failed", "event", event.Event, "request_id", event.RequestID, "error", err)
	}
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func orgIDFromParams(params map[string]any) *string {
	if len(params) == 0 {
		return nil
	}
	raw, ok := params["org_id"]
	if !ok {
		return nil
	}
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "" {
		return nil
	}
	return &value
}

func normalizeActionBinding(input map[string]any, params map[string]any) map[string]any {
	if len(input) > 0 {
		return cloneStringAnyMap(input)
	}
	if len(params) == 0 {
		return nil
	}
	raw, ok := params["action_binding"]
	if !ok {
		return nil
	}
	binding, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return cloneStringAnyMap(binding)
}

func cloneStringAnyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func actionBindingHash(binding map[string]any) (string, error) {
	if len(binding) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return "", fmt.Errorf("marshal action binding: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func requestFingerprint(req requestdomain.Request) string {
	var orgID string
	if req.OrgID != nil {
		orgID = strings.TrimSpace(*req.OrgID)
	}
	raw, err := json.Marshal(map[string]any{
		"org_id":          orgID,
		"requester_type":  req.RequesterType,
		"requester_id":    req.RequesterID,
		"requester_name":  req.RequesterName,
		"action_type":     req.ActionType,
		"target_system":   req.TargetSystem,
		"target_resource": req.TargetResource,
		"action_binding":  req.ActionBinding,
		"params":          req.Params,
		"reason":          req.Reason,
		"context":         req.Context,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validateActionBinding(req requestdomain.Request) error {
	if len(req.ActionBinding) == 0 {
		return nil
	}
	required := []string{
		"schema_version", "org_id", "actor_id", "actor_type", "product_surface",
		"run_id", "tool_invocation_id", "connector_id", "capability_id",
		"operation", "target_system", "target_resource", "payload_hash",
		"idempotency_key",
	}
	for _, key := range required {
		if strings.TrimSpace(fmt.Sprint(req.ActionBinding[key])) == "" {
			return fmt.Errorf("action_binding.%s is required", key)
		}
	}
	if version := strings.TrimSpace(fmt.Sprint(req.ActionBinding["schema_version"])); version != ToolIntentSchemaVersion {
		return fmt.Errorf("action_binding.schema_version must be %s", ToolIntentSchemaVersion)
	}
	if req.OrgID == nil || strings.TrimSpace(*req.OrgID) == "" {
		return fmt.Errorf("action_binding requires request org_id")
	}
	if bindingOrg := strings.TrimSpace(fmt.Sprint(req.ActionBinding["org_id"])); bindingOrg != strings.TrimSpace(*req.OrgID) {
		return fmt.Errorf("action_binding.org_id does not match request org_id")
	}
	if bindingTarget := strings.TrimSpace(fmt.Sprint(req.ActionBinding["target_system"])); req.TargetSystem != "" && bindingTarget != "" && bindingTarget != req.TargetSystem {
		return fmt.Errorf("action_binding.target_system does not match request target_system")
	}
	if bindingTarget := strings.TrimSpace(fmt.Sprint(req.ActionBinding["target_resource"])); req.TargetResource != "" && bindingTarget != "" && bindingTarget != req.TargetResource {
		return fmt.Errorf("action_binding.target_resource does not match request target_resource")
	}
	return nil
}

func (u *Usecases) validateContract(ctx context.Context, name, subjectType, subjectID string, orgID *string, payload map[string]any) error {
	if u.contracts == nil || len(payload) == 0 {
		return nil
	}
	_, err := u.contracts.Validate(ctx, contracts.ValidateInput{
		OrgID:       orgID,
		Name:        name,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Payload:     payload,
	})
	return err
}

func timePtrRFC3339(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func (u *Usecases) cacheIdempotency(ctx context.Context, idemKey *string, reqID uuid.UUID, out SubmitOutput, expiresAt time.Time, requestFingerprint string) {
	if u.idemStore == nil || idemKey == nil || *idemKey == "" {
		return
	}
	resp := map[string]any{
		"request_id": reqID.String(), "decision": out.Decision,
		"risk_level": out.RiskLevel, "status": out.Status,
		"binding_hash": out.BindingHash,
		"ai_summary":   out.AISummary, "ai_degraded": out.AIDegraded,
	}
	if requestFingerprint != "" {
		resp["_request_fingerprint"] = requestFingerprint
	}
	if out.Approval != nil {
		resp["approval"] = map[string]any{
			"id":         out.Approval.ID.String(),
			"expires_at": out.Approval.ExpiresAt.Format(time.RFC3339),
		}
	}
	if err := u.idemStore.Set(ctx, *idemKey, reqID, resp, expiresAt); err != nil {
		slog.Error("idempotency store set failed", "error", err, "key", *idemKey)
	}
}

func (u *Usecases) rebuildSubmitOutputByIdempotency(ctx context.Context, idemKey *string) (SubmitOutput, error) {
	if idemKey == nil || strings.TrimSpace(*idemKey) == "" {
		return SubmitOutput{}, ErrIdempotencyConflict
	}
	for i := 0; i < 20; i++ {
		existing, err := u.reqRepo.GetByIdempotencyKey(ctx, strings.TrimSpace(*idemKey))
		if err != nil {
			return SubmitOutput{}, fmt.Errorf("get request by idempotency key: %w", err)
		}
		if existing != nil {
			if existing.Status != requestdomain.StatusPendingApproval || existing.ApprovalID != nil || i == 19 {
				return submitOutputFromRequest(*existing), nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return SubmitOutput{}, ErrIdempotencyConflict
}

func submitOutputFromRequest(req requestdomain.Request) SubmitOutput {
	out := SubmitOutput{
		RequestID:      req.ID,
		Decision:       string(req.Decision),
		RiskLevel:      string(req.RiskLevel),
		DecisionReason: req.DecisionReason,
		Status:         string(req.Status),
		BindingHash:    req.BindingHash,
		AISummary:      req.AISummary,
		AIDegraded:     req.AIDegraded,
	}
	if req.ApprovalID != nil {
		expiresAt := time.Time{}
		if req.ExpiresAt != nil {
			expiresAt = *req.ExpiresAt
		}
		out.Approval = &struct {
			ID        uuid.UUID
			ExpiresAt time.Time
		}{ID: *req.ApprovalID, ExpiresAt: expiresAt}
	}
	return out
}

func rebuildOutputFromCache(reqID uuid.UUID, resp map[string]any) SubmitOutput {
	out := SubmitOutput{RequestID: reqID}
	if id, ok := resp["request_id"].(string); ok && id != "" {
		parsed, err := uuid.Parse(id)
		if err == nil {
			out.RequestID = parsed
		}
	}
	if v, ok := resp["decision"].(string); ok {
		out.Decision = v
	}
	if v, ok := resp["risk_level"].(string); ok {
		out.RiskLevel = v
	}
	if v, ok := resp["status"].(string); ok {
		out.Status = v
	}
	if v, ok := resp["binding_hash"].(string); ok {
		out.BindingHash = v
	}
	if v, ok := resp["ai_summary"].(string); ok {
		out.AISummary = v
	}
	if v, ok := resp["ai_degraded"].(bool); ok {
		out.AIDegraded = v
	}
	if a, ok := resp["approval"].(map[string]any); ok {
		out.Approval = &struct {
			ID        uuid.UUID
			ExpiresAt time.Time
		}{}
		if id, ok := a["id"].(string); ok && id != "" {
			parsed, err := uuid.Parse(id)
			if err == nil {
				out.Approval.ID = parsed
			}
		}
		if exp, ok := a["expires_at"].(string); ok && exp != "" {
			parsed, err := time.Parse(time.RFC3339, exp)
			if err == nil {
				out.Approval.ExpiresAt = parsed
			}
		}
	}
	return out
}

// checkBreakGlass determina si una request requiere break-glass y cuántos aprobadores
func (u *Usecases) checkBreakGlass(actionType, riskLevel string) (bool, int) {
	for _, rule := range u.breakGlassCfg.Rules {
		// Verificar action type match
		actionMatch := len(rule.ActionTypes) == 0
		for _, at := range rule.ActionTypes {
			if at == actionType {
				actionMatch = true
				break
			}
		}
		if !actionMatch {
			continue
		}
		// Verificar risk level match
		if rule.RiskLevel != "" && rule.RiskLevel != riskLevel {
			continue
		}
		// Matcheó — break-glass activado
		approvals := rule.RequiredApprovals
		if approvals <= 1 {
			approvals = u.breakGlassCfg.DefaultApprovals
		}
		if approvals <= 1 {
			approvals = 2
		}
		return true, approvals
	}
	return false, 1
}

func riskOverrideFromActionType(riskClass string) *string {
	riskClass = strings.TrimSpace(strings.ToLower(riskClass))
	switch riskClass {
	case "critical", "high":
		v := "high"
		return &v
	case "medium":
		v := "medium"
		return &v
	case "low":
		v := "low"
		return &v
	default:
		return nil
	}
}

func firstRiskOverride(values ...*string) *string {
	for _, value := range values {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed != "" {
			return value
		}
	}
	return nil
}

func validateParamsSchema(params map[string]any, schema map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	errs := contracts.ValidateSchema(params, schema)
	if len(errs) == 0 {
		return nil
	}
	for _, msg := range errs {
		if strings.HasSuffix(msg, " is required") {
			key := strings.TrimSuffix(msg, " is required")
			return fmt.Errorf("missing required param %q for action_type", key)
		}
	}
	return fmt.Errorf("action_type schema validation failed: %s", strings.Join(errs, "; "))
}

func requestToMap(r requestdomain.Request) map[string]any {
	params := r.Params
	if params == nil {
		params = make(map[string]any)
	}
	return map[string]any{
		"action_type":     r.ActionType,
		"target_system":   r.TargetSystem,
		"target_resource": r.TargetResource,
		"org_id":          stringOrEmpty(r.OrgID),
		"action_binding":  r.ActionBinding,
		"binding_hash":    r.BindingHash,
		"params":          params,
		"reason":          r.Reason,
		"context":         r.Context,
		"requester_type":  string(r.RequesterType),
		"requester_id":    r.RequesterID,
	}
}

// SimulateOutput es el resultado de una simulación (dry-run)
type SimulateOutput struct {
	Decision             string          `json:"decision"`
	RiskLevel            string          `json:"risk_level"`
	DecisionReason       string          `json:"decision_reason"`
	Status               string          `json:"status"`
	PolicyMatched        *string         `json:"policy_matched,omitempty"`
	RiskAssessment       *RiskAssessment `json:"risk_assessment"`
	WouldRequireApproval bool            `json:"would_require_approval"`
	AISummary            string          `json:"ai_summary,omitempty"`
}

// Simulate evalúa una request sin persistir nada. Dry-run puro.
func (u *Usecases) Simulate(ctx context.Context, in SubmitInput) (SimulateOutput, error) {
	now := time.Now().UTC()

	req := requestdomain.Request{
		OrgID:          orgIDFromParams(in.Params),
		RequesterType:  requestdomain.RequesterType(in.RequesterType),
		RequesterID:    in.RequesterID,
		ActionType:     in.ActionType,
		TargetSystem:   in.TargetSystem,
		TargetResource: in.TargetResource,
		ActionBinding:  normalizeActionBinding(in.ActionBinding, in.Params),
		Params:         in.Params,
		Reason:         in.Reason,
		Context:        in.Context,
	}
	if req.RequesterType == "" {
		req.RequesterType = requestdomain.RequesterTypeAgent
	}
	if req.Params == nil {
		req.Params = make(map[string]any)
	}
	bindingHash, err := actionBindingHash(req.ActionBinding)
	if err != nil {
		return SimulateOutput{}, err
	}
	req.BindingHash = bindingHash
	if err := validateActionBinding(req); err != nil {
		return SimulateOutput{}, err
	}
	if err := u.validateContract(ctx, "tool_intent", "action_binding", "simulate", req.OrgID, req.ActionBinding); err != nil {
		return SimulateOutput{}, err
	}
	var actionTypeRiskOverride *string
	if u.actionTypes != nil {
		atInfo, atErr := u.actionTypes.GetByName(ctx, req.ActionType, req.OrgID)
		if atErr != nil {
			return SimulateOutput{}, fmt.Errorf("unknown action_type: %s", req.ActionType)
		}
		if !atInfo.Enabled {
			return SimulateOutput{}, fmt.Errorf("action_type %s is disabled", req.ActionType)
		}
		if err := validateParamsSchema(req.Params, atInfo.Schema); err != nil {
			return SimulateOutput{}, err
		}
		actionTypeRiskOverride = riskOverrideFromActionType(atInfo.RiskClass)
	}

	// Evaluar políticas (misma lógica que Submit, filtradas por org)
	policyList, err := u.policyRepo.ListActive(ctx, req.OrgID)
	if err != nil {
		return SimulateOutput{}, fmt.Errorf("list active policies: %w", err)
	}

	requestMap := requestToMap(req)
	matched := false
	var matchedPolicyName string
	var policyRiskOverride *string

	for _, p := range policyList {
		if (p.ActionType != nil && *p.ActionType != "" && *p.ActionType != req.ActionType) ||
			(p.TargetSystem != nil && *p.TargetSystem != "" && *p.TargetSystem != req.TargetSystem) {
			continue
		}
		match, evalErr := u.evaluator.Matches(p.Expression, requestMap, now)
		if evalErr != nil {
			continue
		}
		if !match {
			continue
		}
		matched = true
		matchedPolicyName = p.Name
		policyRiskOverride = firstRiskOverride(p.RiskOverride, actionTypeRiskOverride)
		req.RiskLevel = TierRisk(req.ActionType, policyRiskOverride, u.riskConfig)
		dec, ok := DecideFromPolicy(p.Effect, req.RiskLevel)
		if !ok {
			continue
		}
		req.Decision = dec
		req.DecisionReason = "Policy '" + p.Name + "'"
		break
	}
	if !matched {
		req.RiskLevel = TierRisk(req.ActionType, actionTypeRiskOverride, u.riskConfig)
		req.Decision = DefaultDecision(req.RiskLevel)
		req.DecisionReason = "No policy matched; default for risk " + string(req.RiskLevel)
	}

	// Obtener success rate del feedback loop (best-effort)
	simSuccessRate := -1.0
	if u.execStats != nil {
		stats, statsErr := u.execStats.GetByActionType(ctx, req.ActionType)
		if statsErr == nil {
			simSuccessRate = stats.SuccessRate()
		}
	}

	// Calcular assessment completo de cascada
	signals := RiskSignals{
		ActionType:    req.ActionType,
		TargetSystem:  req.TargetSystem,
		RequesterID:   req.RequesterID,
		RequesterType: string(req.RequesterType),
		CurrentHour:   now.Hour(),
		SuccessRate:   simSuccessRate,
	}
	_, assessment := TierRiskFromSignals(signals, u.riskConfig, policyRiskOverride)

	// Determinar status que tendría
	var status string
	switch req.Decision {
	case requestdomain.DecisionAllow:
		status = string(requestdomain.StatusAllowed)
	case requestdomain.DecisionDeny:
		status = string(requestdomain.StatusDenied)
	default:
		status = string(requestdomain.StatusPendingApproval)
	}

	out := SimulateOutput{
		Decision:             string(req.Decision),
		RiskLevel:            string(req.RiskLevel),
		DecisionReason:       req.DecisionReason,
		Status:               status,
		RiskAssessment:       &assessment,
		WouldRequireApproval: req.Decision == requestdomain.DecisionRequireApproval,
	}
	if matched {
		out.PolicyMatched = &matchedPolicyName
	}

	return out, nil
}

// BatchSimulateInput es el input para simular múltiples requests a la vez.
type BatchSimulateInput struct {
	Requests []SubmitInput
}

// BatchSimulateOutput contiene los resultados agregados y por request.
type BatchSimulateOutput struct {
	Total           int                 `json:"total"`
	Allowed         int                 `json:"allowed"`
	Denied          int                 `json:"denied"`
	RequireApproval int                 `json:"require_approval"`
	ByRisk          map[string]int      `json:"by_risk"`
	Results         []BatchSimulateItem `json:"results"`
}

// BatchSimulateItem es el resultado de una simulación individual dentro de un batch.
type BatchSimulateItem struct {
	ActionType     string  `json:"action_type"`
	RequesterID    string  `json:"requester_id"`
	TargetSystem   string  `json:"target_system"`
	Decision       string  `json:"decision"`
	RiskLevel      string  `json:"risk_level"`
	DecisionReason string  `json:"decision_reason"`
	PolicyMatched  *string `json:"policy_matched,omitempty"`
}

// BatchSimulate ejecuta múltiples simulaciones y agrega resultados.
func (u *Usecases) BatchSimulate(ctx context.Context, in BatchSimulateInput) (BatchSimulateOutput, error) {
	out := BatchSimulateOutput{
		ByRisk:  make(map[string]int),
		Results: make([]BatchSimulateItem, 0, len(in.Requests)),
	}

	for _, req := range in.Requests {
		simOut, err := u.Simulate(ctx, req)
		if err != nil {
			// Registrar error pero continuar con el resto
			out.Results = append(out.Results, BatchSimulateItem{
				ActionType:     req.ActionType,
				RequesterID:    req.RequesterID,
				TargetSystem:   req.TargetSystem,
				Decision:       "error",
				DecisionReason: "simulation failed",
			})
			out.Total++
			continue
		}

		out.Total++
		switch simOut.Decision {
		case "allow":
			out.Allowed++
		case "deny":
			out.Denied++
		case "require_approval":
			out.RequireApproval++
		}
		out.ByRisk[simOut.RiskLevel]++

		out.Results = append(out.Results, BatchSimulateItem{
			ActionType:     req.ActionType,
			RequesterID:    req.RequesterID,
			TargetSystem:   req.TargetSystem,
			Decision:       simOut.Decision,
			RiskLevel:      simOut.RiskLevel,
			DecisionReason: simOut.DecisionReason,
			PolicyMatched:  simOut.PolicyMatched,
		})
	}

	return out, nil
}

// ApprovalSimulateInput es el input para simular qué pasa si se aprueba/rechaza una request.
type ApprovalSimulateInput struct {
	RequestID  uuid.UUID
	Action     string // "approve" o "reject"
	ApproverID string
}

// ApprovalSimulateOutput muestra el estado resultante sin ejecutar.
type ApprovalSimulateOutput struct {
	CurrentStatus     string `json:"current_status"`
	SimulatedStatus   string `json:"simulated_status"`
	BreakGlass        bool   `json:"break_glass"`
	RequiredApprovals int    `json:"required_approvals"`
	CurrentApprovals  int    `json:"current_approvals"`
	AfterApprovals    int    `json:"after_approvals"`
	WouldFinalize     bool   `json:"would_finalize"`
	AlreadyDecided    bool   `json:"already_decided"`
	Reason            string `json:"reason"`
}

// SimulateApproval simula qué pasa si un aprobador aprueba o rechaza una request pendiente.
func (u *Usecases) SimulateApproval(ctx context.Context, in ApprovalSimulateInput) (ApprovalSimulateOutput, error) {
	req, err := u.reqRepo.GetByID(ctx, in.RequestID)
	if err != nil {
		return ApprovalSimulateOutput{}, fmt.Errorf("get request: %w", err)
	}

	if req.Status != requestdomain.StatusPendingApproval {
		return ApprovalSimulateOutput{
			CurrentStatus:   string(req.Status),
			SimulatedStatus: string(req.Status),
			Reason:          "request is not pending approval",
		}, nil
	}

	if req.ApprovalID == nil {
		return ApprovalSimulateOutput{
			CurrentStatus:   string(req.Status),
			SimulatedStatus: string(req.Status),
			Reason:          "no approval linked to this request",
		}, nil
	}

	// Necesitamos leer la approval — usamos el approvalRepo
	// Para esto necesitamos un port nuevo, o reusar el existente
	// Simplificación: accedemos via el approvalReader del wire
	out := ApprovalSimulateOutput{
		CurrentStatus: string(req.Status),
	}

	// Obtener approval info. Usamos la interfaz approvalGetter.
	if u.approvalGetter == nil {
		return ApprovalSimulateOutput{
			CurrentStatus:   string(req.Status),
			SimulatedStatus: string(req.Status),
			Reason:          "approval reader not configured",
		}, nil
	}

	approval, err := u.approvalGetter.GetByID(ctx, *req.ApprovalID)
	if err != nil {
		return ApprovalSimulateOutput{}, fmt.Errorf("get approval: %w", err)
	}

	out.BreakGlass = approval.BreakGlass
	out.RequiredApprovals = approval.RequiredApprovals
	out.CurrentApprovals = countApprovals(approval.Decisions)

	// Verificar si este approver ya decidió
	for _, d := range approval.Decisions {
		if d.ApproverID == in.ApproverID {
			out.AlreadyDecided = true
			out.SimulatedStatus = string(req.Status)
			out.AfterApprovals = out.CurrentApprovals
			out.Reason = "approver already decided on this request"
			return out, nil
		}
	}

	if in.Action == "reject" {
		// Un rechazo siempre finaliza
		out.SimulatedStatus = "rejected"
		out.AfterApprovals = out.CurrentApprovals
		out.WouldFinalize = true
		out.Reason = "reject always finalizes the approval chain"
		return out, nil
	}

	// Approve
	out.AfterApprovals = out.CurrentApprovals + 1
	if out.AfterApprovals >= out.RequiredApprovals {
		out.SimulatedStatus = "approved"
		out.WouldFinalize = true
		out.Reason = fmt.Sprintf("quorum reached: %d/%d approvals", out.AfterApprovals, out.RequiredApprovals)
	} else {
		out.SimulatedStatus = "pending_approval"
		out.WouldFinalize = false
		out.Reason = fmt.Sprintf("partial: %d/%d approvals (need %d more)", out.AfterApprovals, out.RequiredApprovals, out.RequiredApprovals-out.AfterApprovals)
	}

	return out, nil
}

func countApprovals(decisions []approvaldomain.ApprovalDecision) int {
	count := 0
	for _, d := range decisions {
		if d.Action == "approve" {
			count++
		}
	}
	return count
}

// ReplaySimulateInput es el input para re-evaluar requests históricas contra una policy propuesta
type ReplaySimulateInput struct {
	Expression string // expresión CEL a probar
	Effect     string // allow, deny, require_approval
	Limit      int    // máx requests a evaluar
}

// ReplaySimulateOutput es el resultado de una replay simulation
type ReplaySimulateOutput struct {
	SimulationID    string                 `json:"simulation_id,omitempty"`
	ReportHash      string                 `json:"report_hash,omitempty"`
	SnapshotQuality string                 `json:"snapshot_quality,omitempty"`
	TotalEvaluated  int                    `json:"total_evaluated"`
	WouldMatch      int                    `json:"would_match"`
	WouldAllow      int                    `json:"would_allow"`
	WouldDeny       int                    `json:"would_deny"`
	WouldRequire    int                    `json:"would_require_approval"`
	Samples         []ReplaySimulateSample `json:"samples"`
}

type ReplaySimulateSample struct {
	RequestID      string `json:"request_id"`
	ActionType     string `json:"action_type"`
	RequesterID    string `json:"requester_id"`
	TargetSystem   string `json:"target_system"`
	OriginalStatus string `json:"original_status"`
	WouldDecide    string `json:"would_decide"`
	Changed        bool   `json:"changed"`
}

// ReplaySimulate re-evalúa requests históricas contra una expresión CEL propuesta
func (u *Usecases) ReplaySimulate(ctx context.Context, in ReplaySimulateInput) (ReplaySimulateOutput, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	// ReplaySimulate es operación admin (cross-org): no aplicar filtro de tenancy.
	reqs, err := u.reqRepo.List(ctx, "", "", limit, nil, true)
	if err != nil {
		return ReplaySimulateOutput{}, fmt.Errorf("list requests for replay: %w", err)
	}

	out := ReplaySimulateOutput{
		Samples: make([]ReplaySimulateSample, 0),
	}

	for _, req := range reqs {
		out.TotalEvaluated++
		reqMap := requestToMap(req)
		match, evalErr := u.evaluator.Matches(in.Expression, reqMap, req.CreatedAt)
		if evalErr != nil {
			continue
		}
		if !match {
			continue
		}

		out.WouldMatch++
		risk := TierRisk(req.ActionType, nil, u.riskConfig)
		decision, ok := DecideFromPolicy(in.Effect, risk)
		if !ok {
			continue
		}

		switch decision {
		case requestdomain.DecisionAllow:
			out.WouldAllow++
		case requestdomain.DecisionDeny:
			out.WouldDeny++
		case requestdomain.DecisionRequireApproval:
			out.WouldRequire++
		}

		changed := string(decision) != string(req.Decision)

		if len(out.Samples) < 20 {
			out.Samples = append(out.Samples, ReplaySimulateSample{
				RequestID:      req.ID.String(),
				ActionType:     req.ActionType,
				RequesterID:    req.RequesterID,
				TargetSystem:   req.TargetSystem,
				OriginalStatus: string(req.Status),
				WouldDecide:    string(decision),
				Changed:        changed,
			})
		}
	}
	out.SnapshotQuality = "legacy"
	if recorder, ok := u.reqRepo.(interface {
		RecordPolicySimulation(context.Context, ReplaySimulateInput, ReplaySimulateOutput) (string, string, error)
	}); ok {
		id, hash, err := recorder.RecordPolicySimulation(ctx, in, out)
		if err != nil {
			return ReplaySimulateOutput{}, fmt.Errorf("record policy simulation: %w", err)
		}
		out.SimulationID = id
		out.ReportHash = hash
	}
	return out, nil
}

func (u *Usecases) GetByID(ctx context.Context, id uuid.UUID) (requestdomain.Request, error) {
	return u.reqRepo.GetByID(ctx, id)
}

// List lista requests con filtro de tenancy. Ver Repository.List para semántica
// de orgID/allowAll.
func (u *Usecases) List(ctx context.Context, status, actionType string, limit int, orgID *string, allowAll bool) ([]requestdomain.Request, error) {
	return u.reqRepo.List(ctx, status, actionType, limit, orgID, allowAll)
}

type ReportResultInput struct {
	ResultKey    string
	ActorID      string
	OrgID        *string
	Success      bool
	Result       map[string]any
	DurationMs   int64
	ErrorMessage string
}

func (u *Usecases) ReportResult(ctx context.Context, requestID uuid.UUID, in ReportResultInput) error {
	if in.Result == nil {
		in.Result = make(map[string]any)
	}
	sanitizedResult := sanitizeResultPayload(in.Result)
	resultHash, err := resultPayloadHash(ReportResultInput{
		Success:      in.Success,
		Result:       sanitizedResult,
		DurationMs:   in.DurationMs,
		ErrorMessage: in.ErrorMessage,
	})
	if err != nil {
		return err
	}
	if u.resultReports != nil && strings.TrimSpace(in.ResultKey) != "" {
		existing, ok, err := u.resultReports.Get(ctx, requestID, strings.TrimSpace(in.ResultKey))
		if err != nil {
			return fmt.Errorf("get result report: %w", err)
		}
		if ok {
			if existing.PayloadHash == resultHash {
				return nil
			}
			return ErrIdempotencyConflict
		}
	}

	req, err := u.reqRepo.GetByID(ctx, requestID)
	if err != nil {
		return fmt.Errorf("get request: %w", err)
	}
	if in.OrgID != nil {
		if req.OrgID == nil || strings.TrimSpace(*in.OrgID) != strings.TrimSpace(*req.OrgID) {
			return domainerr.TenantMismatch()
		}
	}
	if err := u.validateContract(ctx, "result_report", "result_report", requestID.String(), req.OrgID, map[string]any{
		"result_id":     strings.TrimSpace(in.ResultKey),
		"success":       in.Success,
		"result":        sanitizedResult,
		"duration_ms":   in.DurationMs,
		"error_message": strings.TrimSpace(in.ErrorMessage),
	}); err != nil {
		return err
	}
	if req.Status != requestdomain.StatusAllowed && req.Status != requestdomain.StatusApproved {
		return ErrInvalidState
	}
	now := time.Now().UTC()
	if u.resultReports != nil && strings.TrimSpace(in.ResultKey) != "" {
		_, err := u.resultReports.Save(ctx, ResultReport{
			ID:           uuid.New(),
			RequestID:    requestID,
			ResultKey:    strings.TrimSpace(in.ResultKey),
			ActorID:      strings.TrimSpace(in.ActorID),
			OrgID:        req.OrgID,
			Success:      in.Success,
			Result:       sanitizedResult,
			ErrorMessage: in.ErrorMessage,
			DurationMs:   in.DurationMs,
			PayloadHash:  resultHash,
			CreatedAt:    now,
		})
		if err != nil {
			if existing, ok, getErr := u.resultReports.Get(ctx, requestID, strings.TrimSpace(in.ResultKey)); getErr == nil && ok {
				if existing.PayloadHash == resultHash {
					return nil
				}
			}
			return err
		}
	}
	req.UpdatedAt = now
	req.ExecutedAt = &now
	if in.Success {
		req.Status = requestdomain.StatusExecuted
		req.ExecutionResult = sanitizedResult
		logAuditError(
			u.audit.AppendEvent(ctx, requestID, auditdomain.EventExecuted, "requester", req.RequesterID,
				"Executed successfully", map[string]any{"result": sanitizedResult, "duration_ms": in.DurationMs}),
			requestID, auditdomain.EventExecuted,
		)
		// Feedback loop: registrar éxito para ajustar riesgo futuro
		if u.execStats != nil {
			if err := u.execStats.RecordSuccess(ctx, req.ActionType); err != nil {
				slog.Error("record execution success failed", "error", err, "action_type", req.ActionType)
			}
		}
	} else {
		req.Status = requestdomain.StatusFailed
		req.ErrorMessage = in.ErrorMessage
		logAuditError(
			u.audit.AppendEvent(ctx, requestID, auditdomain.EventExecutionFailed, "requester", req.RequesterID,
				in.ErrorMessage, nil),
			requestID, auditdomain.EventExecutionFailed,
		)
		// Feedback loop: registrar fallo para ajustar riesgo futuro
		if u.execStats != nil {
			if err := u.execStats.RecordFailure(ctx, req.ActionType); err != nil {
				slog.Error("record execution failure failed", "error", err, "action_type", req.ActionType)
			}
		}
	}
	if _, err := u.reqRepo.Update(ctx, req); err != nil {
		return fmt.Errorf("update request: %w", err)
	}
	return nil
}

func resultPayloadHash(in ReportResultInput) (string, error) {
	payload := map[string]any{
		"success":       in.Success,
		"result":        sanitizeResultPayload(in.Result),
		"error_message": strings.TrimSpace(in.ErrorMessage),
		"duration_ms":   in.DurationMs,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal result report hash payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func sanitizeResultPayload(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if isSensitiveResultKey(key) {
			out[key] = "***"
			continue
		}
		out[key] = sanitizeResultValue(value)
	}
	return out
}

func sanitizeResultValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return sanitizeResultPayload(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sanitizeResultValue(item)
		}
		return out
	default:
		return value
	}
}

func isSensitiveResultKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "private_key", "client_secret":
		return true
	default:
		return false
	}
}

// AttestInput es el input para registrar una attestation verificable.
type AttestInput struct {
	Status       string         // success, failure, partial
	ProviderRefs map[string]any // refs externas (tx_id, deploy_id, etc.)
	Signature    string         // firma del attester
	Attester     string         // identidad del attester (pep:treasury_gateway)
	Metadata     map[string]any // contexto adicional
}

// Attest registra una prueba verificable de qué ejecutó el sistema target.
func (u *Usecases) Attest(ctx context.Context, requestID uuid.UUID, in AttestInput) (requestdomain.Attestation, error) {
	if u.attestations == nil {
		return requestdomain.Attestation{}, fmt.Errorf("attestation store not configured")
	}

	// Validación estricta de inputs: una attestation sin attester o signature
	// no es una prueba — antes se aceptaba y quedaba persistida igual.
	in.Status = strings.TrimSpace(in.Status)
	in.Attester = strings.TrimSpace(in.Attester)
	in.Signature = strings.TrimSpace(in.Signature)
	switch in.Status {
	case "success", "failure", "partial":
	default:
		return requestdomain.Attestation{}, fmt.Errorf("attestation status must be success, failure or partial, got: %q", in.Status)
	}
	if in.Attester == "" {
		return requestdomain.Attestation{}, fmt.Errorf("attestation attester is required")
	}
	if in.Signature == "" {
		return requestdomain.Attestation{}, fmt.Errorf("attestation signature is required")
	}

	// Verificar que la request existe
	req, err := u.reqRepo.GetByID(ctx, requestID)
	if err != nil {
		return requestdomain.Attestation{}, fmt.Errorf("get request: %w", err)
	}

	// Solo se puede attestar requests ejecutadas o fallidas
	if req.Status != requestdomain.StatusExecuted && req.Status != requestdomain.StatusFailed {
		return requestdomain.Attestation{}, fmt.Errorf("request status must be executed or failed to attest, got: %s", req.Status)
	}

	attestation := requestdomain.Attestation{
		ID:           uuid.New(),
		RequestID:    requestID,
		Status:       in.Status,
		ProviderRefs: in.ProviderRefs,
		Signature:    in.Signature,
		Attester:     in.Attester,
		Metadata:     in.Metadata,
	}

	// Verificación criptográfica condicional. La regla nueva es persistir
	// SIEMPRE, marcando explícitamente verified=false cuando no se pudo
	// verificar (en lugar de dejar el campo silencioso, que era el bug B.3).
	// Así el caller puede distinguir attestations confiables de claims.
	if u.attestVerifier == nil {
		attestation.Verified = false
		attestation.VerificationError = "verifier_not_configured"
	} else if err := u.attestVerifier.Verify(ctx, attestation); err != nil {
		attestation.Verified = false
		attestation.VerificationError = err.Error()
	} else {
		attestation.Verified = true
		attestation.VerificationError = ""
	}

	created, err := u.attestations.Create(ctx, attestation)
	if err != nil {
		return requestdomain.Attestation{}, fmt.Errorf("create attestation: %w", err)
	}

	// Audit trail
	logAuditError(
		u.audit.AppendEvent(ctx, requestID, auditdomain.EventAttested, "attester", in.Attester,
			"Outcome attested by "+in.Attester+": "+in.Status,
			map[string]any{"provider_refs": in.ProviderRefs, "attester": in.Attester}),
		requestID, auditdomain.EventAttested,
	)

	return created, nil
}

// GetAttestation obtiene la attestation de una request.
func (u *Usecases) GetAttestation(ctx context.Context, requestID uuid.UUID) (requestdomain.Attestation, error) {
	if u.attestations == nil {
		return requestdomain.Attestation{}, fmt.Errorf("attestation store not configured")
	}
	return u.attestations.GetByRequestID(ctx, requestID)
}
