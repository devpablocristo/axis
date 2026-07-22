package virployees

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

var errInvalidStructuredAnswer = errors.New("runtime returned an invalid structured answer")

func (u *UseCases) processOrchestratedAssist(ctx context.Context, run AssistRun, answerInput json.RawMessage, initialParts []artifacts.ContentPart) (AssistRun, bool, error) {
	if u.coordinationRepo == nil || run.CaseID == uuid.Nil {
		return AssistRun{}, false, nil
	}
	policy, err := u.coordinationRepo.FindOrchestrationPolicy(ctx, run.OrgID, run.ProductSurface, run.AssistType, run.VirployeeID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return AssistRun{}, false, nil
		}
		return AssistRun{}, true, err
	}
	if policy.Mode == OrchestrationModeDisabled {
		return AssistRun{}, false, nil
	}
	routes, err := u.coordinationRepo.ListSpecialistRoutes(ctx, run.OrgID, run.ProductSurface, run.AssistType, run.VirployeeID, true)
	if err != nil {
		return AssistRun{}, true, err
	}
	if len(routes) > policy.MaxSpecialists {
		// The selector still sees every configured code, but Go will enforce the
		// bounded fan-out on the returned proposal.
		sort.Slice(routes, func(i, j int) bool { return routes[i].SpecialtyCode < routes[j].SpecialtyCode })
	}
	runtimeContext, err := u.RuntimeContext(ctx, run.OrgID, run.ResponsibleVirployeeID)
	if err != nil {
		return AssistRun{}, true, err
	}
	estimatedTokens := estimatedAnswerTokens(answerInput, initialParts)
	if err := u.consumeQuota(ctx, quotaKey(run.OrgID, run.ProductSurface, quotas.AreaLLM), run.ID.String()+":selector", "assist_run", run.ID.String(), estimatedTokens); err != nil {
		return run, true, err
	}
	if _, err := u.assistRepo.SetAssistRunStatus(ctx, run.OrgID, run.ID, AssistStatusPlanning); err != nil {
		return AssistRun{}, true, err
	}
	decisionSchema := orchestrationDecisionSchema(policy.OutputSchema, routes)
	started := time.Now()
	out, decision, err := u.answerDecisionWithRepair(ctx, AnswerInput{
		SystemPrompt: runtimeContext.ProfileTemplate.SystemPrompt + orchestrationSelectorInstruction(),
		JobRole:      runtimeContext.JobRole.Name,
		InputJSON:    answerInput, ResponseSchema: decisionSchema, ContentParts: initialParts,
	}, policy.MaxSpecialists)
	durationMS := time.Since(started).Milliseconds()
	if err != nil {
		if policy.Mode == OrchestrationModeShadow {
			_, _ = u.assistRepo.SetAssistRunStatus(ctx, run.OrgID, run.ID, "answering")
			return AssistRun{}, false, nil
		}
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, "", "", "orchestration_plan_invalid", durationMS)
		u.emitCoordinationAudit(ctx, run, run.ResponsibleVirployeeID, "orchestration_failed", "specialist orchestration planning failed", map[string]any{"error_code": "orchestration_plan_invalid"})
		return failed, true, domainerr.Unavailable("specialist orchestration planning failed")
	}
	u.recordLLMUsage(ctx, run, "selector", out)
	proposal := append(json.RawMessage(nil), out.OutputJSON...)
	planHash := runtraces.HashString(string(proposal))
	if policy.Mode == OrchestrationModeShadow {
		u.emitCoordinationAudit(ctx, run, run.ResponsibleVirployeeID, "orchestration_shadow_planned", "specialist orchestration shadow decision recorded", map[string]any{
			"decision": decision.Decision, "plan_hash": planHash, "policy_id": policy.ID.String(), "policy_version": policy.Version,
			"requested_count": len(decision.Consultations),
		})
		_, _ = u.assistRepo.SetAssistRunStatus(ctx, run.OrgID, run.ID, "answering")
		return AssistRun{}, false, nil
	}

	consultations, err := u.validateConsultationProposal(ctx, run, policy, routes, decision)
	if err != nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, out.ModelID, out.PromptVersion, "orchestration_policy_rejected", durationMS)
		u.emitCoordinationAudit(ctx, run, run.ResponsibleVirployeeID, "orchestration_failed", "specialist orchestration policy rejected the proposal", map[string]any{"error_code": "orchestration_policy_rejected", "plan_hash": planHash})
		return failed, true, err
	}
	if decision.Decision == "consult" {
		reservationUnits := estimatedTokens * int64(len(consultations)+1)
		if err := u.consumeQuota(ctx, quotaKey(run.OrgID, run.ProductSurface, quotas.AreaLLM), run.ID.String()+":fanout", "assist_run", run.ID.String(), reservationUnits); err != nil {
			return run, true, err
		}
	}
	plan, consultations, err := u.coordinationRepo.CreateOrchestrationPlan(ctx, run, policy, decision, proposal, planHash, out.ModelID, out.PromptVersion, consultations)
	if err != nil {
		return AssistRun{}, true, err
	}
	u.emitCoordinationAudit(ctx, run, run.ResponsibleVirployeeID, "orchestration_planned", "specialist orchestration plan recorded", map[string]any{
		"decision": decision.Decision, "plan_id": plan.ID.String(), "plan_hash": planHash, "policy_id": policy.ID.String(),
		"policy_version": policy.Version, "requested_count": len(consultations),
	})
	switch decision.Decision {
	case "direct":
		done, err := u.completeForOwner(ctx, run, "done", decision.DirectOutput, "", true, false, out.ModelID, out.PromptVersion, "", durationMS)
		if err != nil {
			return AssistRun{}, true, err
		}
		_ = u.coordinationRepo.SetPlanStatus(ctx, run.OrgID, plan.ID, "completed")
		u.emitAssistAudit(ctx, run.OrgID, run.ResponsibleVirployeeID, done, run.InputHash)
		return done, true, nil
	case "needs_human":
		reason, urgency := escalationValues(decision.Escalation)
		if _, err := u.coordinationRepo.CreateHumanReview(ctx, run.OrgID, run.CaseID, run.ID, reason, urgency); err != nil {
			return AssistRun{}, true, err
		}
		output := needsHumanOutput(reason, urgency)
		needsHuman, err := u.completeForOwner(ctx, run, AssistStatusNeedsHuman, output, "", false, true, out.ModelID, out.PromptVersion, "", durationMS)
		if err != nil {
			return AssistRun{}, true, err
		}
		u.emitCoordinationAudit(ctx, run, run.ResponsibleVirployeeID, "human_review_requested", "assist run requires human review", map[string]any{"reason_code": reason, "urgency": urgency, "plan_hash": planHash})
		return needsHuman, true, nil
	case "consult":
		if u.coordinationQueue == nil {
			return AssistRun{}, true, domainerr.Conflict("coordination queue is not configured")
		}
		for _, consultation := range consultations {
			if err := u.coordinationQueue.EnqueueConsultation(ctx, consultation, time.Duration(policy.ConsultationTimeoutSeconds)*time.Second); err != nil {
				return AssistRun{}, true, err
			}
		}
		current, err := u.assistRepo.GetAssistRunByID(ctx, run.OrgID, run.ID)
		return current, true, err
	default:
		return AssistRun{}, true, domainerr.Validation("unsupported orchestration decision")
	}
}

func (u *UseCases) answerDecisionWithRepair(ctx context.Context, input AnswerInput, maxSpecialists int) (AnswerOutput, OrchestrationDecision, error) {
	for attempt := 0; attempt < 2; attempt++ {
		out, err := u.answerer.Answer(ctx, input)
		if err != nil {
			return AnswerOutput{}, OrchestrationDecision{}, err
		}
		if !out.Answered {
			return out, OrchestrationDecision{}, errInvalidStructuredAnswer
		}
		var decision OrchestrationDecision
		if json.Unmarshal(out.OutputJSON, &decision) == nil && validDecision(decision, maxSpecialists) {
			return out, decision, nil
		}
		input.InputJSON = withRepairInstruction(input.InputJSON, "Return exactly one valid axis.orchestration_decision.v1 object.")
	}
	return AnswerOutput{}, OrchestrationDecision{}, errInvalidStructuredAnswer
}

func validDecision(decision OrchestrationDecision, max int) bool {
	switch decision.Decision {
	case "direct":
		return len(decision.DirectOutput) > 0 && json.Valid(decision.DirectOutput)
	case "consult":
		return len(decision.Consultations) > 0 && len(decision.Consultations) <= max
	case "needs_human":
		return decision.Escalation != nil && strings.TrimSpace(decision.Escalation.ReasonCode) != ""
	default:
		return false
	}
}

func (u *UseCases) validateConsultationProposal(ctx context.Context, run AssistRun, policy OrchestrationPolicy, routes []SpecialistRoute, decision OrchestrationDecision) ([]SpecialistConsultation, error) {
	if decision.Decision != "consult" {
		return nil, nil
	}
	byCode := make(map[string]SpecialistRoute, len(routes))
	for _, route := range routes {
		byCode[route.SpecialtyCode] = route
	}
	seen := map[string]struct{}{}
	out := make([]SpecialistConsultation, 0, len(decision.Consultations))
	for _, proposal := range decision.Consultations {
		code := strings.ToLower(strings.TrimSpace(proposal.SpecialtyCode))
		route, ok := byCode[code]
		if !ok {
			return nil, domainerr.Forbidden("orchestration proposed an unavailable specialty")
		}
		if _, duplicate := seen[code]; duplicate {
			return nil, domainerr.Validation("orchestration proposed a duplicate specialty")
		}
		seen[code] = struct{}{}
		if route.TargetVirployeeID == run.ResponsibleVirployeeID || route.TargetVirployeeID == run.VirployeeID {
			return nil, domainerr.Forbidden("orchestration cycle is not allowed")
		}
		runtimeContext, err := u.RuntimeContext(ctx, run.OrgID, route.TargetVirployeeID)
		if err != nil {
			return nil, err
		}
		assigned := false
		for _, capability := range runtimeContext.Capabilities {
			if capability.ID == route.CapabilityID {
				assigned = true
				break
			}
		}
		if !assigned {
			return nil, domainerr.Forbidden("specialist route capability is not active and assigned")
		}
		requirement := strings.ToLower(strings.TrimSpace(proposal.Requirement))
		switch route.RequirementMode {
		case "advisory_only":
			requirement = "advisory"
		case "required":
			requirement = "required"
		default:
			if requirement != "required" {
				requirement = "advisory"
			}
		}
		focus, _ := json.Marshal(map[string]any{"focus": strings.TrimSpace(proposal.Focus), "reason_codes": proposal.ReasonCodes, "evidence_refs": proposal.EvidenceRefs})
		out = append(out, SpecialistConsultation{SpecialtyCode: code, TargetVirployeeID: route.TargetVirployeeID, CapabilityID: route.CapabilityID, Requirement: requirement, FocusJSON: focus, FocusHash: runtraces.HashString(string(focus)), Status: "queued"})
	}
	return out, nil
}

func (u *UseCases) ProcessSpecialistConsultation(ctx context.Context, orgID string, id uuid.UUID, attempt int) (SpecialistConsultation, error) {
	if u.coordinationRepo == nil {
		return SpecialistConsultation{}, domainerr.Conflict("coordination repository is not configured")
	}
	item, claimed, err := u.coordinationRepo.ClaimConsultation(ctx, normalizeOrgID(orgID), id)
	if err != nil {
		return SpecialistConsultation{}, err
	}
	if !claimed {
		if item.Status == "running" && attempt > 1 {
			return u.failInterruptedConsultation(ctx, item)
		}
		return item, nil
	}
	plan, err := u.coordinationRepo.GetOrchestrationPlan(ctx, item.OrgID, item.PlanID)
	if err != nil {
		return item, err
	}
	if !plan.DeadlineAt.After(time.Now().UTC()) {
		item, _ = u.coordinationRepo.CompleteConsultation(ctx, item.OrgID, item.ID, "timed_out", nil, "", "", "", "consultation_timeout", 0)
		_ = u.enqueueReconcile(ctx, plan, item.ID.String())
		return item, nil
	}
	run, err := u.assistRepo.GetAssistRunByID(ctx, item.OrgID, item.RootRunID)
	if err != nil {
		return item, err
	}
	runtimeContext, err := u.RuntimeContext(ctx, item.OrgID, item.TargetVirployeeID)
	if err != nil {
		return u.failConsultation(ctx, item, plan, attempt, "specialist_context_unavailable", err)
	}
	parts, err := u.loadCorpus(ctx, run, focusQuery(item.FocusJSON))
	if err != nil {
		return u.failConsultation(ctx, item, plan, attempt, "specialist_corpus_unavailable", err)
	}
	input, _ := json.Marshal(map[string]any{"focus": json.RawMessage(item.FocusJSON), "root_input": json.RawMessage(run.InputJSON), "specialty_code": item.SpecialtyCode})
	started := time.Now()
	out, opinion, answerErr := u.answerOpinionWithRepair(ctx, AnswerInput{SystemPrompt: runtimeContext.ProfileTemplate.SystemPrompt + specialistConsultInstruction(item.SpecialtyCode), JobRole: runtimeContext.JobRole.Name, InputJSON: input, ResponseSchema: specialistOpinionSchema(item.SpecialtyCode), ContentParts: parts}, item.SpecialtyCode)
	duration := time.Since(started).Milliseconds()
	if answerErr != nil {
		return u.failConsultation(ctx, item, plan, attempt, "specialist_runtime_failed", answerErr)
	}
	u.recordLLMUsage(ctx, run, "consult:"+item.ID.String(), out)
	raw, _ := json.Marshal(opinion)
	item, err = u.coordinationRepo.CompleteConsultation(ctx, item.OrgID, item.ID, "completed", raw, runtraces.HashString(string(raw)), out.ModelID, out.PromptVersion, "", duration)
	if err != nil {
		return item, err
	}
	u.emitCoordinationAudit(ctx, run, item.TargetVirployeeID, "specialist_consult_completed", "specialist consultation completed", map[string]any{"consultation_id": item.ID.String(), "plan_id": item.PlanID.String(), "specialty_code": item.SpecialtyCode, "requirement": item.Requirement, "output_hash": item.OutputHash, "model": item.Model, "prompt_version": item.PromptVersion})
	return item, u.enqueueReconcile(ctx, plan, item.ID.String())
}

func (u *UseCases) failInterruptedConsultation(ctx context.Context, item SpecialistConsultation) (SpecialistConsultation, error) {
	const errorCode = "specialist_interrupted_after_lease_loss"
	plan, err := u.coordinationRepo.GetOrchestrationPlan(ctx, item.OrgID, item.PlanID)
	if err != nil {
		return item, err
	}
	failed, err := u.coordinationRepo.CompleteConsultation(ctx, item.OrgID, item.ID, "failed", nil, "", "", "", errorCode, 0)
	if err != nil {
		return item, err
	}
	run, _ := u.assistRepo.GetAssistRunByID(ctx, item.OrgID, item.RootRunID)
	u.emitCoordinationAudit(ctx, run, item.TargetVirployeeID, "specialist_consult_failed", "specialist consultation failed after lease loss", map[string]any{"consultation_id": item.ID.String(), "plan_id": item.PlanID.String(), "specialty_code": item.SpecialtyCode, "requirement": item.Requirement, "error_code": errorCode})
	return failed, u.enqueueReconcile(ctx, plan, item.ID.String())
}

func (u *UseCases) failConsultation(ctx context.Context, item SpecialistConsultation, plan OrchestrationPlan, attempt int, code string, cause error) (SpecialistConsultation, error) {
	if attempt < 3 {
		_ = u.coordinationRepo.ReleaseConsultation(ctx, item.OrgID, item.ID, code)
		return item, cause
	}
	failed, err := u.coordinationRepo.CompleteConsultation(ctx, item.OrgID, item.ID, "failed", nil, "", "", "", code, 0)
	if err != nil {
		return item, err
	}
	run, _ := u.assistRepo.GetAssistRunByID(ctx, item.OrgID, item.RootRunID)
	u.emitCoordinationAudit(ctx, run, item.TargetVirployeeID, "specialist_consult_failed", "specialist consultation failed", map[string]any{"consultation_id": item.ID.String(), "plan_id": item.PlanID.String(), "specialty_code": item.SpecialtyCode, "requirement": item.Requirement, "error_code": code})
	return failed, u.enqueueReconcile(ctx, plan, item.ID.String())
}

func (u *UseCases) answerOpinionWithRepair(ctx context.Context, input AnswerInput, specialty string) (AnswerOutput, SpecialistOpinion, error) {
	for attempt := 0; attempt < 2; attempt++ {
		out, err := u.answerer.Answer(ctx, input)
		if err != nil {
			return AnswerOutput{}, SpecialistOpinion{}, err
		}
		var opinion SpecialistOpinion
		if out.Answered && json.Unmarshal(out.OutputJSON, &opinion) == nil && strings.EqualFold(strings.TrimSpace(opinion.SpecialtyCode), specialty) && strings.TrimSpace(opinion.Opinion) != "" {
			return out, opinion, nil
		}
		input.InputJSON = withRepairInstruction(input.InputJSON, "Return exactly one valid axis.specialist_opinion.v1 object.")
	}
	return AnswerOutput{}, SpecialistOpinion{}, errInvalidStructuredAnswer
}

func (u *UseCases) ReconcileOrchestration(ctx context.Context, orgID string, planID uuid.UUID) (OrchestrationPlan, error) {
	plan, err := u.coordinationRepo.GetOrchestrationPlan(ctx, normalizeOrgID(orgID), planID)
	if err != nil {
		return OrchestrationPlan{}, err
	}
	if plan.Status == "completed" || plan.Status == "failed" || plan.Status == "needs_human" {
		return plan, nil
	}
	if !plan.DeadlineAt.After(time.Now().UTC()) {
		if err := u.coordinationRepo.TimeoutConsultations(ctx, plan.OrgID, plan.ID); err != nil {
			return plan, err
		}
	}
	items, err := u.coordinationRepo.ListConsultations(ctx, plan.OrgID, plan.ID)
	if err != nil {
		return plan, err
	}
	for _, item := range items {
		if item.Status == "queued" || item.Status == "running" {
			return plan, nil
		}
	}
	plan, err = u.coordinationRepo.RefreshPlanCounts(ctx, plan.OrgID, plan.ID)
	if err != nil {
		return plan, err
	}
	run, err := u.assistRepo.GetAssistRunByID(ctx, plan.OrgID, plan.RootRunID)
	if err != nil {
		return plan, err
	}
	for _, item := range items {
		if item.Status == "completed" {
			var opinion SpecialistOpinion
			if json.Unmarshal(item.Output, &opinion) == nil && opinion.HumanReview != nil && opinion.HumanReview.Urgency == "urgent" {
				reason, urgency := escalationValues(opinion.HumanReview)
				_, err = u.coordinationRepo.CreateHumanReview(ctx, plan.OrgID, plan.CaseID, plan.RootRunID, reason, urgency)
				if err != nil {
					return plan, err
				}
				_, err = u.completeForOwner(ctx, run, AssistStatusNeedsHuman, needsHumanOutput(reason, urgency), "", false, true, "", "", "", 0)
				if err != nil {
					return plan, err
				}
				_ = u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, "needs_human")
				u.emitCoordinationAudit(ctx, run, run.ResponsibleVirployeeID, "human_review_requested", "specialist requested urgent human review", map[string]any{"plan_id": plan.ID.String(), "consultation_id": item.ID.String(), "reason_code": reason, "urgency": urgency})
				plan.Status = "needs_human"
				return plan, nil
			}
		}
		if item.Requirement == "required" && item.Status != "completed" {
			failed, completeErr := u.completeForOwner(ctx, run, "failed", nil, "", false, false, "", "", "specialist_required_unavailable", 0)
			if completeErr != nil {
				return plan, completeErr
			}
			_ = failed
			_ = u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, "failed")
			u.emitCoordinationAudit(ctx, run, run.ResponsibleVirployeeID, "orchestration_failed", "required specialist consultation unavailable", map[string]any{"plan_id": plan.ID.String(), "consultation_id": item.ID.String(), "specialty_code": item.SpecialtyCode, "error_code": "specialist_required_unavailable"})
			plan.Status = "failed"
			return plan, nil
		}
	}
	if _, err = u.assistRepo.SetAssistRunStatus(ctx, plan.OrgID, plan.RootRunID, AssistStatusSynthesizing); err != nil {
		return plan, err
	}
	_ = u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, "ready")
	plan.Status = "ready"
	if u.coordinationQueue == nil {
		return plan, domainerr.Conflict("coordination queue is not configured")
	}
	return plan, u.coordinationQueue.EnqueueSynthesis(ctx, plan)
}

func (u *UseCases) SynthesizeOrchestration(ctx context.Context, orgID string, planID uuid.UUID, attempt int) (AssistRun, error) {
	plan, claimed, err := u.coordinationRepo.ClaimSynthesis(ctx, normalizeOrgID(orgID), planID)
	if err != nil {
		return AssistRun{}, err
	}
	if !claimed && plan.Status == "completed" {
		return u.assistRepo.GetAssistRunByID(ctx, plan.OrgID, plan.RootRunID)
	}
	if !claimed && (plan.Status == "failed" || plan.Status == "needs_human") {
		return u.assistRepo.GetAssistRunByID(ctx, plan.OrgID, plan.RootRunID)
	}
	if !claimed && plan.Status == "synthesizing" && attempt > 1 {
		return u.finalizeInterruptedSynthesis(ctx, plan)
	}
	if !claimed {
		return AssistRun{}, domainerr.Conflict("orchestration plan is not ready for synthesis")
	}
	run, err := u.assistRepo.GetAssistRunByID(ctx, plan.OrgID, plan.RootRunID)
	if err != nil {
		return AssistRun{}, err
	}
	assistCase, err := u.coordinationRepo.GetAssistCase(ctx, plan.OrgID, plan.CaseID)
	if err != nil {
		return AssistRun{}, err
	}
	run.ResponsibleVirployeeID = assistCase.OwnerVirployeeID
	runtimeContext, err := u.RuntimeContext(ctx, plan.OrgID, run.ResponsibleVirployeeID)
	if err != nil {
		return AssistRun{}, err
	}
	items, err := u.coordinationRepo.ListConsultations(ctx, plan.OrgID, plan.ID)
	if err != nil {
		return AssistRun{}, err
	}
	opinions := make([]json.RawMessage, 0)
	limitations := make([]OrchestrationLimitation, 0)
	for _, item := range items {
		if item.Status == "completed" {
			opinions = append(opinions, item.Output)
		} else {
			limitations = append(limitations, OrchestrationLimitation{Code: item.ErrorCode, SpecialtyCode: item.SpecialtyCode})
		}
	}
	parts, err := u.loadCorpus(ctx, run, "synthesize the complete answer using the cited specialist findings")
	if err != nil {
		return AssistRun{}, err
	}
	input, _ := json.Marshal(map[string]any{"root_input": json.RawMessage(run.InputJSON), "specialist_opinions": opinions, "limitations": limitations})
	started := time.Now()
	out, err := u.answerFinalWithRepair(ctx, AnswerInput{SystemPrompt: runtimeContext.ProfileTemplate.SystemPrompt + orchestrationSynthesisInstruction(), JobRole: runtimeContext.JobRole.Name, InputJSON: input, ResponseSchema: plan.OutputSchema, ContentParts: parts})
	duration := time.Since(started).Milliseconds()
	if err != nil {
		_ = u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, "ready")
		return AssistRun{}, err
	}
	u.recordLLMUsage(ctx, run, "synthesis:"+plan.ID.String(), out)
	done, err := u.completeForOwner(ctx, run, "done", out.OutputJSON, out.OutputText, true, false, out.ModelID, out.PromptVersion, "", duration)
	if err != nil {
		_ = u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, "ready")
		return AssistRun{}, err
	}
	if err := u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, "completed"); err != nil {
		return done, err
	}
	u.emitCoordinationAudit(ctx, done, run.ResponsibleVirployeeID, "synthesis_completed", "specialist orchestration synthesis completed", map[string]any{"plan_id": plan.ID.String(), "plan_hash": plan.PlanHash, "output_hash": runtraces.HashString(string(out.OutputJSON)), "consultation_count": len(items), "limitation_count": len(limitations), "model": out.ModelID, "prompt_version": out.PromptVersion})
	u.emitAssistAudit(ctx, done.OrgID, run.ResponsibleVirployeeID, done, done.InputHash)
	return done, nil
}

func (u *UseCases) finalizeInterruptedSynthesis(ctx context.Context, plan OrchestrationPlan) (AssistRun, error) {
	run, err := u.assistRepo.GetAssistRunByID(ctx, plan.OrgID, plan.RootRunID)
	if err != nil {
		return AssistRun{}, err
	}
	if run.Status == "done" || run.Status == "failed" || run.Status == AssistStatusNeedsHuman {
		planStatus := run.Status
		if planStatus == "done" {
			planStatus = "completed"
		}
		if err := u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, planStatus); err != nil {
			return run, err
		}
		return run, nil
	}
	failed, err := u.completeForOwner(ctx, run, "failed", nil, "", false, false, "", "", "synthesis_interrupted_after_lease_loss", 0)
	if err != nil {
		return AssistRun{}, err
	}
	if err := u.coordinationRepo.SetPlanStatus(ctx, plan.OrgID, plan.ID, "failed"); err != nil {
		return failed, err
	}
	u.emitCoordinationAudit(ctx, failed, run.ResponsibleVirployeeID, "orchestration_failed", "specialist synthesis interrupted after lease loss", map[string]any{"plan_id": plan.ID.String(), "error_code": "synthesis_interrupted_after_lease_loss"})
	return failed, nil
}

func (u *UseCases) answerFinalWithRepair(ctx context.Context, input AnswerInput) (AnswerOutput, error) {
	for attempt := 0; attempt < 2; attempt++ {
		out, err := u.answerer.Answer(ctx, input)
		if err != nil {
			return AnswerOutput{}, err
		}
		if out.Answered && len(out.OutputJSON) > 0 && json.Valid(out.OutputJSON) {
			var object map[string]any
			if json.Unmarshal(out.OutputJSON, &object) == nil && object != nil {
				return out, nil
			}
		}
		input.InputJSON = withRepairInstruction(input.InputJSON, "Return exactly one object conforming to the requested product output schema.")
	}
	return AnswerOutput{}, errInvalidStructuredAnswer
}

func (u *UseCases) loadCorpus(ctx context.Context, run AssistRun, query string) ([]artifacts.ContentPart, error) {
	if u.corpusReader == nil {
		return nil, nil
	}
	return u.corpusReader.Load(ctx, artifacts.Scope{OrgID: run.OrgID, VirployeeID: run.VirployeeID, ProductSurface: run.ProductSurface, SubjectID: run.SubjectID, RepositoryGeneration: run.RepositoryGeneration}, query, 12)
}
func (u *UseCases) enqueueReconcile(ctx context.Context, plan OrchestrationPlan, trigger string) error {
	if u.coordinationQueue == nil {
		return domainerr.Conflict("coordination queue is not configured")
	}
	return u.coordinationQueue.EnqueueReconcile(ctx, plan, trigger)
}

type ownedAssistCompleter interface {
	CompleteAssistRunForOwner(context.Context, string, uuid.UUID, int64, string, json.RawMessage, string, bool, bool, string, string, string, int64) (AssistRun, error)
}

func (u *UseCases) completeForOwner(ctx context.Context, run AssistRun, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error) {
	if repo, ok := u.assistRepo.(ownedAssistCompleter); ok {
		return repo.CompleteAssistRunForOwner(ctx, run.OrgID, run.ID, run.OwnershipVersion, status, output, outputText, answered, degraded, model, promptVersion, runErr, durationMS)
	}
	return u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, status, output, outputText, answered, degraded, model, promptVersion, runErr, durationMS)
}

func (u *UseCases) LoadOrchestrationSummary(ctx context.Context, run AssistRun) (*OrchestrationSummary, error) {
	if u.coordinationRepo == nil || run.OrchestrationPlanID == uuid.Nil {
		return nil, nil
	}
	plan, err := u.coordinationRepo.GetOrchestrationPlan(ctx, run.OrgID, run.OrchestrationPlanID)
	if err != nil {
		return nil, err
	}
	items, err := u.coordinationRepo.ListConsultations(ctx, run.OrgID, plan.ID)
	if err != nil {
		return nil, err
	}
	summary := &OrchestrationSummary{State: plan.Status, Decision: plan.Decision, Requested: plan.RequestedCount, Completed: plan.CompletedCount, Failed: plan.FailedCount}
	for _, item := range items {
		summary.Specialists = append(summary.Specialists, SpecialistSummary{SpecialtyCode: item.SpecialtyCode, Requirement: item.Requirement, Status: item.Status})
		if item.Requirement == "advisory" && item.Status != "completed" {
			summary.Limitations = append(summary.Limitations, OrchestrationLimitation{Code: item.ErrorCode, SpecialtyCode: item.SpecialtyCode})
		}
	}
	return summary, nil
}

// RequeueCoordinationWork is the safety reconciler for the transactional
// state-to-job boundary after consultations complete. Initial consultation
// jobs are inserted atomically with the plan; this repairs missing reconcile
// and synthesis work and also makes pre-existing queued consultations runnable.
func (u *UseCases) RequeueCoordinationWork(ctx context.Context, limit int) (CoordinationRecoveryResult, error) {
	var result CoordinationRecoveryResult
	if u.coordinationRepo == nil || u.coordinationQueue == nil {
		return result, nil
	}
	plans, err := u.coordinationRepo.ListRecoverableOrchestrationPlans(ctx, limit)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	for _, plan := range plans {
		if plan.Status == "synthesizing" {
			if !plan.DeadlineAt.After(now) {
				if _, err := u.finalizeInterruptedSynthesis(ctx, plan); err != nil {
					return result, err
				}
			}
			continue
		}
		if plan.Status == "ready" {
			if err := u.coordinationQueue.EnqueueSynthesis(ctx, plan); err != nil {
				return result, err
			}
			result.Syntheses++
			continue
		}
		items, err := u.coordinationRepo.ListConsultations(ctx, plan.OrgID, plan.ID)
		if err != nil {
			return result, err
		}
		allTerminal := true
		for _, item := range items {
			if item.Status == "queued" {
				timeout := plan.DeadlineAt.Sub(now)
				if timeout <= 0 {
					continue
				}
				if timeout > 2*time.Minute {
					timeout = 2 * time.Minute
				}
				if err := u.coordinationQueue.EnqueueConsultation(ctx, item, timeout); err != nil {
					return result, err
				}
				result.Consultations++
			}
			if item.Status == "queued" || item.Status == "running" {
				allTerminal = false
			}
		}
		if allTerminal || !plan.DeadlineAt.After(now) {
			trigger := "recovery:" + plan.UpdatedAt.UTC().Format(time.RFC3339Nano)
			if err := u.coordinationQueue.EnqueueReconcile(ctx, plan, trigger); err != nil {
				return result, err
			}
			result.Reconciles++
		}
	}
	return result, nil
}

func (u *UseCases) emitCoordinationAudit(ctx context.Context, run AssistRun, chainVirployee uuid.UUID, eventType, summary string, data map[string]any) {
	if u.auditEmitter == nil || chainVirployee == uuid.Nil {
		return
	}
	data["root_run_id"] = run.ID.String()
	data["case_id"] = run.CaseID.String()
	if err := u.auditEmitter.AppendAuditEvent(ctx, AuditEventInput{OrgID: run.OrgID, VirployeeID: chainVirployee.String(), ActorType: "virployee", ActorID: chainVirployee.String(), SubjectType: "assist_run", SubjectID: run.ID.String(), EventType: eventType, Summary: summary, Data: data}); err != nil {
		slog.ErrorContext(ctx, "coordination audit emit failed", "error", err, "org_id", run.OrgID, "root_run_id", run.ID.String(), "event_type", eventType)
	}
}

func orchestrationDecisionSchema(outputSchema map[string]any, routes []SpecialistRoute) map[string]any {
	codes := make([]any, 0, len(routes))
	for _, route := range routes {
		codes = append(codes, route.SpecialtyCode)
	}
	decisions := []string{"direct", "needs_human"}
	properties := map[string]any{
		"decision":      map[string]any{"type": "string", "enum": decisions},
		"direct_output": outputSchema,
		"escalation": map[string]any{"type": "object", "properties": map[string]any{
			"reason_code": map[string]any{"type": "string"},
			"urgency":     map[string]any{"type": "string", "enum": []string{"routine", "urgent"}},
		}},
	}
	if len(codes) > 0 {
		properties["decision"] = map[string]any{"type": "string", "enum": []string{"direct", "consult", "needs_human"}}
		properties["consultations"] = map[string]any{"type": "array", "maxItems": len(codes), "items": map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"specialty_code", "requirement", "focus", "reason_codes", "evidence_refs"},
			"properties": map[string]any{
				"specialty_code": map[string]any{"type": "string", "enum": codes},
				"requirement":    map[string]any{"type": "string", "enum": []string{"required", "advisory"}},
				"focus":          map[string]any{"type": "string"},
				"reason_codes":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"evidence_refs":  map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			},
		}}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"decision"}, "properties": properties}
}
func specialistOpinionSchema(code string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"specialty_code", "opinion", "findings", "limitations", "recommendation_codes"}, "properties": map[string]any{"specialty_code": map[string]any{"type": "string", "enum": []string{code}}, "opinion": map[string]any{"type": "string"}, "findings": map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []string{"statement", "evidence_refs"}, "properties": map[string]any{"statement": map[string]any{"type": "string"}, "evidence_refs": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}}}, "limitations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "recommendation_codes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "human_review": map[string]any{"type": "object", "properties": map[string]any{"reason_code": map[string]any{"type": "string"}, "urgency": map[string]any{"type": "string", "enum": []string{"routine", "urgent"}}}}}}
}
func withRepairInstruction(raw json.RawMessage, instruction string) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		value = map[string]any{"input": string(raw)}
	}
	out, _ := json.Marshal(map[string]any{"input": value, "schema_repair_instruction": instruction})
	return out
}
func focusQuery(raw json.RawMessage) string {
	var value struct {
		Focus string `json:"focus"`
	}
	_ = json.Unmarshal(raw, &value)
	return value.Focus
}
func escalationValues(value *EscalationProposal) (string, string) {
	reason, urgency := "human_review_required", "routine"
	if value != nil {
		if strings.TrimSpace(value.ReasonCode) != "" {
			reason = strings.TrimSpace(value.ReasonCode)
		}
		if value.Urgency == "urgent" {
			urgency = "urgent"
		}
	}
	return reason, urgency
}
func needsHumanOutput(reason, urgency string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"answered": false, "needs_human": true, "reason_code": reason, "urgency": urgency, "message": "El caso necesita revisión humana antes de emitir una respuesta clínica."})
	return raw
}

func orchestrationSelectorInstruction() string {
	return "\n\nAxis governance instruction: decide whether to answer directly, consult only the allowlisted specialty codes in the response schema, or request human review. Never invent a Virployee ID, capability, specialty, or recursive delegation."
}

func specialistConsultInstruction(code string) string {
	return "\n\nAxis governance instruction: you are a bounded advisory specialist for " + code + ". Analyze only the requested focus and cited corpus. Return a structured opinion to the responsible Virployee; do not issue the final product response and do not delegate again."
}

func orchestrationSynthesisInstruction() string {
	return "\n\nAxis governance instruction: you are the single responsible owner. Synthesize one final product response from the original corpus and specialist opinions. Preserve disagreements and limitations; specialists are advisory and do not become co-authors."
}
