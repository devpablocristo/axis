package nexusclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
)

func TestCheckSendsGovernanceRequestToNexus(t *testing.T) {
	var gotTenant string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/governance/check" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotTenant = r.Header.Get("X-Tenant-ID")
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

	client := New(srv.URL, srv.Client())
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
