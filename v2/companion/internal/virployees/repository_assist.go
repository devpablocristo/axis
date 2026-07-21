package virployees

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AssistRun is one product "assist" run (process-and-respond): a virployee
// interprets the input and answers. It is the accountability trace returned to
// the product (run id). Output holds the model's structured answer; OutputText
// its raw text (also used to surface a degraded/Echo run).
type AssistRun struct {
	ID             uuid.UUID
	VirployeeID    uuid.UUID
	AssistType     string
	IdempotencyKey string
	Status         string // running | done | failed
	InputHash      string
	InputPreview   string
	Output         json.RawMessage
	OutputText     string
	Answered       bool
	Degraded       bool
	Model          string
	PromptVersion  string
	Error          string
	DurationMS     int64
	StartedAt      time.Time
	CompletedAt    *time.Time
}

// BeginAssistRun reserves the run before the model is called: it inserts a
// 'running' row and reports reserved=true only when this call created it.
// A concurrent retry with the same idempotency key gets reserved=false and the
// existing row, so the expensive model call happens at most once.
func (r *Repository) BeginAssistRun(ctx context.Context, tenantID string, virployeeID uuid.UUID, assistType, idempotencyKey, inputHash, inputPreview string) (AssistRun, bool, error) {
	id := uuid.New()
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO companion_assist_runs (
			id, tenant_id, virployee_id, assist_type, idempotency_key,
			status, input_hash, input_preview, started_at
		) VALUES ($1, $2, $3, $4, $5, 'running', $6, $7, now())
		ON CONFLICT (tenant_id, virployee_id, idempotency_key) DO NOTHING
	`, id, tenantID, virployeeID, assistType, idempotencyKey, inputHash, inputPreview)
	if err != nil {
		return AssistRun{}, false, err
	}
	run, err := r.GetAssistRunByKey(ctx, tenantID, virployeeID, idempotencyKey)
	return run, tag.RowsAffected() == 1, err
}

// CompleteAssistRun finalizes a reserved run with the model's result.
func (r *Repository) CompleteAssistRun(ctx context.Context, tenantID string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, promptVersion, runErr string, durationMS int64) (AssistRun, error) {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE companion_assist_runs
		SET status = $3, output = $4::jsonb, output_text = $5, answered = $6, degraded = $7,
		    model = $8, prompt_version = $9, error = $10, duration_ms = $11, completed_at = now()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, status, []byte(output), outputText, answered, degraded, model, promptVersion, runErr, durationMS)
	if err != nil {
		return AssistRun{}, err
	}
	return r.getAssistRunByID(ctx, tenantID, id)
}

func (r *Repository) GetAssistRunByKey(ctx context.Context, tenantID string, virployeeID uuid.UUID, idempotencyKey string) (AssistRun, error) {
	return r.scanAssistRun(r.pool.QueryRow(ctx, assistRunSelect+`
		WHERE tenant_id = $1 AND virployee_id = $2 AND idempotency_key = $3
	`, tenantID, virployeeID, idempotencyKey))
}

func (r *Repository) getAssistRunByID(ctx context.Context, tenantID string, id uuid.UUID) (AssistRun, error) {
	return r.scanAssistRun(r.pool.QueryRow(ctx, assistRunSelect+`
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id))
}

const assistRunSelect = `
	SELECT id, virployee_id, assist_type, idempotency_key, status, input_hash, input_preview,
	       output, output_text, answered, degraded, model, prompt_version, error, duration_ms,
	       started_at, completed_at
	FROM companion_assist_runs
`

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *Repository) scanAssistRun(row rowScanner) (AssistRun, error) {
	var out AssistRun
	var output []byte
	err := row.Scan(
		&out.ID, &out.VirployeeID, &out.AssistType, &out.IdempotencyKey, &out.Status,
		&out.InputHash, &out.InputPreview, &output, &out.OutputText, &out.Answered, &out.Degraded,
		&out.Model, &out.PromptVersion, &out.Error, &out.DurationMS, &out.StartedAt, &out.CompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return AssistRun{}, domainerr.NotFound("assist run not found")
		}
		return AssistRun{}, err
	}
	if len(output) > 0 {
		out.Output = json.RawMessage(output)
	}
	return out, nil
}
