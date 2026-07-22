package virployees

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AssistRun is the durable accountability trace for product-facing work. The
// raw input is persisted for the worker, but never returned by HTTP or emitted
// to logs/evidence.
type AssistRun struct {
	ID                      uuid.UUID
	TenantID                string
	VirployeeID             uuid.UUID
	CaseID                  uuid.UUID
	ResponsibleVirployeeID  uuid.UUID
	OrchestrationPlanID     uuid.UUID
	OrchestrationDeadlineAt *time.Time
	OwnershipVersion        int64
	AssistType              string
	ProductSurface          string
	SubjectID               string
	RepositoryGeneration    string
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
func (r *Repository) BeginAssistRun(ctx context.Context, tenantID string, virployeeID uuid.UUID, metadata AssistMetadata, idempotencyKey, inputHash, inputPreview string, inputJSON json.RawMessage) (AssistRun, bool, error) {
	if len(inputJSON) == 0 {
		inputJSON = json.RawMessage(`{}`)
	}
	id := uuid.New()
	var caseID any
	responsibleID := virployeeID
	if metadata.SubjectID != "" && metadata.ProductSurface != "" && metadata.AssistType != "" {
		assistCase, caseErr := r.EnsureAssistCase(ctx, tenantID, virployeeID, metadata)
		if caseErr != nil {
			return AssistRun{}, false, caseErr
		}
		caseID = assistCase.ID
		responsibleID = assistCase.OwnerVirployeeID
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO companion_assist_runs (
			id, tenant_id, virployee_id, assist_type, product_surface, subject_id, repository_generation,
			idempotency_key, status, input_hash, input_preview, input_json, case_id, responsible_virployee_id,
			started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'received', $9, $10, $11::jsonb, $12, $13, now(), now())
		ON CONFLICT (tenant_id, virployee_id, idempotency_key) DO NOTHING
	`, id, tenantID, virployeeID, metadata.AssistType, metadata.ProductSurface, metadata.SubjectID, metadata.RepositoryGeneration,
		idempotencyKey, inputHash, inputPreview, []byte(inputJSON), caseID, responsibleID)
	if err != nil {
		return AssistRun{}, false, err
	}
	run, err := r.GetAssistRunByKey(ctx, tenantID, virployeeID, idempotencyKey)
	return run, tag.RowsAffected() == 1, err
}

// ClaimAssistRun provides a second idempotency barrier at the work item. The
// queue lease is renewable; this transition ensures a duplicate delivery still
// cannot execute the model twice.
func (r *Repository) ClaimAssistRun(ctx context.Context, tenantID string, id uuid.UUID, recoverPreAnswer bool) (AssistRun, bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE companion_assist_runs
		SET status = 'staging', updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		  AND (status = 'received' OR ($3 AND status IN ('staging','extracting','indexing','planning')))
	`, tenantID, id, recoverPreAnswer)
	if err != nil {
		return AssistRun{}, false, err
	}
	run, err := r.GetAssistRunByID(ctx, tenantID, id)
	return run, tag.RowsAffected() == 1, err
}

func (r *Repository) SetAssistRunStatus(ctx context.Context, tenantID string, id uuid.UUID, status string) (AssistRun, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE companion_assist_runs SET status=$3, updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status NOT IN ('done','failed','needs_human')
	`, tenantID, id, status)
	if err != nil {
		return AssistRun{}, err
	}
	return r.GetAssistRunByID(ctx, tenantID, id)
}

func (r *Repository) CompleteAssistRunForOwner(ctx context.Context, tenantID string, id uuid.UUID, ownershipVersion int64, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error) {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx, `UPDATE companion_assist_runs SET status=$4,output=$5::jsonb,output_text=$6,
		answered=$7,degraded=$8,model=$9,prompt_version=$10,error=$11,duration_ms=$12,
		completed_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND ownership_version=$3
		AND status NOT IN ('done','failed','needs_human')`, tenantID, id, ownershipVersion, status, []byte(output), outputText,
		answered, degraded, model, promptVersion, runErr, durationMS)
	if err != nil {
		return AssistRun{}, err
	}
	if tag.RowsAffected() != 1 {
		return AssistRun{}, domainerr.Conflict("assist ownership changed while the answer was being produced")
	}
	return r.GetAssistRunByID(ctx, tenantID, id)
}

func (r *Repository) CompleteAssistRun(ctx context.Context, tenantID string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error) {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE companion_assist_runs
		SET status = $3, output = $4::jsonb, output_text = $5, answered = $6, degraded = $7,
		    model = $8, prompt_version = $9, error = $10, duration_ms = $11, completed_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, status, []byte(output), outputText, answered, degraded, model, promptVersion, runErr, durationMS)
	if err != nil {
		return AssistRun{}, err
	}
	return r.GetAssistRunByID(ctx, tenantID, id)
}

func (r *Repository) GetAssistRunByKey(ctx context.Context, tenantID string, virployeeID uuid.UUID, idempotencyKey string) (AssistRun, error) {
	return r.scanAssistRun(r.pool.QueryRow(ctx, assistRunSelect+`
		WHERE tenant_id = $1 AND virployee_id = $2 AND idempotency_key = $3
	`, tenantID, virployeeID, idempotencyKey))
}

func (r *Repository) GetAssistRunByID(ctx context.Context, tenantID string, id uuid.UUID) (AssistRun, error) {
	return r.scanAssistRun(r.pool.QueryRow(ctx, assistRunSelect+`
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id))
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
	SELECT id, tenant_id, virployee_id,
	       COALESCE(case_id,'00000000-0000-0000-0000-000000000000'::uuid),
	       COALESCE(responsible_virployee_id,virployee_id),
	       COALESCE(orchestration_plan_id,'00000000-0000-0000-0000-000000000000'::uuid),
	       orchestration_deadline_at,ownership_version,
	       assist_type, product_surface, subject_id, repository_generation,
	       idempotency_key, status, input_hash, input_preview,
	       input_json, output, output_text, answered, degraded, model, prompt_version, error, duration_ms,
	       started_at, completed_at
	FROM companion_assist_runs
`

type rowScanner interface{ Scan(dest ...any) error }

func (r *Repository) scanAssistRun(row rowScanner) (AssistRun, error) {
	var out AssistRun
	var input, output []byte
	err := row.Scan(
		&out.ID, &out.TenantID, &out.VirployeeID, &out.CaseID, &out.ResponsibleVirployeeID,
		&out.OrchestrationPlanID, &out.OrchestrationDeadlineAt, &out.OwnershipVersion,
		&out.AssistType, &out.ProductSurface, &out.SubjectID,
		&out.RepositoryGeneration, &out.IdempotencyKey, &out.Status,
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
	return out, nil
}
