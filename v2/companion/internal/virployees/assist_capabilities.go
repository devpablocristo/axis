package virployees

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func validateAssistCapabilitySnapshot(rc runtimecontext.Context, run AssistRun) error {
	if run.CapabilityKey == "" {
		return nil
	}
	if !isClinicalAssistCapability(run.CapabilityKey) {
		return domainerr.Conflict("Assist capability is no longer supported")
	}
	for _, capability := range rc.Capabilities {
		if capability.CapabilityKey != run.CapabilityKey {
			continue
		}
		if capability.SideEffectClass != "read" || capability.RequiresNexusApproval ||
			capability.ManifestHash == "" || capability.ManifestHash != capability.ConformedHash ||
			capability.ManifestHash != run.CapabilityManifestHash ||
			capability.Manifest.ProductSurface != run.ProductSurface {
			return domainerr.Conflict("Assist capability manifest or promotion changed after acceptance")
		}
		return nil
	}
	return domainerr.Conflict("Assist capability assignment changed after acceptance")
}

func (u *UseCases) processGovernedCapabilityAssist(ctx context.Context, run AssistRun, responsibleID, jobRoleID uuid.UUID) (AssistRun, error) {
	if u.governedReads == nil || !u.governedReads.SupportsGovernedRead(run.CapabilityKey) {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, "", "", "capability_executor_unavailable", 0)
		return failed, domainerr.Conflict("executor is not configured for Assist capability")
	}
	var arguments map[string]any
	if err := json.Unmarshal(run.InputJSON, &arguments); err != nil || arguments == nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, "", "", "invalid_capability_input", 0)
		return failed, domainerr.Validation("clinical capability input must be a JSON object")
	}
	subjectID, err := uuid.Parse(strings.TrimSpace(run.SubjectID))
	if err != nil || subjectID == uuid.Nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, "", "", "invalid_capability_subject", 0)
		return failed, domainerr.Conflict("clinical capability subject is invalid")
	}
	if _, err := u.assistRepo.SetAssistRunStatus(ctx, run.OrgID, run.ID, "answering"); err != nil {
		return AssistRun{}, err
	}
	if run.CapabilityKey == CapabilityClinicalTimelineBuild {
		if err := u.consumeQuota(ctx, quotaKey(run.OrgID, run.ProductSurface, quotas.AreaLLM), run.ID.String(), "assist_run", run.ID.String(), estimatedAnswerTokens(run.InputJSON, nil)); err != nil {
			return run, err
		}
	}
	started := time.Now()
	result, invokeErr := u.governedReads.InvokeGovernedRead(ctx, GovernedReadInvocation{
		OrgID: run.OrgID, ActorID: "service:" + run.ProductSurface,
		VirployeeID: responsibleID, SubjectID: subjectID, CaseID: run.CaseID,
		AssignmentID: run.AssignmentID, AssignmentVersion: run.AssignmentVersion,
		ProductSurface: run.ProductSurface, RepositoryGeneration: run.RepositoryGeneration,
		CapabilityKey: run.CapabilityKey, CapabilityManifestHash: run.CapabilityManifestHash,
		IdempotencyKey: run.IdempotencyKey, Arguments: arguments,
	})
	durationMS := time.Since(started).Milliseconds()
	if invokeErr != nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, "", "", "governed_capability_failed", durationMS)
		u.emitAssistAudit(ctx, run.OrgID, responsibleID, failed, run.InputHash)
		return failed, invokeErr
	}
	citations, err := canonicalCapabilityCitations(result)
	if err != nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, "", "", "invalid_capability_citations", durationMS)
		return failed, err
	}
	sourceAuthorizationHash, err := u.resolveAssistSourceAuthorizationHash(ctx, run, responsibleID, jobRoleID, citations)
	if err != nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, run.OrgID, run.ID, "failed", nil, "", false, false, "", "", "source_authorization_changed", durationMS)
		return failed, err
	}
	run.SourceAuthorizationHash = sourceAuthorizationHash
	status, _ := result["status"].(string)
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = "completed"
	}
	answered := status != "abstained"
	raw, err := json.Marshal(result)
	if err != nil {
		return AssistRun{}, err
	}
	metadata := assistMetadataForRun(run)
	metadata.SourceAuthorizationHash = sourceAuthorizationHash
	contextHash := assistContextHash(run.OrgID, responsibleID, jobRoleID, metadata, citations, "")
	done, err := u.completeAssistWithGrounding(ctx, run, AssistCompletion{
		Status: "done", Output: raw, Answered: answered, DurationMS: durationMS,
		GroundingMode: "sources_only", AnswerStatus: status, ContextHash: contextHash,
		Citations: citations, SourceContext: citations, JobRoleSnapshotHash: run.JobRoleSnapshotHash,
		SourceAuthorizationHash: sourceAuthorizationHash,
	})
	if err != nil {
		return AssistRun{}, err
	}
	u.emitAssistAudit(ctx, run.OrgID, responsibleID, done, run.InputHash)
	return done, nil
}

func canonicalCapabilityCitations(result map[string]any) ([]knowledgebases.Citation, error) {
	var raw []any
	if matches, ok := result["matches"].([]any); ok {
		for _, item := range matches {
			match, _ := item.(map[string]any)
			if reference := match["reference"]; reference != nil {
				raw = append(raw, reference)
			}
		}
	}
	if events, ok := result["events"].([]any); ok {
		for _, item := range events {
			event, _ := item.(map[string]any)
			if references, ok := event["references"].([]any); ok {
				raw = append(raw, references...)
			}
		}
	}
	out := make([]knowledgebases.Citation, 0, len(raw))
	seen := map[string]struct{}{}
	for _, value := range raw {
		reference, ok := value.(map[string]any)
		if !ok {
			return nil, domainerr.Conflict("capability returned a malformed citation")
		}
		documentID, _ := reference["document_id"].(string)
		sourceVersion, _ := reference["source_version"].(string)
		sha256, _ := reference["sha256"].(string)
		if strings.TrimSpace(documentID) == "" || strings.TrimSpace(sourceVersion) == "" || strings.TrimSpace(sha256) == "" {
			return nil, domainerr.Conflict("capability returned an incomplete citation")
		}
		locator, err := json.Marshal(reference["locator"])
		if err != nil {
			return nil, domainerr.Conflict("capability returned an invalid citation locator")
		}
		key := documentID + "\x00" + sourceVersion + "\x00" + sha256 + "\x00" + string(locator)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, knowledgebases.Citation{DocumentID: documentID, SourceVersion: sourceVersion, SHA256: sha256, Locator: locator})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DocumentID != out[j].DocumentID {
			return out[i].DocumentID < out[j].DocumentID
		}
		return string(out[i].Locator) < string(out[j].Locator)
	})
	return out, nil
}
