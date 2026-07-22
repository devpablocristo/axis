package virployees

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type assistDocRef struct {
	DocumentID  string `json:"document_id"`
	Key         string `json:"key"`
	ReadURL     string `json:"read_url"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
	Required    *bool  `json:"required,omitempty"`
}

func (u *UseCases) resolveDocuments(ctx context.Context, inputJSON json.RawMessage) json.RawMessage {
	if u.docFetcher == nil {
		return inputJSON
	}
	var parsed struct {
		Documents []assistDocRef `json:"documents"`
	}
	if err := json.Unmarshal(inputJSON, &parsed); err != nil || len(parsed.Documents) == 0 {
		return inputJSON
	}
	fetched := make([]FetchedDocument, 0, len(parsed.Documents))
	for _, doc := range parsed.Documents {
		fetched = append(fetched, u.docFetcher.Fetch(ctx, doc.Key, doc.ReadURL, doc.ContentType))
	}
	enriched, err := json.Marshal(map[string]any{"documents": fetched})
	if err != nil {
		return inputJSON
	}
	return enriched
}

// SubmitAssist durably stores an idempotent run without performing model work.
func (u *UseCases) SubmitAssist(ctx context.Context, tenantID string, id uuid.UUID, inputJSON json.RawMessage, idempotencyKey string, metadata AssistMetadata) (AssistRun, bool, error) {
	tenantID = normalizeTenantID(tenantID)
	if u.answerer == nil {
		return AssistRun{}, false, domainerr.Conflict("runtime answerer is not configured")
	}
	if u.assistRepo == nil {
		return AssistRun{}, false, domainerr.Conflict("assist repository is not configured")
	}
	if strings.TrimSpace(string(inputJSON)) == "" || !json.Valid(inputJSON) {
		return AssistRun{}, false, domainerr.Validation("input_json must be valid JSON")
	}
	metadata.SubjectID = strings.TrimSpace(metadata.SubjectID)
	if metadata.CaseID != uuid.Nil && metadata.SubjectID == "" {
		return AssistRun{}, false, domainerr.Validation("subject_id is required when case_id is provided")
	}
	var parsedSubjectID uuid.UUID
	if metadata.SubjectID != "" {
		parsedSubjectID, _ = uuid.Parse(metadata.SubjectID)
	}
	if metadata.AssignmentID == uuid.Nil && u.continuity != nil {
		required, requirementErr := u.continuity.RequiresAssistAssignment(ctx, tenantID, parsedSubjectID, id)
		if requirementErr != nil {
			return AssistRun{}, false, requirementErr
		}
		if required {
			return AssistRun{}, false, domainerr.Conflict("assignment_id and a valid work-subject subject_id are required for routed Assist work")
		}
	}
	if metadata.AssignmentID != uuid.Nil {
		if metadata.SubjectID == "" {
			return AssistRun{}, false, domainerr.Validation("subject_id is required when assignment_id is provided")
		}
		if u.continuity == nil {
			return AssistRun{}, false, domainerr.Conflict("continuity assignment validator is not configured")
		}
		if parsedSubjectID == uuid.Nil {
			return AssistRun{}, false, domainerr.Validation("subject_id must be a valid work subject UUID when assignment_id is provided")
		}
		version, validationErr := u.continuity.ValidateAssistAssignment(ctx, tenantID, metadata.AssignmentID, parsedSubjectID, id, 0)
		if validationErr != nil {
			return AssistRun{}, false, validationErr
		}
		metadata.AssignmentVersion = version
	}
	// Fail before accepting work when the virployee/profile is not executable.
	runtimeContext, err := u.RuntimeContext(ctx, tenantID, id)
	if err != nil {
		return AssistRun{}, false, err
	}
	metadata.GroundingMode = string(runtimeContext.Virployee.GroundingMode)
	if metadata.GroundingMode == "" {
		metadata.GroundingMode = "general"
	}
	metadata.JobRoleSnapshotHash = professionalContextHash(professionalContextFromJobRole(runtimeContext.JobRole))
	metadata.ContextHash = assistContextHash(tenantID, id, runtimeContext.Virployee.JobRoleID, metadata, nil, "")
	inputHash := runtraces.HashString(metadata.ContextHash + "\x00" + string(inputJSON))
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = runtraces.HashString(tenantID + ":" + id.String() + ":" + inputHash)
	}
	if err := u.consumeQuota(ctx, quotaKey(tenantID, metadata.ProductSurface, quotas.AreaInbound), idempotencyKey, "virployee", id.String(), 1); err != nil {
		return AssistRun{}, false, err
	}
	run, reserved, err := u.assistRepo.BeginAssistRun(ctx, tenantID, id, metadata, idempotencyKey, inputHash, runtraces.InputPreview(string(inputJSON)), inputJSON)
	if err != nil {
		return AssistRun{}, false, err
	}
	if !reserved && run.InputHash != "" && run.InputHash != inputHash {
		legacyInputHash := runtraces.HashString(string(inputJSON))
		if run.InputHash != legacyInputHash || !assistRunScopeMatches(run, metadata) {
			return AssistRun{}, false, domainerr.Conflict("idempotency key was already used with different input or scope")
		}
	}
	return run, reserved, nil
}

func assistRunScopeMatches(run AssistRun, metadata AssistMetadata) bool {
	if strings.TrimSpace(run.SubjectID) != strings.TrimSpace(metadata.SubjectID) ||
		strings.TrimSpace(run.ProductSurface) != strings.TrimSpace(metadata.ProductSurface) ||
		strings.TrimSpace(run.AssistType) != strings.TrimSpace(metadata.AssistType) ||
		strings.TrimSpace(run.RepositoryGeneration) != strings.TrimSpace(metadata.RepositoryGeneration) ||
		run.AssignmentID != metadata.AssignmentID || run.AssignmentVersion != metadata.AssignmentVersion {
		return false
	}
	return metadata.CaseID == uuid.Nil || run.CaseID == metadata.CaseID
}

// SubmitAssistAsync persists then enqueues identifier-only work. If enqueueing
// is interrupted, operational reconciliation finds the received row and queues
// it later; no request depends on a process-local goroutine.
func (u *UseCases) SubmitAssistAsync(ctx context.Context, tenantID string, id uuid.UUID, inputJSON json.RawMessage, idempotencyKey string, metadata AssistMetadata) (AssistRun, error) {
	if u.assistQueue == nil {
		return AssistRun{}, domainerr.Conflict("assist queue is not configured")
	}
	run, reserved, err := u.SubmitAssist(ctx, tenantID, id, inputJSON, idempotencyKey, metadata)
	if err != nil {
		return AssistRun{}, err
	}
	if run.Status == "done" || run.Status == "failed" || run.Status == AssistStatusNeedsHuman {
		return run, nil
	}
	if reserved || run.Status == "received" {
		if err := u.assistQueue.EnqueueAssist(ctx, run); err != nil {
			return AssistRun{}, err
		}
	}
	return run, nil
}

// Assist preserves the synchronous internal endpoint while sharing the same
// durable reservation and claim semantics as asynchronous product work.
func (u *UseCases) Assist(ctx context.Context, tenantID string, id uuid.UUID, inputJSON json.RawMessage, idempotencyKey string, metadata AssistMetadata) (AssistRun, error) {
	run, reserved, err := u.SubmitAssist(ctx, tenantID, id, inputJSON, idempotencyKey, metadata)
	if err != nil {
		return AssistRun{}, err
	}
	if !reserved {
		switch run.Status {
		case "done":
			return run, nil
		case "failed":
			return AssistRun{}, domainerr.Unavailable("assist run failed")
		case AssistStatusNeedsHuman:
			return run, nil
		default:
			return AssistRun{}, domainerr.Conflict("assist run already in progress")
		}
	}
	return u.ProcessAssistRun(ctx, run.TenantID, run.ID, false)
}

// ProcessAssistRun is the durable job handler's domain operation.
func (u *UseCases) ProcessAssistRun(ctx context.Context, tenantID string, runID uuid.UUID, recoverPreAnswer bool) (AssistRun, error) {
	tenantID = normalizeTenantID(tenantID)
	run, claimed, err := u.assistRepo.ClaimAssistRun(ctx, tenantID, runID, recoverPreAnswer)
	if err != nil {
		return AssistRun{}, err
	}
	if !claimed {
		switch run.Status {
		case "done":
			return run, nil
		case "failed":
			return run, domainerr.Unavailable("assist run failed")
		case AssistStatusNeedsHuman:
			return run, nil
		default:
			return run, domainerr.Conflict("assist run is already being processed")
		}
	}
	responsibleID := responsibleVirployeeID(run)
	run.ResponsibleVirployeeID = responsibleID
	if run.AssignmentID != uuid.Nil {
		if u.continuity == nil {
			failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", "assignment_validation_unavailable", 0)
			return failed, domainerr.Conflict("continuity assignment validator is not configured")
		}
		subjectID, parseErr := uuid.Parse(strings.TrimSpace(run.SubjectID))
		if parseErr != nil || subjectID == uuid.Nil {
			failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", "invalid_assignment_subject", 0)
			return failed, domainerr.Conflict("Assist run has an invalid assignment subject")
		}
		if _, validationErr := u.continuity.ValidateAssistAssignment(ctx, tenantID, run.AssignmentID, subjectID, responsibleID, run.AssignmentVersion); validationErr != nil {
			failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", "assignment_changed", 0)
			return failed, validationErr
		}
	}
	runtimeContext, err := u.RuntimeContext(ctx, tenantID, responsibleID)
	if err != nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", "runtime_context_unavailable", 0)
		return failed, err
	}
	currentJobRoleSnapshotHash := professionalContextHash(professionalContextFromJobRole(runtimeContext.JobRole))
	if run.JobRoleSnapshotHash != "" && run.JobRoleSnapshotHash != currentJobRoleSnapshotHash {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", "job_role_changed", 0)
		return failed, domainerr.Conflict("Job Role changed after the Assist run was accepted")
	}
	run.JobRoleSnapshotHash = currentJobRoleSnapshotHash

	contentParts, ingested, ingestErr := u.ingestArtifacts(ctx, run)
	if ingestErr != nil {
		if _, quotaExceeded := quotas.RetryAfter(ingestErr); quotaExceeded {
			return run, ingestErr
		}
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", artifacts.StableErrorCode(ingestErr), 0)
		u.emitAssistAudit(ctx, tenantID, run.VirployeeID, failed, run.InputHash)
		return failed, domainerr.Unavailable("required artifact processing failed")
	}
	var answerInputJSON json.RawMessage
	if ingested {
		answerInputJSON = sanitizeAssistInput(run.InputJSON)
	} else {
		answerInputJSON = u.resolveDocuments(ctx, run.InputJSON)
	}
	groundingMode := strings.ToLower(strings.TrimSpace(run.GroundingMode))
	if groundingMode == "" {
		groundingMode = strings.ToLower(strings.TrimSpace(string(runtimeContext.Virployee.GroundingMode)))
	}
	if groundingMode == "" {
		groundingMode = "general"
	}
	query := assistRetrievalQuery(run.InputJSON)
	scopeSnapshotHash := ""
	if scope, evaluated := u.evaluateConversationScope(ctx, tenantID, responsibleID, runtimeContext.Virployee.JobRoleID, query); evaluated {
		scopeSnapshotHash = scope.SnapshotHash
		if !scope.Allowed {
			contextHash := assistContextHash(tenantID, responsibleID, runtimeContext.Virployee.JobRoleID, assistMetadataForRun(run), nil, scopeSnapshotHash)
			return u.completeScopeDecisionAssist(ctx, run, responsibleID, groundingMode, scope.Decision, scope.Reason, contextHash)
		}
	}
	allowedCitations := citationsForParts(contentParts, run.RepositoryGeneration)
	if u.knowledge != nil {
		evidence, retrievalErr := u.knowledge.Retrieve(ctx, knowledgebases.RetrievalScope{
			TenantID: tenantID, VirployeeID: responsibleID, SubjectID: run.SubjectID, CaseID: run.CaseID,
		}, query, 12)
		if retrievalErr != nil {
			if groundingMode == "sources_only" && !hasTextEvidence(contentParts) {
				contextHash := assistContextHash(tenantID, responsibleID, runtimeContext.Virployee.JobRoleID, assistMetadataForRun(run), allowedCitations, scopeSnapshotHash)
				return u.completeAbstainedAssist(ctx, run, responsibleID, groundingMode, "retrieval_unavailable", contextHash)
			}
			slog.WarnContext(ctx, "knowledge_retrieval_failed", "error", runtraces.RedactText(retrievalErr.Error()), "assist_run_id", run.ID.String())
		} else {
			contentParts = append(contentParts, evidence.Parts...)
			allowedCitations = append(allowedCitations, evidence.Citations...)
		}
	}
	sourceAuthorizationHash, sourceAuthorizationErr := u.resolveAssistSourceAuthorizationHash(
		ctx, run, responsibleID, runtimeContext.Virployee.JobRoleID, allowedCitations,
	)
	if sourceAuthorizationErr != nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", "source_authorization_changed", 0)
		return failed, sourceAuthorizationErr
	}
	run.SourceAuthorizationHash = sourceAuthorizationHash
	if groundingMode == "sources_only" && !hasTextEvidence(contentParts) {
		contextHash := assistContextHash(tenantID, responsibleID, runtimeContext.Virployee.JobRoleID, assistMetadataForRun(run), allowedCitations, scopeSnapshotHash)
		return u.completeAbstainedAssist(ctx, run, responsibleID, groundingMode, "no_source_evidence", contextHash)
	}
	var memoryReferences []memories.Reference
	memoryContextHash := ""
	if groundingMode == "general" {
		answerInputJSON, memoryReferences, memoryContextHash = u.addScopedMemoryContext(ctx, run, responsibleID, query, answerInputJSON)
	}
	if groundingMode != "sources_only" {
		if orchestrated, handled, orchestrationErr := u.processOrchestratedAssist(ctx, run, answerInputJSON, contentParts); handled {
			return orchestrated, orchestrationErr
		}
	}
	if err := u.consumeQuota(ctx, quotaKey(tenantID, run.ProductSurface, quotas.AreaLLM), run.ID.String(), "assist_run", run.ID.String(), estimatedAnswerTokens(answerInputJSON, contentParts)); err != nil {
		return run, err
	}
	run, err = u.assistRepo.SetAssistRunStatus(ctx, tenantID, run.ID, "answering")
	if err != nil {
		return AssistRun{}, err
	}
	started := time.Now()
	out, answerErr := u.answerer.Answer(ctx, AnswerInput{
		SystemPrompt:        runtimeContext.ProfileTemplate.SystemPrompt,
		JobRole:             runtimeContext.JobRole.Name,
		ProfessionalContext: professionalContextFromJobRole(runtimeContext.JobRole),
		InputJSON:           answerInputJSON,
		ContentParts:        contentParts,
		GroundingMode:       groundingMode,
	})
	durationMS := time.Since(started).Milliseconds()
	if answerErr != nil {
		failedErr := runtraces.RedactText(answerErr.Error())
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", failedErr, durationMS)
		u.emitAssistAudit(ctx, tenantID, run.VirployeeID, failed, run.InputHash)
		return failed, domainerr.Unavailable("assist runtime failed")
	}
	u.recordLLMUsage(ctx, run, "answer", out)
	answerStatus := out.Status
	canonicalCitations := []knowledgebases.Citation{}
	if groundingMode == "sources_only" {
		validated, valid := validateGroundedCitations(out, allowedCitations)
		if !valid {
			out = abstainedAnswer(out, "unsupported_or_invalid_citations")
			answerStatus = "abstained"
		} else {
			canonicalCitations = validated
			answerStatus = "answered"
			out.OutputJSON = canonicalizeGroundedOutput(out.OutputJSON, answerStatus, canonicalCitations)
		}
	} else {
		if answerStatus == "" {
			if out.Answered {
				answerStatus = "answered"
			} else {
				answerStatus = "abstained"
			}
		}
		if validated, valid := validateOptionalCitations(out.Citations, allowedCitations); valid {
			canonicalCitations = validated
		}
	}
	degraded := !out.Answered && groundingMode != "sources_only"
	metadata := assistMetadataForRun(run)
	metadata.MemoryContextHash = memoryContextHash
	contextHash := assistContextHash(tenantID, responsibleID, runtimeContext.Virployee.JobRoleID, metadata, allowedCitations, scopeSnapshotHash)
	done, err := u.completeAssistWithGrounding(ctx, run, AssistCompletion{
		Status: "done", Output: out.OutputJSON, OutputText: out.OutputText, Answered: out.Answered,
		Degraded: degraded, Model: out.ModelID, PromptVersion: out.PromptVersion, DurationMS: durationMS,
		GroundingMode: groundingMode, AnswerStatus: answerStatus, ContextHash: contextHash,
		Citations: canonicalCitations, SourceContext: allowedCitations, MemoryContextHash: memoryContextHash,
		MemoryReferences: memoryReferences, JobRoleSnapshotHash: run.JobRoleSnapshotHash,
		SourceAuthorizationHash: run.SourceAuthorizationHash,
	})
	if err != nil {
		return AssistRun{}, err
	}
	u.emitAssistAudit(ctx, tenantID, responsibleID, done, run.InputHash)
	return done, nil
}

func responsibleVirployeeID(run AssistRun) uuid.UUID {
	if run.ResponsibleVirployeeID != uuid.Nil {
		return run.ResponsibleVirployeeID
	}
	return run.VirployeeID
}

func assistMetadataForRun(run AssistRun) AssistMetadata {
	return AssistMetadata{
		AssistType: run.AssistType, ProductSurface: run.ProductSurface, SubjectID: run.SubjectID,
		CaseID: run.CaseID, AssignmentID: run.AssignmentID, AssignmentVersion: run.AssignmentVersion,
		RepositoryGeneration: run.RepositoryGeneration, GroundingMode: run.GroundingMode,
		MemoryContextHash: run.MemoryContextHash, JobRoleSnapshotHash: run.JobRoleSnapshotHash,
		SourceAuthorizationHash: run.SourceAuthorizationHash,
	}
}

func assistContextHash(tenantID string, virployeeID, jobRoleID uuid.UUID, metadata AssistMetadata, citations []knowledgebases.Citation, policySnapshotHash string) string {
	parts := []string{
		"assist-context.v1", strings.TrimSpace(tenantID), virployeeID.String(), jobRoleID.String(),
		strings.TrimSpace(metadata.SubjectID), metadata.CaseID.String(), metadata.AssignmentID.String(),
		strconv.FormatInt(metadata.AssignmentVersion, 10), strings.TrimSpace(metadata.ProductSurface),
		strings.TrimSpace(metadata.AssistType), strings.TrimSpace(metadata.RepositoryGeneration),
		strings.ToLower(strings.TrimSpace(metadata.GroundingMode)), strings.TrimSpace(metadata.MemoryContextHash),
		strings.TrimSpace(metadata.JobRoleSnapshotHash), strings.TrimSpace(metadata.SourceAuthorizationHash),
		strings.TrimSpace(policySnapshotHash),
	}
	sources := make([]string, 0, len(citations))
	for _, citation := range citations {
		baseID := ""
		if citation.KnowledgeBaseID != nil {
			baseID = citation.KnowledgeBaseID.String()
		}
		sources = append(sources, strings.Join([]string{
			baseID, strings.TrimSpace(citation.DocumentID), strings.TrimSpace(citation.SourceVersion),
			strings.TrimSpace(citation.SHA256), string(citation.Locator),
		}, "\x1f"))
	}
	sort.Strings(sources)
	parts = append(parts, sources...)
	return runtraces.HashString(strings.Join(parts, "\x00"))
}

func (u *UseCases) resolveAssistSourceAuthorizationHash(ctx context.Context, run AssistRun, virployeeID, jobRoleID uuid.UUID, citations []knowledgebases.Citation) (string, error) {
	if u.assistExecution != nil {
		return u.assistExecution.AssistSourceAuthorizationHash(ctx, run.TenantID, virployeeID, jobRoleID, run, citations)
	}
	// Non-database adapters still bind the exact source set. Production wires
	// the Repository above and therefore also binds monotonic catalog revisions.
	return runtraces.HashString("assist-source-authorization.fallback.v1\x00" + assistSourcesHash(citations)), nil
}

func sanitizeAssistInput(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`{}`)
	}
	var scrub func(any)
	scrub = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if strings.EqualFold(strings.TrimSpace(key), "read_url") {
					delete(typed, key)
					continue
				}
				scrub(child)
			}
		case []any:
			for _, child := range typed {
				scrub(child)
			}
		}
	}
	scrub(value)
	out, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

func assistRetrievalQuery(raw json.RawMessage) string {
	var value map[string]any
	if json.Unmarshal(sanitizeAssistInput(raw), &value) == nil {
		for _, key := range []string{"question", "query", "prompt", "text", "input"} {
			if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	fallback := strings.TrimSpace(string(sanitizeAssistInput(raw)))
	if len(fallback) > 4000 {
		fallback = fallback[:4000]
	}
	if fallback == "" {
		return "assist request"
	}
	return fallback
}

func hasTextEvidence(parts []artifacts.ContentPart) bool {
	for _, part := range parts {
		if part.Kind == artifacts.PartText && strings.TrimSpace(part.Text) != "" && strings.TrimSpace(part.DocumentID) != "" {
			return true
		}
	}
	return false
}

func citationsForParts(parts []artifacts.ContentPart, sourceVersion string) []knowledgebases.Citation {
	out := make([]knowledgebases.Citation, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		if part.Kind != artifacts.PartText || strings.TrimSpace(part.Text) == "" || strings.TrimSpace(part.DocumentID) == "" {
			continue
		}
		locator, _ := json.Marshal(part.Locator)
		key := part.DocumentID + "\x00" + part.SHA256 + "\x00" + string(locator)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, knowledgebases.Citation{DocumentID: part.DocumentID, SourceVersion: strings.TrimSpace(sourceVersion), SHA256: part.SHA256, Locator: locator})
	}
	return out
}

func (u *UseCases) addScopedMemoryContext(ctx context.Context, run AssistRun, responsibleID uuid.UUID, query string, input json.RawMessage) (json.RawMessage, []memories.Reference, string) {
	reader, ok := u.memories.(ScopedMemoryReaderPort)
	if !ok || reader == nil {
		return input, nil, ""
	}
	scope := memories.Scope{Type: memories.ScopeVirployee}
	if run.CaseID != uuid.Nil && strings.TrimSpace(run.SubjectID) != "" {
		caseID := run.CaseID
		scope = memories.Scope{Type: memories.ScopeCase, SubjectID: run.SubjectID, CaseID: &caseID}
	} else if strings.TrimSpace(run.SubjectID) != "" {
		scope = memories.Scope{Type: memories.ScopeSubject, SubjectID: run.SubjectID}
	}
	items, err := reader.RecallScopedInternal(ctx, run.TenantID, responsibleID, scope, query, 5)
	if err != nil {
		slog.WarnContext(ctx, "scoped_memory_recall_failed", "error", runtraces.RedactText(err.Error()), "assist_run_id", run.ID.String())
		return input, nil, ""
	}
	if len(items) == 0 {
		return input, nil, ""
	}
	contextItems := make([]memories.ContextItem, 0, len(items))
	references := make([]memories.Reference, 0, len(items))
	for _, item := range items {
		contextItems = append(contextItems, memories.ContextItem{Title: item.Memory.Title, Type: item.Memory.Type, Content: item.Memory.Content})
		references = append(references, item.Reference)
	}
	out, err := json.Marshal(map[string]any{"request": json.RawMessage(input), "memory_context": contextItems})
	if err != nil {
		return input, nil, ""
	}
	return out, references, memories.ContextHash(references)
}

func validateGroundedCitations(out AnswerOutput, allowed []knowledgebases.Citation) ([]knowledgebases.Citation, bool) {
	if !out.Answered || strings.ToLower(strings.TrimSpace(out.Status)) != "answered" || len(out.Citations) == 0 {
		return nil, false
	}
	return validateOptionalCitations(out.Citations, allowed)
}

func validateOptionalCitations(requested []RuntimeCitation, allowed []knowledgebases.Citation) ([]knowledgebases.Citation, bool) {
	if len(requested) == 0 {
		return []knowledgebases.Citation{}, true
	}
	byDocument := make(map[string][]knowledgebases.Citation)
	for _, citation := range allowed {
		byDocument[citation.DocumentID] = append(byDocument[citation.DocumentID], citation)
	}
	validated := make([]knowledgebases.Citation, 0, len(requested))
	seen := map[string]struct{}{}
	for _, citation := range requested {
		candidates := byDocument[strings.TrimSpace(citation.DocumentID)]
		matched := false
		for _, candidate := range candidates {
			if strings.TrimSpace(citation.SHA256) != "" && citation.SHA256 != candidate.SHA256 {
				continue
			}
			if len(citation.Locator) > 0 && string(citation.Locator) != "null" && !sameJSON(citation.Locator, candidate.Locator) {
				continue
			}
			key := candidate.DocumentID + "\x00" + candidate.SHA256 + "\x00" + string(candidate.Locator)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				validated = append(validated, candidate)
			}
			matched = true
			break
		}
		if !matched {
			return nil, false
		}
	}
	sort.SliceStable(validated, func(i, j int) bool {
		if validated[i].DocumentID != validated[j].DocumentID {
			return validated[i].DocumentID < validated[j].DocumentID
		}
		return string(validated[i].Locator) < string(validated[j].Locator)
	})
	return validated, true
}

func sameJSON(left, right json.RawMessage) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil &&
		runtraces.HashString(string(mustCanonicalJSON(a))) == runtraces.HashString(string(mustCanonicalJSON(b)))
}

func mustCanonicalJSON(value any) []byte {
	out, _ := json.Marshal(value)
	return out
}

func canonicalizeGroundedOutput(raw json.RawMessage, status string, citations []knowledgebases.Citation) json.RawMessage {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil || payload == nil {
		payload = map[string]any{"answer": ""}
	}
	payload["status"] = status
	payload["citations"] = citations
	out, _ := json.Marshal(payload)
	return out
}

func abstainedAnswer(previous AnswerOutput, reason string) AnswerOutput {
	previous.OutputText = "No está en las fuentes disponibles."
	previous.OutputJSON, _ = json.Marshal(map[string]any{
		"status": "abstained", "answer": previous.OutputText, "citations": []any{}, "reason": reason,
	})
	previous.Answered = false
	previous.Status = "abstained"
	previous.Citations = nil
	return previous
}

func (u *UseCases) completeAbstainedAssist(ctx context.Context, run AssistRun, responsibleID uuid.UUID, groundingMode, reason, contextHash string) (AssistRun, error) {
	out := abstainedAnswer(AnswerOutput{}, reason)
	done, err := u.completeAssistWithGrounding(ctx, run, AssistCompletion{
		Status: "done", Output: out.OutputJSON, OutputText: out.OutputText,
		GroundingMode: groundingMode, AnswerStatus: "abstained", ContextHash: contextHash,
		JobRoleSnapshotHash: run.JobRoleSnapshotHash, SourceAuthorizationHash: run.SourceAuthorizationHash,
	})
	if err != nil {
		return AssistRun{}, err
	}
	u.emitAssistAudit(ctx, run.TenantID, responsibleID, done, run.InputHash)
	return done, nil
}

func (u *UseCases) completeScopeDecisionAssist(ctx context.Context, run AssistRun, responsibleID uuid.UUID, groundingMode, decision, reason, contextHash string) (AssistRun, error) {
	answerStatus, runStatus := "abstained", "done"
	message := "Esta consulta está fuera de mi alcance autorizado."
	if strings.EqualFold(strings.TrimSpace(decision), "escalate") {
		answerStatus, runStatus = "escalation_required", AssistStatusNeedsHuman
		message = "Esta consulta requiere revisión humana antes de responder."
	}
	output, _ := json.Marshal(map[string]any{
		"status": answerStatus, "answer": message, "citations": []any{}, "reason": reason,
	})
	done, err := u.completeAssistWithGrounding(ctx, run, AssistCompletion{
		Status: runStatus, Output: output, OutputText: message,
		GroundingMode: groundingMode, AnswerStatus: answerStatus, ContextHash: contextHash,
		JobRoleSnapshotHash: run.JobRoleSnapshotHash, SourceAuthorizationHash: run.SourceAuthorizationHash,
	})
	if err != nil {
		return AssistRun{}, err
	}
	u.emitAssistAudit(ctx, run.TenantID, responsibleID, done, run.InputHash)
	return done, nil
}

func (u *UseCases) completeAssistWithGrounding(ctx context.Context, run AssistRun, completion AssistCompletion) (AssistRun, error) {
	writer, ok := u.assistRepo.(AssistGroundedCompletionRepositoryPort)
	if !ok || writer == nil {
		return AssistRun{}, domainerr.Conflict("Assist repository does not support atomic grounded completion")
	}
	return writer.CompleteAssistRunWithGrounding(ctx, run.TenantID, run.ID, completion)
}

func (u *UseCases) ingestArtifacts(ctx context.Context, run AssistRun) ([]artifacts.ContentPart, bool, error) {
	if u.artifactIngestor == nil {
		return nil, false, nil
	}
	var parsed struct {
		Documents []assistDocRef `json:"documents"`
	}
	if err := json.Unmarshal(run.InputJSON, &parsed); err != nil || len(parsed.Documents) == 0 {
		return nil, false, nil
	}
	if strings.TrimSpace(run.SubjectID) == "" || strings.TrimSpace(run.ProductSurface) == "" || strings.TrimSpace(run.RepositoryGeneration) == "" {
		return nil, false, domainerr.Validation("artifact assist metadata is incomplete")
	}
	manifests := make([]artifacts.Manifest, 0, len(parsed.Documents))
	var totalBytes int64
	for _, doc := range parsed.Documents {
		required := true
		if doc.Required != nil {
			required = *doc.Required
		}
		manifests = append(manifests, artifacts.Manifest{
			DocumentID: doc.DocumentID, Name: doc.Key, SourceRef: doc.Key, ReadURL: doc.ReadURL,
			SHA256: doc.SHA256, MIMEType: doc.ContentType, SizeBytes: doc.SizeBytes, Required: required,
		})
		totalBytes += doc.SizeBytes
	}
	if err := u.consumeQuota(ctx, quotaKey(run.TenantID, run.ProductSurface, quotas.AreaBytes), run.ID.String(), "assist_run", run.ID.String(), totalBytes); err != nil {
		return nil, true, err
	}
	result, err := u.artifactIngestor.Ingest(ctx, artifacts.IngestRequest{
		Scope: artifacts.Scope{
			TenantID: run.TenantID, VirployeeID: run.VirployeeID, ProductSurface: run.ProductSurface,
			SubjectID: run.SubjectID, RepositoryGeneration: caseScopedRepositoryGeneration(run.RepositoryGeneration, run.CaseID),
		},
		Artifacts: manifests,
		Progress: func(progressCtx context.Context, status artifacts.Status) error {
			assistStatus := ""
			switch status {
			case artifacts.StatusExtracting:
				assistStatus = "extracting"
			case artifacts.StatusIndexing:
				assistStatus = "indexing"
			}
			if assistStatus == "" {
				return nil
			}
			_, progressErr := u.assistRepo.SetAssistRunStatus(progressCtx, run.TenantID, run.ID, assistStatus)
			return progressErr
		},
	})
	if err != nil {
		return nil, true, err
	}
	return result.Parts, true, nil
}

func caseScopedRepositoryGeneration(repositoryGeneration string, caseID uuid.UUID) string {
	repositoryGeneration = strings.TrimSpace(repositoryGeneration)
	if caseID == uuid.Nil {
		return repositoryGeneration
	}
	return repositoryGeneration + ":case:" + caseID.String()
}

func (u *UseCases) GetAssistRun(ctx context.Context, tenantID string, virployeeID, runID uuid.UUID) (AssistRun, error) {
	run, err := u.assistRepo.GetAssistRunByID(ctx, normalizeTenantID(tenantID), runID)
	if err != nil {
		return AssistRun{}, err
	}
	if run.CaseID != uuid.Nil {
		if u.coordinationRepo == nil {
			return AssistRun{}, domainerr.NotFound("assist run not found")
		}
		assistCase, caseErr := u.coordinationRepo.GetAssistCase(ctx, normalizeTenantID(tenantID), run.CaseID)
		if caseErr != nil || assistCase.OwnerVirployeeID != virployeeID {
			return AssistRun{}, domainerr.NotFound("assist run not found")
		}
	} else if run.VirployeeID != virployeeID {
		return AssistRun{}, domainerr.NotFound("assist run not found")
	}
	if summary, summaryErr := u.LoadOrchestrationSummary(ctx, run); summaryErr == nil {
		run.Orchestration = summary
	}
	return run, nil
}

func (u *UseCases) RequeueReceivedAssistRuns(ctx context.Context, limit int) (int, error) {
	if u.assistQueue == nil {
		return 0, domainerr.Conflict("assist queue is not configured")
	}
	runs, err := u.assistRepo.ListReceivedAssistRuns(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, run := range runs {
		if err := u.assistQueue.EnqueueAssist(ctx, run); err != nil {
			return 0, err
		}
	}
	return len(runs), nil
}

func (u *UseCases) emitAssistAudit(ctx context.Context, tenantID string, virployeeID uuid.UUID, run AssistRun, inputHash string) {
	if u.auditEmitter == nil {
		return
	}
	eventType, summary := "assist_completed", "assist run completed"
	if run.Status == "failed" {
		eventType, summary = "assist_failed", "assist run failed"
	}
	data := map[string]any{
		"input_hash": inputHash, "model": run.Model, "prompt_version": run.PromptVersion,
		"answered": run.Answered, "degraded": run.Degraded, "status": run.Status, "duration_ms": run.DurationMS,
		"grounding_mode": run.GroundingMode, "answer_status": run.AnswerStatus, "citation_count": len(run.Citations),
	}
	if len(run.Output) > 0 {
		data["output_hash"] = runtraces.HashString(string(run.Output))
	}
	if run.Error != "" {
		data["error"] = run.Error
	}
	if err := u.auditEmitter.AppendAuditEvent(ctx, AuditEventInput{
		TenantID: tenantID, VirployeeID: virployeeID.String(), ActorType: "virployee", ActorID: virployeeID.String(),
		SubjectType: "assist_run", SubjectID: run.ID.String(), EventType: eventType, Summary: summary, Data: data,
	}); err != nil {
		slog.ErrorContext(ctx, "audit emit failed for assist run", "error", err, "tenant_id", tenantID, "virployee_id", virployeeID.String(), "assist_run_id", run.ID.String())
	}
}
