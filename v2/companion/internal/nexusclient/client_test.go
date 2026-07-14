package nexusclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCheckSendsGovernanceRequestToNexus(t *testing.T) {
	var gotTenant string
	var gotActor string
	var gotInternalToken string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/governance/check" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotActor = r.Header.Get("X-Actor-ID")
		gotInternalToken = r.Header.Get("X-Axis-Internal-Token")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"decision":"require_approval",
			"risk_level":"high",
			"status":"pending_approval",
			"decision_reason":"default high risk action",
			"would_require_approval":true,
			"mode":"simulation",
			"binding_hash":"binding-123",
			"approval_id":"approval-123",
			"approval_status":"pending"
		}`))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), "trusted-secret")
	out, err := client.Check(context.Background(), executiongate.GovernanceCheckInput{
		TenantID:       "tenant-1",
		RequesterType:  "virployee",
		RequesterID:    "virployee-1",
		ActionType:     "calendar.events.delete",
		TargetSystem:   "calendar",
		TargetResource: "events",
		Params:         map[string]any{"draft_status": "ready"},
		Reason:         "delete the event",
		Context:        "Ops",
		BindingHash:    "binding-123",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if gotTenant != "tenant-1" {
		t.Fatalf("expected tenant header tenant-1, got %q", gotTenant)
	}
	if gotActor != "virployee-1" || gotInternalToken != "trusted-secret" {
		t.Fatalf("expected trusted actor and token headers, got actor=%q token=%q", gotActor, gotInternalToken)
	}
	if gotBody["action_type"] != "calendar.events.delete" || gotBody["requester_id"] != "virployee-1" {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}
	if gotBody["binding_hash"] != "binding-123" {
		t.Fatalf("expected binding hash in request, got %+v", gotBody)
	}
	if out.Decision != "require_approval" || !out.WouldRequireApproval || out.BindingHash != "binding-123" || out.ApprovalID != "approval-123" || out.ApprovalStatus != "pending" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestGetApprovalReadsNexusApproval(t *testing.T) {
	approvalID := uuid.New()
	var gotTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/approvals/"+approvalID.String() {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotTenant = r.Header.Get("X-Tenant-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"` + approvalID.String() + `",
			"requester_id":"virployee-1",
			"binding_hash":"binding-123",
			"status":"approved"
		}`))
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	out, err := client.GetApproval(context.Background(), "tenant-1", approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if gotTenant != "tenant-1" {
		t.Fatalf("expected tenant header tenant-1, got %q", gotTenant)
	}
	if out.ID != approvalID.String() || out.RequesterID != "virployee-1" || out.BindingHash != "binding-123" || out.Status != "approved" {
		t.Fatalf("unexpected approval: %+v", out)
	}
}

func TestGetApprovalMapsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	_, err := client.GetApproval(context.Background(), "tenant-1", uuid.New())
	if !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}
