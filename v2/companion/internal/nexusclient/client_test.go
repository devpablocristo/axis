package nexusclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/professionalauthority"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCheckSendsGovernanceRequestToNexus(t *testing.T) {
	var gotOrg string
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
		gotOrg = r.Header.Get("X-Org-ID")
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
		OrgID:          "organization-1",
		RequesterType:  "virployee",
		RequesterID:    "virployee-1",
		ActionType:     "calendar.events.delete",
		TargetSystem:   "calendar",
		TargetResource: "events",
		Reason:         "delete the event",
		BindingHash:    "binding-123",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if gotOrg != "organization-1" {
		t.Fatalf("expected organization header organization-1, got %q", gotOrg)
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
	if _, ok := gotBody["params"]; ok {
		t.Fatalf("governance request must not contain action arguments: %+v", gotBody)
	}
	if _, ok := gotBody["context"]; ok {
		t.Fatalf("governance request must not contain conversation context: %+v", gotBody)
	}
	if out.Decision != "require_approval" || !out.WouldRequireApproval || out.BindingHash != "binding-123" || out.ApprovalID != "approval-123" || out.ApprovalStatus != "pending" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestRevalidateSendsMetadataOnlyAuthorityBinding(t *testing.T) {
	checkID := uuid.NewString()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/governance/checks/"+checkID+"/revalidate" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"valid":true,"reason":"still current","policy_snapshot_hash":"snapshot-a"}`))
	}))
	defer srv.Close()
	out, err := New(srv.URL, srv.Client()).Revalidate(context.Background(), executiongate.GovernanceRevalidationInput{
		OrgID: "organization-1", CheckID: checkID, BindingHash: "binding-a", PolicySnapshotHash: "snapshot-a",
		AuthorityBindingHash: "authority-a", ScopeRevision: 3, PolicyRevisionHash: "professional-a",
		DelegationID: "delegation-a", DelegationRevision: 2,
	})
	if err != nil || !out.Valid || body["binding_hash"] != "binding-a" || body["policy_snapshot_hash"] != "snapshot-a" {
		t.Fatalf("unexpected revalidation: out=%+v body=%+v err=%v", out, body, err)
	}
}

func TestCheckDelegationAuthorizationUsesInternalEndpoint(t *testing.T) {
	var gotRole string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/internal/authorization:check" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotRole = r.Header.Get("X-Axis-Org-Role")
		_, _ = w.Write([]byte(`{"allowed":true,"reason":"functional grant"}`))
	}))
	defer srv.Close()
	out, err := New(srv.URL, srv.Client()).CheckDelegationAuthorization(context.Background(), professionalauthority.DelegationAuthorizationCheck{
		OrgID: "organization-1", ActorID: "delegate-1", ActorRole: "member", Permission: "delegations.write",
		ProductSurface: "clinical", ActionType: "records.read", ResourceType: "case", ResourceID: "case-a", RiskClass: "medium",
	})
	if err != nil || !out.Allowed || gotRole != "member" {
		t.Fatalf("unexpected functional authorization: out=%+v role=%q err=%v", out, gotRole, err)
	}
}

func TestGetApprovalReadsNexusApproval(t *testing.T) {
	approvalID := uuid.New()
	var gotOrg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/approvals/"+approvalID.String() {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotOrg = r.Header.Get("X-Org-ID")
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
	out, err := client.GetApproval(context.Background(), "organization-1", approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if gotOrg != "organization-1" {
		t.Fatalf("expected organization header organization-1, got %q", gotOrg)
	}
	if out.ID != approvalID.String() || out.RequesterID != "virployee-1" || out.BindingHash != "binding-123" || out.Status != "approved" {
		t.Fatalf("unexpected approval: %+v", out)
	}
}

func TestGetApprovalMapsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	client := New(srv.URL, srv.Client())
	_, err := client.GetApproval(context.Background(), "organization-1", uuid.New())
	if !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestAppendAuditEventIdempotentSendsStableEventKey(t *testing.T) {
	id := uuid.NewString()
	var gotKey, gotOrg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		gotOrg = r.Header.Get("X-Org-ID")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	client := New(srv.URL, srv.Client())
	if err := client.AppendAuditEventIdempotent(context.Background(), "organization-1", id, AuditEvent{
		VirployeeID: "vp-1", ActorType: "human", ActorID: "owner-1", EventType: "scope_policy_changed",
	}); err != nil {
		t.Fatalf("append audit: %v", err)
	}
	if gotKey != id || gotOrg != "organization-1" {
		t.Fatalf("unexpected idempotency/organization headers: key=%q organization=%q", gotKey, gotOrg)
	}
}

func TestAppendAuditEventClassifiesNexusFourHundreds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	err := New(srv.URL, srv.Client()).AppendAuditEventIdempotent(context.Background(), "organization-1", uuid.NewString(), AuditEvent{
		VirployeeID: "vp-1", ActorID: "owner-1", EventType: "scope_policy_changed",
	})
	if !IsPermanentHTTPError(err) {
		t.Fatalf("409 must be classified as permanent, got %v", err)
	}
}
