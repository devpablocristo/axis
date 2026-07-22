package operations

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type testOperationsRepo struct {
	RepositoryPort
	tenants []string
	calls   []CreateReconciliationInput
}

func (r *testOperationsRepo) ListTenantIDs(context.Context) ([]string, error) { return r.tenants, nil }
func (r *testOperationsRepo) Fleet(context.Context, string, string) ([]FleetMember, error) {
	return []FleetMember{}, nil
}
func (r *testOperationsRepo) CreateAndRunReconciliation(_ context.Context, t, p, a string, in CreateReconciliationInput) (ReconciliationRun, bool, error) {
	r.calls = append(r.calls, in)
	return ReconciliationRun{ID: uuid.New(), TenantID: t, ProductSurface: p, Mode: in.Mode, TriggerType: in.TriggerType}, true, nil
}

type testOperationsAuthorizer struct {
	result AuthorizationResult
	err    error
	input  AuthorizationCheck
}

func (a *testOperationsAuthorizer) CheckOperationAuthorization(_ context.Context, in AuthorizationCheck) (AuthorizationResult, error) {
	a.input = in
	return a.result, a.err
}

func TestOperationsFailsClosedWithoutAuthorizer(t *testing.T) {
	u := NewUseCases(&testOperationsRepo{}, nil)
	if _, err := u.Fleet(context.Background(), "tenant", "companion", "actor", "member"); err == nil {
		t.Fatal("operations must fail closed when Nexus authorization is unavailable")
	}
}
func TestOperationsPassesScopedAuthorityMetadata(t *testing.T) {
	authz := &testOperationsAuthorizer{result: AuthorizationResult{Allowed: true}}
	u := NewUseCases(&testOperationsRepo{}, authz)
	_, err := u.Fleet(context.Background(), "tenant-a", "companion", "operator-a", "member")
	if err != nil {
		t.Fatal(err)
	}
	if authz.input.Permission != "ops.read" || authz.input.ResourceType != "fleet" || authz.input.TenantID != "tenant-a" {
		t.Fatalf("unexpected authorization input: %+v", authz.input)
	}
}
func TestScheduledReconciliationUsesStableBucketKeyPerTenant(t *testing.T) {
	repo := &testOperationsRepo{tenants: []string{"tenant-a", "tenant-b"}}
	u := NewUseCases(repo, nil)
	runs, err := u.RunScheduled(context.Background(), "companion")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || len(repo.calls) != 2 {
		t.Fatalf("expected one run per tenant: %+v", runs)
	}
	if repo.calls[0].IdempotencyKey != repo.calls[1].IdempotencyKey || repo.calls[0].TriggerType != "scheduled" {
		t.Fatalf("scheduled runs must share a stable bucket key: %+v", repo.calls)
	}
}
func TestReconciliationRequiresIdempotencyBeforeAuthorization(t *testing.T) {
	u := NewUseCases(&testOperationsRepo{}, &testOperationsAuthorizer{result: AuthorizationResult{Allowed: true}})
	if _, _, err := u.RunReconciliation(context.Background(), "t", "companion", "a", "owner", CreateReconciliationInput{Mode: "safe_repair"}); err == nil {
		t.Fatal("manual reconciliation must require Idempotency-Key")
	}
}
