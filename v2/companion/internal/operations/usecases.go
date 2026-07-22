package operations

import (
	"context"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	Fleet(context.Context, string, string) ([]FleetMember, error)
	Overview(context.Context, string, string) (Overview, error)
	CreateAndRunReconciliation(context.Context, string, string, string, CreateReconciliationInput) (ReconciliationRun, bool, error)
	ListReconciliations(context.Context, string, string, int, int) ([]ReconciliationRun, bool, error)
	GetReconciliation(context.Context, string, uuid.UUID) (ReconciliationRun, error)
	ListJobs(context.Context, string, string, string, int, int) ([]OperationalJob, bool, error)
	GetJob(context.Context, string, uuid.UUID) (OperationalJob, error)
	CancelJob(context.Context, string, string, uuid.UUID, string, string) (OperationalJob, error)
	ReplayJob(context.Context, string, string, uuid.UUID, string) (OperationalJob, error)
	ListOutbox(context.Context, string, string, int, int) ([]OutboxMessage, bool, error)
	ReplayOutbox(context.Context, string, string, uuid.UUID, string) (OutboxMessage, error)
	ListWorkerControls(context.Context, string, string) ([]WorkerControl, error)
	PutWorkerControl(context.Context, string, string, string, PutWorkerControlInput) (WorkerControl, error)
}
type AuthorizerPort interface {
	CheckOperationAuthorization(context.Context, AuthorizationCheck) (AuthorizationResult, error)
}
type UseCases struct {
	repo       RepositoryPort
	authorizer AuthorizerPort
}

func NewUseCases(repo RepositoryPort, authorizer AuthorizerPort) *UseCases {
	return &UseCases{repo: repo, authorizer: authorizer}
}
func (u *UseCases) authorize(ctx context.Context, in AuthorizationCheck) error {
	if u.authorizer == nil {
		return domainerr.Forbidden("operations authorization is unavailable")
	}
	out, err := u.authorizer.CheckOperationAuthorization(ctx, in)
	if err != nil {
		return domainerr.Forbidden("operations authorization is unavailable")
	}
	if !out.Allowed {
		return domainerr.Forbidden(out.Reason)
	}
	return nil
}
func auth(tenant, product, actor, role, permission, action, resourceType, resourceID string) AuthorizationCheck {
	return AuthorizationCheck{TenantID: strings.TrimSpace(tenant), ProductSurface: strings.TrimSpace(product), ActorID: strings.TrimSpace(actor), ActorRole: strings.TrimSpace(role), Permission: permission, ActionType: action, ResourceType: resourceType, ResourceID: resourceID}
}
func (u *UseCases) Fleet(ctx context.Context, t, p, a, r string) ([]FleetMember, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.fleet.read", "fleet", "*")); err != nil {
		return nil, err
	}
	return u.repo.Fleet(ctx, t, p)
}
func (u *UseCases) Overview(ctx context.Context, t, p, a, r string) (Overview, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.overview.read", "operations", "overview")); err != nil {
		return Overview{}, err
	}
	return u.repo.Overview(ctx, t, p)
}
func (u *UseCases) RunReconciliation(ctx context.Context, t, p, a, r string, in CreateReconciliationInput) (ReconciliationRun, bool, error) {
	var err error
	in, err = normalizeReconciliation(in)
	if err != nil {
		return ReconciliationRun{}, false, err
	}
	if err = u.authorize(ctx, auth(t, p, a, r, "reconciliation.run", "ops.reconciliation.run", "reconciliation", p)); err != nil {
		return ReconciliationRun{}, false, err
	}
	return u.repo.CreateAndRunReconciliation(ctx, t, p, a, in)
}
func (u *UseCases) ListReconciliations(ctx context.Context, t, p, a, r string, l, o int) ([]ReconciliationRun, bool, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.reconciliation.read", "reconciliation", "*")); err != nil {
		return nil, false, err
	}
	return u.repo.ListReconciliations(ctx, t, p, l, o)
}
func (u *UseCases) GetReconciliation(ctx context.Context, t, p, a, r string, id uuid.UUID) (ReconciliationRun, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.reconciliation.read", "reconciliation", id.String())); err != nil {
		return ReconciliationRun{}, err
	}
	return u.repo.GetReconciliation(ctx, t, id)
}
func (u *UseCases) ListJobs(ctx context.Context, t, p, a, r, status string, l, o int) ([]OperationalJob, bool, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.job.read", "job", "*")); err != nil {
		return nil, false, err
	}
	return u.repo.ListJobs(ctx, t, status, p, l, o)
}
func (u *UseCases) GetJob(ctx context.Context, t, p, a, r string, id uuid.UUID) (OperationalJob, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.job.read", "job", id.String())); err != nil {
		return OperationalJob{}, err
	}
	return u.repo.GetJob(ctx, t, id)
}
func (u *UseCases) CancelJob(ctx context.Context, t, p, a, r, key, reason string, id uuid.UUID) (OperationalJob, error) {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if strings.TrimSpace(key) == "" || !safeCode.MatchString(reason) {
		return OperationalJob{}, domainerr.Validation("Idempotency-Key and reason_code are required")
	}
	if err := u.authorize(ctx, auth(t, p, a, r, "job.cancel", "ops.job.cancel", "job", id.String())); err != nil {
		return OperationalJob{}, err
	}
	return u.repo.CancelJob(ctx, t, a, id, reason, key)
}
func (u *UseCases) ReplayJob(ctx context.Context, t, p, a, r, key string, id uuid.UUID) (OperationalJob, error) {
	if strings.TrimSpace(key) == "" {
		return OperationalJob{}, domainerr.Validation("Idempotency-Key is required")
	}
	if err := u.authorize(ctx, auth(t, p, a, r, "job.replay", "ops.job.replay", "job", id.String())); err != nil {
		return OperationalJob{}, err
	}
	return u.repo.ReplayJob(ctx, t, a, id, key)
}
func (u *UseCases) ListOutbox(ctx context.Context, t, p, a, r, status string, l, o int) ([]OutboxMessage, bool, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.outbox.read", "outbox", "*")); err != nil {
		return nil, false, err
	}
	return u.repo.ListOutbox(ctx, t, status, l, o)
}
func (u *UseCases) ReplayOutbox(ctx context.Context, t, p, a, r, key string, id uuid.UUID) (OutboxMessage, error) {
	if strings.TrimSpace(key) == "" {
		return OutboxMessage{}, domainerr.Validation("Idempotency-Key is required")
	}
	if err := u.authorize(ctx, auth(t, p, a, r, "outbox.replay", "ops.outbox.replay", "outbox", id.String())); err != nil {
		return OutboxMessage{}, err
	}
	return u.repo.ReplayOutbox(ctx, t, a, id, key)
}
func (u *UseCases) WorkerControls(ctx context.Context, t, p, a, r string) ([]WorkerControl, error) {
	if err := u.authorize(ctx, auth(t, p, a, r, "ops.read", "ops.worker_controls.read", "worker_control", "*")); err != nil {
		return nil, err
	}
	return u.repo.ListWorkerControls(ctx, t, p)
}
func (u *UseCases) PutWorkerControl(ctx context.Context, t, p, a, r string, in PutWorkerControlInput) (WorkerControl, error) {
	var err error
	in, err = normalizeControl(in)
	if err != nil {
		return WorkerControl{}, err
	}
	perm := "worker.resume"
	if in.State == "paused" {
		perm = "worker.pause"
	}
	if err = u.authorize(ctx, auth(t, p, a, r, perm, "ops.worker."+in.State, "job_kind", in.JobKind)); err != nil {
		return WorkerControl{}, err
	}
	return u.repo.PutWorkerControl(ctx, t, p, a, in)
}

func (u *UseCases) RunScheduled(ctx context.Context, product string) ([]ReconciliationRun, error) {
	repo, ok := u.repo.(interface {
		ListTenantIDs(context.Context) ([]string, error)
	})
	if !ok {
		return nil, domainerr.Unavailable("scheduled reconciliation repository is unavailable")
	}
	tenants, err := repo.ListTenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	bucket := time.Now().UTC().Truncate(15 * time.Minute).Format(time.RFC3339)
	runs := make([]ReconciliationRun, 0, len(tenants))
	for _, tenant := range tenants {
		input := CreateReconciliationInput{
			Mode:           string(ModeDetect),
			TriggerType:    "scheduled",
			IdempotencyKey: "scheduled:" + product + ":" + bucket,
		}
		run, _, runErr := u.repo.CreateAndRunReconciliation(ctx, tenant, product, "system:reconciler", input)
		if runErr != nil {
			return runs, runErr
		}
		runs = append(runs, run)
	}
	return runs, nil
}
