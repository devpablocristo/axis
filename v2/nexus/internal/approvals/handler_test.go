package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
)

func TestHandlerListApprovals(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := setupApprovalsRouter(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/approvals?status=pending&limit=10&cursor=cursor-1", nil)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.lastTenant != "tenant-1" || fake.lastInput.StatusRaw != "pending" || fake.lastInput.Limit != 10 || fake.lastInput.Cursor != "cursor-1" {
		t.Fatalf("unexpected list call: %+v", fake)
	}
	var payload struct {
		Items []struct {
			ID         string `json:"id"`
			ActionType string `json:"action_type"`
			Status     string `json:"status"`
		} `json:"items"`
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	}
	decodeApprovalsJSON(t, rec, &payload)
	if len(payload.Items) != 1 || payload.Items[0].ActionType != "calendar.events.delete" || payload.Items[0].Status != "pending" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if !payload.HasMore || payload.NextCursor != "cursor-2" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHandlerGetApproval(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := setupApprovalsRouter(fake)
	id := uuid.New()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/approvals/"+id.String(), nil)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.lastTenant != "tenant-1" || fake.lastID != id {
		t.Fatalf("unexpected get call: %+v", fake)
	}
	var payload struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decodeApprovalsJSON(t, rec, &payload)
	if payload.ID != id.String() || payload.Status != "pending" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHandlerApproveApproval(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := setupApprovalsRouter(fake)
	id := uuid.New()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+id.String()+"/approve", strings.NewReader(`{"note":"approved"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	req.Header.Set("X-Actor-ID", "approver-1")
	req.Header.Set("X-Axis-Tenant-Role", "admin")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.lastID != id || fake.lastActor.ID != "approver-1" || fake.lastActor.Role != "admin" || fake.lastNote != "approved" || fake.lastDecision != "approve" {
		t.Fatalf("unexpected approve call: %+v", fake)
	}
}

func setupApprovalsRouter(ucs UseCasesPort) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(ucs).Routes(router.Group("/v1"))
	return router
}

func decodeApprovalsJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode json: %v; body=%s", err, rec.Body.String())
	}
}

type handlerFakeUseCases struct {
	lastTenant   string
	lastInput    domain.ListInput
	lastID       uuid.UUID
	lastActor    domain.DecisionActor
	lastNote     string
	lastDecision string
}

func (f *handlerFakeUseCases) List(_ context.Context, tenantID string, input domain.ListInput) (domain.ListPage, error) {
	f.lastTenant = tenantID
	f.lastInput = input
	return domain.ListPage{
		Items:      []domain.Approval{fakeApproval(tenantID, domain.StatusPending)},
		HasMore:    true,
		NextCursor: "cursor-2",
	}, nil
}

func (f *handlerFakeUseCases) Get(_ context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	f.lastTenant = tenantID
	f.lastID = id
	item := fakeApproval(tenantID, domain.StatusPending)
	item.ID = id
	return item, nil
}

func (f *handlerFakeUseCases) Approve(_ context.Context, tenantID string, id uuid.UUID, actor domain.DecisionActor, input domain.DecisionInput) (domain.Approval, error) {
	f.lastTenant = tenantID
	f.lastID = id
	f.lastActor = actor
	f.lastNote = input.Note
	f.lastDecision = "approve"
	item := fakeApproval(tenantID, domain.StatusApproved)
	item.ID = id
	item.DecidedBy = actor.ID
	item.DecisionNote = input.Note
	return item, nil
}

func (f *handlerFakeUseCases) Reject(_ context.Context, tenantID string, id uuid.UUID, actor domain.DecisionActor, input domain.DecisionInput) (domain.Approval, error) {
	f.lastTenant = tenantID
	f.lastID = id
	f.lastActor = actor
	f.lastNote = input.Note
	f.lastDecision = "reject"
	item := fakeApproval(tenantID, domain.StatusRejected)
	item.ID = id
	item.DecidedBy = actor.ID
	item.DecisionNote = input.Note
	return item, nil
}

func (f *handlerFakeUseCases) Review(_ context.Context, tenantID string, id uuid.UUID, actor domain.DecisionActor, input domain.DecisionInput) (domain.Approval, error) {
	f.lastTenant, f.lastID, f.lastActor, f.lastNote, f.lastDecision = tenantID, id, actor, input.Note, "review"
	item := fakeApproval(tenantID, domain.StatusApproved)
	item.ID, item.ReviewedBy, item.ReviewNote = id, actor.ID, input.Note
	return item, nil
}

func fakeApproval(tenantID string, status domain.Status) domain.Approval {
	now := time.Now().UTC()
	return domain.Approval{
		ID:                uuid.New(),
		TenantID:          tenantID,
		GovernanceCheckID: uuid.New(),
		RequesterID:       "virployee-1",
		ActionType:        "calendar.events.delete",
		TargetSystem:      "calendar",
		TargetResource:    "events",
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
}
