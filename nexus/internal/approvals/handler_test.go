package approvals

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	approvaldto "github.com/devpablocristo/nexus/internal/approvals/handler/dto"
	approvaldomain "github.com/devpablocristo/nexus/internal/approvals/usecases/domain"
	"github.com/devpablocristo/nexus/internal/callbacks"
	"github.com/devpablocristo/nexus/internal/orgctx"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

const testApprovalOrgID = "org-test-001"

// --- Fakes ---

// fakeApprovalRepo simula el repositorio de approvals en memoria.
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
		return approvaldomain.Approval{}, ErrNotFound
	}
	return a, nil
}

func (r *fakeApprovalRepo) GetByRequestID(_ context.Context, _ uuid.UUID) (*approvaldomain.Approval, error) {
	return nil, nil
}

func (r *fakeApprovalRepo) ListPending(_ context.Context, limit int, orgID *string, allowAll bool) ([]approvaldomain.Approval, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []approvaldomain.Approval
	for _, a := range r.byID {
		if a.Status != approvaldomain.ApprovalStatusPending {
			continue
		}
		if !allowAll {
			if orgID != nil {
				if a.OrgID == nil || *a.OrgID != *orgID {
					continue
				}
			} else {
				if a.OrgID != nil {
					continue
				}
			}
		}
		out = append(out, a)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeApprovalRepo) Update(_ context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[a.ID]; !ok {
		return approvaldomain.Approval{}, ErrNotFound
	}
	r.byID[a.ID] = a
	return a, nil
}

// fakeRequestUpdater simula el repositorio de requests para las operaciones de approve/reject.
type fakeRequestUpdater struct {
	mu       sync.RWMutex
	requests map[uuid.UUID]requestdomain.Request
}

func newFakeRequestUpdater() *fakeRequestUpdater {
	return &fakeRequestUpdater{requests: make(map[uuid.UUID]requestdomain.Request)}
}

func (s *fakeRequestUpdater) GetByID(_ context.Context, id uuid.UUID) (requestdomain.Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.requests[id], nil
}

func (s *fakeRequestUpdater) Update(_ context.Context, r requestdomain.Request) (requestdomain.Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[r.ID] = r
	return r, nil
}

// auditEvent registra un evento emitido al audit sink.
type auditEvent struct {
	RequestID uuid.UUID
	EventType string
	ActorType string
	ActorID   string
	Summary   string
	Data      map[string]any
}

// fakeAuditSink captura eventos de auditoría para verificar que se emiten correctamente.
type fakeAuditSink struct {
	mu     sync.Mutex
	events []auditEvent
}

func newFakeAuditSink() *fakeAuditSink {
	return &fakeAuditSink{}
}

func (s *fakeAuditSink) AppendEvent(_ context.Context, requestID uuid.UUID, eventType, actorType, actorID, summary string, data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, auditEvent{
		RequestID: requestID,
		EventType: eventType,
		ActorType: actorType,
		ActorID:   actorID,
		Summary:   summary,
		Data:      data,
	})
	return nil
}

func (s *fakeAuditSink) getEvents() []auditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]auditEvent, len(s.events))
	copy(cp, s.events)
	return cp
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

// --- Helpers ---

// setupMux crea un mux con el handler de approvals sin audit sink.
func setupMux() (*http.ServeMux, *fakeApprovalRepo, *fakeRequestUpdater) {
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)
	return mux, repo, reqUpdater
}

// setupMuxWithAudit crea un mux con el handler de approvals y audit sink.
func setupMuxWithAudit() (*http.ServeMux, *fakeApprovalRepo, *fakeRequestUpdater, *fakeAuditSink) {
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	sink := newFakeAuditSink()
	uc := NewUsecases(repo, reqUpdater).WithAuditSink(sink)
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)
	return mux, repo, reqUpdater, sink
}

// seedApproval inserta una approval pendiente con su request asociada y retorna el ID.
func seedApproval(t *testing.T, repo *fakeApprovalRepo, reqUpdater *fakeRequestUpdater) uuid.UUID {
	t.Helper()
	return seedApprovalForOrg(t, repo, reqUpdater, testApprovalOrgID)
}

// seedApprovalForOrg es la variante de seedApproval con org explícito.
func seedApprovalForOrg(t *testing.T, repo *fakeApprovalRepo, reqUpdater *fakeRequestUpdater, orgID string) uuid.UUID {
	t.Helper()
	requestID := uuid.New()
	reqUpdater.mu.Lock()
	reqUpdater.requests[requestID] = requestdomain.Request{ID: requestID, Status: requestdomain.StatusPendingApproval}
	reqUpdater.mu.Unlock()
	a := approvaldomain.Approval{
		ID:        uuid.New(),
		OrgID:     &orgID,
		RequestID: requestID,
		Status:    approvaldomain.ApprovalStatusPending,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	if _, err := repo.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// doReq ejecuta una petición HTTP contra el mux y retorna el recorder.
func doReq(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, r))
	return rec
}

// --- Tests: ListPending ---

func TestListPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		seedCount int
		wantCode  int
		wantLen   int
	}{
		{
			name:      "lista vacía retorna 200 con array vacío",
			seedCount: 0,
			wantCode:  http.StatusOK,
			wantLen:   0,
		},
		{
			name:      "una approval pendiente",
			seedCount: 1,
			wantCode:  http.StatusOK,
			wantLen:   1,
		},
		{
			name:      "múltiples approvals pendientes",
			seedCount: 3,
			wantCode:  http.StatusOK,
			wantLen:   3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux, repo, reqUpdater := setupMux()
			for range tc.seedCount {
				seedApproval(t, repo, reqUpdater)
			}
			rec := doReq(t, mux, http.MethodGet, "/v1/approvals/pending", "")
			if rec.Code != tc.wantCode {
				t.Fatalf("código esperado %d, obtenido %d", tc.wantCode, rec.Code)
			}
			var resp struct {
				Data []approvaldto.ApprovalResponse `json:"data"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("error decodificando respuesta: %v", err)
			}
			if len(resp.Data) != tc.wantLen {
				t.Fatalf("se esperaban %d approvals, se obtuvieron %d", tc.wantLen, len(resp.Data))
			}
			for _, item := range resp.Data {
				if item.OrgID != testApprovalOrgID {
					t.Fatalf("se esperaba org_id %q, se obtuvo %q", testApprovalOrgID, item.OrgID)
				}
			}
		})
	}
}

// TestListPendingExcludesNonPending verifica que ListPending no incluye approvals decididas.
func TestListPendingExcludesNonPending(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()

	// Crear una pendiente y una ya aprobada
	seedApproval(t, repo, reqUpdater)

	approvedID := uuid.New()
	now := time.Now()
	repo.mu.Lock()
	repo.byID[approvedID] = approvaldomain.Approval{
		ID:        approvedID,
		RequestID: uuid.New(),
		Status:    approvaldomain.ApprovalStatusApproved,
		DecidedBy: "admin",
		DecidedAt: &now,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	repo.mu.Unlock()

	rec := doReq(t, mux, http.MethodGet, "/v1/approvals/pending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d", rec.Code)
	}
	var resp struct {
		Data []approvaldto.ApprovalResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("error decodificando respuesta: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("se esperaba 1 approval pendiente, se obtuvieron %d", len(resp.Data))
	}
	if resp.Data[0].OrgID != testApprovalOrgID {
		t.Fatalf("se esperaba org_id %q, se obtuvo %q", testApprovalOrgID, resp.Data[0].OrgID)
	}
}

// decodePendingList decodifica el body {"data": [...]} de listPending.
func decodePendingList(t *testing.T, rec *httptest.ResponseRecorder) []approvaldto.ApprovalResponse {
	t.Helper()
	var resp struct {
		Data []approvaldto.ApprovalResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("error decodificando respuesta: %v", err)
	}
	return resp.Data
}

// TestListPendingHonorsLimit verifica que ?limit= acota la cantidad de rows.
func TestListPendingHonorsLimit(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	for range 5 {
		seedApproval(t, repo, reqUpdater)
	}

	rec := doReq(t, mux, http.MethodGet, "/v1/approvals/pending?limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodePendingList(t, rec); len(got) != 2 {
		t.Fatalf("se esperaban 2 approvals con limit=2, se obtuvieron %d", len(got))
	}
}

// TestListLimitParsing verifica default, techo y valores inválidos de ?limit=.
func TestListLimitParsing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want int
	}{
		{"", defaultListLimit},
		{"abc", defaultListLimit},
		{"0", defaultListLimit},
		{"-5", defaultListLimit},
		{"2", 2},
		{"200", maxListLimit},
		{"500", maxListLimit},
	}
	for _, tc := range tests {
		if got := listLimit(tc.raw); got != tc.want {
			t.Errorf("listLimit(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

// TestListPendingFilterByRequestID verifica que ?request_id= retorna solo las
// approvals de esa request.
func TestListPendingFilterByRequestID(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	targetApprovalID := seedApproval(t, repo, reqUpdater)
	seedApproval(t, repo, reqUpdater)
	seedApproval(t, repo, reqUpdater)

	target, err := repo.GetByID(context.Background(), targetApprovalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}

	rec := doReq(t, mux, http.MethodGet, "/v1/approvals/pending?request_id="+target.RequestID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	got := decodePendingList(t, rec)
	if len(got) != 1 {
		t.Fatalf("se esperaba 1 approval para request_id, se obtuvieron %d", len(got))
	}
	if got[0].RequestID != target.RequestID.String() {
		t.Fatalf("request_id = %q, want %q", got[0].RequestID, target.RequestID.String())
	}
}

// TestListPendingInvalidRequestID verifica que un request_id no-UUID es 400.
func TestListPendingInvalidRequestID(t *testing.T) {
	t.Parallel()
	mux, _, _ := setupMux()

	rec := doReq(t, mux, http.MethodGet, "/v1/approvals/pending?request_id=not-a-uuid", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("código esperado 400, obtenido %d: %s", rec.Code, rec.Body.String())
	}
}

// pendingReqWithPrincipal arma un GET /v1/approvals/pending con el org
// solicitado preservado en orgctx (como hace el middleware de wire ANTES de
// WithPrincipal) y el principal indicado.
func pendingReqWithPrincipal(requestedOrg string, principal *authn.Principal) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/approvals/pending", nil)
	if requestedOrg != "" {
		req = req.WithContext(orgctx.WithRequested(req.Context(), requestedOrg))
	}
	return identityhttp.WithPrincipal(req, principal, principal.AuthMethod)
}

// TestListPendingCrossOrgNarrowedByRequestedOrg verifica que un principal
// cross_org con X-Org-ID inbound (preservado en orgctx) ve SOLO ese org en
// vez de allowAll.
func TestListPendingCrossOrgNarrowedByRequestedOrg(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	seedApprovalForOrg(t, repo, reqUpdater, "acme")
	seedApprovalForOrg(t, repo, reqUpdater, "globex")

	req := pendingReqWithPrincipal("acme", &authn.Principal{
		Actor:      "pymes-service",
		Scopes:     []string{scopeNexusApprovalsDecide, scopeNexusCrossOrg},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	got := decodePendingList(t, rec)
	if len(got) != 1 {
		t.Fatalf("se esperaba 1 approval (solo acme), se obtuvieron %d", len(got))
	}
	if got[0].OrgID != "acme" {
		t.Fatalf("org_id = %q, want acme", got[0].OrgID)
	}
}

// TestListPendingCrossOrgWithoutRequestedKeepsAllowAll verifica compat: un
// principal cross_org sin org solicitado ni bound sigue viendo todos los orgs.
func TestListPendingCrossOrgWithoutRequestedKeepsAllowAll(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	seedApprovalForOrg(t, repo, reqUpdater, "acme")
	seedApprovalForOrg(t, repo, reqUpdater, "globex")

	req := pendingReqWithPrincipal("", &authn.Principal{
		Actor:      "pymes-service",
		Scopes:     []string{scopeNexusApprovalsDecide, scopeNexusCrossOrg},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodePendingList(t, rec); len(got) != 2 {
		t.Fatalf("se esperaban 2 approvals (allowAll), se obtuvieron %d", len(got))
	}
}

// TestListPendingNonCrossOrgIgnoresRequestedOrg verifica que un principal SIN
// cross_org no puede escaparse de su org bound vía X-Org-ID inbound.
func TestListPendingNonCrossOrgIgnoresRequestedOrg(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	seedApprovalForOrg(t, repo, reqUpdater, "acme")
	seedApprovalForOrg(t, repo, reqUpdater, "globex")

	req := pendingReqWithPrincipal("globex", &authn.Principal{
		OrgID:      "acme",
		Actor:      "acme-service",
		Scopes:     []string{scopeNexusApprovalsDecide},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	got := decodePendingList(t, rec)
	if len(got) != 1 {
		t.Fatalf("se esperaba 1 approval (solo acme, su org bound), se obtuvieron %d", len(got))
	}
	if got[0].OrgID != "acme" {
		t.Fatalf("org_id = %q, want acme (el org del principal, no el solicitado)", got[0].OrgID)
	}
}

// --- Tests: Approve ---

func TestApprove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		body     string
		seedFn   func(*testing.T, *http.ServeMux, *fakeApprovalRepo, *fakeRequestUpdater) string
		wantCode int
		wantBody string
	}{
		{
			name: "happy path — aprobación exitosa",
			body: `{"decided_by":"admin","note":"ok"}`,
			seedFn: func(t *testing.T, _ *http.ServeMux, repo *fakeApprovalRepo, ru *fakeRequestUpdater) string {
				return seedApproval(t, repo, ru).String()
			},
			wantCode: http.StatusOK,
			wantBody: "approved",
		},
		{
			name:     "id inválido retorna 400",
			path:     "/v1/approvals/not-a-uuid/approve",
			body:     `{"decided_by":"admin"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "approval no encontrada retorna 404",
			path:     "/v1/approvals/" + uuid.New().String() + "/approve",
			body:     `{"decided_by":"admin"}`,
			wantCode: http.StatusNotFound,
		},
		{
			name: "json inválido retorna 400",
			seedFn: func(t *testing.T, _ *http.ServeMux, repo *fakeApprovalRepo, ru *fakeRequestUpdater) string {
				return seedApproval(t, repo, ru).String()
			},
			body:     `{invalid json`,
			wantCode: http.StatusBadRequest,
		},
		{
			name: "aprobar una ya aprobada retorna 409 conflict",
			seedFn: func(t *testing.T, mux *http.ServeMux, repo *fakeApprovalRepo, ru *fakeRequestUpdater) string {
				id := seedApproval(t, repo, ru)
				// Aprobar primero
				rec := doReq(t, mux, http.MethodPost, "/v1/approvals/"+id.String()+"/approve", `{"decided_by":"admin","note":"first"}`)
				if rec.Code != http.StatusOK {
					t.Fatalf("setup: approve esperaba 200, obtuvo %d", rec.Code)
				}
				return id.String()
			},
			body:     `{"decided_by":"admin","note":"second"}`,
			wantCode: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux, repo, reqUpdater := setupMux()
			path := tc.path
			if tc.seedFn != nil {
				id := tc.seedFn(t, mux, repo, reqUpdater)
				if path == "" {
					path = "/v1/approvals/" + id + "/approve"
				}
			}
			rec := doReq(t, mux, http.MethodPost, path, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("código esperado %d, obtenido %d: %s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("respuesta esperada contener %q, obtenido: %s", tc.wantBody, rec.Body.String())
			}
		})
	}
}

// TestApproveUpdatesRequestStatus verifica que al aprobar, la request asociada cambia a "approved".
func TestApproveUpdatesRequestStatus(t *testing.T) {
	t.Parallel()
	_, repo, reqUpdater := setupMux()

	uc := NewUsecases(repo, reqUpdater)
	approvalID := seedApproval(t, repo, reqUpdater)

	if err := uc.Approve(context.Background(), approvalID, "admin", "ok"); err != nil {
		t.Fatalf("approve falló: %v", err)
	}

	// Verificar que la approval cambió de status
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.Status != approvaldomain.ApprovalStatusApproved {
		t.Fatalf("status esperado %q, obtenido %q", approvaldomain.ApprovalStatusApproved, a.Status)
	}
	if a.DecidedBy != "admin" {
		t.Fatalf("decided_by esperado %q, obtenido %q", "admin", a.DecidedBy)
	}
	if a.DecidedAt == nil {
		t.Fatal("decided_at no debería ser nil")
	}

	// Verificar que la request cambió a approved
	req, err := reqUpdater.GetByID(context.Background(), a.RequestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if req.Status != requestdomain.StatusApproved {
		t.Fatalf("status de request esperado %q, obtenido %q", requestdomain.StatusApproved, req.Status)
	}
}

func TestApprovePublishesResolvedCallback(t *testing.T) {
	t.Parallel()

	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	publisher := &fakeApprovalCallbackPublisher{}
	orgID := "00000000-0000-0000-0000-000000000001"
	requestID := uuid.New()
	reqUpdater.requests[requestID] = requestdomain.Request{
		ID:     requestID,
		OrgID:  &orgID,
		Status: requestdomain.StatusPendingApproval,
	}
	approvalID := uuid.New()
	repo.byID[approvalID] = approvaldomain.Approval{
		ID:        approvalID,
		OrgID:     &orgID,
		RequestID: requestID,
		Status:    approvaldomain.ApprovalStatusPending,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now().UTC(),
	}
	uc := NewUsecases(repo, reqUpdater).WithApprovalCallbacks(publisher)

	if err := uc.Approve(context.Background(), approvalID, "admin", "ok"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}

	events := publisher.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 callback event, got %d", len(events))
	}
	if events[0].Event != callbacks.EventApprovalResolved {
		t.Fatalf("expected resolved event, got %s", events[0].Event)
	}
	if events[0].Decision != string(approvaldomain.ApprovalStatusApproved) {
		t.Fatalf("expected approved decision, got %s", events[0].Decision)
	}
	if events[0].OrgID != orgID {
		t.Fatalf("expected org_id %s, got %s", orgID, events[0].OrgID)
	}
}

// --- Tests: Reject ---

func TestReject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		body     string
		seedFn   func(*testing.T, *http.ServeMux, *fakeApprovalRepo, *fakeRequestUpdater) string
		wantCode int
		wantBody string
	}{
		{
			name: "happy path — rechazo exitoso",
			body: `{"decided_by":"admin","note":"no cumple requisitos"}`,
			seedFn: func(t *testing.T, _ *http.ServeMux, repo *fakeApprovalRepo, ru *fakeRequestUpdater) string {
				return seedApproval(t, repo, ru).String()
			},
			wantCode: http.StatusOK,
			wantBody: "rejected",
		},
		{
			name:     "id inválido retorna 400",
			path:     "/v1/approvals/bad-id/reject",
			body:     `{"decided_by":"admin"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "approval no encontrada retorna 404",
			path:     "/v1/approvals/" + uuid.New().String() + "/reject",
			body:     `{"decided_by":"admin"}`,
			wantCode: http.StatusNotFound,
		},
		{
			name: "json inválido retorna 400",
			seedFn: func(t *testing.T, _ *http.ServeMux, repo *fakeApprovalRepo, ru *fakeRequestUpdater) string {
				return seedApproval(t, repo, ru).String()
			},
			body:     `not json`,
			wantCode: http.StatusBadRequest,
		},
		{
			name: "rechazar una ya rechazada retorna 409 conflict",
			seedFn: func(t *testing.T, mux *http.ServeMux, repo *fakeApprovalRepo, ru *fakeRequestUpdater) string {
				id := seedApproval(t, repo, ru)
				rec := doReq(t, mux, http.MethodPost, "/v1/approvals/"+id.String()+"/reject", `{"decided_by":"admin","note":"first"}`)
				if rec.Code != http.StatusOK {
					t.Fatalf("setup: reject esperaba 200, obtuvo %d", rec.Code)
				}
				return id.String()
			},
			body:     `{"decided_by":"admin","note":"second"}`,
			wantCode: http.StatusConflict,
		},
		{
			name: "rechazar una ya aprobada retorna 409 conflict",
			seedFn: func(t *testing.T, mux *http.ServeMux, repo *fakeApprovalRepo, ru *fakeRequestUpdater) string {
				id := seedApproval(t, repo, ru)
				rec := doReq(t, mux, http.MethodPost, "/v1/approvals/"+id.String()+"/approve", `{"decided_by":"admin","note":"approved"}`)
				if rec.Code != http.StatusOK {
					t.Fatalf("setup: approve esperaba 200, obtuvo %d", rec.Code)
				}
				return id.String()
			},
			body:     `{"decided_by":"admin","note":"reject after approve"}`,
			wantCode: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux, repo, reqUpdater := setupMux()
			path := tc.path
			if tc.seedFn != nil {
				id := tc.seedFn(t, mux, repo, reqUpdater)
				if path == "" {
					path = "/v1/approvals/" + id + "/reject"
				}
			}
			rec := doReq(t, mux, http.MethodPost, path, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("código esperado %d, obtenido %d: %s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("respuesta esperada contener %q, obtenido: %s", tc.wantBody, rec.Body.String())
			}
		})
	}
}

// TestRejectUpdatesRequestStatus verifica que al rechazar, la request asociada cambia a "rejected".
func TestRejectUpdatesRequestStatus(t *testing.T) {
	t.Parallel()
	_, repo, reqUpdater := setupMux()

	uc := NewUsecases(repo, reqUpdater)
	approvalID := seedApproval(t, repo, reqUpdater)

	if err := uc.Reject(context.Background(), approvalID, "admin", "no cumple"); err != nil {
		t.Fatalf("reject falló: %v", err)
	}

	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.Status != approvaldomain.ApprovalStatusRejected {
		t.Fatalf("status esperado %q, obtenido %q", approvaldomain.ApprovalStatusRejected, a.Status)
	}
	if a.DecidedBy != "admin" {
		t.Fatalf("decided_by esperado %q, obtenido %q", "admin", a.DecidedBy)
	}

	req, err := reqUpdater.GetByID(context.Background(), a.RequestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if req.Status != requestdomain.StatusRejected {
		t.Fatalf("status de request esperado %q, obtenido %q", requestdomain.StatusRejected, req.Status)
	}
}

// --- Tests: Audit Sink ---

// TestApproveEmitsAuditEvent verifica que Approve emite un evento al audit sink.
func TestApproveEmitsAuditEvent(t *testing.T) {
	t.Parallel()
	_, repo, reqUpdater, sink := setupMuxWithAudit()

	uc := NewUsecases(repo, reqUpdater).WithAuditSink(sink)
	approvalID := seedApproval(t, repo, reqUpdater)

	if err := uc.Approve(context.Background(), approvalID, "auditor", "aprobado con nota"); err != nil {
		t.Fatalf("approve falló: %v", err)
	}

	events := sink.getEvents()
	if len(events) != 1 {
		t.Fatalf("se esperaba 1 evento de audit, se obtuvieron %d", len(events))
	}
	ev := events[0]
	if ev.EventType != "approved" {
		t.Fatalf("tipo de evento esperado %q, obtenido %q", "approved", ev.EventType)
	}
	if ev.ActorType != "human" {
		t.Fatalf("actor_type esperado %q, obtenido %q", "human", ev.ActorType)
	}
	if ev.ActorID != "auditor" {
		t.Fatalf("actor_id esperado %q, obtenido %q", "auditor", ev.ActorID)
	}
	if !strings.Contains(ev.Summary, "aprobado con nota") {
		t.Fatalf("summary debería contener la nota, obtenido: %q", ev.Summary)
	}
}

// TestRejectEmitsAuditEvent verifica que Reject emite un evento al audit sink.
func TestRejectEmitsAuditEvent(t *testing.T) {
	t.Parallel()
	_, repo, reqUpdater, sink := setupMuxWithAudit()

	uc := NewUsecases(repo, reqUpdater).WithAuditSink(sink)
	approvalID := seedApproval(t, repo, reqUpdater)

	if err := uc.Reject(context.Background(), approvalID, "reviewer", "rechazado por policy"); err != nil {
		t.Fatalf("reject falló: %v", err)
	}

	events := sink.getEvents()
	if len(events) != 1 {
		t.Fatalf("se esperaba 1 evento de audit, se obtuvieron %d", len(events))
	}
	ev := events[0]
	if ev.EventType != "rejected" {
		t.Fatalf("tipo de evento esperado %q, obtenido %q", "rejected", ev.EventType)
	}
	if ev.ActorID != "reviewer" {
		t.Fatalf("actor_id esperado %q, obtenido %q", "reviewer", ev.ActorID)
	}
}

// TestNoAuditSinkDoesNotPanic verifica que sin audit sink configurado, la operación no falla.
func TestNoAuditSinkDoesNotPanic(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	// Sin audit sink
	uc := NewUsecases(repo, reqUpdater)
	approvalID := seedApproval(t, repo, reqUpdater)

	if err := uc.Approve(context.Background(), approvalID, "admin", "ok"); err != nil {
		t.Fatalf("approve sin audit sink falló: %v", err)
	}
}

// --- Tests: Approve vía HTTP con audit ---

// TestApproveHTTPEmitsAudit verifica el flujo completo HTTP -> usecase -> audit.
func TestApproveHTTPEmitsAudit(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater, sink := setupMuxWithAudit()
	approvalID := seedApproval(t, repo, reqUpdater)

	rec := doReq(t, mux, http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", `{"decided_by":"admin","note":"approved via HTTP"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}

	events := sink.getEvents()
	if len(events) != 1 {
		t.Fatalf("se esperaba 1 evento de audit, se obtuvieron %d", len(events))
	}
}

func TestApproveHTTPUsesAuthenticatedPrincipalOverSpoofedBody(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater, sink := setupMuxWithAudit()
	approvalID := seedApproval(t, repo, reqUpdater)

	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", strings.NewReader(`{"decided_by":"spoofed-body","note":"approved via HTTP"}`))
	req = identityhttp.WithPrincipal(req, &authn.Principal{
		OrgID:      testApprovalOrgID,
		Actor:      "authenticated-reviewer",
		Scopes:     []string{scopeNexusApprovalsDecide},
		AuthMethod: "jwt",
	}, "jwt")
	req.Header.Set("X-User-ID", "spoofed-header")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.DecidedBy != "authenticated-reviewer" {
		t.Fatalf("decided_by = %q, want authenticated principal", a.DecidedBy)
	}
	events := sink.getEvents()
	if len(events) != 1 || events[0].ActorID != "authenticated-reviewer" {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

// TestApproveHTTPServicePrincipalUsesDelegatedActor verifica que un service
// principal (ej. ponti-backend) con X-On-Behalf-Of registra la decisión a
// nombre del usuario delegado, no del principal del servicio.
func TestApproveHTTPServicePrincipalUsesDelegatedActor(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater, sink := setupMuxWithAudit()
	approvalID := seedApproval(t, repo, reqUpdater)

	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", strings.NewReader(`{"note":"approved on behalf"}`))
	req = identityhttp.WithPrincipal(req, &authn.Principal{
		OrgID:      testApprovalOrgID,
		Actor:      "ponti-backend",
		Scopes:     []string{scopeNexusApprovalsDecide},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	}, "api_key")
	req.Header.Set("X-On-Behalf-Of", "user:ponti-operator")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.DecidedBy != "user:ponti-operator" {
		t.Fatalf("decided_by = %q, want delegated actor", a.DecidedBy)
	}
	events := sink.getEvents()
	if len(events) != 1 || events[0].ActorID != "user:ponti-operator" {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

// TestRejectHTTPServicePrincipalUsesDecidedByBody verifica la delegación vía
// body.decided_by para reject cuando no hay header X-On-Behalf-Of.
func TestRejectHTTPServicePrincipalUsesDecidedByBody(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	approvalID := seedApproval(t, repo, reqUpdater)

	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/reject", strings.NewReader(`{"decided_by":"user:ponti-operator","note":"rejected on behalf"}`))
	req = identityhttp.WithPrincipal(req, &authn.Principal{
		OrgID:      testApprovalOrgID,
		Actor:      "ponti-backend",
		Scopes:     []string{scopeNexusApprovalsDecide},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	}, "api_key")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.DecidedBy != "user:ponti-operator" {
		t.Fatalf("decided_by = %q, want delegated actor from body", a.DecidedBy)
	}
}

// TestApproveHTTPHumanIgnoresOnBehalfOf verifica que un humano autenticado no
// puede delegar: su propio actor gana sobre X-On-Behalf-Of y body.decided_by.
func TestApproveHTTPHumanIgnoresOnBehalfOf(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	approvalID := seedApproval(t, repo, reqUpdater)

	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", strings.NewReader(`{"decided_by":"spoofed-body","note":"approved"}`))
	req = identityhttp.WithPrincipal(req, &authn.Principal{
		OrgID:      testApprovalOrgID,
		Actor:      "authenticated-reviewer",
		Scopes:     []string{scopeNexusApprovalsDecide},
		AuthMethod: "jwt",
	}, "jwt")
	req.Header.Set("X-On-Behalf-Of", "spoofed-header")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.DecidedBy != "authenticated-reviewer" {
		t.Fatalf("decided_by = %q, want authenticated human actor", a.DecidedBy)
	}
}

// TestApproveHTTPProductJWTServicePrincipalCannotDelegate es el guard de
// regresión del rollout de product JWTs (Ola B): la delegación de decided_by
// exige AuthMethod "api_key" (trusted boundary del producto). Un principal
// autenticado con product JWT (AuthMethod "product_jwt") con
// service_principal:true NO debe poder forjar decided_by vía X-On-Behalf-Of
// ni body.decided_by — si los product JWTs necesitan delegación legítima más
// adelante, requiere un diseño explícito, no un bypass silencioso.
func TestApproveHTTPProductJWTServicePrincipalCannotDelegate(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	approvalID := seedApproval(t, repo, reqUpdater)

	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", strings.NewReader(`{"decided_by":"user:forged-operator","note":"approved via product jwt"}`))
	req = identityhttp.WithPrincipal(req, &authn.Principal{
		OrgID:      testApprovalOrgID,
		Actor:      "ponti-backend",
		Scopes:     []string{scopeNexusApprovalsDecide},
		Claims:     map[string]any{"service_principal": true, "product_surface": "ponti"},
		AuthMethod: "product_jwt",
	}, "product_jwt")
	req.Header.Set("X-On-Behalf-Of", "user:forged-operator")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.DecidedBy != "ponti-backend" {
		t.Fatalf("decided_by = %q: la delegación vía product JWT debe rechazarse y registrar el actor del principal", a.DecidedBy)
	}
}

// TestApproveHTTPServicePrincipalWithoutDelegationFallsBack verifica compat:
// service principal sin X-On-Behalf-Of ni decided_by usa su propio actor.
func TestApproveHTTPServicePrincipalWithoutDelegationFallsBack(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater := setupMux()
	approvalID := seedApproval(t, repo, reqUpdater)

	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", strings.NewReader(`{"note":"approved by service"}`))
	req = identityhttp.WithPrincipal(req, &authn.Principal{
		OrgID:      testApprovalOrgID,
		Actor:      "ponti-backend",
		Scopes:     []string{scopeNexusApprovalsDecide},
		Claims:     map[string]any{"service_principal": true},
		AuthMethod: "api_key",
	}, "api_key")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.DecidedBy != "ponti-backend" {
		t.Fatalf("decided_by = %q, want service principal actor", a.DecidedBy)
	}
}

// TestApproveHTTPBFFHumanTokenCannotDelegate verifica el shape REAL del token
// del BFF de consola: internal JWT con service_principal:true pero actor
// humano (actor_type=human), AuthMethod "internal_jwt". La delegación se
// rechaza: decided_by es el actor autenticado, no el X-On-Behalf-Of ni el
// body.decided_by forjados (no-repudio + separation of duties).
func TestApproveHTTPBFFHumanTokenCannotDelegate(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater, sink := setupMuxWithAudit()
	approvalID := seedApproval(t, repo, reqUpdater)

	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", strings.NewReader(`{"decided_by":"forged-body-approver","note":"approved"}`))
	req = identityhttp.WithPrincipal(req, &authn.Principal{
		OrgID:  testApprovalOrgID,
		Actor:  "console-user@axis",
		Scopes: []string{scopeNexusApprovalsDecide},
		Claims: map[string]any{
			"service_principal": true,
			"actor_type":        "human",
			"on_behalf_of":      "console-user@axis",
			"product_surface":   "axis-console",
		},
		AuthMethod: "internal_jwt",
	}, "internal_jwt")
	req.Header.Set("X-On-Behalf-Of", "forged-header-approver")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.DecidedBy != "console-user@axis" {
		t.Fatalf("decided_by = %q, want authenticated console actor", a.DecidedBy)
	}
	events := sink.getEvents()
	if len(events) != 1 || events[0].ActorID != "console-user@axis" {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

// TestRejectHTTPEmitsAudit verifica el flujo completo HTTP -> usecase -> audit para reject.
func TestRejectHTTPEmitsAudit(t *testing.T) {
	t.Parallel()
	mux, repo, reqUpdater, sink := setupMuxWithAudit()
	approvalID := seedApproval(t, repo, reqUpdater)

	rec := doReq(t, mux, http.MethodPost, "/v1/approvals/"+approvalID.String()+"/reject", `{"decided_by":"admin","note":"rejected via HTTP"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}

	events := sink.getEvents()
	if len(events) != 1 {
		t.Fatalf("se esperaba 1 evento de audit, se obtuvieron %d", len(events))
	}
}

// --- Tests: writeApprovalUsecaseError ---

// TestWriteApprovalUsecaseError verifica que los errores de dominio se mapean correctamente a HTTP.
func TestWriteApprovalUsecaseError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{
			name:     "ErrNotPending retorna 409",
			err:      ErrNotPending,
			wantCode: http.StatusConflict,
			wantMsg:  "approval is not pending",
		},
		{
			name:     "ErrNotFound retorna 404",
			err:      ErrNotFound,
			wantCode: http.StatusNotFound,
			wantMsg:  "approval not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeApprovalUsecaseError(rec, tc.err)
			if rec.Code != tc.wantCode {
				t.Fatalf("código esperado %d, obtenido %d", tc.wantCode, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Fatalf("respuesta esperada contener %q, obtenido: %s", tc.wantMsg, rec.Body.String())
			}
		})
	}
}

// --- Tests: Usecases directos ---

// TestUsecasesApproveNotFound verifica que Approve retorna error cuando la approval no existe.
func TestUsecasesApproveNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)

	err := uc.Approve(context.Background(), uuid.New(), "admin", "ok")
	if err == nil {
		t.Fatal("se esperaba error, se obtuvo nil")
	}
}

// TestUsecasesRejectNotFound verifica que Reject retorna error cuando la approval no existe.
func TestUsecasesRejectNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)

	err := uc.Reject(context.Background(), uuid.New(), "admin", "no")
	if err == nil {
		t.Fatal("se esperaba error, se obtuvo nil")
	}
}

// TestUsecasesApproveNotPending verifica que Approve retorna ErrNotPending si ya está decidida.
func TestUsecasesApproveNotPending(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)

	approvalID := seedApproval(t, repo, reqUpdater)

	// Aprobar primero
	if err := uc.Approve(context.Background(), approvalID, "admin", "ok"); err != nil {
		t.Fatalf("primer approve falló: %v", err)
	}

	// Intentar aprobar de nuevo
	err := uc.Approve(context.Background(), approvalID, "admin", "again")
	if err == nil {
		t.Fatal("se esperaba error, se obtuvo nil")
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("error esperado contener 'not pending', obtenido: %v", err)
	}
}

func TestUsecasesApproveExpired(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)

	approvalID := seedApproval(t, repo, reqUpdater)
	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	a.ExpiresAt = time.Now().Add(-time.Minute)
	if _, err := repo.Update(context.Background(), a); err != nil {
		t.Fatalf("update approval: %v", err)
	}

	err = uc.Approve(context.Background(), approvalID, "admin", "ok")
	if err == nil {
		t.Fatal("se esperaba error, se obtuvo nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("error esperado contener 'expired', obtenido: %v", err)
	}
}

func TestUsecasesApproveRequiresActor(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)

	approvalID := seedApproval(t, repo, reqUpdater)
	err := uc.Approve(context.Background(), approvalID, "  ", "ok")
	if err == nil {
		t.Fatal("se esperaba error, se obtuvo nil")
	}
	if !strings.Contains(err.Error(), "actor") {
		t.Fatalf("error esperado contener 'actor', obtenido: %v", err)
	}
}

// TestUsecasesRejectNotPending verifica que Reject retorna ErrNotPending si ya está decidida.
func TestUsecasesRejectNotPending(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)

	approvalID := seedApproval(t, repo, reqUpdater)

	// Rechazar primero
	if err := uc.Reject(context.Background(), approvalID, "admin", "no"); err != nil {
		t.Fatalf("primer reject falló: %v", err)
	}

	// Intentar rechazar de nuevo
	err := uc.Reject(context.Background(), approvalID, "admin", "again")
	if err == nil {
		t.Fatal("se esperaba error, se obtuvo nil")
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("error esperado contener 'not pending', obtenido: %v", err)
	}
}

// TestUsecasesListPending verifica que ListPending delega correctamente al repo.
func TestUsecasesListPending(t *testing.T) {
	t.Parallel()
	repo := newFakeApprovalRepo()
	reqUpdater := newFakeRequestUpdater()
	uc := NewUsecases(repo, reqUpdater)

	seedApproval(t, repo, reqUpdater)
	seedApproval(t, repo, reqUpdater)

	list, err := uc.ListPending(context.Background(), 10, nil, true)
	if err != nil {
		t.Fatalf("list pending falló: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("se esperaban 2 approvals, se obtuvieron %d", len(list))
	}
}

func TestApproveFallsBackToUserHeader(t *testing.T) {
	t.Parallel()

	mux, repo, reqUpdater := setupMux()
	approvalID := seedApproval(t, repo, reqUpdater)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/approvals/"+approvalID.String()+"/approve", strings.NewReader(`{"note":"approved from header"}`))
	req.Header.Set("X-User-ID", "header-user")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado %d, obtenido %d", http.StatusOK, rec.Code)
	}

	a, err := repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("error obteniendo approval: %v", err)
	}
	if a.DecidedBy != "header-user" {
		t.Fatalf("decided_by esperado %q, obtenido %q", "header-user", a.DecidedBy)
	}
}
