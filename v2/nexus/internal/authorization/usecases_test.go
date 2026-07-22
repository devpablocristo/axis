package authorization

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

type fakeRepo struct{ grants []Grant }

func (f *fakeRepo) Create(_ context.Context, g Grant) (Grant, error) {
	f.grants = append(f.grants, g)
	return g, nil
}
func (f *fakeRepo) List(context.Context, string, string) ([]Grant, error) { return f.grants, nil }
func (f *fakeRepo) ActiveForUser(context.Context, string, string, time.Time) ([]Grant, error) {
	return f.grants, nil
}
func (f *fakeRepo) Revoke(_ context.Context, _ string, id uuid.UUID, _ string, _ string, _ int64) (Grant, error) {
	for _, g := range f.grants {
		if g.ID == id {
			return g, nil
		}
	}
	return Grant{}, nil
}
func TestFunctionalGrantScopeAndExpiry(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	repo := &fakeRepo{grants: []Grant{{ID: uuid.New(), TenantID: "t", UserID: "u", RoleKey: RoleApprover, ActionTypePattern: "calendar.*", MaxRiskClass: "medium", ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), Revision: 1}}}
	uc := NewUseCases(repo)
	uc.now = func() time.Time { return now }
	allowed, err := uc.Check(context.Background(), CheckInput{TenantID: "t", ActorID: "u", ActorRole: "member", Permission: "approvals.decide", ActionType: "calendar.events.create", RiskClass: "medium"})
	if err != nil || !allowed.Allowed {
		t.Fatalf("expected scoped grant: %+v %v", allowed, err)
	}
	denied, _ := uc.Check(context.Background(), CheckInput{TenantID: "t", ActorID: "u", ActorRole: "member", Permission: "approvals.decide", ActionType: "payments.send", RiskClass: "medium"})
	if denied.Allowed {
		t.Fatal("grant must not escape action scope")
	}
	high, _ := uc.Check(context.Background(), CheckInput{TenantID: "t", ActorID: "u", ActorRole: "member", Permission: "approvals.decide", ActionType: "calendar.events.create", RiskClass: "high"})
	if high.Allowed {
		t.Fatal("grant must enforce max risk")
	}
}

func TestOperatorGrantIsExplicitlyScoped(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	repo := &fakeRepo{grants: []Grant{{
		ID: uuid.New(), TenantID: "t", UserID: "operator", RoleKey: RoleOperator,
		ProductSurface: "axis", ActionTypePattern: "ops.job.*", ResourceType: "job", ResourceID: "job-1",
		MaxRiskClass: "low", ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), Revision: 1,
	}}}
	uc := NewUseCases(repo)
	uc.now = func() time.Time { return now }
	allowed, err := uc.Check(context.Background(), CheckInput{
		TenantID: "t", ActorID: "operator", ActorRole: "member", Permission: "job.replay",
		ProductSurface: "axis", ActionType: "ops.job.replay", ResourceType: "job", ResourceID: "job-1", RiskClass: "low",
	})
	if err != nil || !allowed.Allowed {
		t.Fatalf("expected scoped operator grant: %+v %v", allowed, err)
	}
	denied, _ := uc.Check(context.Background(), CheckInput{
		TenantID: "t", ActorID: "operator", ActorRole: "member", Permission: "job.replay",
		ProductSurface: "axis", ActionType: "ops.job.replay", ResourceType: "job", ResourceID: "job-2", RiskClass: "low",
	})
	if denied.Allowed {
		t.Fatal("operator grant must not escape resource scope")
	}
}
