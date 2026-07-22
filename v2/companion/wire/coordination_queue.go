package wire

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobs"
	"github.com/devpablocristo/companion-v2/internal/virployees"
)

type coordinationQueueAdapter struct{ repository jobs.Repository }

func (a coordinationQueueAdapter) EnqueueConsultation(ctx context.Context, item virployees.SpecialistConsultation, timeout time.Duration) error {
	payload, err := json.Marshal(map[string]string{"consultation_id": item.ID.String(), "plan_id": item.PlanID.String()})
	if err != nil {
		return err
	}
	deadline := time.Now().UTC().Add(timeout)
	_, _, err = a.repository.Enqueue(ctx, jobs.EnqueueInput{TenantID: item.TenantID, ProductSurface: "companion", Kind: virployees.JobKindSpecialistConsult,
		ShardKey: item.RootRunID.String(), DedupeKey: item.ID.String(), Payload: payload, MaxAttempts: 3, Timeout: timeout, DeadlineAt: &deadline})
	return err
}

func (a coordinationQueueAdapter) EnqueueReconcile(ctx context.Context, plan virployees.OrchestrationPlan, trigger string) error {
	payload, err := json.Marshal(map[string]string{"plan_id": plan.ID.String()})
	if err != nil {
		return err
	}
	_, _, err = a.repository.Enqueue(ctx, jobs.EnqueueInput{TenantID: plan.TenantID, ProductSurface: "companion", Kind: virployees.JobKindOrchestrationReconcile,
		ShardKey: plan.RootRunID.String(), DedupeKey: plan.ID.String() + ":reconcile:" + trigger, Payload: payload, MaxAttempts: 3, Timeout: 30 * time.Second, DeadlineAt: &plan.DeadlineAt})
	return err
}

func (a coordinationQueueAdapter) EnqueueSynthesis(ctx context.Context, plan virployees.OrchestrationPlan) error {
	payload, err := json.Marshal(map[string]string{"plan_id": plan.ID.String()})
	if err != nil {
		return err
	}
	_, _, err = a.repository.Enqueue(ctx, jobs.EnqueueInput{TenantID: plan.TenantID, ProductSurface: "companion", Kind: virployees.JobKindOrchestrationSynthesis,
		ShardKey: plan.RootRunID.String(), DedupeKey: plan.ID.String() + ":synthesis", Payload: payload, MaxAttempts: 3, Timeout: 2 * time.Minute, DeadlineAt: &plan.DeadlineAt})
	return err
}
