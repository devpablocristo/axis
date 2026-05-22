package approvals_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/nexus/governance/internal/approvals"
	approvaldto "github.com/devpablocristo/nexus/governance/internal/approvals/handler/dto"
	approvaldomain "github.com/devpablocristo/nexus/governance/internal/approvals/usecases/domain"
	requestdomain "github.com/devpablocristo/nexus/governance/internal/requests/usecases/domain"
)

const testApprovalOrgID = "org-test-001"

// --- Fakes ---

// fakeRepo simula el repositorio de approvals en memoria para tests.
type fakeRepo struct {
	mu   sync.RWMutex
	byID map[uuid.UUID]approvaldomain.Approval
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: make(map[uuid.UUID]approvaldomain.Approval)}
}

func (r *fakeRepo) Create(_ context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	r.byID[a.ID] = a
	return a, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (approvaldomain.Approval, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.byID[id]
	if !ok {
		return approvaldomain.Approval{}, approvals.ErrNotFound
	}
	return a, nil
}

func (r *fakeRepo) GetByRequestID(_ context.Context, _ uuid.UUID) (*approvaldomain.Approval, error) {
	return nil, nil
}

func (r *fakeRepo) ListPending(_ context.Context, limit int, orgID *string, allowAll bool) ([]approvaldomain.Approval, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []approvaldomain.Approval
	for _, a := range r.byID {
		if a.Status != approvaldomain.ApprovalStatusPending {
			continue
		}
		// Espejar el filtro SQL del repo Postgres.
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

func (r *fakeRepo) Update(_ context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[a.ID]; !ok {
		return approvaldomain.Approval{}, approvals.ErrNotFound
	}
	r.byID[a.ID] = a
	return a, nil
}

// fakeRequestUpdater simula el repositorio de requests.
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

// fakeAuditSink captura eventos de auditoría (no-op en la mayoría de tests).
type fakeAuditSink struct {
	mu     sync.Mutex
	events []auditEvent
}

type auditEvent struct {
	RequestID uuid.UUID
	EventType string
	ActorType string
	ActorID   string
	Summary   string
	Data      map[string]any
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

// --- Helpers ---

// testEnv agrupa las dependencias de un test.
type testEnv struct {
	mux        *http.ServeMux
	repo       *fakeRepo
	reqUpdater *fakeRequestUpdater
	audit      *fakeAuditSink
	uc         *approvals.Usecases
}

// newTestEnv crea un entorno de test completo con audit sink.
func newTestEnv() testEnv {
	repo := newFakeRepo()
	reqUpdater := newFakeRequestUpdater()
	sink := newFakeAuditSink()
	uc := approvals.NewUsecases(repo, reqUpdater).WithAuditSink(sink)
	mux := http.NewServeMux()
	approvals.NewHandler(uc).Register(mux)
	return testEnv{mux: mux, repo: repo, reqUpdater: reqUpdater, audit: sink, uc: uc}
}

// seedPendingApproval inserta una approval pendiente simple (requiere 1 aprobador).
func seedPendingApproval(t *testing.T, env testEnv) uuid.UUID {
	t.Helper()
	requestID := uuid.New()
	env.reqUpdater.mu.Lock()
	env.reqUpdater.requests[requestID] = requestdomain.Request{
		ID:     requestID,
		Status: requestdomain.StatusPendingApproval,
	}
	env.reqUpdater.mu.Unlock()
	orgID := testApprovalOrgID

	a := approvaldomain.Approval{
		ID:                uuid.New(),
		OrgID:             &orgID,
		RequestID:         requestID,
		Status:            approvaldomain.ApprovalStatusPending,
		RequiredApprovals: 1,
		ExpiresAt:         time.Now().Add(time.Hour),
		CreatedAt:         time.Now(),
	}
	if _, err := env.repo.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// seedBreakGlassApproval inserta una approval break-glass que requiere múltiples aprobadores.
func seedBreakGlassApproval(t *testing.T, env testEnv, requiredApprovals int) uuid.UUID {
	t.Helper()
	requestID := uuid.New()
	env.reqUpdater.mu.Lock()
	env.reqUpdater.requests[requestID] = requestdomain.Request{
		ID:     requestID,
		Status: requestdomain.StatusPendingApproval,
	}
	env.reqUpdater.mu.Unlock()

	a := approvaldomain.Approval{
		ID:                uuid.New(),
		RequestID:         requestID,
		Status:            approvaldomain.ApprovalStatusPending,
		BreakGlass:        true,
		RequiredApprovals: requiredApprovals,
		ExpiresAt:         time.Now().Add(time.Hour),
		CreatedAt:         time.Now(),
	}
	if _, err := env.repo.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// doRequest ejecuta una petición HTTP contra el mux y retorna el recorder.
func doRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, r))
	return rec
}

// --- Tests: Approve happy path ---

func TestApproveHappyPath(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedPendingApproval(t, env)

	// Aprobar vía HTTP
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"admin","note":"looks good"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}

	// Verificar que la respuesta contiene "approved"
	if !strings.Contains(rec.Body.String(), "approved") {
		t.Fatalf("respuesta debería contener 'approved', obtenido: %s", rec.Body.String())
	}

	// Verificar que la approval cambió a approved en el repo
	a, err := env.repo.GetByID(context.Background(), approvalID)
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
		t.Fatal("decided_at no debería ser nil después de aprobar")
	}
	if len(a.Decisions) != 1 {
		t.Fatalf("se esperaba 1 decisión, se obtuvieron %d", len(a.Decisions))
	}
	if a.Decisions[0].Action != "approve" {
		t.Fatalf("acción esperada 'approve', obtenida %q", a.Decisions[0].Action)
	}

	// Verificar que la request asociada cambió a approved
	req, err := env.reqUpdater.GetByID(context.Background(), a.RequestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if req.Status != requestdomain.StatusApproved {
		t.Fatalf("status de request esperado %q, obtenido %q", requestdomain.StatusApproved, req.Status)
	}
}

// --- Tests: Reject happy path ---

func TestRejectHappyPath(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedPendingApproval(t, env)

	// Rechazar vía HTTP
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/reject",
		`{"decided_by":"reviewer","note":"no cumple política"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "rejected") {
		t.Fatalf("respuesta debería contener 'rejected', obtenido: %s", rec.Body.String())
	}

	// Verificar que la approval cambió a rejected
	a, err := env.repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.Status != approvaldomain.ApprovalStatusRejected {
		t.Fatalf("status esperado %q, obtenido %q", approvaldomain.ApprovalStatusRejected, a.Status)
	}
	if a.DecidedBy != "reviewer" {
		t.Fatalf("decided_by esperado %q, obtenido %q", "reviewer", a.DecidedBy)
	}
	if a.DecidedAt == nil {
		t.Fatal("decided_at no debería ser nil después de rechazar")
	}
	if len(a.Decisions) != 1 {
		t.Fatalf("se esperaba 1 decisión, se obtuvieron %d", len(a.Decisions))
	}
	if a.Decisions[0].Action != "reject" {
		t.Fatalf("acción esperada 'reject', obtenida %q", a.Decisions[0].Action)
	}

	// Verificar que la request asociada cambió a rejected
	req, err := env.reqUpdater.GetByID(context.Background(), a.RequestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if req.Status != requestdomain.StatusRejected {
		t.Fatalf("status de request esperado %q, obtenido %q", requestdomain.StatusRejected, req.Status)
	}
}

// --- Tests: Break-glass multi-approver ---

func TestBreakGlassMultiApprover(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedBreakGlassApproval(t, env, 2)

	// Primera aprobación: parcial (1/2)
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-1","note":"primera aprobación"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("primera aprobación: código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}

	// Verificar que la approval sigue pendiente (parcial)
	a, err := env.repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval después de primera aprobación: %v", err)
	}
	if a.Status != approvaldomain.ApprovalStatusPending {
		t.Fatalf("después de 1/2 aprobaciones, status esperado %q, obtenido %q",
			approvaldomain.ApprovalStatusPending, a.Status)
	}
	if len(a.Decisions) != 1 {
		t.Fatalf("se esperaba 1 decisión parcial, se obtuvieron %d", len(a.Decisions))
	}

	// Segunda aprobación: finaliza (2/2)
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-2","note":"segunda aprobación"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("segunda aprobación: código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}

	// Verificar que la approval se finalizó como approved
	a, err = env.repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval después de segunda aprobación: %v", err)
	}
	if a.Status != approvaldomain.ApprovalStatusApproved {
		t.Fatalf("después de 2/2 aprobaciones, status esperado %q, obtenido %q",
			approvaldomain.ApprovalStatusApproved, a.Status)
	}
	if len(a.Decisions) != 2 {
		t.Fatalf("se esperaban 2 decisiones, se obtuvieron %d", len(a.Decisions))
	}

	// Verificar que la request cambió a approved
	req, err := env.reqUpdater.GetByID(context.Background(), a.RequestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if req.Status != requestdomain.StatusApproved {
		t.Fatalf("status de request esperado %q, obtenido %q", requestdomain.StatusApproved, req.Status)
	}

	// Verificar que el audit sink registró dos eventos: uno parcial y uno final
	events := env.audit.getEvents()
	if len(events) != 2 {
		t.Fatalf("se esperaban 2 eventos de audit, se obtuvieron %d", len(events))
	}
	// El primer evento es la aprobación parcial
	if !strings.Contains(events[0].Summary, "Partial") {
		t.Fatalf("primer evento debería ser parcial, summary: %q", events[0].Summary)
	}
	// El segundo evento es la aprobación final
	if events[1].EventType != "approved" {
		t.Fatalf("segundo evento debería ser 'approved', obtenido %q", events[1].EventType)
	}
}

// --- Tests: Break-glass reject cancela la cadena ---

func TestBreakGlassRejectCancelsChain(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedBreakGlassApproval(t, env, 3)

	// Primera aprobación parcial
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-1","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("primera aprobación: código esperado 200, obtenido %d", rec.Code)
	}

	// Un rechazo cancela todo, sin importar las aprobaciones previas
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/reject",
		`{"decided_by":"approver-2","note":"policy violation"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rechazo break-glass: código esperado 200, obtenido %d: %s", rec.Code, rec.Body.String())
	}

	// Verificar que la approval se marcó como rejected
	a, err := env.repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.Status != approvaldomain.ApprovalStatusRejected {
		t.Fatalf("status esperado %q, obtenido %q", approvaldomain.ApprovalStatusRejected, a.Status)
	}
	// Debería tener 2 decisiones: la aprobación parcial + el rechazo
	if len(a.Decisions) != 2 {
		t.Fatalf("se esperaban 2 decisiones, se obtuvieron %d", len(a.Decisions))
	}
	if a.Decisions[1].Action != "reject" {
		t.Fatalf("última decisión debería ser 'reject', obtenida %q", a.Decisions[1].Action)
	}

	// Verificar que la request cambió a rejected
	req, err := env.reqUpdater.GetByID(context.Background(), a.RequestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if req.Status != requestdomain.StatusRejected {
		t.Fatalf("status de request esperado %q, obtenido %q", requestdomain.StatusRejected, req.Status)
	}
}

// --- Tests: Duplicate approver ---

func TestBreakGlassDuplicateApprover(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedBreakGlassApproval(t, env, 2)

	// Primera aprobación por approver-1
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-1","note":"first"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("primera aprobación: código esperado 200, obtenido %d", rec.Code)
	}

	// Mismo approver intenta aprobar de nuevo: debe fallar con 409
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-1","note":"trying again"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("aprobación duplicada: código esperado 409, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already decided") {
		t.Fatalf("respuesta debería mencionar 'already decided', obtenido: %s", rec.Body.String())
	}

	// Verificar que la approval sigue pendiente con una sola decisión
	a, err := env.repo.GetByID(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if a.Status != approvaldomain.ApprovalStatusPending {
		t.Fatalf("status debería seguir pending, obtenido %q", a.Status)
	}
	if len(a.Decisions) != 1 {
		t.Fatalf("debería tener solo 1 decisión, se obtuvieron %d", len(a.Decisions))
	}
}

// TestBreakGlassDuplicateRejecter verifica que un mismo aprobador no puede rechazar dos veces.
func TestBreakGlassDuplicateRejecter(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedBreakGlassApproval(t, env, 2)

	// approver-1 aprueba
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-1","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("primera aprobación: código esperado 200, obtenido %d", rec.Code)
	}

	// approver-1 intenta rechazar (ya decidió): debe fallar con 409
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/reject",
		`{"decided_by":"approver-1","note":"changed my mind"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("rechazo duplicado: código esperado 409, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already decided") {
		t.Fatalf("respuesta debería mencionar 'already decided', obtenido: %s", rec.Body.String())
	}
}

// --- Tests: List pending ---

func TestListPendingReturnsOnlyPending(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	// Crear dos approvals pendientes
	seedPendingApproval(t, env)
	seedPendingApproval(t, env)

	// Crear una ya aprobada directamente en el repo
	approvedID := uuid.New()
	now := time.Now()
	env.repo.mu.Lock()
	env.repo.byID[approvedID] = approvaldomain.Approval{
		ID:        approvedID,
		RequestID: uuid.New(),
		Status:    approvaldomain.ApprovalStatusApproved,
		DecidedBy: "someone",
		DecidedAt: &now,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	env.repo.mu.Unlock()

	// Crear una rechazada
	rejectedID := uuid.New()
	env.repo.mu.Lock()
	env.repo.byID[rejectedID] = approvaldomain.Approval{
		ID:        rejectedID,
		RequestID: uuid.New(),
		Status:    approvaldomain.ApprovalStatusRejected,
		DecidedBy: "someone",
		DecidedAt: &now,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	env.repo.mu.Unlock()

	// Solo deberían retornar las 2 pendientes
	rec := doRequest(t, env.mux, http.MethodGet, "/v1/approvals/pending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d", rec.Code)
	}

	var resp struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("error decodificando respuesta: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("se esperaban 2 approvals pendientes, se obtuvieron %d", len(resp.Data))
	}
	for _, raw := range resp.Data {
		var item approvaldto.ApprovalResponse
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatalf("error decodificando approval: %v", err)
		}
		if item.OrgID != testApprovalOrgID {
			t.Fatalf("se esperaba org_id %q, se obtuvo %q", testApprovalOrgID, item.OrgID)
		}
	}
}

// Regresión B.2: cuando el caller pide pending de su org y existen approvals
// de OTRO org, el filtro de tenancy debe aplicarse en SQL (no post-filter).
// Si fuera post-filter, el LIMIT podría llenarse con rows del otro org y el
// caller vería menos de los suyos que en realidad existen.
func TestListPending_TenantIsolation_NoPostFilter(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	orgA := "org-tenant-A"
	orgB := "org-tenant-B"
	insert := func(orgID string) {
		requestID := uuid.New()
		env.reqUpdater.mu.Lock()
		env.reqUpdater.requests[requestID] = requestdomain.Request{
			ID:     requestID,
			Status: requestdomain.StatusPendingApproval,
		}
		env.reqUpdater.mu.Unlock()
		oid := orgID
		a := approvaldomain.Approval{
			ID:                uuid.New(),
			OrgID:             &oid,
			RequestID:         requestID,
			Status:            approvaldomain.ApprovalStatusPending,
			RequiredApprovals: 1,
			ExpiresAt:         time.Now().Add(time.Hour),
			CreatedAt:         time.Now(),
		}
		if _, err := env.repo.Create(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}

	// 5 approvals en org A, 5 en org B.
	for i := 0; i < 5; i++ {
		insert(orgA)
		insert(orgB)
	}

	// Caller de org A pide hasta 3 pendientes con filtro de tenancy en SQL.
	listA, err := env.uc.ListPending(context.Background(), 3, &orgA, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 3 {
		t.Fatalf("expected 3 approvals for orgA after tenant SQL filter, got %d", len(listA))
	}
	for _, a := range listA {
		if a.OrgID == nil || *a.OrgID != orgA {
			t.Fatalf("expected only orgA approvals, got %v", a.OrgID)
		}
	}

	// Si pido todas las de A (limit alto), debo obtener las 5.
	allA, err := env.uc.ListPending(context.Background(), 100, &orgA, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(allA) != 5 {
		t.Fatalf("expected 5 orgA approvals total, got %d", len(allA))
	}

	// Caller cross-org admin (allowAll=true) ve las 10.
	allAdmin, err := env.uc.ListPending(context.Background(), 100, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(allAdmin) != 10 {
		t.Fatalf("expected 10 total approvals for admin scope, got %d", len(allAdmin))
	}
}

// TestListPendingEmpty verifica que una lista sin pendientes retorna array vacío.
func TestListPendingEmpty(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	rec := doRequest(t, env.mux, http.MethodGet, "/v1/approvals/pending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado 200, obtenido %d", rec.Code)
	}

	var resp struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("error decodificando respuesta: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("se esperaba lista vacía, se obtuvieron %d elementos", len(resp.Data))
	}
}

// TestListPendingAfterApproval verifica que una approval aprobada ya no aparece en pending.
func TestListPendingAfterApproval(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedPendingApproval(t, env)

	// Verificar que hay 1 pendiente
	rec := doRequest(t, env.mux, http.MethodGet, "/v1/approvals/pending", "")
	var resp struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("error decodificando: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("se esperaba 1 pendiente, se obtuvieron %d", len(resp.Data))
	}

	// Aprobar
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"admin","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: código esperado 200, obtenido %d", rec.Code)
	}

	// Verificar que ya no aparece como pendiente
	rec = doRequest(t, env.mux, http.MethodGet, "/v1/approvals/pending", "")
	resp.Data = nil
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("error decodificando: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("después de aprobar, se esperaban 0 pendientes, se obtuvieron %d", len(resp.Data))
	}
}

// --- Tests: Validation ---

func TestValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode int
		wantMsg  string
	}{
		{
			name:     "approve con UUID inválido retorna 400",
			method:   http.MethodPost,
			path:     "/v1/approvals/not-a-uuid/approve",
			body:     `{"decided_by":"admin"}`,
			wantCode: http.StatusBadRequest,
			wantMsg:  "invalid id",
		},
		{
			name:     "reject con UUID inválido retorna 400",
			method:   http.MethodPost,
			path:     "/v1/approvals/xyz/reject",
			body:     `{"decided_by":"admin"}`,
			wantCode: http.StatusBadRequest,
			wantMsg:  "invalid id",
		},
		{
			name:     "approve con JSON inválido retorna 400",
			method:   http.MethodPost,
			path:     "/v1/approvals/" + uuid.New().String() + "/approve",
			body:     `{broken json`,
			wantCode: http.StatusBadRequest,
			wantMsg:  "invalid json",
		},
		{
			name:     "reject con JSON inválido retorna 400",
			method:   http.MethodPost,
			path:     "/v1/approvals/" + uuid.New().String() + "/reject",
			body:     `not json at all`,
			wantCode: http.StatusBadRequest,
			wantMsg:  "invalid json",
		},
		{
			name:     "approve con body vacío retorna 400",
			method:   http.MethodPost,
			path:     "/v1/approvals/" + uuid.New().String() + "/approve",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantMsg:  "invalid json",
		},
		{
			name:     "reject con body vacío retorna 400",
			method:   http.MethodPost,
			path:     "/v1/approvals/" + uuid.New().String() + "/reject",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantMsg:  "invalid json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv()
			rec := doRequest(t, env.mux, tc.method, tc.path, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("código esperado %d, obtenido %d: %s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if tc.wantMsg != "" && !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Fatalf("respuesta debería contener %q, obtenido: %s", tc.wantMsg, rec.Body.String())
			}
		})
	}
}

// --- Tests: Not found ---

func TestApproveNotFound(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	nonExistentID := uuid.New()

	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+nonExistentID.String()+"/approve",
		`{"decided_by":"admin","note":"ok"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("código esperado 404, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not found") {
		t.Fatalf("respuesta debería contener 'not found', obtenido: %s", rec.Body.String())
	}
}

func TestRejectNotFound(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	nonExistentID := uuid.New()

	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+nonExistentID.String()+"/reject",
		`{"decided_by":"admin","note":"no"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("código esperado 404, obtenido %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not found") {
		t.Fatalf("respuesta debería contener 'not found', obtenido: %s", rec.Body.String())
	}
}

// --- Tests: Break-glass con 3 aprobadores ---

func TestBreakGlassThreeApprovers(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedBreakGlassApproval(t, env, 3)

	// Aprobación 1/3: parcial
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-1","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("1/3: código esperado 200, obtenido %d", rec.Code)
	}
	a, _ := env.repo.GetByID(context.Background(), approvalID)
	if a.Status != approvaldomain.ApprovalStatusPending {
		t.Fatalf("1/3: status esperado pending, obtenido %q", a.Status)
	}

	// Aprobación 2/3: todavía parcial
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-2","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("2/3: código esperado 200, obtenido %d", rec.Code)
	}
	a, _ = env.repo.GetByID(context.Background(), approvalID)
	if a.Status != approvaldomain.ApprovalStatusPending {
		t.Fatalf("2/3: status esperado pending, obtenido %q", a.Status)
	}

	// Aprobación 3/3: finaliza
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"approver-3","note":"all good"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("3/3: código esperado 200, obtenido %d", rec.Code)
	}
	a, _ = env.repo.GetByID(context.Background(), approvalID)
	if a.Status != approvaldomain.ApprovalStatusApproved {
		t.Fatalf("3/3: status esperado approved, obtenido %q", a.Status)
	}
	if len(a.Decisions) != 3 {
		t.Fatalf("se esperaban 3 decisiones, se obtuvieron %d", len(a.Decisions))
	}
}

// --- Tests: Flujo combinado reject después de aprobación ---

func TestApproveAlreadyApproved(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedPendingApproval(t, env)

	// Aprobar
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"admin","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: código esperado 200, obtenido %d", rec.Code)
	}

	// Intentar rechazar una ya aprobada: debe retornar 409
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/reject",
		`{"decided_by":"admin","note":"oops"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("reject después de approve: código esperado 409, obtenido %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRejectAlreadyRejected(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedPendingApproval(t, env)

	// Rechazar
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/reject",
		`{"decided_by":"admin","note":"no"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: código esperado 200, obtenido %d", rec.Code)
	}

	// Intentar aprobar una ya rechazada: debe retornar 409
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"admin","note":"changed mind"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("approve después de reject: código esperado 409, obtenido %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Tests: Audit events en break-glass ---

func TestBreakGlassAuditEvents(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	approvalID := seedBreakGlassApproval(t, env, 2)

	// Primera aprobación parcial
	if err := env.uc.Approve(context.Background(), approvalID, "approver-1", "partial"); err != nil {
		t.Fatalf("primera aprobación falló: %v", err)
	}

	events := env.audit.getEvents()
	if len(events) != 1 {
		t.Fatalf("después de aprobación parcial, se esperaba 1 evento, se obtuvieron %d", len(events))
	}
	if !strings.Contains(events[0].Summary, "Partial") {
		t.Fatalf("evento parcial debería contener 'Partial', summary: %q", events[0].Summary)
	}
	if !strings.Contains(events[0].Summary, "1/2") {
		t.Fatalf("evento parcial debería contener '1/2', summary: %q", events[0].Summary)
	}

	// Segunda aprobación finaliza
	if err := env.uc.Approve(context.Background(), approvalID, "approver-2", "complete"); err != nil {
		t.Fatalf("segunda aprobación falló: %v", err)
	}

	events = env.audit.getEvents()
	if len(events) != 2 {
		t.Fatalf("después de aprobación final, se esperaban 2 eventos, se obtuvieron %d", len(events))
	}
	if events[1].EventType != "approved" {
		t.Fatalf("evento final debería ser 'approved', obtenido %q", events[1].EventType)
	}
}

// --- Tests: Usecase directo sin HTTP ---

func TestUsecaseApproveNotFoundDirectly(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	err := env.uc.Approve(context.Background(), uuid.New(), "admin", "ok")
	if err == nil {
		t.Fatal("se esperaba error para approval inexistente, se obtuvo nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error debería contener 'not found', obtenido: %v", err)
	}
}

func TestUsecaseRejectNotFoundDirectly(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	err := env.uc.Reject(context.Background(), uuid.New(), "admin", "no")
	if err == nil {
		t.Fatal("se esperaba error para approval inexistente, se obtuvo nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error debería contener 'not found', obtenido: %v", err)
	}
}

func TestUsecaseListPendingDirectly(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	seedPendingApproval(t, env)
	seedPendingApproval(t, env)
	seedPendingApproval(t, env)

	list, err := env.uc.ListPending(context.Background(), 10, nil, true)
	if err != nil {
		t.Fatalf("list pending falló: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("se esperaban 3 approvals, se obtuvieron %d", len(list))
	}
}

// --- Tests: Sin audit sink no falla ---

func TestNoAuditSinkSafe(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	reqUpdater := newFakeRequestUpdater()
	// Sin audit sink
	uc := approvals.NewUsecases(repo, reqUpdater)
	mux := http.NewServeMux()
	approvals.NewHandler(uc).Register(mux)

	env := testEnv{mux: mux, repo: repo, reqUpdater: reqUpdater, uc: uc}
	approvalID := seedPendingApproval(t, env)

	// Aprobar sin audit sink: no debería hacer panic
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"admin","note":"safe"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve sin audit sink: código esperado 200, obtenido %d", rec.Code)
	}
}

// fakeDecisionTx emula el contrato de approvals.DecisionTx en memoria.
// El "lock" no serializa concurrencia real (los tests son single-goroutine
// con t.Parallel a nivel test, no a nivel approval), pero permite verificar
// el control flow de Persist*/Rollback y los datos que llegan al persist.
type fakeDecisionTx struct {
	repo        *fakeRepo
	mu          sync.Mutex
	beginCalls  int
	failBegin   bool
	failPersist bool

	lastApprID  uuid.UUID
	lastReqID   uuid.UUID
	lastApprSt  approvaldomain.ApprovalStatus
	lastReqStat requestdomain.RequestStatus
	wasFinal    bool
}

func newFakeDecisionTx(repo *fakeRepo) *fakeDecisionTx {
	return &fakeDecisionTx{repo: repo}
}

func (f *fakeDecisionTx) BeginDecision(ctx context.Context, id uuid.UUID) (approvals.DecisionLock, approvaldomain.Approval, error) {
	f.mu.Lock()
	f.beginCalls++
	fail := f.failBegin
	f.mu.Unlock()
	if fail {
		return nil, approvaldomain.Approval{}, errors.New("simulated begin failure")
	}
	a, err := f.repo.GetByID(ctx, id)
	if err != nil {
		return nil, approvaldomain.Approval{}, err
	}
	return &fakeDecisionLock{tx: f, repo: f.repo, approvalID: id}, a, nil
}

type fakeDecisionLock struct {
	tx         *fakeDecisionTx
	repo       *fakeRepo
	approvalID uuid.UUID
	finished   bool
}

func (l *fakeDecisionLock) PersistPartial(ctx context.Context, a approvaldomain.Approval) error {
	if l.finished {
		return errors.New("already finished")
	}
	if l.tx.failPersist {
		return errors.New("simulated persist failure")
	}
	if _, err := l.repo.Update(ctx, a); err != nil {
		return err
	}
	l.tx.mu.Lock()
	l.tx.lastApprID = a.ID
	l.tx.lastApprSt = a.Status
	l.tx.wasFinal = false
	l.tx.mu.Unlock()
	l.finished = true
	return nil
}

func (l *fakeDecisionLock) PersistFinal(ctx context.Context, a approvaldomain.Approval, r requestdomain.Request) error {
	if l.finished {
		return errors.New("already finished")
	}
	if l.tx.failPersist {
		return errors.New("simulated persist failure")
	}
	if _, err := l.repo.Update(ctx, a); err != nil {
		return err
	}
	l.tx.mu.Lock()
	l.tx.lastApprID = a.ID
	l.tx.lastApprSt = a.Status
	l.tx.lastReqID = r.ID
	l.tx.lastReqStat = r.Status
	l.tx.wasFinal = true
	l.tx.mu.Unlock()
	l.finished = true
	return nil
}

func (l *fakeDecisionLock) Rollback(_ context.Context) error {
	l.finished = true
	return nil
}

// TestApprove_UsesDecisionTx_HappyPath verifica que el camino atómico se
// dispara cuando hay un DecisionTx inyectado.
func TestApprove_UsesDecisionTx_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	reqUpdater := newFakeRequestUpdater()
	tx := newFakeDecisionTx(repo)
	uc := approvals.NewUsecases(repo, reqUpdater).WithDecisionTx(tx)
	mux := http.NewServeMux()
	approvals.NewHandler(uc).Register(mux)

	env := testEnv{mux: mux, repo: repo, reqUpdater: reqUpdater, uc: uc}
	approvalID := seedPendingApproval(t, env)

	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"admin","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if tx.beginCalls != 1 {
		t.Fatalf("expected BeginDecision llamado 1 vez, got %d", tx.beginCalls)
	}
	if !tx.wasFinal {
		t.Fatalf("expected PersistFinal, got Partial")
	}
	if tx.lastApprSt != approvaldomain.ApprovalStatusApproved {
		t.Fatalf("approval no llegó approved al tx: %s", tx.lastApprSt)
	}
	if tx.lastReqStat != requestdomain.StatusApproved {
		t.Fatalf("request no llegó approved al tx: %s", tx.lastReqStat)
	}
}

// TestApprove_DecisionTxFails_PropagatesError verifica que si la tx falla,
// el usecase devuelve error sin emitir audit ni callbacks de "resolved".
func TestApprove_DecisionTxFails_PropagatesError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	reqUpdater := newFakeRequestUpdater()
	tx := newFakeDecisionTx(repo)
	tx.failPersist = true
	sink := newFakeAuditSink()
	uc := approvals.NewUsecases(repo, reqUpdater).
		WithAuditSink(sink).
		WithDecisionTx(tx)
	mux := http.NewServeMux()
	approvals.NewHandler(uc).Register(mux)

	env := testEnv{mux: mux, repo: repo, reqUpdater: reqUpdater, uc: uc, audit: sink}
	approvalID := seedPendingApproval(t, env)

	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"admin","note":"x"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("esperado error, llegó 200: %s", rec.Body.String())
	}
	for _, e := range sink.getEvents() {
		if e.EventType == "request.approved" {
			t.Fatalf("audit emitido a pesar de tx fallido: %+v", e)
		}
	}
}

// TestApprove_BreakGlass_LockedSnapshotPreventsDoubleSign verifica C11:
// con lock, dos approves del mismo approver son detectados como duplicado
// usando el snapshot bloqueado (no el snapshot pre-lock obsoleto).
func TestApprove_BreakGlass_LockedSnapshotPreventsDoubleSign(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	reqUpdater := newFakeRequestUpdater()
	tx := newFakeDecisionTx(repo)
	uc := approvals.NewUsecases(repo, reqUpdater).WithDecisionTx(tx)
	mux := http.NewServeMux()
	approvals.NewHandler(uc).Register(mux)

	env := testEnv{mux: mux, repo: repo, reqUpdater: reqUpdater, uc: uc}
	approvalID := seedBreakGlassApproval(t, env, 2)

	// Primer approve de "alice": parcial 1/2
	rec := doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"alice","note":"first"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("primer approve esperado 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Segundo approve de "alice" (replay): debe rechazarse por ErrAlreadyDecided
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"alice","note":"replay"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("replay del mismo aprobador esperado 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// Approve de "bob": completa 2/2 → final
	rec = doRequest(t, env.mux, http.MethodPost,
		"/v1/approvals/"+approvalID.String()+"/approve",
		`{"decided_by":"bob","note":"second"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve de bob esperado 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !tx.wasFinal {
		t.Fatalf("threshold alcanzado debería haber sido final, got partial")
	}
}
