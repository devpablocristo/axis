package virployees

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AssistRun is the durable accountability trace for product-facing work. The
// raw input is persisted for the worker, but never returned by HTTP or emitted
// to logs/evidence.
type AssistRun struct {
	ID                      uuid.UUID
	OrgID                   string
	VirployeeID             uuid.UUID
	CaseID                  uuid.UUID
	ResponsibleVirployeeID  uuid.UUID
	OrchestrationPlanID     uuid.UUID
	OrchestrationDeadlineAt *time.Time
	OwnershipVersion        int64
	AssistType              string
	ProductSurface          string
	SubjectID               string
	AssignmentID            uuid.UUID
	AssignmentVersion       int64
	RepositoryGeneration    string
	CapabilityKey           string
	CapabilityManifestHash  string
	GroundingMode           string
	ContextHash             string
	MemoryContextHash       string
	MemoryReferences        []memories.Reference
	JobRoleSnapshotHash     string
	SourceAuthorizationHash string
	AnswerStatus            string
	Citations               []knowledgebases.Citation
	SourceContext           []knowledgebases.Citation
	IdempotencyKey          string
	Status                  string // received ... planning | consulting | synthesizing | done | failed | needs_human
	InputHash               string
	InputPreview            string
	InputJSON               json.RawMessage
	Output                  json.RawMessage
	OutputText              string
	Answered                bool
	Degraded                bool
	Model                   string
	PromptVersion           string
	Error                   string
	DurationMS              int64
	StartedAt               time.Time
	CompletedAt             *time.Time
	Orchestration           *OrchestrationSummary
}

// BeginAssistRun stores the full input before a durable job is enqueued. A
// concurrent retry receives the existing run and cannot create a second model
// invocation for the same stable idempotency key.
func (r *Repository) BeginAssistRun(ctx context.Context, orgID string, virployeeID uuid.UUID, metadata AssistMetadata, idempotencyKey, inputHash, inputPreview string, inputJSON json.RawMessage) (AssistRun, bool, error) {
	if len(inputJSON) == 0 {
		inputJSON = json.RawMessage(`{}`)
	}
	id := uuid.New()
	var caseID any
	responsibleID := virployeeID
	if metadata.CaseID != uuid.Nil {
		assistCase, caseErr := r.GetAssistCase(ctx, orgID, metadata.CaseID)
		if caseErr != nil {
			return AssistRun{}, false, caseErr
		}
		if assistCase.Status != "open" && assistCase.Status != AssistStatusNeedsHuman {
			return AssistRun{}, false, domainerr.Conflict("case is closed and cannot receive new Assist work")
		}
		if strings.TrimSpace(assistCase.SubjectID) != strings.TrimSpace(metadata.SubjectID) {
			return AssistRun{}, false, domainerr.Conflict("case does not belong to the requested subject")
		}
		if metadata.ProductSurface != "" && strings.TrimSpace(metadata.ProductSurface) != assistCase.ProductSurface {
			return AssistRun{}, false, domainerr.Conflict("case product_surface does not match")
		}
		if metadata.AssistType != "" && strings.TrimSpace(metadata.AssistType) != assistCase.AssistType {
			return AssistRun{}, false, domainerr.Conflict("case assist_type does not match")
		}
		if assistCase.OwnerVirployeeID != virployeeID {
			return AssistRun{}, false, domainerr.Conflict("case does not belong to the requested Virployee")
		}
		metadata.ProductSurface = assistCase.ProductSurface
		metadata.AssistType = assistCase.AssistType
		caseID = assistCase.ID
		responsibleID = assistCase.OwnerVirployeeID
	} else if metadata.SubjectID != "" && metadata.ProductSurface != "" && metadata.AssistType != "" {
		assistCase, caseErr := r.EnsureAssistCase(ctx, orgID, virployeeID, metadata)
		if caseErr != nil {
			return AssistRun{}, false, caseErr
		}
		if assistCase.OwnerVirployeeID != virployeeID {
			return AssistRun{}, false, domainerr.Conflict("case owner differs from the requested Virployee; audited reassignment is required")
		}
		caseID = assistCase.ID
		responsibleID = assistCase.OwnerVirployeeID
	}
	if metadata.AssignmentID != uuid.Nil {
		// Stable routing may select who owns new work, but it is not an audited
		// case-transfer mechanism. Existing ownership can only change through a
		// handoff/reassignment workflow, never as a side effect of BeginAssistRun.
		if caseID != nil && responsibleID != virployeeID {
			return AssistRun{}, false, domainerr.Conflict("case owner differs from the continuity assignee; audited reassignment is required")
		}
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO companion_assist_runs (
			id, org_id, virployee_id, assist_type, product_surface, subject_id, repository_generation,
			capability_key, capability_manifest_hash,
			idempotency_key, status, input_hash, input_preview, input_json, case_id, responsible_virployee_id,
			grounding_mode, continuity_assignment_id, continuity_assignment_version, context_hash,
			job_role_snapshot_hash, source_authorization_hash, started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8,''), NULLIF($9,''), $10, 'received', $11, $12, $13::jsonb, $14, $15, $16, $17, $18, $19, $20, $21, now(), now())
		ON CONFLICT (org_id, virployee_id, idempotency_key) DO NOTHING
	`, id, orgID, virployeeID, metadata.AssistType, metadata.ProductSurface, metadata.SubjectID, metadata.RepositoryGeneration,
		metadata.CapabilityKey, metadata.CapabilityManifestHash, idempotencyKey, inputHash, inputPreview, []byte(inputJSON), caseID, responsibleID, metadata.GroundingMode,
		nullableAssistUUID(metadata.AssignmentID), metadata.AssignmentVersion, metadata.ContextHash, metadata.JobRoleSnapshotHash,
		metadata.SourceAuthorizationHash)
	if err != nil {
		return AssistRun{}, false, err
	}
	run, err := r.GetAssistRunByKey(ctx, orgID, virployeeID, idempotencyKey)
	return run, tag.RowsAffected() == 1, err
}

// ClaimAssistRun provides a second idempotency barrier at the work item. The
// queue lease is renewable; this transition ensures a duplicate delivery still
// cannot execute the model twice.
func (r *Repository) ClaimAssistRun(ctx context.Context, orgID string, id uuid.UUID, recoverPreAnswer bool) (AssistRun, bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE companion_assist_runs
		SET status = 'staging', updated_at = now()
		WHERE org_id = $1 AND id = $2
		  AND (status = 'received' OR ($3 AND status IN ('staging','extracting','indexing','planning')))
	`, orgID, id, recoverPreAnswer)
	if err != nil {
		return AssistRun{}, false, err
	}
	run, err := r.GetAssistRunByID(ctx, orgID, id)
	return run, tag.RowsAffected() == 1, err
}

func (r *Repository) SetAssistRunStatus(ctx context.Context, orgID string, id uuid.UUID, status string) (AssistRun, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE companion_assist_runs SET status=$3, updated_at=now()
		WHERE org_id=$1 AND id=$2 AND status NOT IN ('done','failed','needs_human')
	`, orgID, id, status)
	if err != nil {
		return AssistRun{}, err
	}
	return r.GetAssistRunByID(ctx, orgID, id)
}

func (r *Repository) CompleteAssistRunForOwner(ctx context.Context, orgID string, id uuid.UUID, ownershipVersion int64, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error) {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx, `UPDATE companion_assist_runs SET status=$4,output=$5::jsonb,output_text=$6,
		answered=$7,degraded=$8,model=$9,prompt_version=$10,error=$11,duration_ms=$12,
		completed_at=now(),updated_at=now() WHERE org_id=$1 AND id=$2 AND ownership_version=$3
		AND status NOT IN ('done','failed','needs_human')`, orgID, id, ownershipVersion, status, []byte(output), outputText,
		answered, degraded, model, promptVersion, runErr, durationMS)
	if err != nil {
		return AssistRun{}, err
	}
	if tag.RowsAffected() != 1 {
		return AssistRun{}, domainerr.Conflict("assist ownership changed while the answer was being produced")
	}
	return r.GetAssistRunByID(ctx, orgID, id)
}

func (r *Repository) CompleteAssistRun(ctx context.Context, orgID string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error) {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE companion_assist_runs
		SET status = $3, output = $4::jsonb, output_text = $5, answered = $6, degraded = $7,
		    model = $8, prompt_version = $9, error = $10, duration_ms = $11, completed_at = now(), updated_at = now()
		WHERE org_id = $1 AND id = $2
	`, orgID, id, status, []byte(output), outputText, answered, degraded, model, promptVersion, runErr, durationMS)
	if err != nil {
		return AssistRun{}, err
	}
	return r.GetAssistRunByID(ctx, orgID, id)
}

func (r *Repository) SetAssistGrounding(ctx context.Context, orgID string, id uuid.UUID, groundingMode, answerStatus, contextHash string, citations, sourceContext []knowledgebases.Citation, memoryContextHash string, memoryReferences []memories.Reference, jobRoleSnapshotHash, sourceAuthorizationHash string) (AssistRun, error) {
	if citations == nil {
		citations = []knowledgebases.Citation{}
	}
	if memoryReferences == nil {
		memoryReferences = []memories.Reference{}
	}
	if sourceContext == nil {
		sourceContext = []knowledgebases.Citation{}
	}
	raw, err := json.Marshal(citations)
	if err != nil {
		return AssistRun{}, err
	}
	rawMemoryReferences, err := json.Marshal(memoryReferences)
	if err != nil {
		return AssistRun{}, err
	}
	rawSourceContext, err := json.Marshal(sourceContext)
	if err != nil {
		return AssistRun{}, err
	}
	_, err = r.pool.Exec(ctx, `UPDATE companion_assist_runs
		SET grounding_mode=$3,answer_status=$4,context_hash=$5,citations=$6::jsonb,
		    source_context=$7::jsonb,memory_context_hash=$8,memory_references=$9::jsonb,
		    job_role_snapshot_hash=$10,source_authorization_hash=$11,updated_at=now()
		WHERE org_id=$1 AND id=$2`, orgID, id, groundingMode, answerStatus, contextHash, raw,
		rawSourceContext, memoryContextHash, rawMemoryReferences, jobRoleSnapshotHash, sourceAuthorizationHash)
	if err != nil {
		return AssistRun{}, err
	}
	return r.GetAssistRunByID(ctx, orgID, id)
}

func (r *Repository) CompleteAssistRunWithGrounding(ctx context.Context, orgID string, id uuid.UUID, completion AssistCompletion) (AssistRun, error) {
	if len(completion.Output) == 0 {
		completion.Output = json.RawMessage(`{}`)
	}
	if completion.Citations == nil {
		completion.Citations = []knowledgebases.Citation{}
	}
	if completion.SourceContext == nil {
		completion.SourceContext = []knowledgebases.Citation{}
	}
	if completion.MemoryReferences == nil {
		completion.MemoryReferences = []memories.Reference{}
	}
	citations, err := json.Marshal(completion.Citations)
	if err != nil {
		return AssistRun{}, err
	}
	sourceContext, err := json.Marshal(completion.SourceContext)
	if err != nil {
		return AssistRun{}, err
	}
	memoryReferences, err := json.Marshal(completion.MemoryReferences)
	if err != nil {
		return AssistRun{}, err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE companion_assist_runs
		SET status=$3,output=$4::jsonb,output_text=$5,answered=$6,degraded=$7,
		    model=$8,prompt_version=$9,error=$10,duration_ms=$11,
		    grounding_mode=$12,answer_status=$13,context_hash=$14,citations=$15::jsonb,
		    source_context=$16::jsonb,memory_context_hash=$17,memory_references=$18::jsonb,
		    job_role_snapshot_hash=$19,source_authorization_hash=$20,
		    completed_at=now(),updated_at=now()
		WHERE org_id=$1 AND id=$2 AND status NOT IN ('done','failed','needs_human')`,
		orgID, id, completion.Status, []byte(completion.Output), completion.OutputText,
		completion.Answered, completion.Degraded, completion.Model, completion.PromptVersion,
		completion.RunError, completion.DurationMS, completion.GroundingMode, completion.AnswerStatus,
		completion.ContextHash, citations, sourceContext, completion.MemoryContextHash, memoryReferences,
		completion.JobRoleSnapshotHash, completion.SourceAuthorizationHash)
	if err != nil {
		return AssistRun{}, err
	}
	if tag.RowsAffected() != 1 {
		return AssistRun{}, domainerr.Conflict("assist run is already terminal")
	}
	return r.GetAssistRunByID(ctx, orgID, id)
}

func (r *Repository) GetAssistRunByKey(ctx context.Context, orgID string, virployeeID uuid.UUID, idempotencyKey string) (AssistRun, error) {
	return r.scanAssistRun(r.pool.QueryRow(ctx, assistRunSelect+`
		WHERE org_id = $1 AND virployee_id = $2 AND idempotency_key = $3
	`, orgID, virployeeID, idempotencyKey))
}

func (r *Repository) GetAssistRunByID(ctx context.Context, orgID string, id uuid.UUID) (AssistRun, error) {
	return r.scanAssistRun(r.pool.QueryRow(ctx, assistRunSelect+`
		WHERE org_id = $1 AND id = $2
	`, orgID, id))
}

func (r *Repository) ListReceivedAssistRuns(ctx context.Context, limit int) ([]AssistRun, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, assistRunSelect+`
		WHERE status = 'received'
		ORDER BY updated_at, id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AssistRun, 0, limit)
	for rows.Next() {
		run, err := r.scanAssistRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

const assistRunSelect = `
	SELECT id, org_id, virployee_id,
	       COALESCE(case_id,'00000000-0000-0000-0000-000000000000'::uuid),
	       COALESCE(responsible_virployee_id,virployee_id),
	       COALESCE(orchestration_plan_id,'00000000-0000-0000-0000-000000000000'::uuid),
	       orchestration_deadline_at,ownership_version,
	       assist_type, product_surface, subject_id,
	       COALESCE(continuity_assignment_id,'00000000-0000-0000-0000-000000000000'::uuid),
	       continuity_assignment_version,repository_generation,
	       COALESCE(capability_key,''),COALESCE(capability_manifest_hash,''),
	       grounding_mode,context_hash,memory_context_hash,memory_references,job_role_snapshot_hash,
	       source_authorization_hash,
	       answer_status,citations,source_context,
	       idempotency_key, status, input_hash, input_preview,
	       input_json, output, output_text, answered, degraded, model, prompt_version, error, duration_ms,
	       started_at, completed_at
	FROM companion_assist_runs
`

type rowScanner interface{ Scan(dest ...any) error }

func (r *Repository) scanAssistRun(row rowScanner) (AssistRun, error) {
	var out AssistRun
	var input, output, citations, sourceContext, memoryReferences []byte
	err := row.Scan(
		&out.ID, &out.OrgID, &out.VirployeeID, &out.CaseID, &out.ResponsibleVirployeeID,
		&out.OrchestrationPlanID, &out.OrchestrationDeadlineAt, &out.OwnershipVersion,
		&out.AssistType, &out.ProductSurface, &out.SubjectID, &out.AssignmentID, &out.AssignmentVersion,
		&out.RepositoryGeneration, &out.CapabilityKey, &out.CapabilityManifestHash,
		&out.GroundingMode, &out.ContextHash, &out.MemoryContextHash, &memoryReferences,
		&out.JobRoleSnapshotHash, &out.SourceAuthorizationHash, &out.AnswerStatus, &citations, &sourceContext, &out.IdempotencyKey, &out.Status,
		&out.InputHash, &out.InputPreview, &input, &output, &out.OutputText, &out.Answered, &out.Degraded,
		&out.Model, &out.PromptVersion, &out.Error, &out.DurationMS, &out.StartedAt, &out.CompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return AssistRun{}, domainerr.NotFound("assist run not found")
		}
		return AssistRun{}, err
	}
	if len(input) > 0 {
		out.InputJSON = json.RawMessage(input)
	}
	if len(output) > 0 {
		out.Output = json.RawMessage(output)
	}
	if len(citations) > 0 {
		if err := json.Unmarshal(citations, &out.Citations); err != nil {
			return AssistRun{}, err
		}
	}
	if len(sourceContext) > 0 {
		if err := json.Unmarshal(sourceContext, &out.SourceContext); err != nil {
			return AssistRun{}, err
		}
	}
	if len(memoryReferences) > 0 {
		if err := json.Unmarshal(memoryReferences, &out.MemoryReferences); err != nil {
			return AssistRun{}, err
		}
	}
	return out, nil
}

func nullableAssistUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
