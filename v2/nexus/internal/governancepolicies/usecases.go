package governancepolicies

import (
	"context"
	"strings"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/authorization"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	CreateArtifact(context.Context, Artifact) (Artifact, error)
	ListArtifacts(context.Context, string) ([]Artifact, error)
	GetArtifact(context.Context, string, uuid.UUID) (Artifact, error)
	CreateVersion(context.Context, string, uuid.UUID, string, CreateVersionInput, string, time.Time) (Version, error)
	GetVersion(context.Context, string, uuid.UUID) (Version, error)
	ListEvaluatable(context.Context, string) ([]Version, error)
	HistoricalInputs(context.Context, string, int) ([]SafeInput, error)
	CreateSimulation(context.Context, Simulation) (Simulation, error)
	GetSimulation(context.Context, string, uuid.UUID) (Simulation, error)
	CreatePromotion(context.Context, Promotion) (Promotion, error)
	GetPromotion(context.Context, string, uuid.UUID) (Promotion, error)
	DecidePromotion(context.Context, string, uuid.UUID, string, string, string, time.Time) (Promotion, error)
	ListEvaluations(context.Context, string, int) ([]Evaluation, error)
	ListPromotions(context.Context, string, int) ([]Promotion, error)
	ListChanges(context.Context, string, int) ([]Change, error)
}

type AuthorizationPort interface {
	Check(context.Context, authorization.CheckInput) (authorization.CheckResult, error)
}

type UseCases struct {
	repo       RepositoryPort
	evaluator  *Evaluator
	authorizer AuthorizationPort
	now        func() time.Time
}

func NewUseCases(repo RepositoryPort, evaluator *Evaluator, authorizer AuthorizationPort) *UseCases {
	return &UseCases{repo: repo, evaluator: evaluator, authorizer: authorizer, now: func() time.Time { return time.Now().UTC() }}
}

func (u *UseCases) CreateArtifact(ctx context.Context, tenantID, actorID, actorRole string, input CreateArtifactInput) (Artifact, error) {
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.write", "", "", "", ""); err != nil {
		return Artifact{}, err
	}
	normalized, err := NormalizeArtifact(input)
	if err != nil {
		return Artifact{}, err
	}
	now := u.now()
	return u.repo.CreateArtifact(ctx, Artifact{ID: uuid.New(), TenantID: strings.TrimSpace(tenantID), PolicyKey: normalized.PolicyKey,
		Name: normalized.Name, Description: normalized.Description, CreatedBy: strings.TrimSpace(actorID), CreatedAt: now, UpdatedAt: now})
}

func (u *UseCases) ListArtifacts(ctx context.Context, tenantID, actorID, actorRole string) ([]Artifact, error) {
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.read", "", "", "", ""); err != nil {
		return nil, err
	}
	return u.repo.ListArtifacts(ctx, strings.TrimSpace(tenantID))
}

func (u *UseCases) GetArtifact(ctx context.Context, tenantID, actorID, actorRole string, id uuid.UUID) (Artifact, error) {
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.read", "", "", "", ""); err != nil {
		return Artifact{}, err
	}
	return u.repo.GetArtifact(ctx, strings.TrimSpace(tenantID), id)
}

func (u *UseCases) CreateVersion(ctx context.Context, tenantID, actorID, actorRole string, policyID uuid.UUID, input CreateVersionInput) (Version, error) {
	normalized, err := NormalizeVersion(input)
	if err != nil {
		return Version{}, err
	}
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.write", normalized.ProductSurface, normalized.ActionTypePattern, normalized.TargetSystem, normalized.RiskOverride); err != nil {
		return Version{}, err
	}
	if err := u.evaluator.Validate(normalized.Expression); err != nil {
		return Version{}, domainerr.Validation("CEL expression is invalid: " + err.Error())
	}
	if _, err := u.repo.GetArtifact(ctx, strings.TrimSpace(tenantID), policyID); err != nil {
		return Version{}, err
	}
	return u.repo.CreateVersion(ctx, strings.TrimSpace(tenantID), policyID, strings.TrimSpace(actorID), normalized, ContentHash(normalized), u.now())
}

func (u *UseCases) Simulate(ctx context.Context, tenantID, actorID, actorRole string, versionID uuid.UUID) (Simulation, error) {
	version, err := u.repo.GetVersion(ctx, strings.TrimSpace(tenantID), versionID)
	if err != nil {
		return Simulation{}, err
	}
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.simulate", version.ProductSurface, version.ActionTypePattern, version.TargetSystem, version.RiskOverride); err != nil {
		return Simulation{}, err
	}
	inputs, err := u.repo.HistoricalInputs(ctx, strings.TrimSpace(tenantID), 500)
	if err != nil {
		return Simulation{}, err
	}
	report := Simulation{ID: uuid.New(), TenantID: strings.TrimSpace(tenantID), PolicyVersionID: version.ID, RequestedBy: strings.TrimSpace(actorID), TotalEvaluated: len(inputs), CreatedAt: u.now()}
	for _, input := range inputs {
		matched, evalErr := u.evaluator.MatchOne(version, input)
		if evalErr != nil {
			return Simulation{}, domainerr.Conflict("policy simulation failed: " + evalErr.Error())
		}
		if !matched {
			continue
		}
		report.WouldMatch++
		switch version.Effect {
		case EffectDeny:
			report.WouldDeny++
		case EffectRequireApproval:
			report.WouldRequireApproval++
		case EffectAllow:
			if RiskRequiresApproval(RaiseRisk(input.RiskClass, version.RiskOverride)) {
				report.WouldRequireApproval++
			} else {
				report.WouldAllow++
			}
		}
	}
	report.ReportHash = Hash(map[string]any{"version_id": report.PolicyVersionID, "content_hash": version.ContentHash, "total": report.TotalEvaluated,
		"matches": report.WouldMatch, "allow": report.WouldAllow, "deny": report.WouldDeny, "approval": report.WouldRequireApproval})
	return u.repo.CreateSimulation(ctx, report)
}

func (u *UseCases) RequestPromotion(ctx context.Context, tenantID, actorID, actorRole string, versionID uuid.UUID, input PromotionInput) (Promotion, error) {
	version, err := u.repo.GetVersion(ctx, strings.TrimSpace(tenantID), versionID)
	if err != nil {
		return Promotion{}, err
	}
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.promote", version.ProductSurface, version.ActionTypePattern, version.TargetSystem, version.RiskOverride); err != nil {
		return Promotion{}, err
	}
	input.TargetState = NormalizeState(input.TargetState)
	if input.TargetState != StateShadow && input.TargetState != StateActive {
		return Promotion{}, domainerr.Validation("target_state must be shadow or active")
	}
	if input.SimulationID == uuid.Nil {
		return Promotion{}, domainerr.Validation("simulation_id is required")
	}
	simulation, err := u.repo.GetSimulation(ctx, strings.TrimSpace(tenantID), input.SimulationID)
	if err != nil {
		return Promotion{}, err
	}
	if simulation.PolicyVersionID != version.ID || u.now().Sub(simulation.CreatedAt) >= 24*time.Hour {
		return Promotion{}, domainerr.Conflict("promotion requires a simulation for this version from the last 24 hours")
	}
	if input.TargetState == StateShadow && version.State != StateDraft {
		return Promotion{}, domainerr.Conflict("only draft versions can enter shadow")
	}
	if input.TargetState == StateActive && version.State != StateShadow && version.State != StateRetired {
		return Promotion{}, domainerr.Conflict("only shadow or retired versions can become active")
	}
	now := u.now()
	return u.repo.CreatePromotion(ctx, Promotion{ID: uuid.New(), TenantID: strings.TrimSpace(tenantID), PolicyVersionID: version.ID,
		SimulationID: simulation.ID, TargetState: input.TargetState, Status: "pending", RequestedBy: strings.TrimSpace(actorID), CreatedAt: now})
}

func (u *UseCases) DecidePromotion(ctx context.Context, tenantID, actorID, actorRole string, promotionID uuid.UUID, approve bool, input PromotionDecisionInput) (Promotion, error) {
	promotion, err := u.repo.GetPromotion(ctx, strings.TrimSpace(tenantID), promotionID)
	if err != nil {
		return Promotion{}, err
	}
	version, err := u.repo.GetVersion(ctx, strings.TrimSpace(tenantID), promotion.PolicyVersionID)
	if err != nil {
		return Promotion{}, err
	}
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.promote", version.ProductSurface, version.ActionTypePattern, version.TargetSystem, version.RiskOverride); err != nil {
		return Promotion{}, err
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == promotion.RequestedBy || actorID == version.CreatedBy {
		return Promotion{}, domainerr.Forbidden("policy promotion requires a different policy administrator")
	}
	decision := "rejected"
	if approve {
		decision = "approved"
	}
	return u.repo.DecidePromotion(ctx, strings.TrimSpace(tenantID), promotionID, actorID, decision, strings.TrimSpace(input.Reason), u.now())
}

func (u *UseCases) Evaluate(ctx context.Context, tenantID string, input SafeInput) (EvaluationResult, error) {
	versions, err := u.repo.ListEvaluatable(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		return EvaluationResult{}, err
	}
	input.Now = u.now()
	return u.evaluator.Evaluate(ctx, strings.TrimSpace(tenantID), versions, input)
}

func (u *UseCases) ListEvaluations(ctx context.Context, tenantID, actorID, actorRole string, limit int) ([]Evaluation, error) {
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.read", "", "", "", ""); err != nil {
		return nil, err
	}
	return u.repo.ListEvaluations(ctx, strings.TrimSpace(tenantID), limit)
}

func (u *UseCases) ListPromotions(ctx context.Context, tenantID, actorID, actorRole string, limit int) ([]Promotion, error) {
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.read", "", "", "", ""); err != nil {
		return nil, err
	}
	return u.repo.ListPromotions(ctx, strings.TrimSpace(tenantID), limit)
}

func (u *UseCases) ListChanges(ctx context.Context, tenantID, actorID, actorRole string, limit int) ([]Change, error) {
	if err := u.authorize(ctx, tenantID, actorID, actorRole, "policies.read", "", "", "", ""); err != nil {
		return nil, err
	}
	return u.repo.ListChanges(ctx, strings.TrimSpace(tenantID), limit)
}

func (u *UseCases) authorize(ctx context.Context, tenantID, actorID, actorRole, permission, product, action, resourceType, risk string) error {
	tenantID, actorID, actorRole = strings.TrimSpace(tenantID), strings.TrimSpace(actorID), strings.ToLower(strings.TrimSpace(actorRole))
	if tenantID == "" || actorID == "" {
		return domainerr.Validation("tenant and actor are required")
	}
	if actorRole == "owner" || actorRole == "admin" {
		return nil
	}
	if u.authorizer == nil {
		return domainerr.Forbidden("functional authorization is unavailable")
	}
	if strings.TrimSpace(action) == "" && strings.TrimSpace(risk) == "" {
		risk = "low"
	}
	result, err := u.authorizer.Check(ctx, authorization.CheckInput{TenantID: tenantID, ActorID: actorID, ActorRole: actorRole, Permission: permission,
		ProductSurface: product, ActionType: action, ResourceType: resourceType, RiskClass: risk})
	if err != nil {
		return err
	}
	if !result.Allowed {
		return domainerr.Forbidden(result.Reason)
	}
	return nil
}
