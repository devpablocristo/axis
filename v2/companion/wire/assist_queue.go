package wire

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobs"
	"github.com/devpablocristo/companion-v2/internal/virployees"
)

const assistProcessJobKind = "assist.process"

type assistJobPayload struct {
	RunID       string `json:"run_id"`
	VirployeeID string `json:"virployee_id"`
}

// assistQueueAdapter deliberately enqueues identifiers only. PHI and signed
// URLs remain in the scoped assist row and never enter job evidence or logs.
type assistQueueAdapter struct{ repository jobs.Repository }

func (a assistQueueAdapter) EnqueueAssist(ctx context.Context, run virployees.AssistRun) error {
	payload, err := json.Marshal(assistJobPayload{RunID: run.ID.String(), VirployeeID: run.VirployeeID.String()})
	if err != nil {
		return err
	}
	job, _, err := a.repository.Enqueue(ctx, jobs.EnqueueInput{
		TenantID: run.TenantID, ProductSurface: "companion", Kind: assistProcessJobKind,
		ShardKey: run.VirployeeID.String(), DedupeKey: run.ID.String(), Payload: payload,
		MaxAttempts: 10, Timeout: 2 * time.Minute,
	})
	if err != nil {
		return err
	}
	if job.Status == jobs.StatusDeadLetter {
		_, err = a.repository.ReplayDeadLetter(ctx, run.TenantID, job.ID, time.Now().UTC())
	}
	return err
}
