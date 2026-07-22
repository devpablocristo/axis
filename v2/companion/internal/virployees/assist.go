package virployees

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
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
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = runtraces.HashString(tenantID + ":" + id.String() + ":" + string(inputJSON))
	}
	if err := u.consumeQuota(ctx, quotaKey(tenantID, metadata.ProductSurface, quotas.AreaInbound), idempotencyKey, "virployee", id.String(), 1); err != nil {
		return AssistRun{}, false, err
	}
	// Fail before accepting work when the virployee/profile is not executable.
	if _, err := u.RuntimeContext(ctx, tenantID, id); err != nil {
		return AssistRun{}, false, err
	}
	inputHash := runtraces.HashString(string(inputJSON))
	run, reserved, err := u.assistRepo.BeginAssistRun(ctx, tenantID, id, metadata, idempotencyKey, inputHash, runtraces.InputPreview(string(inputJSON)), inputJSON)
	if err != nil {
		return AssistRun{}, false, err
	}
	if !reserved && run.InputHash != "" && run.InputHash != inputHash {
		return AssistRun{}, false, domainerr.Conflict("idempotency key was already used with different input")
	}
	return run, reserved, nil
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
	if run.Status == "done" || run.Status == "failed" {
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
		default:
			return run, domainerr.Conflict("assist run is already being processed")
		}
	}
	runtimeContext, err := u.RuntimeContext(ctx, tenantID, run.VirployeeID)
	if err != nil {
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", "runtime_context_unavailable", 0)
		return failed, err
	}

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
		enriched, err := json.Marshal(map[string]any{"content_parts": contentParts})
		if err != nil {
			return AssistRun{}, err
		}
		answerInputJSON = enriched
	} else {
		answerInputJSON = u.resolveDocuments(ctx, run.InputJSON)
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
		SystemPrompt: runtimeContext.ProfileTemplate.SystemPrompt,
		JobRole:      runtimeContext.JobRole.Name,
		InputJSON:    answerInputJSON,
		ContentParts: contentParts,
	})
	durationMS := time.Since(started).Milliseconds()
	if answerErr != nil {
		failedErr := runtraces.RedactText(answerErr.Error())
		failed, _ := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", failedErr, durationMS)
		u.emitAssistAudit(ctx, tenantID, run.VirployeeID, failed, run.InputHash)
		return failed, domainerr.Unavailable("assist runtime failed")
	}
	u.recordLLMUsage(ctx, run, out)

	done, err := u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "done", out.OutputJSON, out.OutputText, out.Answered, !out.Answered, out.ModelID, out.PromptVersion, "", durationMS)
	if err != nil {
		return AssistRun{}, err
	}
	u.emitAssistAudit(ctx, tenantID, run.VirployeeID, done, run.InputHash)
	return done, nil
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
			SubjectID: run.SubjectID, RepositoryGeneration: run.RepositoryGeneration,
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

func (u *UseCases) GetAssistRun(ctx context.Context, tenantID string, virployeeID, runID uuid.UUID) (AssistRun, error) {
	run, err := u.assistRepo.GetAssistRunByID(ctx, normalizeTenantID(tenantID), runID)
	if err != nil {
		return AssistRun{}, err
	}
	if run.VirployeeID != virployeeID {
		return AssistRun{}, domainerr.NotFound("assist run not found")
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
