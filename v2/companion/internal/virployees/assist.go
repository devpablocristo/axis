package virployees

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// assistObjectSchema is a minimal "return a JSON object" schema. It triggers the
// runtime's structured-output path (so Echo/no-model degrades to Answered=false);
// the exact field shape is driven by the virployee's system prompt, not here.
var assistObjectSchema = map[string]any{"type": "object"}

// Assist runs the "process and respond" path: the virployee interprets the input
// and answers, with NO external effects and NO governance approval (read/explain).
// It reserves the run before calling the model (idempotent) and fails closed.
//
// Governance seam: this is intentionally NOT the action path. When a product later
// needs the virployee to take an ACTION with external effects, that must route
// through DryRun → ExecutionGate → Nexus → ExecuteApprovedAction — never here.
func (u *UseCases) Assist(ctx context.Context, tenantID string, id uuid.UUID, inputJSON json.RawMessage, idempotencyKey string) (AssistRun, error) {
	tenantID = normalizeTenantID(tenantID)
	if u.answerer == nil {
		return AssistRun{}, domainerr.Conflict("runtime answerer is not configured")
	}
	if u.assistRepo == nil {
		return AssistRun{}, domainerr.Conflict("assist repository is not configured")
	}
	if strings.TrimSpace(string(inputJSON)) == "" {
		return AssistRun{}, domainerr.Validation("input_json is required")
	}

	// Validates the virployee, its (active) job role/profile and autonomy, and
	// assembles the system prompt the answer runs under.
	rc, err := u.RuntimeContext(ctx, tenantID, id)
	if err != nil {
		return AssistRun{}, err
	}

	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = runtraces.HashString(tenantID + ":" + id.String() + ":" + string(inputJSON))
	}
	inputHash := runtraces.HashString(string(inputJSON))
	inputPreview := runtraces.InputPreview(string(inputJSON))

	run, reserved, err := u.assistRepo.BeginAssistRun(ctx, tenantID, id, "", idempotencyKey, inputHash, inputPreview)
	if err != nil {
		return AssistRun{}, err
	}
	if !reserved {
		// Idempotent replay: a completed run returns its stored answer without a
		// second (expensive) model call; a still-running one is a conflict. A
		// previously failed run falls through and is retried.
		switch run.Status {
		case "done":
			return run, nil
		case "running":
			return AssistRun{}, domainerr.Conflict("assist run already in progress")
		}
	}

	started := time.Now()
	out, answerErr := u.answerer.Answer(ctx, AnswerInput{
		SystemPrompt:   rc.ProfileTemplate.SystemPrompt,
		JobRole:        rc.JobRole.Name,
		InputJSON:      inputJSON,
		ResponseSchema: assistObjectSchema,
	})
	durationMS := time.Since(started).Milliseconds()

	if answerErr != nil {
		// Fail-closed: record the failure and surface it — never a silent success.
		_, _ = u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "failed", nil, "", false, false, "", "", runtraces.RedactText(answerErr.Error()), durationMS)
		return AssistRun{}, domainerr.Unavailable("assist runtime failed")
	}

	// degraded = the model did not produce a usable answer (Echo / no credentials).
	degraded := !out.Answered
	return u.assistRepo.CompleteAssistRun(ctx, tenantID, run.ID, "done", out.OutputJSON, out.OutputText, out.Answered, degraded, out.ModelID, out.PromptVersion, "", durationMS)
}
