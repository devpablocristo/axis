package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	learningdomain "github.com/devpablocristo/nexus/internal/learning/usecases/domain"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
	"github.com/google/uuid"
)

// --- Fakes ---

// fakeProposalRepo simula el repositorio de proposals en memoria.
type fakeProposalRepo struct {
	mu    sync.RWMutex
	byID  map[uuid.UUID]learningdomain.PolicyProposal
	order []uuid.UUID

	// Para inyectar errores en tests específicos
	createErr error
	updateErr error
}

func newFakeProposalRepo() *fakeProposalRepo {
	return &fakeProposalRepo{byID: make(map[uuid.UUID]learningdomain.PolicyProposal)}
}

func (r *fakeProposalRepo) CreateProposal(_ context.Context, p learningdomain.PolicyProposal) (learningdomain.PolicyProposal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return learningdomain.PolicyProposal{}, r.createErr
	}
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	r.byID[p.ID] = p
	r.order = append(r.order, p.ID)
	return p, nil
}

func (r *fakeProposalRepo) ListPendingProposals(_ context.Context, limit int, orgID *string, allowAll bool) ([]learningdomain.PolicyProposal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []learningdomain.PolicyProposal
	for _, id := range r.order {
		p := r.byID[id]
		if !allowAll {
			if orgID != nil {
				if p.OrgID == nil || *p.OrgID != *orgID {
					continue
				}
			} else if p.OrgID != nil {
				continue
			}
		}
		if p.Status == learningdomain.ProposalStatusPending {
			out = append(out, p)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeProposalRepo) GetProposalByID(_ context.Context, id uuid.UUID) (learningdomain.PolicyProposal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	if !ok {
		return learningdomain.PolicyProposal{}, ErrNotFound
	}
	return p, nil
}

func (r *fakeProposalRepo) UpdateProposal(_ context.Context, p learningdomain.PolicyProposal) (learningdomain.PolicyProposal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return learningdomain.PolicyProposal{}, r.updateErr
	}
	r.byID[p.ID] = p
	return p, nil
}

// fakePolicyCreator simula la creación de policies a partir de proposals.
type fakePolicyCreator struct {
	err      error
	resultID uuid.UUID
}

func (s *fakePolicyCreator) CreateFromProposal(_ context.Context, _ learningdomain.PolicyProposal) (uuid.UUID, error) {
	if s.err != nil {
		return uuid.Nil, s.err
	}
	if s.resultID != uuid.Nil {
		return s.resultID, nil
	}
	return uuid.New(), nil
}

// fakeAnalyzer simula el análisis de patrones.
type fakeAnalyzer struct {
	patterns []Pattern
	err      error
}

func (a *fakeAnalyzer) Analyze(_ context.Context, _ int, _ int, _ float64) ([]Pattern, error) {
	return a.patterns, a.err
}

// fakeProposer simula la generación de proposals.
type fakeProposer struct {
	proposals []*learningdomain.PolicyProposal
	idx       int
	err       error
}

func (p *fakeProposer) GenerateProposal(_ context.Context, pattern Pattern) (*learningdomain.PolicyProposal, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.idx < len(p.proposals) {
		prop := p.proposals[p.idx]
		p.idx++
		return prop, nil
	}
	// Generación por defecto
	return &learningdomain.PolicyProposal{
		ID:                 uuid.New(),
		ProposedName:       fmt.Sprintf("auto-approve-%s", pattern.ActionType),
		ProposedExpression: fmt.Sprintf("request.action_type == '%s'", pattern.ActionType),
		ProposedEffect:     "allow",
		ProposedPriority:   100,
		PatternSummary:     "test pattern",
		Confidence:         pattern.ApprovalRate,
		SampleSize:         pattern.Total,
		TimeWindow:         pattern.TimeWindow,
		Status:             learningdomain.ProposalStatusPending,
		CreatedAt:          time.Now().UTC(),
	}, nil
}

// fakeRequestLister simula el listado de requests históricas para el analyzer.
type fakeRequestLister struct {
	requests []requestdomain.Request
	err      error
}

func (l *fakeRequestLister) List(_ context.Context, _, _ string, _ int, _ *string, _ bool) ([]requestdomain.Request, error) {
	return l.requests, l.err
}

// --- Helpers ---

func setupLearningMux() (*http.ServeMux, *fakeProposalRepo) {
	repo := newFakeProposalRepo()
	uc := NewUsecases(repo, &fakePolicyCreator{})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)
	return mux, repo
}

func setupLearningMuxWithDeps(repo *fakeProposalRepo, creator *fakePolicyCreator) *http.ServeMux {
	uc := NewUsecases(repo, creator)
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)
	return mux
}

func seedProposal(t *testing.T, repo *fakeProposalRepo) uuid.UUID {
	t.Helper()
	p := learningdomain.PolicyProposal{
		ID:                 uuid.New(),
		ProposedName:       "auto-approve-escalate",
		ProposedExpression: "request.action_type == 'alert.escalate'",
		ProposedEffect:     "allow",
		ProposedPriority:   100,
		PatternSummary:     "96% approved",
		Confidence:         0.96,
		SampleSize:         285,
		TimeWindow:         "14d",
		Status:             learningdomain.ProposalStatusPending,
		CreatedAt:          time.Now(),
	}
	if _, err := repo.CreateProposal(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func seedProposalWithStatus(t *testing.T, repo *fakeProposalRepo, status learningdomain.ProposalStatus) uuid.UUID {
	t.Helper()
	p := learningdomain.PolicyProposal{
		ID:                 uuid.New(),
		ProposedName:       "test-proposal",
		ProposedExpression: "request.action_type == 'test'",
		ProposedEffect:     "allow",
		ProposedPriority:   100,
		PatternSummary:     "test",
		Confidence:         0.95,
		SampleSize:         100,
		TimeWindow:         "14d",
		Status:             status,
		CreatedAt:          time.Now(),
	}
	if _, err := repo.CreateProposal(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func doLReq(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, r))
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode json: %v", err)
	}
}

// =============================================================================
// Tests de Handler
// =============================================================================

func TestHandler_ListProposals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		seedCount  int
		wantStatus int
		wantLen    int
	}{
		{
			name:       "lista vacía devuelve array vacío",
			seedCount:  0,
			wantStatus: http.StatusOK,
			wantLen:    0,
		},
		{
			name:       "lista con un proposal",
			seedCount:  1,
			wantStatus: http.StatusOK,
			wantLen:    1,
		},
		{
			name:       "lista con múltiples proposals",
			seedCount:  3,
			wantStatus: http.StatusOK,
			wantLen:    3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux, repo := setupLearningMux()
			for i := 0; i < tc.seedCount; i++ {
				seedProposal(t, repo)
			}
			rec := doLReq(t, mux, http.MethodGet, "/v1/learning/proposals", "")
			if rec.Code != tc.wantStatus {
				t.Fatalf("esperado %d, obtuve %d", tc.wantStatus, rec.Code)
			}
			// Decodificar la respuesta como mapa genérico
			var resp map[string]json.RawMessage
			decodeJSON(t, rec, &resp)
			var data []learningdomain.PolicyProposal
			if err := json.Unmarshal(resp["data"], &data); err != nil {
				t.Fatalf("decode data: %v", err)
			}
			if len(data) != tc.wantLen {
				t.Fatalf("esperado %d proposals, obtuve %d", tc.wantLen, len(data))
			}
		})
	}
}

func TestHandler_GetProposal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pathID     string
		seed       bool
		wantStatus int
	}{
		{
			name:       "happy path: proposal existente",
			seed:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found: proposal inexistente",
			pathID:     uuid.New().String(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "validación: ID inválido",
			pathID:     "not-a-uuid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux, repo := setupLearningMux()
			pathID := tc.pathID
			if tc.seed {
				pathID = seedProposal(t, repo).String()
			}
			rec := doLReq(t, mux, http.MethodGet, "/v1/learning/proposals/"+pathID, "")
			if rec.Code != tc.wantStatus {
				t.Fatalf("esperado %d, obtuve %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandler_AcceptProposal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pathID     string
		body       string
		seed       bool
		preAccept  bool // Si true, se acepta primero para provocar conflicto
		wantStatus int
		wantField  string // Campo esperado en la respuesta
	}{
		{
			name:       "happy path: aceptar proposal pendiente",
			seed:       true,
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusOK,
			wantField:  "policy_id",
		},
		{
			// Idempotente: aceptar dos veces devuelve el mismo policy_id sin
			// crear policies duplicadas (clave para safe-retry tras update fail).
			name:       "idempotente: aceptar proposal ya aceptado devuelve mismo policy_id",
			seed:       true,
			preAccept:  true,
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusOK,
			wantField:  "policy_id",
		},
		{
			name:       "not found: proposal inexistente",
			pathID:     uuid.New().String(),
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "validación: ID inválido",
			pathID:     "bad-id",
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "validación: JSON inválido",
			seed:       true,
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux, repo := setupLearningMux()
			pathID := tc.pathID
			if tc.seed {
				pathID = seedProposal(t, repo).String()
			}
			if tc.preAccept {
				doLReq(t, mux, http.MethodPost, "/v1/learning/proposals/"+pathID+"/accept", `{"decided_by":"admin"}`)
			}
			rec := doLReq(t, mux, http.MethodPost, "/v1/learning/proposals/"+pathID+"/accept", tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("esperado %d, obtuve %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if tc.wantField != "" {
				var resp map[string]any
				decodeJSON(t, rec, &resp)
				if _, ok := resp[tc.wantField]; !ok {
					t.Fatalf("campo %q no encontrado en respuesta: %v", tc.wantField, resp)
				}
			}
		})
	}
}

func TestHandler_DismissProposal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pathID     string
		body       string
		seed       bool
		preDismiss bool
		wantStatus int
	}{
		{
			name:       "happy path: dismiss proposal pendiente",
			seed:       true,
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "conflicto: dismiss proposal ya dismissed",
			seed:       true,
			preDismiss: true,
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "not found: proposal inexistente",
			pathID:     uuid.New().String(),
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "validación: ID inválido",
			pathID:     "bad-id",
			body:       `{"decided_by":"admin"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "validación: JSON inválido",
			seed:       true,
			body:       `not-json`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux, repo := setupLearningMux()
			pathID := tc.pathID
			if tc.seed {
				pathID = seedProposal(t, repo).String()
			}
			if tc.preDismiss {
				doLReq(t, mux, http.MethodPost, "/v1/learning/proposals/"+pathID+"/dismiss", `{"decided_by":"admin"}`)
			}
			rec := doLReq(t, mux, http.MethodPost, "/v1/learning/proposals/"+pathID+"/dismiss", tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("esperado %d, obtuve %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandler_Analyze(t *testing.T) {
	t.Parallel()

	// Sin analyzer/proposer configurados, debería devolver 500
	t.Run("sin analyzer configurado devuelve error interno", func(t *testing.T) {
		t.Parallel()
		mux, _ := setupLearningMux()
		rec := doLReq(t, mux, http.MethodPost, "/v1/learning/analyze", "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("esperado 500, obtuve %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Con analyzer y proposer configurados
	t.Run("happy path: analyze genera proposals", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		analyzer := &fakeAnalyzer{
			patterns: []Pattern{
				{ActionType: "alert.escalate", Total: 100, Approved: 95, ApprovalRate: 0.95, TimeWindow: "14d"},
			},
		}
		proposer := &fakeProposer{}

		uc := NewUsecases(repo, &fakePolicyCreator{}).
			WithAnalyzer(analyzer).
			WithProposer(proposer)

		mux := http.NewServeMux()
		NewHandler(uc).Register(mux)

		rec := doLReq(t, mux, http.MethodPost, "/v1/learning/analyze", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtuve %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]int
		decodeJSON(t, rec, &resp)
		if resp["proposals_created"] != 1 {
			t.Fatalf("esperado 1 proposal creado, obtuve %d", resp["proposals_created"])
		}
	})
}

// =============================================================================
// Tests de Usecases
// =============================================================================

func TestUsecases_AcceptProposal(t *testing.T) {
	t.Parallel()

	t.Run("happy path: acepta y crea policy", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		policyID := uuid.New()
		creator := &fakePolicyCreator{resultID: policyID}
		uc := NewUsecases(repo, creator)

		id := seedProposal(t, repo)
		result, err := uc.AcceptProposal(context.Background(), id, "admin")
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if *result != policyID {
			t.Fatalf("esperado policyID %s, obtuve %s", policyID, result)
		}
		// Verificar que el proposal se actualizó
		p, err := repo.GetProposalByID(context.Background(), id)
		if err != nil {
			t.Fatalf("error al obtener proposal: %v", err)
		}
		if p.Status != learningdomain.ProposalStatusAccepted {
			t.Fatalf("esperado status accepted, obtuve %s", p.Status)
		}
		if p.DecidedBy == nil || *p.DecidedBy != "admin" {
			t.Fatal("decidedBy no se actualizó correctamente")
		}
	})

	t.Run("not found: proposal inexistente", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		_, err := uc.AcceptProposal(context.Background(), uuid.New(), "admin")
		if !domainerr.IsNotFound(err) {
			t.Fatalf("esperado ErrNotFound, obtuve %v", err)
		}
	})

	t.Run("conflicto: proposal no está pending", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		id := seedProposalWithStatus(t, repo, learningdomain.ProposalStatusDismissed)
		_, err := uc.AcceptProposal(context.Background(), id, "admin")
		if !domainerr.IsConflict(err) {
			t.Fatalf("esperado ErrNotPending, obtuve %v", err)
		}
	})

	t.Run("error del policy creator se propaga", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		creator := &fakePolicyCreator{err: errors.New("policy service down")}
		uc := NewUsecases(repo, creator)

		id := seedProposal(t, repo)
		_, err := uc.AcceptProposal(context.Background(), id, "admin")
		if err == nil {
			t.Fatal("esperado error, obtuve nil")
		}
		if !strings.Contains(err.Error(), "create policy from proposal") {
			t.Fatalf("error no contiene contexto esperado: %v", err)
		}
	})

	t.Run("fallo en update propaga error y devuelve la policy creada", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		creator := &fakePolicyCreator{}
		uc := NewUsecases(repo, creator)

		id := seedProposal(t, repo)
		// Configurar error en update después de seedear
		repo.updateErr = errors.New("db connection lost")

		result, err := uc.AcceptProposal(context.Background(), id, "admin")
		// Ahora propagamos el error: la policy se creó pero el proposal quedó
		// inconsistente. El caller debe enterarse para retry/reconciliación.
		if err == nil {
			t.Fatal("esperado error de update, obtuve nil")
		}
		if !strings.Contains(err.Error(), "update proposal") {
			t.Fatalf("error no contiene contexto esperado: %v", err)
		}
		if result == nil {
			t.Fatal("esperado policyID huérfano para reconciliación, obtuve nil")
		}
	})
}

func TestUsecases_DismissProposal(t *testing.T) {
	t.Parallel()

	t.Run("happy path: dismiss exitoso", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		id := seedProposal(t, repo)
		err := uc.DismissProposal(context.Background(), id, "admin")
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		// Verificar estado
		p, err := repo.GetProposalByID(context.Background(), id)
		if err != nil {
			t.Fatalf("error al obtener proposal: %v", err)
		}
		if p.Status != learningdomain.ProposalStatusDismissed {
			t.Fatalf("esperado status dismissed, obtuve %s", p.Status)
		}
	})

	t.Run("not found: proposal inexistente", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		err := uc.DismissProposal(context.Background(), uuid.New(), "admin")
		if !domainerr.IsNotFound(err) {
			t.Fatalf("esperado ErrNotFound, obtuve %v", err)
		}
	})

	t.Run("conflicto: proposal ya aceptado", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		id := seedProposalWithStatus(t, repo, learningdomain.ProposalStatusAccepted)
		err := uc.DismissProposal(context.Background(), id, "admin")
		if !domainerr.IsConflict(err) {
			t.Fatalf("esperado ErrNotPending, obtuve %v", err)
		}
	})

	t.Run("error en update se propaga", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		id := seedProposal(t, repo)
		repo.updateErr = errors.New("db error")

		err := uc.DismissProposal(context.Background(), id, "admin")
		if err == nil {
			t.Fatal("esperado error, obtuve nil")
		}
	})
}

func TestUsecases_AnalyzeAndPropose(t *testing.T) {
	t.Parallel()

	t.Run("sin analyzer retorna error", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		_, err := uc.AnalyzeAndPropose(context.Background())
		if err == nil {
			t.Fatal("esperado error, obtuve nil")
		}
		if !strings.Contains(err.Error(), "not configured") {
			t.Fatalf("error inesperado: %v", err)
		}
	})

	t.Run("sin patrones detectados retorna 0", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		analyzer := &fakeAnalyzer{patterns: nil}
		proposer := &fakeProposer{}
		uc := NewUsecases(repo, &fakePolicyCreator{}).
			WithAnalyzer(analyzer).
			WithProposer(proposer)

		count, err := uc.AnalyzeAndPropose(context.Background())
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if count != 0 {
			t.Fatalf("esperado 0, obtuve %d", count)
		}
	})

	t.Run("happy path: crea proposals a partir de patrones", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		analyzer := &fakeAnalyzer{
			patterns: []Pattern{
				{ActionType: "deploy", Total: 200, Approved: 190, ApprovalRate: 0.95, TimeWindow: "14d"},
				{ActionType: "restart", Total: 100, Approved: 95, ApprovalRate: 0.95, TimeWindow: "14d"},
			},
		}
		proposer := &fakeProposer{}
		uc := NewUsecases(repo, &fakePolicyCreator{}).
			WithAnalyzer(analyzer).
			WithProposer(proposer)

		count, err := uc.AnalyzeAndPropose(context.Background())
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if count != 2 {
			t.Fatalf("esperado 2, obtuve %d", count)
		}
	})

	t.Run("error en analyzer se propaga", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		analyzer := &fakeAnalyzer{err: errors.New("db timeout")}
		proposer := &fakeProposer{}
		uc := NewUsecases(repo, &fakePolicyCreator{}).
			WithAnalyzer(analyzer).
			WithProposer(proposer)

		_, err := uc.AnalyzeAndPropose(context.Background())
		if err == nil {
			t.Fatal("esperado error, obtuve nil")
		}
	})

	t.Run("error en proposer no detiene el proceso", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		analyzer := &fakeAnalyzer{
			patterns: []Pattern{
				{ActionType: "deploy", Total: 200, Approved: 190, ApprovalRate: 0.95, TimeWindow: "14d"},
				{ActionType: "restart", Total: 100, Approved: 95, ApprovalRate: 0.95, TimeWindow: "14d"},
			},
		}
		// El proposer falla siempre
		proposer := &fakeProposer{err: errors.New("proposer broken")}
		uc := NewUsecases(repo, &fakePolicyCreator{}).
			WithAnalyzer(analyzer).
			WithProposer(proposer)

		count, err := uc.AnalyzeAndPropose(context.Background())
		if err != nil {
			t.Fatalf("no esperaba error, obtuve: %v", err)
		}
		if count != 0 {
			t.Fatalf("esperado 0 (todos fallaron), obtuve %d", count)
		}
	})

	t.Run("error en repo.CreateProposal no detiene el proceso", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		repo.createErr = errors.New("insert failed")
		analyzer := &fakeAnalyzer{
			patterns: []Pattern{
				{ActionType: "deploy", Total: 200, Approved: 190, ApprovalRate: 0.95, TimeWindow: "14d"},
			},
		}
		proposer := &fakeProposer{}
		uc := NewUsecases(repo, &fakePolicyCreator{}).
			WithAnalyzer(analyzer).
			WithProposer(proposer)

		count, err := uc.AnalyzeAndPropose(context.Background())
		if err != nil {
			t.Fatalf("no esperaba error, obtuve: %v", err)
		}
		if count != 0 {
			t.Fatalf("esperado 0 (save falló), obtuve %d", count)
		}
	})
}

func TestUsecases_ListPendingProposals(t *testing.T) {
	t.Parallel()

	t.Run("excluye proposals no-pending", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		seedProposal(t, repo) // pending
		seedProposalWithStatus(t, repo, learningdomain.ProposalStatusAccepted)
		seedProposalWithStatus(t, repo, learningdomain.ProposalStatusDismissed)

		list, err := uc.ListPendingProposals(context.Background(), 100, nil, true)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("esperado 1 pending, obtuve %d", len(list))
		}
	})
}

func TestUsecases_GetProposalByID(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		id := seedProposal(t, repo)
		p, err := uc.GetProposalByID(context.Background(), id)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if p.ID != id {
			t.Fatalf("esperado ID %s, obtuve %s", id, p.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		repo := newFakeProposalRepo()
		uc := NewUsecases(repo, &fakePolicyCreator{})

		_, err := uc.GetProposalByID(context.Background(), uuid.New())
		if !domainerr.IsNotFound(err) {
			t.Fatalf("esperado ErrNotFound, obtuve %v", err)
		}
	})
}

// =============================================================================
// Tests de InMemoryPatternAnalyzer
// =============================================================================

func TestInMemoryPatternAnalyzer_Analyze(t *testing.T) {
	t.Parallel()

	t.Run("detecta patrones sobre el umbral", func(t *testing.T) {
		t.Parallel()
		// Crear 60 requests de tipo "deploy": 57 aprobadas (95%)
		var requests []requestdomain.Request
		for i := 0; i < 57; i++ {
			requests = append(requests, requestdomain.Request{
				ID:         uuid.New(),
				ActionType: "deploy",
				Decision:   requestdomain.DecisionRequireApproval,
				Status:     requestdomain.StatusApproved,
			})
		}
		for i := 0; i < 3; i++ {
			requests = append(requests, requestdomain.Request{
				ID:         uuid.New(),
				ActionType: "deploy",
				Decision:   requestdomain.DecisionRequireApproval,
				Status:     requestdomain.StatusRejected,
			})
		}

		lister := &fakeRequestLister{requests: requests}
		analyzer := NewInMemoryPatternAnalyzer(lister)

		patterns, err := analyzer.Analyze(context.Background(), 14, 50, 0.90)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if len(patterns) != 1 {
			t.Fatalf("esperado 1 patrón, obtuve %d", len(patterns))
		}
		if patterns[0].ActionType != "deploy" {
			t.Fatalf("esperado action_type deploy, obtuve %s", patterns[0].ActionType)
		}
		if patterns[0].Total != 60 {
			t.Fatalf("esperado total 60, obtuve %d", patterns[0].Total)
		}
	})

	t.Run("excluye patrones bajo el umbral de approval rate", func(t *testing.T) {
		t.Parallel()
		// 60 requests, solo 50% aprobadas
		var requests []requestdomain.Request
		for i := 0; i < 30; i++ {
			requests = append(requests, requestdomain.Request{
				ID:         uuid.New(),
				ActionType: "risky-action",
				Decision:   requestdomain.DecisionRequireApproval,
				Status:     requestdomain.StatusApproved,
			})
		}
		for i := 0; i < 30; i++ {
			requests = append(requests, requestdomain.Request{
				ID:         uuid.New(),
				ActionType: "risky-action",
				Decision:   requestdomain.DecisionRequireApproval,
				Status:     requestdomain.StatusRejected,
			})
		}

		lister := &fakeRequestLister{requests: requests}
		analyzer := NewInMemoryPatternAnalyzer(lister)

		patterns, err := analyzer.Analyze(context.Background(), 14, 50, 0.90)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if len(patterns) != 0 {
			t.Fatalf("esperado 0 patrones, obtuve %d", len(patterns))
		}
	})

	t.Run("excluye patrones bajo el umbral de sample size", func(t *testing.T) {
		t.Parallel()
		// Solo 10 requests (minSampleSize=50)
		var requests []requestdomain.Request
		for i := 0; i < 10; i++ {
			requests = append(requests, requestdomain.Request{
				ID:         uuid.New(),
				ActionType: "rare-action",
				Decision:   requestdomain.DecisionRequireApproval,
				Status:     requestdomain.StatusApproved,
			})
		}

		lister := &fakeRequestLister{requests: requests}
		analyzer := NewInMemoryPatternAnalyzer(lister)

		patterns, err := analyzer.Analyze(context.Background(), 14, 50, 0.90)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if len(patterns) != 0 {
			t.Fatalf("esperado 0 patrones, obtuve %d", len(patterns))
		}
	})

	t.Run("ignora requests sin decision require_approval", func(t *testing.T) {
		t.Parallel()
		// 100 requests pero con decision "allow" (no "require_approval")
		var requests []requestdomain.Request
		for i := 0; i < 100; i++ {
			requests = append(requests, requestdomain.Request{
				ID:         uuid.New(),
				ActionType: "auto-allowed",
				Decision:   requestdomain.DecisionAllow,
				Status:     requestdomain.StatusAllowed,
			})
		}

		lister := &fakeRequestLister{requests: requests}
		analyzer := NewInMemoryPatternAnalyzer(lister)

		patterns, err := analyzer.Analyze(context.Background(), 14, 50, 0.90)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if len(patterns) != 0 {
			t.Fatalf("esperado 0 patrones (no require_approval), obtuve %d", len(patterns))
		}
	})

	t.Run("cuenta executed como aprobado", func(t *testing.T) {
		t.Parallel()
		// 50 requests executed (deben contar como approved)
		var requests []requestdomain.Request
		for i := 0; i < 50; i++ {
			requests = append(requests, requestdomain.Request{
				ID:         uuid.New(),
				ActionType: "exec-action",
				Decision:   requestdomain.DecisionRequireApproval,
				Status:     requestdomain.StatusExecuted,
			})
		}

		lister := &fakeRequestLister{requests: requests}
		analyzer := NewInMemoryPatternAnalyzer(lister)

		patterns, err := analyzer.Analyze(context.Background(), 14, 50, 0.90)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if len(patterns) != 1 {
			t.Fatalf("esperado 1 patrón (executed cuenta como approved), obtuve %d", len(patterns))
		}
		if patterns[0].Approved != 50 {
			t.Fatalf("esperado 50 approved, obtuve %d", patterns[0].Approved)
		}
	})

	t.Run("error en lister se propaga", func(t *testing.T) {
		t.Parallel()
		lister := &fakeRequestLister{err: errors.New("db error")}
		analyzer := NewInMemoryPatternAnalyzer(lister)

		_, err := analyzer.Analyze(context.Background(), 14, 50, 0.90)
		if err == nil {
			t.Fatal("esperado error, obtuve nil")
		}
	})

	t.Run("múltiples action types se agrupan correctamente", func(t *testing.T) {
		t.Parallel()
		var requests []requestdomain.Request
		// 60 de "deploy" con 95% approval
		for i := 0; i < 57; i++ {
			requests = append(requests, requestdomain.Request{
				ID: uuid.New(), ActionType: "deploy",
				Decision: requestdomain.DecisionRequireApproval, Status: requestdomain.StatusApproved,
			})
		}
		for i := 0; i < 3; i++ {
			requests = append(requests, requestdomain.Request{
				ID: uuid.New(), ActionType: "deploy",
				Decision: requestdomain.DecisionRequireApproval, Status: requestdomain.StatusRejected,
			})
		}
		// 55 de "scale" con 100% approval
		for i := 0; i < 55; i++ {
			requests = append(requests, requestdomain.Request{
				ID: uuid.New(), ActionType: "scale",
				Decision: requestdomain.DecisionRequireApproval, Status: requestdomain.StatusApproved,
			})
		}
		// 10 de "delete" (bajo umbral de sample size)
		for i := 0; i < 10; i++ {
			requests = append(requests, requestdomain.Request{
				ID: uuid.New(), ActionType: "delete",
				Decision: requestdomain.DecisionRequireApproval, Status: requestdomain.StatusApproved,
			})
		}

		lister := &fakeRequestLister{requests: requests}
		analyzer := NewInMemoryPatternAnalyzer(lister)

		patterns, err := analyzer.Analyze(context.Background(), 14, 50, 0.90)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		// deploy y scale cumplen; delete no (sample size < 50)
		if len(patterns) != 2 {
			t.Fatalf("esperado 2 patrones, obtuve %d", len(patterns))
		}
	})
}

// =============================================================================
// Tests de StubProposer
// =============================================================================

func TestStubProposer_GenerateProposal(t *testing.T) {
	t.Parallel()

	t.Run("genera proposal con campos correctos", func(t *testing.T) {
		t.Parallel()
		proposer := NewStubProposer()
		pattern := Pattern{
			ActionType:   "deploy",
			Total:        200,
			Approved:     190,
			ApprovalRate: 0.95,
			TimeWindow:   "14d",
		}

		proposal, err := proposer.GenerateProposal(context.Background(), pattern)
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if proposal.ID == uuid.Nil {
			t.Fatal("esperado ID no nulo")
		}
		if proposal.ProposedName != "auto-approve-deploy" {
			t.Fatalf("esperado nombre auto-approve-deploy, obtuve %s", proposal.ProposedName)
		}
		if proposal.ProposedEffect != "allow" {
			t.Fatalf("esperado effect allow, obtuve %s", proposal.ProposedEffect)
		}
		if proposal.Status != learningdomain.ProposalStatusPending {
			t.Fatalf("esperado status pending, obtuve %s", proposal.Status)
		}
		if proposal.ProposedActionType == nil || *proposal.ProposedActionType != "deploy" {
			t.Fatal("ProposedActionType incorrecto")
		}
		if proposal.Confidence != 0.95 {
			t.Fatalf("esperado confidence 0.95, obtuve %f", proposal.Confidence)
		}
		if proposal.SampleSize != 200 {
			t.Fatalf("esperado sample_size 200, obtuve %d", proposal.SampleSize)
		}
		expectedExpr := "request.action_type == 'deploy'"
		if proposal.ProposedExpression != expectedExpr {
			t.Fatalf("esperado expression %q, obtuve %q", expectedExpr, proposal.ProposedExpression)
		}
	})
}

func TestAcceptFallsBackToUserHeader(t *testing.T) {
	t.Parallel()

	mux, repo := setupLearningMux()
	proposalID := seedProposal(t, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/learning/proposals/"+proposalID.String()+"/accept", strings.NewReader(`{}`))
	req.Header.Set("X-User-ID", "header-approver")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código esperado %d, obtenido %d", http.StatusOK, rec.Code)
	}

	proposal, err := repo.GetProposalByID(context.Background(), proposalID)
	if err != nil {
		t.Fatalf("error obteniendo proposal: %v", err)
	}
	if proposal.DecidedBy == nil || *proposal.DecidedBy != "header-approver" {
		t.Fatalf("decided_by esperado %q, obtenido %v", "header-approver", proposal.DecidedBy)
	}
}
