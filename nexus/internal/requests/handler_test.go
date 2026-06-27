package requests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/nexus/internal/approvals"
	approvaldomain "github.com/devpablocristo/nexus/internal/approvals/usecases/domain"
	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	"github.com/devpablocristo/nexus/internal/callbacks"
	"github.com/devpablocristo/nexus/internal/orgctx"
	"github.com/devpablocristo/nexus/internal/productctx"
	"github.com/devpablocristo/nexus/internal/requests"
	requestdto "github.com/devpablocristo/nexus/internal/requests/handler/dto"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
)

// --- Fakes para tests ---

type fakeRequestRepo struct {
	mu   sync.RWMutex
	byID map[uuid.UUID]requestdomain.Request
}

func newFakeRequestRepo() *fakeRequestRepo {
	return &fakeRequestRepo{byID: make(map[uuid.UUID]requestdomain.Request)}
}

func (r *fakeRequestRepo) Create(_ context.Context, req requestdomain.Request) (requestdomain.Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	r.byID[req.ID] = req
	return req, nil
}

func (r *fakeRequestRepo) GetByID(_ context.Context, id uuid.UUID) (requestdomain.Request, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	req, ok := r.byID[id]
	if !ok {
		return requestdomain.Request{}, requests.ErrNotFound
	}
	return req, nil
}

func (r *fakeRequestRepo) GetByIdempotencyKey(_ context.Context, _ string) (*requestdomain.Request, error) {
	return nil, nil
}

func (r *fakeRequestRepo) List(_ context.Context, status, actionType string, limit int, orgID *string, allowAll bool) ([]requestdomain.Request, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []requestdomain.Request
	for _, req := range r.byID {
		if status != "" && string(req.Status) != status {
			continue
		}
		if actionType != "" && req.ActionType != actionType {
			continue
		}
		// Espejar el filtro SQL del repo Postgres.
		if !allowAll {
			if orgID != nil {
				if req.OrgID == nil || strings.TrimSpace(*req.OrgID) != strings.TrimSpace(*orgID) {
					continue
				}
			} else {
				if req.OrgID != nil {
					continue
				}
			}
		}
		out = append(out, req)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeRequestRepo) Update(_ context.Context, req requestdomain.Request) (requestdomain.Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[req.ID] = req
	return req, nil
}

type atomicRequestRepo struct {
	*fakeRequestRepo
	approval approvaldomain.Approval
}

func (r *atomicRequestRepo) CreateWithApproval(_ context.Context, req requestdomain.Request, approval approvaldomain.Approval) (requestdomain.Request, approvaldomain.Approval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	if approval.ID == uuid.Nil {
		approval.ID = uuid.New()
	}
	approval.RequestID = req.ID
	req.ApprovalID = &approval.ID
	r.byID[req.ID] = req
	r.approval = approval
	return req, approval, nil
}

type fakeApprovalRepo struct {
	mu   sync.RWMutex
	byID map[uuid.UUID]approvaldomain.Approval
}

func newFakeApprovalRepo() *fakeApprovalRepo {
	return &fakeApprovalRepo{byID: make(map[uuid.UUID]approvaldomain.Approval)}
}

func (r *fakeApprovalRepo) Create(_ context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	r.byID[a.ID] = a
	return a, nil
}

func (r *fakeApprovalRepo) GetByID(_ context.Context, id uuid.UUID) (approvaldomain.Approval, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.byID[id]
	if !ok {
		return approvaldomain.Approval{}, approvals.ErrNotFound
	}
	return a, nil
}

func (r *fakeApprovalRepo) GetByRequestID(_ context.Context, _ uuid.UUID) (*approvaldomain.Approval, error) {
	return nil, nil
}

func (r *fakeApprovalRepo) ListPending(_ context.Context, _ int) ([]approvaldomain.Approval, error) {
	return nil, nil
}

func (r *fakeApprovalRepo) Update(_ context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[a.ID] = a
	return a, nil
}

type fakeAuditRepo struct{}

func (r *fakeAuditRepo) Append(_ context.Context, _ auditdomain.RequestEvent) error { return nil }
func (r *fakeAuditRepo) ListByRequestID(_ context.Context, _ uuid.UUID) ([]auditdomain.RequestEvent, error) {
	return nil, nil
}

type fakePolicyLister struct {
	policies []requests.PolicyForEval
}

func (e *fakePolicyLister) ListActive(_ context.Context, _ *string) ([]requests.PolicyForEval, error) {
	return e.policies, nil
}

type fakeIdemStore struct {
	mu   sync.RWMutex
	keys map[string]struct {
		id   uuid.UUID
		resp map[string]any
	}
}

func newFakeIdemStore() *fakeIdemStore {
	return &fakeIdemStore{keys: make(map[string]struct {
		id   uuid.UUID
		resp map[string]any
	})}
}

func (s *fakeIdemStore) Get(_ context.Context, key string) (uuid.UUID, map[string]any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.keys[key]
	if !ok {
		return uuid.Nil, nil, false
	}
	return e.id, e.resp, true
}

func (s *fakeIdemStore) Set(_ context.Context, key string, id uuid.UUID, resp map[string]any, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key] = struct {
		id   uuid.UUID
		resp map[string]any
	}{id, resp}
	return nil
}

type fakeApprovalCallbackPublisher struct {
	mu     sync.Mutex
	events []callbacks.ApprovalEvent
}

func (p *fakeApprovalCallbackPublisher) Publish(_ context.Context, event callbacks.ApprovalEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return nil
}

func (p *fakeApprovalCallbackPublisher) snapshot() []callbacks.ApprovalEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]callbacks.ApprovalEvent, len(p.events))
	copy(out, p.events)
	return out
}

type fakeDelegationChecker struct {
	mu         sync.Mutex
	allowed    bool
	agentID    string
	actionType string
	orgID      *string
}

func (c *fakeDelegationChecker) CheckDelegation(_ context.Context, agentID, actionType string, orgID *string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agentID = agentID
	c.actionType = actionType
	if orgID != nil {
		v := *orgID
		c.orgID = &v
	} else {
		c.orgID = nil
	}
	return c.allowed, nil
}

func (c *fakeDelegationChecker) snapshot() (string, string, *string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var orgID *string
	if c.orgID != nil {
		v := *c.orgID
		orgID = &v
	}
	return c.agentID, c.actionType, orgID
}

// --- Helpers ---

// setupRequestMux crea un mux con fakes y sin políticas activas (default risk → allow para low).
func setupRequestMux() *http.ServeMux {
	return setupRequestMuxWithPolicies(nil)
}

// setupRequestMuxWithPolicies crea un mux con las políticas indicadas.
func setupRequestMuxWithPolicies(policies []requests.PolicyForEval) *http.ServeMux {
	mux, _ := setupRequestMuxWithRepo(policies)
	return mux
}

// setupRequestMuxWithRepo expone además el repo fake para sembrar requests
// directamente (ej. rows de orgs distintos para tests de tenancy).
func setupRequestMuxWithRepo(policies []requests.PolicyForEval) (*http.ServeMux, *fakeRequestRepo) {
	reqRepo := newFakeRequestRepo()
	auditSink := requests.NewAuditSinkAdapter(&fakeAuditRepo{})
	evaluator := requests.NewPolicyEvaluator()
	riskConfig := requests.DefaultRiskConfig()

	uc := requests.NewUsecases(reqRepo, &fakePolicyLister{policies: policies}, newFakeApprovalRepo(), evaluator,
		requests.WithIdempotencyStore(newFakeIdemStore()),
		requests.WithAuditSink(auditSink),
		requests.WithRiskConfig(riskConfig),
		requests.WithApprovalTTL(time.Hour),
	)
	mux := http.NewServeMux()
	requests.NewHandler(uc).Register(mux)
	return mux, reqRepo
}

func doReq(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doReqWithHeaders(t, mux, method, path, body, nil)
}

func doReqWithHeaders(t *testing.T, mux *http.ServeMux, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, r)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	mux.ServeHTTP(rec, req)
	return rec
}

func decJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v, body: %s", err, rec.Body.String())
	}
}

// --- Tests de Submit ---

func TestSubmitRequestAllowed(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	rec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"ops-bot","action_type":"alert.escalate"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.SubmitResponse
	decJSON(t, rec, &resp)
	if resp.Decision != "allow" {
		t.Fatalf("esperaba allow, obtuvo %s", resp.Decision)
	}
	if resp.Status != "allowed" {
		t.Fatalf("esperaba status allowed, obtuvo %s", resp.Status)
	}
}

// TestSubmitTagsProductSurfaceFromContext locks in the product-aware write
// path: the product surface carried in the request context (set by the auth
// middleware from the verified JWT claim) is persisted on the request, so a
// tenant = org x product can be partitioned per product.
func TestSubmitTagsProductSurfaceFromContext(t *testing.T) {
	t.Parallel()
	mux, repo := setupRequestMuxWithRepo(nil)

	body := `{"requester_type":"agent","requester_id":"ops-bot","action_type":"alert.escalate"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(body))
	req = req.WithContext(productctx.WithProduct(req.Context(), "companion"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	stored, err := repo.List(context.Background(), "", "", 0, nil, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored request, got %d", len(stored))
	}
	if stored[0].ProductSurface != "companion" {
		t.Fatalf("expected persisted product_surface=companion, got %q", stored[0].ProductSurface)
	}
}

func TestSubmitRequestRequireApproval(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// alert.silence es high risk → require_approval por default
	rec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"ops-bot","action_type":"alert.silence","target_resource":"CPU"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.SubmitResponse
	decJSON(t, rec, &resp)
	if resp.Decision != "require_approval" {
		t.Fatalf("esperaba require_approval, obtuvo %s", resp.Decision)
	}
	if resp.Approval == nil {
		t.Fatal("approval no deberia ser nil")
	}
	if resp.Status != "pending_approval" {
		t.Fatalf("esperaba status pending_approval, obtuvo %s", resp.Status)
	}
	// Nexus es AI-independent: ai_summary debe llegar vacío y ai_degraded en
	// false. Quien necesite el summary lo pide a Companion vía
	// /companion/v1/nexus-assist/explain/{request_id}.
	if resp.AIDegraded {
		t.Fatal("esperaba ai_degraded=false (Nexus AI-free)")
	}
	if resp.AISummary != "" {
		t.Fatalf("esperaba ai_summary vacio (Nexus AI-free), obtuvo %q", resp.AISummary)
	}
}

func TestSubmitRequestRejectsForeignOrgParam(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	rec := doReqWithHeaders(t, mux, http.MethodPost, "/v1/requests", `{
		"requester_type":"agent",
		"requester_id":"bot",
		"action_type":"alert.escalate",
		"params":{"org_id":"org-b"}
	}`, map[string]string{"X-Org-ID": "org-a"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperaba 403, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRequestRejectsForeignOrg(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	createRec := doReqWithHeaders(t, mux, http.MethodPost, "/v1/requests", `{
		"requester_type":"agent",
		"requester_id":"bot",
		"action_type":"alert.escalate"
	}`, map[string]string{"X-Org-ID": "org-a"})
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	rec := doReqWithHeaders(t, mux, http.MethodGet, "/v1/requests/"+submitResp.RequestID, "", map[string]string{"X-Org-ID": "org-b"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperaba 403, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmitPublishesApprovalPendingCallbackWithOrgIDFromParams(t *testing.T) {
	t.Parallel()

	reqRepo := newFakeRequestRepo()
	approvalRepo := newFakeApprovalRepo()
	publisher := &fakeApprovalCallbackPublisher{}
	uc := requests.NewUsecases(reqRepo, &fakePolicyLister{}, approvalRepo, requests.NewPolicyEvaluator(),
		requests.WithIdempotencyStore(newFakeIdemStore()),
		requests.WithAuditSink(requests.NewAuditSinkAdapter(&fakeAuditRepo{})),
		requests.WithApprovalTTL(time.Hour),
		requests.WithApprovalCallbacks(publisher),
	)

	out, err := uc.Submit(context.Background(), requests.SubmitInput{
		RequesterType:  "service",
		RequesterID:    "ai-service",
		ActionType:     "alert.silence",
		TargetSystem:   "pymes",
		TargetResource: "sale-1",
		Params:         map[string]any{"org_id": "00000000-0000-0000-0000-000000000001"},
		Reason:         "requires approval",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if out.Approval == nil {
		t.Fatal("expected approval payload")
	}

	events := publisher.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 callback event, got %d", len(events))
	}
	if events[0].Event != callbacks.EventApprovalPending {
		t.Fatalf("expected pending event, got %s", events[0].Event)
	}
	if events[0].OrgID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected org_id propagated, got %q", events[0].OrgID)
	}
	if events[0].ApprovalID != out.Approval.ID.String() {
		t.Fatalf("expected approval id %s, got %s", out.Approval.ID, events[0].ApprovalID)
	}

	storedApproval, err := approvalRepo.GetByID(context.Background(), out.Approval.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if storedApproval.OrgID == nil || *storedApproval.OrgID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected approval org_id stored, got %#v", storedApproval.OrgID)
	}
}

func TestSubmitRequireApprovalUsesAtomicCreatorWhenAvailable(t *testing.T) {
	t.Parallel()

	reqRepo := &atomicRequestRepo{fakeRequestRepo: newFakeRequestRepo()}
	uc := requests.NewUsecases(reqRepo, &fakePolicyLister{}, newFakeApprovalRepo(), requests.NewPolicyEvaluator(),
		requests.WithAuditSink(requests.NewAuditSinkAdapter(&fakeAuditRepo{})),
		requests.WithApprovalTTL(time.Hour),
	)

	out, err := uc.Submit(context.Background(), requests.SubmitInput{
		RequesterType:  "service",
		RequesterID:    "companion",
		ActionType:     "alert.silence",
		TargetSystem:   "pymes",
		TargetResource: "sale-1",
		Params:         map[string]any{"org_id": "org-a"},
		Reason:         "requires approval",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if out.Approval == nil {
		t.Fatal("expected approval")
	}
	stored, err := reqRepo.GetByID(context.Background(), out.RequestID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if stored.ApprovalID == nil || *stored.ApprovalID != out.Approval.ID {
		t.Fatalf("request approval_id was not persisted atomically: %#v", stored.ApprovalID)
	}
	if reqRepo.approval.RequestID != out.RequestID {
		t.Fatalf("approval request_id = %s, want %s", reqRepo.approval.RequestID, out.RequestID)
	}
}

func TestSubmitPersistsActionBindingHash(t *testing.T) {
	t.Parallel()

	reqRepo := newFakeRequestRepo()
	uc := requests.NewUsecases(reqRepo, &fakePolicyLister{}, newFakeApprovalRepo(), requests.NewPolicyEvaluator(),
		requests.WithAuditSink(requests.NewAuditSinkAdapter(&fakeAuditRepo{})),
	)
	binding := map[string]any{
		"schema_version":     "tool_intent.v1",
		"org_id":             "org-a",
		"actor_id":           "agent-1",
		"actor_type":         "agent",
		"product_surface":    "companion",
		"run_id":             "run-1",
		"tool_invocation_id": "tool-1",
		"connector_id":       "connector-1",
		"capability_id":      "mock.write",
		"operation":          "mock.write",
		"target_system":      "mock",
		"target_resource":    "connector-1",
		"payload_hash":       "payload-hash",
		"idempotency_key":    "idem-1",
	}

	out, err := uc.Submit(context.Background(), requests.SubmitInput{
		RequesterType:  "service",
		RequesterID:    "companion",
		ActionType:     "companion.propose",
		TargetSystem:   "mock",
		TargetResource: "connector-1",
		ActionBinding:  binding,
		Params:         map[string]any{"org_id": "org-a"},
		Reason:         "execute exact connector action",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if out.BindingHash == "" {
		t.Fatal("expected binding hash")
	}
	stored, err := reqRepo.GetByID(context.Background(), out.RequestID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if stored.BindingHash != out.BindingHash {
		t.Fatalf("stored binding hash %q != output %q", stored.BindingHash, out.BindingHash)
	}
	if stored.ActionBinding["payload_hash"] != "payload-hash" {
		t.Fatalf("unexpected action binding %+v", stored.ActionBinding)
	}
}

func TestSubmitPassesOrgIDToDelegationChecker(t *testing.T) {
	t.Parallel()

	checker := &fakeDelegationChecker{allowed: true}
	uc := requests.NewUsecases(newFakeRequestRepo(), &fakePolicyLister{}, newFakeApprovalRepo(), requests.NewPolicyEvaluator(),
		requests.WithAuditSink(requests.NewAuditSinkAdapter(&fakeAuditRepo{})),
		requests.WithDelegationChecker(checker),
	)

	_, err := uc.Submit(context.Background(), requests.SubmitInput{
		RequesterType: "agent",
		RequesterID:   "ops-bot",
		ActionType:    "alert.escalate",
		Params:        map[string]any{"org_id": "org-a"},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	agentID, actionType, orgID := checker.snapshot()
	if agentID != "ops-bot" {
		t.Fatalf("expected agent id ops-bot, got %q", agentID)
	}
	if actionType != "alert.escalate" {
		t.Fatalf("expected action type alert.escalate, got %q", actionType)
	}
	if orgID == nil || *orgID != "org-a" {
		t.Fatalf("expected org_id org-a passed to delegation checker, got %#v", orgID)
	}
}

func TestSubmitRequestDeny(t *testing.T) {
	t.Parallel()

	// Política que deniega todas las acciones "deploy.execute"
	actionType := "deploy.execute"
	policies := []requests.PolicyForEval{
		{
			ID:         uuid.New(),
			Name:       "deny-deploys",
			ActionType: &actionType,
			Expression: "true",
			Effect:     "deny",
		},
	}
	mux := setupRequestMuxWithPolicies(policies)

	rec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"ci-bot","action_type":"deploy.execute"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.SubmitResponse
	decJSON(t, rec, &resp)
	if resp.Decision != "deny" {
		t.Fatalf("esperaba deny, obtuvo %s", resp.Decision)
	}
	if resp.Status != "denied" {
		t.Fatalf("esperaba status denied, obtuvo %s", resp.Status)
	}
}

func TestSubmitRequestWithPolicyAllow(t *testing.T) {
	t.Parallel()

	// Política que permite explicitamente "alert.ack"
	actionType := "alert.ack"
	policies := []requests.PolicyForEval{
		{
			ID:         uuid.New(),
			Name:       "allow-ack",
			ActionType: &actionType,
			Expression: "true",
			Effect:     "allow",
		},
	}
	mux := setupRequestMuxWithPolicies(policies)

	rec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"human","requester_id":"user@co","action_type":"alert.ack"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.SubmitResponse
	decJSON(t, rec, &resp)
	if resp.Decision != "allow" {
		t.Fatalf("esperaba allow, obtuvo %s", resp.Decision)
	}
	if resp.DecisionReason == "" {
		t.Fatal("esperaba decision_reason no vacio para policy match")
	}
}

// --- Tests de validacion ---

func TestSubmitValidation(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"cuerpo vacio", `{}`, http.StatusBadRequest},
		{"falta action_type", `{"requester_type":"agent","requester_id":"bot"}`, http.StatusBadRequest},
		{"falta requester_type", `{"requester_id":"bot","action_type":"x"}`, http.StatusBadRequest},
		{"falta requester_id", `{"requester_type":"agent","action_type":"x"}`, http.StatusBadRequest},
		{"json invalido", `{bad`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doReq(t, mux, http.MethodPost, "/v1/requests", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("esperaba %d, obtuvo %d: %s", tt.status, rec.Code, rec.Body.String())
			}

			// Verificar que el error tiene estructura correcta
			var errResp struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			decJSON(t, rec, &errResp)
			if errResp.Code == "" {
				t.Fatal("esperaba code de error no vacio")
			}
			if errResp.Message == "" {
				t.Fatal("esperaba message de error no vacio")
			}
		})
	}
}

func TestSubmitIdempotencyKeyTooLong(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	body := `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(body))
	// Clave de idempotencia de 257 caracteres (max es 256)
	req.Header.Set("Idempotency-Key", strings.Repeat("a", 257))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Tests de idempotencia ---

func TestIdempotency(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	body := `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`

	// Primera request con clave de idempotencia
	req1 := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(body))
	req1.Header.Set("Idempotency-Key", "test-key-123")
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	// Segunda request con la misma clave
	req2 := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(body))
	req2.Header.Set("Idempotency-Key", "test-key-123")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	var resp1, resp2 requestdto.SubmitResponse
	decJSON(t, rec1, &resp1)
	decJSON(t, rec2, &resp2)

	if resp1.RequestID != resp2.RequestID {
		t.Fatalf("idempotencia fallo: %s vs %s", resp1.RequestID, resp2.RequestID)
	}
	if resp1.Decision != resp2.Decision {
		t.Fatalf("decisiones distintas: %s vs %s", resp1.Decision, resp2.Decision)
	}
}

func TestIdempotencyRejectsSameKeyDifferentPayload(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	req1 := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(
		`{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate","target_resource":"a"}`))
	req1.Header.Set("Idempotency-Key", "conflict-key-123")
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", rec1.Code, rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(
		`{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate","target_resource":"b"}`))
	req2.Header.Set("Idempotency-Key", "conflict-key-123")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("esperaba 409, obtuvo %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestIdempotencyDifferentKeys(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	body := `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`

	// Dos requests con claves distintas deben generar IDs distintos
	req1 := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(body))
	req1.Header.Set("Idempotency-Key", "key-alpha")
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/v1/requests", strings.NewReader(body))
	req2.Header.Set("Idempotency-Key", "key-beta")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	var resp1, resp2 requestdto.SubmitResponse
	decJSON(t, rec1, &resp1)
	decJSON(t, rec2, &resp2)

	if resp1.RequestID == resp2.RequestID {
		t.Fatalf("claves distintas deben generar IDs distintos, ambos son %s", resp1.RequestID)
	}
}

// --- Tests de GetByID ---

func TestGetByIDHappyPath(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Crear una request primero
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d", createRec.Code)
	}
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	// Obtener la request por ID
	rec := doReq(t, mux, http.MethodGet, "/v1/requests/"+submitResp.RequestID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.RequestResponse
	decJSON(t, rec, &resp)
	if resp.ID != submitResp.RequestID {
		t.Fatalf("esperaba id %s, obtuvo %s", submitResp.RequestID, resp.ID)
	}
	if resp.RequesterType != "agent" {
		t.Fatalf("esperaba requester_type agent, obtuvo %s", resp.RequesterType)
	}
	if resp.RequesterID != "bot" {
		t.Fatalf("esperaba requester_id bot, obtuvo %s", resp.RequesterID)
	}
	if resp.ActionType != "alert.escalate" {
		t.Fatalf("esperaba action_type alert.escalate, obtuvo %s", resp.ActionType)
	}
	if resp.Decision != "allow" {
		t.Fatalf("esperaba decision allow, obtuvo %s", resp.Decision)
	}
	if resp.CreatedAt == "" {
		t.Fatal("esperaba created_at no vacio")
	}
	if resp.UpdatedAt == "" {
		t.Fatal("esperaba updated_at no vacio")
	}
}

func TestGetByIDNotFound(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	rec := doReq(t, mux, http.MethodGet, "/v1/requests/00000000-0000-0000-0000-000000000000", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperaba 404, obtuvo %d", rec.Code)
	}

	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "NOT_FOUND" {
		t.Fatalf("esperaba code NOT_FOUND, obtuvo %s", errResp.Code)
	}
}

func TestGetByIDInvalidUUID(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	rec := doReq(t, mux, http.MethodGet, "/v1/requests/not-a-uuid", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "VALIDATION" {
		t.Fatalf("esperaba code VALIDATION, obtuvo %s", errResp.Code)
	}
}

// --- Tests de List ---

func TestListRequestsEmpty(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	rec := doReq(t, mux, http.MethodGet, "/v1/requests", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}

	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 0 {
		t.Fatalf("esperaba 0, obtuvo %d", len(listResp.Data))
	}
}

func TestListRequests(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Crear dos requests
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"human","requester_id":"user@co","action_type":"incident.resolve"}`)

	rec := doReq(t, mux, http.MethodGet, "/v1/requests", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}

	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 2 {
		t.Fatalf("esperaba 2, obtuvo %d", len(listResp.Data))
	}
}

func TestListRequestsFilterByStatus(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// alert.escalate → low risk → allowed
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	// alert.silence → high risk → pending_approval
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.silence"}`)

	// Filtrar por status=allowed
	rec := doReq(t, mux, http.MethodGet, "/v1/requests?status=allowed", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}

	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 1 {
		t.Fatalf("esperaba 1 request allowed, obtuvo %d", len(listResp.Data))
	}
	if listResp.Data[0].Status != "allowed" {
		t.Fatalf("esperaba status allowed, obtuvo %s", listResp.Data[0].Status)
	}
}

func TestListRequestsFilterByActionType(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"human","requester_id":"user@co","action_type":"incident.resolve"}`)

	// Filtrar por action_type
	rec := doReq(t, mux, http.MethodGet, "/v1/requests?action_type=alert.escalate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}

	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 1 {
		t.Fatalf("esperaba 1, obtuvo %d", len(listResp.Data))
	}
	if listResp.Data[0].ActionType != "alert.escalate" {
		t.Fatalf("esperaba action_type alert.escalate, obtuvo %s", listResp.Data[0].ActionType)
	}
}

func TestListRequestsWithLimit(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Crear 3 requests
	for i := 0; i < 3; i++ {
		doReq(t, mux, http.MethodPost, "/v1/requests",
			fmt.Sprintf(`{"requester_type":"agent","requester_id":"bot-%d","action_type":"alert.escalate"}`, i))
	}

	// Pedir con limit=2
	rec := doReq(t, mux, http.MethodGet, "/v1/requests?limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}

	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 2 {
		t.Fatalf("esperaba 2, obtuvo %d", len(listResp.Data))
	}
}

func TestListRequestsInvalidLimit(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Limit invalido se ignora y se usa el default
	rec := doReq(t, mux, http.MethodGet, "/v1/requests?limit=abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}
}

func TestListRequestsNegativeLimit(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Limit negativo se ignora y se usa el default
	rec := doReq(t, mux, http.MethodGet, "/v1/requests?limit=-5", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}
}

func TestListRequestsLimitExceedsMax(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Limit que excede MaxListLimit (1000) se ignora y se usa el default
	rec := doReq(t, mux, http.MethodGet, "/v1/requests?limit=5000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}
}

func TestListRequestsCombinedFilters(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Crear varias requests con distintos tipos
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"human","requester_id":"user@co","action_type":"alert.escalate"}`)
	doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"incident.resolve"}`)

	// Filtrar por status=allowed y action_type=alert.escalate
	rec := doReq(t, mux, http.MethodGet, "/v1/requests?status=allowed&action_type=alert.escalate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec.Code)
	}

	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	// Las 2 alert.escalate deberian tener status=allowed (low risk)
	if len(listResp.Data) != 2 {
		t.Fatalf("esperaba 2, obtuvo %d", len(listResp.Data))
	}
	for _, d := range listResp.Data {
		if d.Status != "allowed" {
			t.Fatalf("esperaba status allowed, obtuvo %s", d.Status)
		}
		if d.ActionType != "alert.escalate" {
			t.Fatalf("esperaba action_type alert.escalate, obtuvo %s", d.ActionType)
		}
	}
}

// --- Tests de List: narrowing cross_org vía orgctx ---

// seedRequestForOrg inserta una request directamente en el repo fake.
func seedRequestForOrg(t *testing.T, repo *fakeRequestRepo, orgID string) {
	t.Helper()
	org := orgID
	if _, err := repo.Create(context.Background(), requestdomain.Request{
		ID:         uuid.New(),
		OrgID:      &org,
		ActionType: "alert.escalate",
		Status:     requestdomain.StatusAllowed,
	}); err != nil {
		t.Fatal(err)
	}
}

// listReqWithPrincipal arma un GET /v1/requests con el org solicitado
// preservado en orgctx (como hace el middleware de wire ANTES de
// WithPrincipal) y el principal indicado.
func listReqWithPrincipal(requestedOrg string, principal *authn.Principal) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/requests", nil)
	if requestedOrg != "" {
		req = req.WithContext(orgctx.WithRequested(req.Context(), requestedOrg))
	}
	return identityhttp.WithPrincipal(req, principal, principal.AuthMethod)
}

// TestListRequestsCrossOrgNarrowedByRequestedOrg verifica que un principal
// cross_org con X-Org-ID inbound (preservado en orgctx) ve SOLO ese org.
func TestListRequestsCrossOrgNarrowedByRequestedOrg(t *testing.T) {
	t.Parallel()
	mux, repo := setupRequestMuxWithRepo(nil)
	seedRequestForOrg(t, repo, "acme")
	seedRequestForOrg(t, repo, "globex")

	req := listReqWithPrincipal("acme", &authn.Principal{
		Actor:      "pymes-service",
		Scopes:     []string{"nexus:requests:read", "nexus:cross_org"},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 1 {
		t.Fatalf("esperaba 1 request (solo acme), obtuvo %d", len(listResp.Data))
	}
	if listResp.Data[0].OrgID != "acme" {
		t.Fatalf("org_id = %q, esperaba acme", listResp.Data[0].OrgID)
	}
}

// TestListRequestsCrossOrgWithoutRequestedKeepsAllowAll verifica compat: un
// principal cross_org sin org solicitado ni bound sigue viendo todos los orgs.
func TestListRequestsCrossOrgWithoutRequestedKeepsAllowAll(t *testing.T) {
	t.Parallel()
	mux, repo := setupRequestMuxWithRepo(nil)
	seedRequestForOrg(t, repo, "acme")
	seedRequestForOrg(t, repo, "globex")

	req := listReqWithPrincipal("", &authn.Principal{
		Actor:      "pymes-service",
		Scopes:     []string{"nexus:requests:read", "nexus:cross_org"},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 2 {
		t.Fatalf("esperaba 2 requests (allowAll), obtuvo %d", len(listResp.Data))
	}
}

// TestListRequestsNonCrossOrgIgnoresRequestedOrg verifica que un principal
// SIN cross_org no puede escaparse de su org bound vía X-Org-ID inbound.
func TestListRequestsNonCrossOrgIgnoresRequestedOrg(t *testing.T) {
	t.Parallel()
	mux, repo := setupRequestMuxWithRepo(nil)
	seedRequestForOrg(t, repo, "acme")
	seedRequestForOrg(t, repo, "globex")

	req := listReqWithPrincipal("globex", &authn.Principal{
		OrgID:      "acme",
		Actor:      "acme-service",
		Scopes:     []string{"nexus:requests:read"},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Data []requestdto.RequestResponse `json:"data"`
	}
	decJSON(t, rec, &listResp)
	if len(listResp.Data) != 1 {
		t.Fatalf("esperaba 1 request (solo acme, su org bound), obtuvo %d", len(listResp.Data))
	}
	if listResp.Data[0].OrgID != "acme" {
		t.Fatalf("org_id = %q, esperaba acme (org del principal, no el solicitado)", listResp.Data[0].OrgID)
	}
}

// --- Tests de ReportResult ---

func TestReportResultSuccess(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Crear request
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	// Reportar resultado exitoso
	resultBody := `{"success":true,"result":{"output":"escalated"},"duration_ms":150}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/result", resultBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resultResp map[string]string
	decJSON(t, rec, &resultResp)
	if resultResp["status"] != "ok" {
		t.Fatalf("esperaba status ok, obtuvo %s", resultResp["status"])
	}

	// Verificar que el estado cambio a executed
	getRec := doReq(t, mux, http.MethodGet, "/v1/requests/"+submitResp.RequestID, "")
	var getResp requestdto.RequestResponse
	decJSON(t, getRec, &getResp)
	if getResp.Status != "executed" {
		t.Fatalf("esperaba status executed, obtuvo %s", getResp.Status)
	}
}

func TestReportResultFailure(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Crear request
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	// Reportar resultado fallido
	resultBody := `{"success":false,"error_message":"timeout connecting to target"}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/result", resultBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	// Verificar que el estado cambio a failed
	getRec := doReq(t, mux, http.MethodGet, "/v1/requests/"+submitResp.RequestID, "")
	var getResp requestdto.RequestResponse
	decJSON(t, getRec, &getResp)
	if getResp.Status != "failed" {
		t.Fatalf("esperaba status failed, obtuvo %s", getResp.Status)
	}
}

func TestReportResultPendingApprovalConflict(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	createRec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.silence"}`)
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)
	if submitResp.Status != "pending_approval" {
		t.Fatalf("esperaba status pending_approval, obtuvo %s", submitResp.Status)
	}

	rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/result", `{"success":true}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperaba 409, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReportResultNotFound(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	resultBody := `{"success":true,"result":{"ok":true}}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/00000000-0000-0000-0000-000000000000/result", resultBody)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperaba 404, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReportResultInvalidUUID(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	resultBody := `{"success":true}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/not-a-uuid/result", resultBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var errResp struct {
		Code string `json:"code"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "VALIDATION" {
		t.Fatalf("esperaba code VALIDATION, obtuvo %s", errResp.Code)
	}
}

func TestReportResultInvalidJSON(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Crear una request primero
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests", `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	// Enviar JSON invalido
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/result", `{bad json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Tests de Submit con params ---

func TestSubmitRequestWithParams(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	body := `{
		"requester_type":"service",
		"requester_id":"monitoring-svc",
		"requester_name":"Monitoring Service",
		"action_type":"alert.escalate",
		"target_system":"pagerduty",
		"target_resource":"team-oncall",
		"params":{"severity":"critical","alert_id":"12345"},
		"reason":"High CPU usage detected",
		"context":"Production server us-east-1"
	}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.SubmitResponse
	decJSON(t, rec, &resp)

	// Verificar que los datos se persisten correctamente via GetByID
	getRec := doReq(t, mux, http.MethodGet, "/v1/requests/"+resp.RequestID, "")
	var getResp requestdto.RequestResponse
	decJSON(t, getRec, &getResp)

	if getResp.RequesterType != "service" {
		t.Fatalf("esperaba requester_type service, obtuvo %s", getResp.RequesterType)
	}
	if getResp.RequesterName != "Monitoring Service" {
		t.Fatalf("esperaba requester_name Monitoring Service, obtuvo %s", getResp.RequesterName)
	}
	if getResp.TargetSystem != "pagerduty" {
		t.Fatalf("esperaba target_system pagerduty, obtuvo %s", getResp.TargetSystem)
	}
	if getResp.TargetResource != "team-oncall" {
		t.Fatalf("esperaba target_resource team-oncall, obtuvo %s", getResp.TargetResource)
	}
	if getResp.Reason != "High CPU usage detected" {
		t.Fatalf("esperaba reason correcto, obtuvo %s", getResp.Reason)
	}
}

// --- Tests de Submit sin idempotency (multiples requests generan IDs distintos) ---

func TestSubmitWithoutIdempotencyKey(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	body := `{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`

	rec1 := doReq(t, mux, http.MethodPost, "/v1/requests", body)
	rec2 := doReq(t, mux, http.MethodPost, "/v1/requests", body)

	var resp1, resp2 requestdto.SubmitResponse
	decJSON(t, rec1, &resp1)
	decJSON(t, rec2, &resp2)

	if resp1.RequestID == resp2.RequestID {
		t.Fatalf("sin idempotency key, cada submit debe generar un ID distinto, ambos son %s", resp1.RequestID)
	}
}

// --- Tests de risk level ---

func TestSubmitRiskLevels(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	tests := []struct {
		name         string
		actionType   string
		wantRisk     string
		wantDecision string
	}{
		{
			name:         "cascade risk: accion desconocida (allow con cualquier nivel)",
			actionType:   "custom.action",
			wantRisk:     "", // cascade: nivel varía según hora y contexto
			wantDecision: "allow",
		},
		{
			name:         "high risk: alert.silence",
			actionType:   "alert.silence",
			wantRisk:     "high",
			wantDecision: "require_approval",
		},
		{
			name:         "high risk: runbook.execute",
			actionType:   "runbook.execute",
			wantRisk:     "high",
			wantDecision: "require_approval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"requester_type":"agent","requester_id":"bot","action_type":"%s"}`, tt.actionType)
			rec := doReq(t, mux, http.MethodPost, "/v1/requests", body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("esperaba 201, obtuvo %d: %s", rec.Code, rec.Body.String())
			}

			var resp requestdto.SubmitResponse
			decJSON(t, rec, &resp)
			if tt.wantRisk != "" && resp.RiskLevel != tt.wantRisk {
				t.Fatalf("esperaba risk %s, obtuvo %s", tt.wantRisk, resp.RiskLevel)
			}
			if resp.Decision != tt.wantDecision {
				t.Fatalf("esperaba decision %s, obtuvo %s", tt.wantDecision, resp.Decision)
			}
		})
	}
}

// --- Fakes adicionales para Simulate, BatchSimulate, SimulateApproval, Attest ---

type fakeAttestationStore struct {
	mu      sync.RWMutex
	byReqID map[uuid.UUID]requestdomain.Attestation
}

func newFakeAttestationStore() *fakeAttestationStore {
	return &fakeAttestationStore{byReqID: make(map[uuid.UUID]requestdomain.Attestation)}
}

func (s *fakeAttestationStore) Create(_ context.Context, a requestdomain.Attestation) (requestdomain.Attestation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	s.byReqID[a.RequestID] = a
	return a, nil
}

func (s *fakeAttestationStore) GetByRequestID(_ context.Context, requestID uuid.UUID) (requestdomain.Attestation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.byReqID[requestID]
	if !ok {
		return requestdomain.Attestation{}, requests.ErrAttestationNotFound
	}
	return a, nil
}

// setupFullMux crea un mux con fakes completos: attestation store + approval getter.
func setupFullMux() *http.ServeMux {
	return setupFullMuxWithPolicies(nil)
}

// setupFullMuxWithPolicies crea un mux con attestation store, approval getter y las políticas indicadas.
func setupFullMuxWithPolicies(policies []requests.PolicyForEval) *http.ServeMux {
	reqRepo := newFakeRequestRepo()
	approvalRepo := newFakeApprovalRepo()
	auditSink := requests.NewAuditSinkAdapter(&fakeAuditRepo{})
	evaluator := requests.NewPolicyEvaluator()
	riskConfig := requests.DefaultRiskConfig()
	attestStore := newFakeAttestationStore()

	uc := requests.NewUsecases(reqRepo, &fakePolicyLister{policies: policies}, approvalRepo, evaluator,
		requests.WithIdempotencyStore(newFakeIdemStore()),
		requests.WithAuditSink(auditSink),
		requests.WithRiskConfig(riskConfig),
		requests.WithApprovalTTL(time.Hour),
		requests.WithAttestationStore(attestStore),
		requests.WithApprovalGetter(approvalRepo),
	)
	mux := http.NewServeMux()
	requests.NewHandler(uc).Register(mux)
	return mux
}

// --- Tests de Simulate ---

func TestSimulateHappyPath(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	// Simular alert.escalate (low risk → allow)
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate",
		`{"requester_type":"agent","requester_id":"ops-bot","action_type":"alert.escalate"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.SimulateResponse
	decJSON(t, rec, &resp)
	if resp.Decision != "allow" {
		t.Fatalf("esperaba decision allow, obtuvo %s", resp.Decision)
	}
	if resp.RiskLevel == "" {
		t.Fatal("esperaba risk_level no vacio")
	}
	if resp.Status != "allowed" {
		t.Fatalf("esperaba status allowed, obtuvo %s", resp.Status)
	}
	if resp.DecisionReason == "" {
		t.Fatal("esperaba decision_reason no vacio")
	}
}

func TestSimulateValidation(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"falta action_type", `{"requester_type":"agent","requester_id":"bot"}`, http.StatusBadRequest},
		{"json invalido", `{bad`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("esperaba %d, obtuvo %d: %s", tt.status, rec.Code, rec.Body.String())
			}
			var errResp struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			decJSON(t, rec, &errResp)
			if errResp.Code != "VALIDATION" {
				t.Fatalf("esperaba code VALIDATION, obtuvo %s", errResp.Code)
			}
		})
	}
}

func TestSimulateInvalidActionBindingReturnsValidation(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	binding := `{
		"schema_version":"tool_intent.v1",
		"org_id":"org-other",
		"actor_id":"pymes-e2e",
		"actor_type":"service",
		"product_surface":"pymes",
		"run_id":"run-e2e",
		"tool_invocation_id":"tool-e2e",
		"connector_id":"pymes.e2e",
		"capability_id":"e2e.notification.bulk_send",
		"operation":"invoke",
		"target_system":"pymes",
		"target_resource":"target-1",
		"payload_hash":"hash-e2e",
		"idempotency_key":"idem-e2e"
	}`
	body := fmt.Sprintf(`{
		"requester_type":"service",
		"requester_id":"pymes-e2e",
		"action_type":"e2e.notification.bulk_send",
		"target_system":"pymes",
		"target_resource":"target-1",
		"action_binding":%s,
		"params":{"org_id":"org-a"}
	}`, binding)

	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "VALIDATION" || !strings.Contains(errResp.Message, "action_binding.org_id") {
		t.Fatalf("respuesta inesperada: %+v", errResp)
	}
}

func TestSimulateWithPolicy(t *testing.T) {
	t.Parallel()

	// Política que deniega deploy.execute
	actionType := "deploy.execute"
	policies := []requests.PolicyForEval{
		{
			ID:         uuid.New(),
			Name:       "deny-deploys",
			ActionType: &actionType,
			Expression: "true",
			Effect:     "deny",
		},
	}
	mux := setupRequestMuxWithPolicies(policies)

	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate",
		`{"requester_type":"agent","requester_id":"ci-bot","action_type":"deploy.execute"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.SimulateResponse
	decJSON(t, rec, &resp)
	if resp.Decision != "deny" {
		t.Fatalf("esperaba decision deny, obtuvo %s", resp.Decision)
	}
	if resp.Status != "denied" {
		t.Fatalf("esperaba status denied, obtuvo %s", resp.Status)
	}
	if resp.PolicyMatched == nil {
		t.Fatal("esperaba policy_matched no nil")
	}
	if *resp.PolicyMatched != "deny-deploys" {
		t.Fatalf("esperaba policy_matched deny-deploys, obtuvo %s", *resp.PolicyMatched)
	}
}

// --- Tests de BatchSimulate ---

func TestBatchSimulateHappyPath(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	body := `{
		"requests": [
			{"requester_type":"agent","requester_id":"bot-1","action_type":"alert.escalate"},
			{"requester_type":"agent","requester_id":"bot-2","action_type":"alert.silence"},
			{"requester_type":"human","requester_id":"user@co","action_type":"incident.resolve"}
		]
	}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate/batch", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp requestdto.BatchSimulateResponse
	decJSON(t, rec, &resp)
	if resp.Total != 3 {
		t.Fatalf("esperaba total 3, obtuvo %d", resp.Total)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("esperaba 3 results, obtuvo %d", len(resp.Results))
	}
	// Verificar que los contadores suman el total
	sum := resp.Allowed + resp.Denied + resp.RequireApproval
	if sum != resp.Total {
		t.Fatalf("contadores no suman: allowed=%d + denied=%d + require_approval=%d != total=%d",
			resp.Allowed, resp.Denied, resp.RequireApproval, resp.Total)
	}
	// alert.silence es high risk → require_approval
	if resp.RequireApproval < 1 {
		t.Fatal("esperaba al menos 1 require_approval (alert.silence)")
	}
	// by_risk debe tener al menos una entrada
	if len(resp.ByRisk) == 0 {
		t.Fatal("esperaba by_risk con al menos una entrada")
	}
}

func TestBatchSimulateEmpty(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate/batch", `{"requests":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "VALIDATION" {
		t.Fatalf("esperaba code VALIDATION, obtuvo %s", errResp.Code)
	}
}

func TestBatchSimulateValidation(t *testing.T) {
	t.Parallel()
	mux := setupRequestMux()

	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate/batch", `{bad json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "VALIDATION" {
		t.Fatalf("esperaba code VALIDATION, obtuvo %s", errResp.Code)
	}
}

// --- Tests de SimulateApproval ---

func TestSimulateApprovalReject(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	// Crear request que requiera aprobación (alert.silence → high risk → pending_approval)
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests",
		`{"requester_type":"agent","requester_id":"ops-bot","action_type":"alert.silence","target_resource":"CPU"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", createRec.Code, createRec.Body.String())
	}
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)
	if submitResp.Status != "pending_approval" {
		t.Fatalf("esperaba status pending_approval, obtuvo %s", submitResp.Status)
	}

	// Simular rechazo
	simBody := fmt.Sprintf(`{"request_id":"%s","action":"reject","approver_id":"admin@co"}`, submitResp.RequestID)
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate/approval", simBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var simResp requestdto.ApprovalSimulateResponse
	decJSON(t, rec, &simResp)
	if simResp.CurrentStatus != "pending_approval" {
		t.Fatalf("esperaba current_status pending_approval, obtuvo %s", simResp.CurrentStatus)
	}
	if !simResp.WouldFinalize {
		t.Fatal("esperaba would_finalize=true para reject")
	}
	if simResp.SimulatedStatus != "rejected" {
		t.Fatalf("esperaba simulated_status rejected, obtuvo %s", simResp.SimulatedStatus)
	}
}

func TestSimulateApprovalNotPending(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	// Crear request allowed (alert.escalate → low risk → allowed)
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests",
		`{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", createRec.Code, createRec.Body.String())
	}
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	// Simular aprobación sobre request que ya está allowed (no pending_approval)
	simBody := fmt.Sprintf(`{"request_id":"%s","action":"approve","approver_id":"admin@co"}`, submitResp.RequestID)
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate/approval", simBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var simResp requestdto.ApprovalSimulateResponse
	decJSON(t, rec, &simResp)
	if simResp.Reason == "" {
		t.Fatal("esperaba reason no vacio indicando que no esta pending")
	}
	// El status no debería cambiar
	if simResp.CurrentStatus != simResp.SimulatedStatus {
		t.Fatalf("esperaba current_status == simulated_status para request no pending, obtuvo %s vs %s",
			simResp.CurrentStatus, simResp.SimulatedStatus)
	}
}

func TestSimulateApprovalValidation(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"json invalido", `{bad`, http.StatusBadRequest},
		{"falta request_id", `{"action":"approve","approver_id":"admin"}`, http.StatusBadRequest},
		{"falta action", `{"request_id":"00000000-0000-0000-0000-000000000001","approver_id":"admin"}`, http.StatusBadRequest},
		{"falta approver_id", `{"request_id":"00000000-0000-0000-0000-000000000001","action":"approve"}`, http.StatusBadRequest},
		{"action invalida", `{"request_id":"00000000-0000-0000-0000-000000000001","action":"cancel","approver_id":"admin"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doReq(t, mux, http.MethodPost, "/v1/requests/simulate/approval", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("esperaba %d, obtuvo %d: %s", tt.status, rec.Code, rec.Body.String())
			}
		})
	}
}

// --- Tests de Attest ---

func TestAttestHappyPath(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	// 1. Crear request (allowed)
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests",
		`{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", createRec.Code, createRec.Body.String())
	}
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	// 2. Reportar resultado exitoso → status ejecutado
	resultRec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/result",
		`{"success":true,"result":{"output":"done"},"duration_ms":100}`)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", resultRec.Code, resultRec.Body.String())
	}

	// 3. Attestar
	attestBody := `{"status":"success","provider_refs":{"tx_id":"abc123"},"signature":"sig-hash-xyz","attester":"pep:treasury","metadata":{"env":"prod"}}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/attest", attestBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var attestResp requestdto.AttestResponse
	decJSON(t, rec, &attestResp)
	if attestResp.ID == "" {
		t.Fatal("esperaba id de attestation no vacio")
	}
	if attestResp.RequestID != submitResp.RequestID {
		t.Fatalf("esperaba request_id %s, obtuvo %s", submitResp.RequestID, attestResp.RequestID)
	}
	if attestResp.Status != "success" {
		t.Fatalf("esperaba status success, obtuvo %s", attestResp.Status)
	}
	if attestResp.Attester != "pep:treasury" {
		t.Fatalf("esperaba attester pep:treasury, obtuvo %s", attestResp.Attester)
	}
	if attestResp.Signature != "sig-hash-xyz" {
		t.Fatalf("esperaba signature sig-hash-xyz, obtuvo %s", attestResp.Signature)
	}
	if attestResp.CreatedAt == "" {
		t.Fatal("esperaba created_at no vacio")
	}

	// 4. Verificar con GET attestation
	getRec := doReq(t, mux, http.MethodGet, "/v1/requests/"+submitResp.RequestID+"/attestation", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", getRec.Code, getRec.Body.String())
	}
	var getResp requestdto.AttestResponse
	decJSON(t, getRec, &getResp)
	if getResp.ID != attestResp.ID {
		t.Fatalf("esperaba mismo id, obtuvo %s vs %s", getResp.ID, attestResp.ID)
	}
}

// Regresión B.3: cuando no hay verifier criptográfico cableado, las
// attestations deben persistirse con verified=false y un verification_error
// explícito ("verifier_not_configured"). Antes, el flag no existía y el
// caller no podía distinguir attestations verificadas de simples claims.
func TestAttest_VerifiedFalseWhenVerifierNotConfigured(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	createRec := doReq(t, mux, http.MethodPost, "/v1/requests",
		`{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createRec.Code)
	}
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	resultRec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/result",
		`{"success":true,"result":{"output":"done"},"duration_ms":100}`)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on result, got %d", resultRec.Code)
	}

	attestBody := `{"status":"success","signature":"sig-xyz","attester":"pep:treasury"}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/attest", attestBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var attestResp requestdto.AttestResponse
	decJSON(t, rec, &attestResp)

	if attestResp.Verified {
		t.Fatal("attestation must NOT be marked verified when no verifier is configured")
	}
	if attestResp.VerificationError != "verifier_not_configured" {
		t.Fatalf("expected verification_error=verifier_not_configured, got %q", attestResp.VerificationError)
	}

	// El GET endpoint debe devolver el mismo flag.
	getRec := doReq(t, mux, http.MethodGet, "/v1/requests/"+submitResp.RequestID+"/attestation", "")
	var getResp requestdto.AttestResponse
	decJSON(t, getRec, &getResp)
	if getResp.Verified {
		t.Fatal("GET attestation must surface verified=false")
	}
	if getResp.VerificationError != "verifier_not_configured" {
		t.Fatalf("GET expected verification_error=verifier_not_configured, got %q", getResp.VerificationError)
	}
}

func TestAttestNotExecuted(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	// Crear request allowed (sin reportar resultado — status "allowed", no "executed")
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests",
		`{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, obtuvo %d: %s", createRec.Code, createRec.Body.String())
	}
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)

	// Intentar attestar sin haber ejecutado
	attestBody := `{"status":"success","signature":"sig","attester":"pep:test"}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/attest", attestBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperaba 409, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "CONFLICT" {
		t.Fatalf("esperaba code CONFLICT, obtuvo %s", errResp.Code)
	}
}

func TestAttestValidation(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	// Crear y ejecutar una request para tener un ID válido
	createRec := doReq(t, mux, http.MethodPost, "/v1/requests",
		`{"requester_type":"agent","requester_id":"bot","action_type":"alert.escalate"}`)
	var submitResp requestdto.SubmitResponse
	decJSON(t, createRec, &submitResp)
	doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/result",
		`{"success":true,"result":{"ok":true}}`)

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"json invalido", `{bad`, http.StatusBadRequest},
		{"falta status", `{"signature":"sig","attester":"pep"}`, http.StatusBadRequest},
		{"falta signature", `{"status":"success","attester":"pep"}`, http.StatusBadRequest},
		{"falta attester", `{"status":"success","signature":"sig"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doReq(t, mux, http.MethodPost, "/v1/requests/"+submitResp.RequestID+"/attest", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("esperaba %d, obtuvo %d: %s", tt.status, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAttestNotFound(t *testing.T) {
	t.Parallel()
	mux := setupFullMux()

	attestBody := `{"status":"success","signature":"sig","attester":"pep:test"}`
	rec := doReq(t, mux, http.MethodPost, "/v1/requests/00000000-0000-0000-0000-000000000000/attest", attestBody)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperaba 404, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decJSON(t, rec, &errResp)
	if errResp.Code != "NOT_FOUND" {
		t.Fatalf("esperaba code NOT_FOUND, obtuvo %s", errResp.Code)
	}
}
