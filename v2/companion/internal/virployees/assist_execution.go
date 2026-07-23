package virployees

import (
	"context"
	"strings"

	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// resolveAssistExecutionBinding reconstructs the complete, current provenance
// for a completed Assist. It never accepts a context hash from an HTTP caller.
func (u *UseCases) resolveAssistExecutionBinding(ctx context.Context, orgID string, virployeeID, runID uuid.UUID) (*preparedactions.AssistContextBinding, error) {
	if runID == uuid.Nil {
		return nil, nil
	}
	if u.assistRepo == nil {
		return nil, domainerr.Conflict("Assist repository is not configured")
	}
	run, err := u.assistRepo.GetAssistRunByID(ctx, orgID, runID)
	if err != nil {
		return nil, err
	}
	if responsibleVirployeeID(run) != virployeeID {
		// Hide the existence of a run owned by another Virployee in the organization.
		return nil, domainerr.NotFound("assist run not found")
	}
	if run.Status != "done" || !run.Answered || run.AnswerStatus != "answered" || strings.TrimSpace(run.ContextHash) == "" {
		return nil, domainerr.Conflict("assist run is not a completed, answered, context-bound run")
	}

	virployee, err := u.repo.Get(ctx, orgID, virployeeID)
	if err != nil {
		return nil, err
	}
	if virployee.State() != virployeedomain.StateActive {
		return nil, domainerr.Conflict("virployee is not active")
	}
	currentGrounding := strings.ToLower(strings.TrimSpace(string(virployee.GroundingMode)))
	if currentGrounding == "" {
		currentGrounding = "general"
	}
	if currentGrounding != strings.ToLower(strings.TrimSpace(run.GroundingMode)) {
		return nil, domainerr.Conflict("virployee grounding policy changed after the Assist run")
	}
	role, err := u.jobRoles.Get(ctx, orgID, virployee.JobRoleID)
	if err != nil {
		return nil, domainerr.Conflict("Job Role could not be revalidated")
	}
	if role.State() != jobroledomain.StateActive {
		return nil, domainerr.Conflict("Job Role is no longer active")
	}
	currentJobRoleSnapshotHash := professionalContextHash(professionalContextFromJobRole(role))
	if strings.TrimSpace(run.JobRoleSnapshotHash) == "" || run.JobRoleSnapshotHash != currentJobRoleSnapshotHash {
		return nil, domainerr.Conflict("Job Role changed after the Assist run")
	}

	if run.AssignmentID != uuid.Nil {
		if u.continuity == nil {
			return nil, domainerr.Conflict("continuity assignment validator is not configured")
		}
		subjectID, parseErr := uuid.Parse(strings.TrimSpace(run.SubjectID))
		if parseErr != nil || subjectID == uuid.Nil {
			return nil, domainerr.Conflict("Assist assignment subject is invalid")
		}
		if _, err := u.continuity.ValidateAssistAssignment(ctx, orgID, run.AssignmentID, subjectID, virployeeID, run.AssignmentVersion); err != nil {
			return nil, err
		}
	}
	if u.assistExecution == nil {
		return nil, domainerr.Conflict("Assist execution context validator is not configured")
	}
	if err := u.assistExecution.ValidateAssistExecutionContext(ctx, orgID, virployeeID, virployee.JobRoleID, run); err != nil {
		return nil, err
	}
	if strings.TrimSpace(run.SourceAuthorizationHash) == "" {
		return nil, domainerr.Conflict("Assist source authorization provenance is missing")
	}
	currentSourceAuthorizationHash, err := u.assistExecution.AssistSourceAuthorizationHash(
		ctx, orgID, virployeeID, virployee.JobRoleID, run, run.SourceContext,
	)
	if err != nil || currentSourceAuthorizationHash != run.SourceAuthorizationHash {
		return nil, domainerr.Conflict("Assist source authorization changed after completion")
	}
	if len(run.MemoryReferences) > 0 {
		if strings.TrimSpace(run.MemoryContextHash) == "" || memories.ContextHash(run.MemoryReferences) != run.MemoryContextHash {
			return nil, domainerr.Conflict("Assist memory context provenance is invalid")
		}
	} else if strings.TrimSpace(run.MemoryContextHash) != "" {
		return nil, domainerr.Conflict("Assist memory context references are missing")
	}

	policySnapshotHash, err := u.currentAssistPolicySnapshot(ctx, orgID, virployeeID, virployee.JobRoleID, run)
	if err != nil {
		return nil, err
	}
	recomputed := assistContextHash(
		orgID,
		virployeeID,
		virployee.JobRoleID,
		assistMetadataForRun(run),
		run.SourceContext,
		policySnapshotHash,
	)
	if recomputed != run.ContextHash {
		return nil, domainerr.Conflict("Assist source or policy context changed after completion")
	}

	return &preparedactions.AssistContextBinding{
		RunID:                   run.ID.String(),
		ContextHash:             run.ContextHash,
		SubjectID:               strings.TrimSpace(run.SubjectID),
		CaseID:                  optionalUUIDString(run.CaseID),
		AssignmentID:            optionalUUIDString(run.AssignmentID),
		AssignmentVersion:       run.AssignmentVersion,
		GroundingMode:           strings.ToLower(strings.TrimSpace(run.GroundingMode)),
		SourcesHash:             assistSourcesHash(run.SourceContext),
		MemoryContextHash:       run.MemoryContextHash,
		JobRoleSnapshotHash:     run.JobRoleSnapshotHash,
		PolicySnapshotHash:      policySnapshotHash,
		SourceAuthorizationHash: run.SourceAuthorizationHash,
	}, nil
}

func (u *UseCases) currentAssistPolicySnapshot(ctx context.Context, orgID string, virployeeID, jobRoleID uuid.UUID, run AssistRun) (string, error) {
	evaluator, ok := u.authority.(ConversationScopeEvaluatorPort)
	if !ok || evaluator == nil {
		return "", nil
	}
	result, err := evaluator.EvaluateConversationScope(ctx, executiongate.ConversationScopeInput{
		OrgID: orgID, VirployeeID: virployeeID, JobRoleID: jobRoleID,
		Query: assistRetrievalQuery(run.InputJSON),
	})
	if err != nil {
		return "", domainerr.Conflict("Assist professional scope could not be revalidated")
	}
	if !result.Allowed {
		return "", domainerr.Conflict("Assist professional scope no longer permits this context")
	}
	if strings.TrimSpace(result.SnapshotHash) == "" {
		return "", domainerr.Conflict("Assist professional scope has no revision snapshot")
	}
	return result.SnapshotHash, nil
}

func (u *UseCases) verifyPreparedAssistContext(ctx context.Context, orgID string, virployeeID uuid.UUID, expected *preparedactions.AssistContextBinding) error {
	if expected == nil {
		return nil
	}
	runID, err := uuid.Parse(strings.TrimSpace(expected.RunID))
	if err != nil || runID == uuid.Nil {
		return domainerr.Conflict("prepared action has an invalid Assist run binding")
	}
	current, err := u.resolveAssistExecutionBinding(ctx, orgID, virployeeID, runID)
	if err != nil {
		return err
	}
	if current.ContextHash != expected.ContextHash ||
		current.SubjectID != expected.SubjectID ||
		current.CaseID != expected.CaseID ||
		current.AssignmentID != expected.AssignmentID ||
		current.AssignmentVersion != expected.AssignmentVersion ||
		current.GroundingMode != expected.GroundingMode ||
		current.SourcesHash != expected.SourcesHash ||
		current.MemoryContextHash != expected.MemoryContextHash ||
		current.JobRoleSnapshotHash != expected.JobRoleSnapshotHash ||
		current.PolicySnapshotHash != expected.PolicySnapshotHash ||
		current.SourceAuthorizationHash != expected.SourceAuthorizationHash {
		return domainerr.Conflict("prepared action Assist context is stale")
	}
	return nil
}

func assistSourcesHash(citations []knowledgebases.Citation) string {
	// assistContextHash already canonicalizes citation order. Supplying only
	// source fields yields a compact independent source-set provenance value.
	metadata := AssistMetadata{GroundingMode: "sources"}
	return assistContextHash("assist-sources.v1", uuid.Nil, uuid.Nil, metadata, citations, "")
}

func optionalUUIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func professionalActionScopeQuery(capabilityKey string, action *preparedactions.Action) string {
	parts := []string{strings.TrimSpace(capabilityKey)}
	if action != nil {
		parts = append(parts, strings.TrimSpace(action.Action), strings.TrimSpace(action.Title))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func professionalActionScopeQueryV2(capabilityKey string, legacy *preparedactions.Action, action *preparedactions.PreparedActionV2) string {
	if action == nil {
		return professionalActionScopeQuery(capabilityKey, legacy)
	}
	return strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(capabilityKey), strings.TrimSpace(action.Operation),
	}, " "))
}

func (u *UseCases) evaluateProfessionalActionScope(ctx context.Context, orgID string, virployeeID, jobRoleID uuid.UUID, query string) (executiongate.ConversationScopeResult, *preparedactions.ProfessionalScopeBinding, bool) {
	evaluator, ok := u.authority.(ConversationScopeEvaluatorPort)
	if !ok || evaluator == nil {
		return executiongate.ConversationScopeResult{}, nil, false
	}
	result, err := evaluator.EvaluateConversationScope(ctx, executiongate.ConversationScopeInput{
		OrgID: orgID, VirployeeID: virployeeID, JobRoleID: jobRoleID, Query: query,
	})
	if err != nil {
		return executiongate.ConversationScopeResult{Allowed: false, Reason: "professional scope evaluation is unavailable"}, nil, true
	}
	if strings.TrimSpace(result.SnapshotHash) == "" {
		result.Allowed = false
		result.Reason = "professional scope has no revision snapshot"
		return result, nil, true
	}
	return result, &preparedactions.ProfessionalScopeBinding{
		QueryHash: runtraces.HashString(query), SnapshotHash: result.SnapshotHash,
	}, true
}

func (u *UseCases) verifyCurrentProfessionalActionScope(ctx context.Context, orgID string, virployeeID, jobRoleID uuid.UUID, capabilityKey string, action preparedactions.Action) error {
	if action.ProfessionalScope == nil {
		return nil // durable legacy approvals predate professional-scope binding
	}
	evaluator, ok := u.authority.(ConversationScopeEvaluatorPort)
	if !ok || evaluator == nil {
		return domainerr.Conflict("professional scope evaluator is unavailable")
	}
	query := professionalActionScopeQuery(capabilityKey, &action)
	if runtraces.HashString(query) != action.ProfessionalScope.QueryHash {
		return domainerr.Conflict("prepared action professional-scope query changed")
	}
	result, err := evaluator.EvaluateConversationScope(ctx, executiongate.ConversationScopeInput{
		OrgID: orgID, VirployeeID: virployeeID, JobRoleID: jobRoleID, Query: query,
	})
	if err != nil || !result.Allowed {
		return domainerr.Conflict("professional scope no longer permits this action")
	}
	if result.SnapshotHash != action.ProfessionalScope.SnapshotHash {
		return domainerr.Conflict("professional scope changed after approval")
	}
	return nil
}

func (u *UseCases) verifyCurrentProfessionalActionScopeV2(ctx context.Context, orgID string, virployeeID, jobRoleID uuid.UUID, capabilityKey string, action preparedactions.PreparedActionV2) error {
	if action.ProfessionalScope == nil {
		return domainerr.Conflict("prepared action v2 has no professional-scope binding")
	}
	evaluator, ok := u.authority.(ConversationScopeEvaluatorPort)
	if !ok || evaluator == nil {
		return domainerr.Conflict("professional scope evaluator is unavailable")
	}
	query := professionalActionScopeQueryV2(capabilityKey, nil, &action)
	if runtraces.HashString(query) != action.ProfessionalScope.QueryHash {
		return domainerr.Conflict("prepared action professional-scope query changed")
	}
	result, err := evaluator.EvaluateConversationScope(ctx, executiongate.ConversationScopeInput{
		OrgID: orgID, VirployeeID: virployeeID, JobRoleID: jobRoleID, Query: query,
	})
	if err != nil || !result.Allowed || result.SnapshotHash != action.ProfessionalScope.SnapshotHash {
		return domainerr.Conflict("professional scope changed after approval")
	}
	return nil
}
