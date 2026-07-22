package virployees

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func (u *UseCases) ListAssistCases(ctx context.Context, tenantID, status string, limit int) ([]AssistCase, error) {
	if u.coordinationRepo == nil {
		return nil, domainerr.Conflict("coordination repository is not configured")
	}
	return u.coordinationRepo.ListAssistCases(ctx, normalizeTenantID(tenantID), strings.TrimSpace(status), limit)
}

func (u *UseCases) GetAssistCase(ctx context.Context, tenantID string, id uuid.UUID) (AssistCase, error) {
	if u.coordinationRepo == nil {
		return AssistCase{}, domainerr.Conflict("coordination repository is not configured")
	}
	return u.coordinationRepo.GetAssistCase(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) ListOrchestrationPolicies(ctx context.Context, tenantID string) ([]OrchestrationPolicy, error) {
	if u.coordinationRepo == nil {
		return nil, domainerr.Conflict("coordination repository is not configured")
	}
	return u.coordinationRepo.ListOrchestrationPolicies(ctx, normalizeTenantID(tenantID))
}

func (u *UseCases) UpsertOrchestrationPolicy(ctx context.Context, tenantID string, actor CoordinationActor, in OrchestrationPolicy) (OrchestrationPolicy, error) {
	if err := requireCoordinationAdmin(actor); err != nil {
		return OrchestrationPolicy{}, err
	}
	if in.EntrypointVirployeeID == uuid.Nil || in.SelectorCapabilityID == uuid.Nil || in.SynthesisCapabilityID == uuid.Nil {
		return OrchestrationPolicy{}, domainerr.Validation("entrypoint, selector capability and synthesis capability are required")
	}
	if strings.TrimSpace(in.ProductSurface) == "" || strings.TrimSpace(in.AssistType) == "" {
		return OrchestrationPolicy{}, domainerr.Validation("product_surface and assist_type are required")
	}
	if in.Mode != "disabled" && in.Mode != "shadow" && in.Mode != "active" {
		return OrchestrationPolicy{}, domainerr.Validation("mode must be disabled, shadow or active")
	}
	if in.MaxSpecialists == 0 {
		in.MaxSpecialists = 3
	}
	if in.MaxSpecialists < 1 || in.MaxSpecialists > 3 {
		return OrchestrationPolicy{}, domainerr.Validation("max_specialists must be between 1 and 3")
	}
	in.MaxDepth = 1
	if in.ConsultationTimeoutSeconds == 0 {
		in.ConsultationTimeoutSeconds = 120
	}
	if in.OrchestrationTimeoutSeconds == 0 {
		in.OrchestrationTimeoutSeconds = 300
	}
	if in.ConsultationTimeoutSeconds < 1 || in.ConsultationTimeoutSeconds > 120 || in.OrchestrationTimeoutSeconds < in.ConsultationTimeoutSeconds || in.OrchestrationTimeoutSeconds > 300 {
		return OrchestrationPolicy{}, domainerr.Validation("consultation timeout must be 1..120 seconds and orchestration timeout must be consultation..300 seconds")
	}
	if len(in.OutputSchema) == 0 {
		return OrchestrationPolicy{}, domainerr.Validation("output_schema must be a non-empty object")
	}
	runtimeContext, err := u.RuntimeContext(ctx, normalizeTenantID(tenantID), in.EntrypointVirployeeID)
	if err != nil {
		return OrchestrationPolicy{}, err
	}
	selector, synthesis := false, false
	for _, capability := range runtimeContext.Capabilities {
		selector = selector || capability.ID == in.SelectorCapabilityID
		synthesis = synthesis || capability.ID == in.SynthesisCapabilityID
	}
	if !selector || !synthesis {
		return OrchestrationPolicy{}, domainerr.Forbidden("entrypoint must have active conformant selector and synthesis capabilities")
	}
	saved, err := u.coordinationRepo.UpsertOrchestrationPolicy(ctx, normalizeTenantID(tenantID), in)
	if err == nil {
		schema, _ := json.Marshal(saved.OutputSchema)
		u.emitCoordinationControlAudit(ctx, saved.TenantID, saved.EntrypointVirployeeID, actor, "orchestration_policy", saved.ID, "orchestration_policy_upserted", "specialist orchestration policy updated", map[string]any{
			"mode": saved.Mode, "policy_version": saved.Version, "product_surface": saved.ProductSurface,
			"assist_type": saved.AssistType, "output_schema_hash": runtraces.HashString(string(schema)),
		})
	}
	return saved, err
}

func (u *UseCases) ListSpecialistRoutes(ctx context.Context, tenantID, productSurface, assistType string, entrypoint uuid.UUID) ([]SpecialistRoute, error) {
	if u.coordinationRepo == nil {
		return nil, domainerr.Conflict("coordination repository is not configured")
	}
	return u.coordinationRepo.ListSpecialistRoutes(ctx, normalizeTenantID(tenantID), productSurface, assistType, entrypoint, false)
}

func (u *UseCases) UpsertSpecialistRoute(ctx context.Context, tenantID string, actor CoordinationActor, in SpecialistRoute) (SpecialistRoute, error) {
	if err := requireCoordinationAdmin(actor); err != nil {
		return SpecialistRoute{}, err
	}
	tenantID = normalizeTenantID(tenantID)
	if in.EntrypointVirployeeID == uuid.Nil || in.TargetVirployeeID == uuid.Nil || in.CapabilityID == uuid.Nil {
		return SpecialistRoute{}, domainerr.Validation("entrypoint, target and capability are required")
	}
	if in.EntrypointVirployeeID == in.TargetVirployeeID {
		return SpecialistRoute{}, domainerr.Validation("specialist route cannot target its entrypoint")
	}
	if strings.TrimSpace(in.SpecialtyCode) == "" {
		return SpecialistRoute{}, domainerr.Validation("specialty_code is required")
	}
	if strings.TrimSpace(in.ProductSurface) == "" || strings.TrimSpace(in.AssistType) == "" || !specialtyCodePattern.MatchString(strings.TrimSpace(in.SpecialtyCode)) {
		return SpecialistRoute{}, domainerr.Validation("product_surface, assist_type and a namespaced specialty_code are required")
	}
	if in.RequirementMode != "" && in.RequirementMode != "advisory_only" && in.RequirementMode != "selector_allowed" && in.RequirementMode != "required" {
		return SpecialistRoute{}, domainerr.Validation("invalid specialist requirement_mode")
	}
	if _, err := u.RuntimeContext(ctx, tenantID, in.EntrypointVirployeeID); err != nil {
		return SpecialistRoute{}, err
	}
	target, err := u.RuntimeContext(ctx, tenantID, in.TargetVirployeeID)
	if err != nil {
		return SpecialistRoute{}, err
	}
	assigned := false
	for _, capability := range target.Capabilities {
		if capability.ID == in.CapabilityID {
			assigned = true
			break
		}
	}
	if !assigned {
		return SpecialistRoute{}, domainerr.Forbidden("target must have the active conformant specialist capability")
	}
	saved, err := u.coordinationRepo.UpsertSpecialistRoute(ctx, tenantID, in)
	if err == nil {
		u.emitCoordinationControlAudit(ctx, saved.TenantID, saved.EntrypointVirployeeID, actor, "specialist_route", saved.ID, "specialist_route_upserted", "specialist route updated", map[string]any{
			"specialty_code": saved.SpecialtyCode, "target_virployee_id": saved.TargetVirployeeID.String(),
			"capability_id": saved.CapabilityID.String(), "requirement_mode": saved.RequirementMode,
			"enabled": saved.Enabled, "route_version": saved.Version,
		})
	}
	return saved, err
}

func (u *UseCases) CreateHandoff(ctx context.Context, tenantID string, actor CoordinationActor, in CreateHandoffInput) (Handoff, error) {
	actor = normalizeCoordinationActor(actor)
	if !coordinationCreatorRole(actor.Role) || actor.ID == "" || isMachineActor(actor) {
		return Handoff{}, domainerr.Forbidden("handoff creation requires a human supervisor, admin or owner")
	}
	tenantID = normalizeTenantID(tenantID)
	if in.CaseID == uuid.Nil || in.ToID == uuid.Nil || strings.TrimSpace(in.ReasonCode) == "" {
		return Handoff{}, domainerr.Validation("case_id, to_virployee_id and reason_code are required")
	}
	if len(strings.TrimSpace(in.Note)) > 500 {
		return Handoff{}, domainerr.Validation("handoff note must not exceed 500 characters")
	}
	assistCase, err := u.coordinationRepo.GetAssistCase(ctx, tenantID, in.CaseID)
	if err != nil {
		return Handoff{}, err
	}
	currentOwner, err := u.repo.Get(ctx, tenantID, assistCase.OwnerVirployeeID)
	if err != nil {
		return Handoff{}, err
	}
	if !canCreateHandoff(actor, currentOwner.SupervisorUserID) {
		return Handoff{}, domainerr.Forbidden("handoff creation requires the current owner's supervisor or an owner/admin")
	}
	if in.SourceRunID != nil {
		run, runErr := u.assistRepo.GetAssistRunByID(ctx, tenantID, *in.SourceRunID)
		if runErr != nil {
			return Handoff{}, runErr
		}
		if run.CaseID != in.CaseID {
			return Handoff{}, domainerr.Validation("source_run_id does not belong to the assist case")
		}
	}
	if assistCase.OwnerVirployeeID == in.ToID {
		return Handoff{}, domainerr.Validation("handoff target is already the case owner")
	}
	if _, err := u.RuntimeContext(ctx, tenantID, in.ToID); err != nil {
		return Handoff{}, err
	}
	handoff, err := u.coordinationRepo.CreateHandoff(ctx, tenantID, assistCase.OwnerVirployeeID, actor.ID, in)
	if err != nil {
		return Handoff{}, err
	}
	u.emitHumanCoordinationAudit(ctx, tenantID, assistCase.OwnerVirployeeID, handoff.SourceRunID, handoff.CaseID, actor, "handoff_requested", "case ownership handoff requested", map[string]any{"handoff_id": handoff.ID.String(), "from_virployee_id": handoff.FromVirployeeID.String(), "to_virployee_id": handoff.ToVirployeeID.String(), "reason_code": handoff.ReasonCode, "note_hash": handoff.NoteHash, "expires_at": handoff.ExpiresAt})
	return handoff, nil
}

func (u *UseCases) ListHandoffs(ctx context.Context, tenantID, status string, limit int) ([]Handoff, error) {
	return u.coordinationRepo.ListHandoffs(ctx, normalizeTenantID(tenantID), status, limit)
}
func (u *UseCases) GetHandoff(ctx context.Context, tenantID string, id uuid.UUID) (Handoff, error) {
	return u.coordinationRepo.GetHandoff(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) DecideHandoff(ctx context.Context, tenantID string, id uuid.UUID, actor CoordinationActor, decision string, in DecideHandoffInput) (Handoff, error) {
	actor = normalizeCoordinationActor(actor)
	tenantID = normalizeTenantID(tenantID)
	handoff, err := u.coordinationRepo.GetHandoff(ctx, tenantID, id)
	if err != nil {
		return Handoff{}, err
	}
	if actor.ID == "" || actor.ID == handoff.RequestedBy || isMachineActor(actor) {
		return Handoff{}, domainerr.Forbidden("handoff requester, virployee and service principals cannot decide")
	}
	target, err := u.repo.Get(ctx, tenantID, handoff.ToVirployeeID)
	if err != nil {
		return Handoff{}, err
	}
	if actor.Role != "owner" && actor.Role != "admin" && (actor.Role != "supervisor" || actor.ID != target.SupervisorUserID) {
		return Handoff{}, domainerr.Forbidden("handoff decision requires the target supervisor or an owner/admin")
	}
	if decision != "accept" && decision != "reject" {
		return Handoff{}, domainerr.Validation("decision must be accept or reject")
	}
	if len(strings.TrimSpace(in.Note)) > 500 {
		return Handoff{}, domainerr.Validation("handoff decision note must not exceed 500 characters")
	}
	decided, err := u.coordinationRepo.DecideHandoff(ctx, tenantID, id, actor.ID, decision, in)
	if err != nil {
		return Handoff{}, err
	}
	eventType := "handoff_rejected"
	if decision == "accept" {
		eventType = "handoff_accepted"
	}
	u.emitHumanCoordinationAudit(ctx, tenantID, decided.ToVirployeeID, decided.SourceRunID, decided.CaseID, actor, eventType, "case ownership handoff decided", map[string]any{"handoff_id": decided.ID.String(), "from_virployee_id": decided.FromVirployeeID.String(), "to_virployee_id": decided.ToVirployeeID.String(), "reason_code": decided.ReasonCode, "status": decided.Status})
	if decision == "accept" {
		u.resumeAfterHandoff(ctx, tenantID, decided.CaseID)
	}
	return decided, nil
}

func (u *UseCases) CancelHandoff(ctx context.Context, tenantID string, id uuid.UUID, actor CoordinationActor, version int64) (Handoff, error) {
	actor = normalizeCoordinationActor(actor)
	tenantID = normalizeTenantID(tenantID)
	handoff, err := u.coordinationRepo.GetHandoff(ctx, tenantID, id)
	if err != nil {
		return Handoff{}, err
	}
	if actor.ID != handoff.RequestedBy && actor.Role != "owner" && actor.Role != "admin" {
		return Handoff{}, domainerr.Forbidden("only the requester or an owner/admin can cancel a handoff")
	}
	if isMachineActor(actor) {
		return Handoff{}, domainerr.Forbidden("service principals cannot cancel a handoff")
	}
	cancelled, err := u.coordinationRepo.CancelHandoff(ctx, tenantID, id, actor.ID, version)
	if err == nil {
		u.emitHumanCoordinationAudit(ctx, tenantID, cancelled.FromVirployeeID, cancelled.SourceRunID, cancelled.CaseID, actor, "handoff_cancelled", "case ownership handoff cancelled", map[string]any{"handoff_id": cancelled.ID.String()})
	}
	return cancelled, err
}

func (u *UseCases) ListHumanReviews(ctx context.Context, tenantID, status string) ([]HumanReview, error) {
	return u.coordinationRepo.ListHumanReviews(ctx, normalizeTenantID(tenantID), status)
}
func (u *UseCases) ClaimHumanReview(ctx context.Context, tenantID string, id uuid.UUID, actor CoordinationActor) (HumanReview, error) {
	actor = normalizeCoordinationActor(actor)
	if actor.ID == "" || !coordinationCreatorRole(actor.Role) || isMachineActor(actor) {
		return HumanReview{}, domainerr.Forbidden("human review claim requires a supervisor, admin or owner")
	}
	tenantID = normalizeTenantID(tenantID)
	pending, err := u.coordinationRepo.GetHumanReview(ctx, tenantID, id)
	if err != nil {
		return HumanReview{}, err
	}
	if err := u.requireCaseReviewer(ctx, tenantID, pending.CaseID, actor); err != nil {
		return HumanReview{}, err
	}
	review, err := u.coordinationRepo.ClaimHumanReview(ctx, tenantID, id, actor.ID)
	if err == nil {
		assistCase, caseErr := u.coordinationRepo.GetAssistCase(ctx, review.TenantID, review.CaseID)
		if caseErr == nil {
			runID := review.RootRunID
			u.emitHumanCoordinationAudit(ctx, review.TenantID, assistCase.OwnerVirployeeID, &runID, review.CaseID, actor, "human_review_claimed", "human review claimed", map[string]any{"review_id": review.ID.String(), "reason_code": review.ReasonCode, "urgency": review.Urgency})
		}
	}
	return review, err
}
func (u *UseCases) ResolveHumanReview(ctx context.Context, tenantID string, id uuid.UUID, actor CoordinationActor, in ResolveReviewInput) (HumanReview, error) {
	actor = normalizeCoordinationActor(actor)
	if actor.ID == "" || !coordinationCreatorRole(actor.Role) || isMachineActor(actor) {
		return HumanReview{}, domainerr.Forbidden("human review resolution requires a supervisor, admin or owner")
	}
	if in.Outcome != "handled_externally" && in.Outcome != "handoff_requested" && in.Outcome != "dismissed" {
		return HumanReview{}, domainerr.Validation("invalid human review outcome")
	}
	if len(strings.TrimSpace(in.Note)) > 500 {
		return HumanReview{}, domainerr.Validation("review note must not exceed 500 characters")
	}
	tenantID = normalizeTenantID(tenantID)
	if in.Outcome == "handoff_requested" && in.HandoffID == nil {
		return HumanReview{}, domainerr.Validation("handoff_requested outcome requires handoff_id")
	}
	if in.Outcome != "handoff_requested" && in.HandoffID != nil {
		return HumanReview{}, domainerr.Validation("handoff_id is only valid for handoff_requested")
	}
	reviewBefore, err := u.coordinationRepo.GetHumanReview(ctx, tenantID, id)
	if err != nil {
		return HumanReview{}, err
	}
	if err := u.requireCaseReviewer(ctx, tenantID, reviewBefore.CaseID, actor); err != nil {
		return HumanReview{}, err
	}
	if in.HandoffID != nil {
		handoff, err := u.coordinationRepo.GetHandoff(ctx, tenantID, *in.HandoffID)
		if err != nil {
			return HumanReview{}, err
		}
		if reviewBefore.CaseID != handoff.CaseID {
			return HumanReview{}, domainerr.Validation("handoff_id does not belong to the human review case")
		}
	}
	review, err := u.coordinationRepo.ResolveHumanReview(ctx, tenantID, id, actor.ID, in)
	if err == nil {
		assistCase, caseErr := u.coordinationRepo.GetAssistCase(ctx, review.TenantID, review.CaseID)
		if caseErr == nil {
			runID := review.RootRunID
			u.emitHumanCoordinationAudit(ctx, review.TenantID, assistCase.OwnerVirployeeID, &runID, review.CaseID, actor, "human_review_resolved", "human review resolved", map[string]any{"review_id": review.ID.String(), "reason_code": review.ReasonCode, "outcome": review.Outcome, "note_hash": review.NoteHash})
		}
	}
	return review, err
}

func (u *UseCases) ExpireHandoffs(ctx context.Context, limit int) (int, error) {
	items, err := u.coordinationRepo.ExpireHandoffs(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		actor := CoordinationActor{ID: "system", Role: "service"}
		u.emitHumanCoordinationAudit(ctx, item.TenantID, item.FromVirployeeID, item.SourceRunID, item.CaseID, actor, "handoff_expired", "case ownership handoff expired", map[string]any{"handoff_id": item.ID.String(), "reason_code": item.ReasonCode})
	}
	return len(items), nil
}

func (u *UseCases) resumeAfterHandoff(ctx context.Context, tenantID string, caseID uuid.UUID) {
	run, err := u.coordinationRepo.ActiveRunForCase(ctx, tenantID, caseID)
	if err != nil {
		return
	}
	if run.Status == AssistStatusSynthesizing && run.OrchestrationPlanID != uuid.Nil {
		plan, planErr := u.coordinationRepo.GetOrchestrationPlan(ctx, tenantID, run.OrchestrationPlanID)
		if planErr == nil && u.coordinationQueue != nil {
			_ = u.coordinationQueue.EnqueueSynthesis(ctx, plan)
		}
	}
}

func requireCoordinationAdmin(actor CoordinationActor) error {
	actor = normalizeCoordinationActor(actor)
	if actor.ID == "" || isMachineActor(actor) || (actor.Role != "owner" && actor.Role != "admin") {
		return domainerr.Forbidden("coordination configuration requires an owner or admin")
	}
	return nil
}

var specialtyCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z0-9][a-z0-9_-]*)+$`)

func normalizeCoordinationActor(actor CoordinationActor) CoordinationActor {
	actor.ID = strings.TrimSpace(actor.ID)
	actor.Role = strings.ToLower(strings.TrimSpace(actor.Role))
	return actor
}
func coordinationCreatorRole(role string) bool {
	return role == "owner" || role == "admin" || role == "supervisor"
}
func canCreateHandoff(actor CoordinationActor, currentSupervisorID string) bool {
	actor = normalizeCoordinationActor(actor)
	if actor.ID == "" || isMachineActor(actor) {
		return false
	}
	return actor.Role == "owner" || actor.Role == "admin" || (actor.Role == "supervisor" && actor.ID == strings.TrimSpace(currentSupervisorID))
}

func (u *UseCases) requireCaseReviewer(ctx context.Context, tenantID string, caseID uuid.UUID, actor CoordinationActor) error {
	actor = normalizeCoordinationActor(actor)
	if actor.ID == "" || isMachineActor(actor) || !coordinationCreatorRole(actor.Role) {
		return domainerr.Forbidden("human review requires a supervisor, admin or owner")
	}
	if actor.Role == "owner" || actor.Role == "admin" {
		return nil
	}
	assistCase, err := u.coordinationRepo.GetAssistCase(ctx, tenantID, caseID)
	if err != nil {
		return err
	}
	currentOwner, err := u.repo.Get(ctx, tenantID, assistCase.OwnerVirployeeID)
	if err != nil {
		return err
	}
	if actor.ID != strings.TrimSpace(currentOwner.SupervisorUserID) {
		return domainerr.Forbidden("human review requires the current owner's supervisor or an owner/admin")
	}
	return nil
}
func isMachineActor(actor CoordinationActor) bool {
	return actor.Role == "service" || actor.Role == "virployee" || strings.HasPrefix(strings.ToLower(actor.ID), "service:")
}

func (u *UseCases) emitHumanCoordinationAudit(ctx context.Context, tenantID string, chainVirployee uuid.UUID, runID *uuid.UUID, caseID uuid.UUID, actor CoordinationActor, eventType, summary string, data map[string]any) {
	if u.auditEmitter == nil {
		return
	}
	subjectID := caseID.String()
	subjectType := "assist_case"
	if runID != nil {
		subjectID = runID.String()
		subjectType = "assist_run"
	}
	data["case_id"] = caseID.String()
	actorType := "user"
	if isMachineActor(actor) {
		actorType = "service"
	}
	if err := u.auditEmitter.AppendAuditEvent(ctx, AuditEventInput{TenantID: tenantID, VirployeeID: chainVirployee.String(), ActorType: actorType, ActorID: actor.ID, SubjectType: subjectType, SubjectID: subjectID, EventType: eventType, Summary: summary, Data: data}); err != nil {
		slog.ErrorContext(ctx, "handoff audit emit failed", "error", err, "tenant_id", tenantID, "case_id", caseID.String(), "event_type", eventType)
	}
}

func (u *UseCases) emitCoordinationControlAudit(ctx context.Context, tenantID string, chainVirployee uuid.UUID, actor CoordinationActor, subjectType string, subjectID uuid.UUID, eventType, summary string, data map[string]any) {
	if u.auditEmitter == nil {
		return
	}
	if err := u.auditEmitter.AppendAuditEvent(ctx, AuditEventInput{
		TenantID: tenantID, VirployeeID: chainVirployee.String(), ActorType: "user", ActorID: actor.ID,
		SubjectType: subjectType, SubjectID: subjectID.String(), EventType: eventType, Summary: summary, Data: data,
	}); err != nil {
		slog.ErrorContext(ctx, "coordination control audit emit failed", "error", err, "tenant_id", tenantID, "subject_type", subjectType, "subject_id", subjectID.String(), "event_type", eventType)
	}
}
