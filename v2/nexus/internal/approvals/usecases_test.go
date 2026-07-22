package approvals

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/authorization"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestUseCasesListDefaultsToPending(t *testing.T) {
	repo := newFakeRepo()
	pending := repo.add("tenant-1", domain.StatusPending)
	repo.add("tenant-1", domain.StatusApproved)
	uc := NewUseCases(repo)

	out, err := uc.List(context.Background(), "tenant-1", domain.ListInput{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != pending.ID {
		t.Fatalf("unexpected list: %+v", out)
	}
}

func TestUseCasesListCursorPaginatesApprovals(t *testing.T) {
	repo := newFakeRepo()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	first := repo.addAt("tenant-1", domain.StatusPending, base.Add(3*time.Minute))
	second := repo.addAt("tenant-1", domain.StatusPending, base.Add(2*time.Minute))
	third := repo.addAt("tenant-1", domain.StatusPending, base.Add(time.Minute))
	repo.addAt("tenant-1", domain.StatusApproved, base.Add(4*time.Minute))
	uc := NewUseCases(repo)

	page, err := uc.List(context.Background(), "tenant-1", domain.ListInput{StatusRaw: "pending", Limit: 2})
	if err != nil {
		t.Fatalf("List first page: %v", err)
	}
	if !page.HasMore || page.NextCursor == "" {
		t.Fatalf("expected next cursor, got %+v", page)
	}
	if len(page.Items) != 2 || page.Items[0].ID != first.ID || page.Items[1].ID != second.ID {
		t.Fatalf("unexpected first page: %+v", page.Items)
	}

	next, err := uc.List(context.Background(), "tenant-1", domain.ListInput{StatusRaw: "pending", Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("List next page: %v", err)
	}
	if next.HasMore || next.NextCursor != "" {
		t.Fatalf("unexpected next metadata: %+v", next)
	}
	if len(next.Items) != 1 || next.Items[0].ID != third.ID {
		t.Fatalf("unexpected next page: %+v", next.Items)
	}
}

func TestUseCasesGet(t *testing.T) {
	repo := newFakeRepo()
	item := repo.add("tenant-1", domain.StatusPending)
	repo.add("tenant-2", domain.StatusPending)
	uc := NewUseCases(repo)

	out, err := uc.Get(context.Background(), "tenant-1", item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.ID != item.ID || out.TenantID != "tenant-1" {
		t.Fatalf("unexpected item: %+v", out)
	}

	_, err = uc.Get(context.Background(), "tenant-2", item.ID)
	if !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found for another tenant, got %v", err)
	}
}

func TestUseCasesApproveAndReject(t *testing.T) {
	repo := newFakeRepo()
	approve := repo.add("tenant-1", domain.StatusPending)
	reject := repo.add("tenant-1", domain.StatusPending)
	uc := NewUseCases(repo)

	approved, err := uc.Approve(context.Background(), "tenant-1", approve.ID, domain.DecisionActor{ID: "approver-1", Role: "admin"}, domain.DecisionInput{Note: "ok"})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != domain.StatusApproved || approved.DecidedBy != "approver-1" || approved.DecisionNote != "ok" || approved.DecidedAt == nil {
		t.Fatalf("unexpected approved item: %+v", approved)
	}

	rejected, err := uc.Reject(context.Background(), "tenant-1", reject.ID, domain.DecisionActor{ID: "approver-2", Role: "owner"}, domain.DecisionInput{Note: "no"})
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if rejected.Status != domain.StatusRejected || rejected.DecidedBy != "approver-2" || rejected.DecisionNote != "no" || rejected.DecidedAt == nil {
		t.Fatalf("unexpected rejected item: %+v", rejected)
	}
}

func TestUseCasesRejectsInvalidDecisions(t *testing.T) {
	repo := newFakeRepo()
	item := repo.add("tenant-1", domain.StatusApproved)
	uc := NewUseCases(repo)

	_, err := uc.Approve(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "approver-1", Role: "admin"}, domain.DecisionInput{})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict deciding already decided approval, got %v", err)
	}

	pending := repo.add("tenant-1", domain.StatusPending)
	_, err = uc.Approve(context.Background(), "tenant-1", pending.ID, domain.DecisionActor{}, domain.DecisionInput{})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing actor, got %v", err)
	}

	_, err = uc.List(context.Background(), "tenant-1", domain.ListInput{StatusRaw: "bad"})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid status, got %v", err)
	}

	_, err = uc.List(context.Background(), "tenant-1", domain.ListInput{Cursor: "bad-cursor"})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid cursor, got %v", err)
	}
}

func TestUseCasesEnforcesSupervisorAndHumanSeparationOfDuties(t *testing.T) {
	repo := newFakeRepo()
	item := repo.add("tenant-1", domain.StatusPending)
	uc := NewUseCases(repo)

	for name, actor := range map[string]domain.DecisionActor{
		"unassigned member": {ID: "another-user", Role: "member"},
		"service principal": {ID: "service:executor", Role: "admin"},
		"virployee":         {ID: "another-virployee", Role: "virployee"},
		"requester":         {ID: item.RequesterID, Role: "admin"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := uc.Approve(context.Background(), "tenant-1", item.ID, actor, domain.DecisionInput{}); !domainerr.IsForbidden(err) {
				t.Fatalf("expected forbidden, got %v", err)
			}
		})
	}

	approved, err := uc.Approve(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: item.SupervisorUserID, Role: "member"}, domain.DecisionInput{})
	if err != nil || approved.Status != domain.StatusApproved {
		t.Fatalf("assigned supervisor should approve: approval=%+v err=%v", approved, err)
	}
}

func TestUseCasesAllowsScopedFunctionalApprover(t *testing.T) {
	repo := newFakeRepo()
	item := repo.add("tenant-1", domain.StatusPending)
	uc := NewUseCases(repo)
	authorizer := &fakeApprovalAuthorizer{allowed: true}
	uc.SetAuthorizer(authorizer)

	approved, err := uc.Approve(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "functional-approver", Role: "member"}, domain.DecisionInput{})
	if err != nil || approved.Status != domain.StatusApproved {
		t.Fatalf("functional approver should approve in scope: approval=%+v err=%v", approved, err)
	}
	if authorizer.last.ProductSurface != "axis" || authorizer.last.ActionType != item.ActionType ||
		authorizer.last.ResourceType != "event" || authorizer.last.ResourceID != item.TargetResource ||
		authorizer.last.RiskClass != item.RiskLevel {
		t.Fatalf("approval authority used the wrong scope: %+v", authorizer.last)
	}
}

func TestUseCasesBreakGlassRequiresTwoPeopleAndIndependentReview(t *testing.T) {
	repo := newFakeRepo()
	item := repo.add("tenant-1", domain.StatusPending)
	item.ApprovalKind = "break_glass"
	item.QuorumRequired = 2
	item.PostReviewRequired = true
	repo.rows[item.ID] = item
	audit := &fakeAuditEmitter{}
	uc := NewUseCases(repo)
	uc.SetAuditEmitter(audit)

	if _, err := uc.Approve(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "admin-1", Role: "admin"}, domain.DecisionInput{}); !domainerr.IsValidation(err) {
		t.Fatalf("expected mandatory justification, got %v", err)
	}
	first, err := uc.Approve(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "admin-1", Role: "admin"}, domain.DecisionInput{Note: "patient safety incident"})
	if err != nil || first.Status != domain.StatusPending || first.ApprovalCount != 1 {
		t.Fatalf("first decision must remain pending: approval=%+v err=%v", first, err)
	}
	if _, err := uc.Approve(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "admin-1", Role: "admin"}, domain.DecisionInput{Note: "again"}); !domainerr.IsConflict(err) {
		t.Fatalf("same actor must not count twice, got %v", err)
	}
	second, err := uc.Approve(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "owner-2", Role: "owner"}, domain.DecisionInput{Note: "independent confirmation"})
	if err != nil || second.Status != domain.StatusApproved || second.ApprovalCount != 2 {
		t.Fatalf("second distinct decision must approve: approval=%+v err=%v", second, err)
	}
	if _, err := uc.Review(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "admin-1", Role: "admin"}, domain.DecisionInput{Note: "review"}); !domainerr.IsConflict(err) {
		t.Fatalf("approver cannot review own break-glass action, got %v", err)
	}
	reviewed, err := uc.Review(context.Background(), "tenant-1", item.ID, domain.DecisionActor{ID: "owner-3", Role: "owner"}, domain.DecisionInput{Note: "controls followed"})
	if err != nil || reviewed.ReviewedBy != "owner-3" || reviewed.ReviewedAt == nil {
		t.Fatalf("independent review failed: approval=%+v err=%v", reviewed, err)
	}
	if len(audit.inputs) != 3 || audit.inputs[0].EventType != auditdomain.EventGovernanceDecided || audit.inputs[2].EventType != auditdomain.EventBreakGlassReviewed {
		t.Fatalf("unexpected audit events: %+v", audit.inputs)
	}
}

type fakeRepo struct {
	rows map[uuid.UUID]domain.Approval
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: make(map[uuid.UUID]domain.Approval)}
}

func (r *fakeRepo) add(tenantID string, status domain.Status) domain.Approval {
	return r.addAt(tenantID, status, time.Now().UTC())
}

func (r *fakeRepo) addAt(tenantID string, status domain.Status, now time.Time) domain.Approval {
	row := domain.Approval{
		ID:                uuid.New(),
		TenantID:          tenantID,
		GovernanceCheckID: uuid.New(),
		RequesterID:       "virployee-1",
		ProductSurface:    "axis",
		ActionType:        "calendar.events.delete",
		TargetSystem:      "calendar",
		TargetResource:    "events",
		ResourceType:      "event",
		RiskLevel:         "high",
		Reason:            "delete event",
		BindingHash:       "binding-hash",
		Status:            status,
		ApprovalKind:      "normal",
		SupervisorUserID:  "supervisor-1",
		QuorumRequired:    1,
		ExpiresAt:         now.Add(time.Hour),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	r.rows[row.ID] = row
	return row
}

func (r *fakeRepo) List(_ context.Context, tenantID string, status domain.Status, limit int, after *domain.ListCursor) ([]domain.Approval, error) {
	out := []domain.Approval{}
	for _, row := range r.rows {
		if row.TenantID != tenantID || row.Status != status {
			continue
		}
		if after != nil && !isAfterCursor(row, *after) {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() > out[j].ID.String()
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func isAfterCursor(row domain.Approval, after domain.ListCursor) bool {
	if row.CreatedAt.Before(after.CreatedAt) {
		return true
	}
	if row.CreatedAt.Equal(after.CreatedAt) {
		return row.ID.String() < after.ID.String()
	}
	return false
}

func (r *fakeRepo) Get(_ context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.Approval{}, domainerr.NotFound("approval not found")
	}
	return row, nil
}

func (r *fakeRepo) Decide(_ context.Context, tenantID string, id uuid.UUID, status domain.Status, actorID, actorRole, note string) (domain.Approval, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.Approval{}, domainerr.NotFound("approval not found")
	}
	if row.Status != domain.StatusPending {
		return domain.Approval{}, domainerr.Conflict("approval is already decided")
	}
	for _, decision := range row.Decisions {
		if decision.ActorID == actorID {
			return domain.Approval{}, domainerr.Conflict("actor already decided this approval")
		}
	}
	now := time.Now().UTC()
	decision := "approve"
	if status == domain.StatusRejected {
		decision = "reject"
		row.Status = status
	}
	row.Decisions = append(row.Decisions, domain.Decision{ID: uuid.New(), ActorID: actorID, ActorRole: actorRole, Decision: decision, Note: note, DecidedAt: now})
	if decision == "approve" {
		row.ApprovalCount++
		if row.ApprovalCount >= row.QuorumRequired {
			row.Status = domain.StatusApproved
		}
	}
	if row.Status != domain.StatusPending {
		row.DecidedBy, row.DecisionNote, row.DecidedAt = actorID, note, &now
	}
	row.UpdatedAt = now
	r.rows[id] = row
	return row, nil
}

func (r *fakeRepo) Review(_ context.Context, tenantID string, id uuid.UUID, actorID, note string) (domain.Approval, error) {
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID {
		return domain.Approval{}, domainerr.NotFound("approval not found")
	}
	if row.ApprovalKind != "break_glass" || row.Status != domain.StatusApproved || row.ReviewedAt != nil {
		return domain.Approval{}, domainerr.Conflict("break-glass approval is not reviewable by this actor")
	}
	for _, decision := range row.Decisions {
		if decision.ActorID == actorID {
			return domain.Approval{}, domainerr.Conflict("break-glass approval is not reviewable by this actor")
		}
	}
	now := time.Now().UTC()
	row.ReviewedBy, row.ReviewNote, row.ReviewedAt = actorID, note, &now
	r.rows[id] = row
	return row, nil
}

type fakeAuditEmitter struct{ inputs []auditdomain.AppendInput }

func (f *fakeAuditEmitter) Append(_ context.Context, _ string, input auditdomain.AppendInput) (auditdomain.AuditEvent, error) {
	f.inputs = append(f.inputs, input)
	return auditdomain.AuditEvent{}, nil
}

type fakeApprovalAuthorizer struct {
	allowed bool
	last    authorization.CheckInput
}

func (f *fakeApprovalAuthorizer) Check(_ context.Context, input authorization.CheckInput) (authorization.CheckResult, error) {
	f.last = input
	return authorization.CheckResult{Allowed: f.allowed}, nil
}
