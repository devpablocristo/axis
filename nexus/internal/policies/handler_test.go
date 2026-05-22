package policies_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/nexus/internal/policies"
	policydto "github.com/devpablocristo/nexus/internal/policies/handler/dto"
	policydomain "github.com/devpablocristo/nexus/internal/policies/usecases/domain"
)

// ---------------------------------------------------------------------------
// Fake repo — implementa policies.Repository para tests sin postgres.
// ---------------------------------------------------------------------------

type fakeRepo struct {
	mu    sync.RWMutex
	byID  map[uuid.UUID]policydomain.Policy
	order []uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: make(map[uuid.UUID]policydomain.Policy)}
}

func (r *fakeRepo) Create(_ context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Mode == "" {
		p.Mode = policydomain.PolicyModeEnforced
	}
	r.byID[p.ID] = p
	r.order = append(r.order, p.ID)
	return p, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (policydomain.Policy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	if !ok {
		return policydomain.Policy{}, policies.ErrNotFound
	}
	return p, nil
}

func (r *fakeRepo) List(_ context.Context, filters policies.ListFilters) ([]policydomain.Policy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []policydomain.Policy
	for _, id := range r.order {
		p := r.byID[id]
		if !filters.IncludeArchived && p.ArchivedAt != nil {
			continue
		}
		if filters.EnabledOnly && !p.Enabled {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out, nil
}

func (r *fakeRepo) Update(_ context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[p.ID]; !ok {
		return policydomain.Policy{}, policies.ErrNotFound
	}
	p.UpdatedAt = time.Now().UTC()
	r.byID[p.ID] = p
	return p, nil
}

func (r *fakeRepo) DeleteByID(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return policies.ErrNotFound
	}
	delete(r.byID, id)
	return nil
}

func (r *fakeRepo) ArchiveByID(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok {
		return policies.ErrNotFound
	}
	// Idempotente
	if p.ArchivedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	p.ArchivedAt = &now
	r.byID[id] = p
	return nil
}

func (r *fakeRepo) RestoreByID(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok {
		return policies.ErrNotFound
	}
	// Idempotente
	if p.ArchivedAt == nil {
		return nil
	}
	p.ArchivedAt = nil
	r.byID[id] = p
	return nil
}

// failingRepo devuelve errores internos para cubrir ramas de error genérico.
type failingRepo struct {
	fakeRepo
	failOn string // nombre de la operacion que debe fallar
}

func newFailingRepo(failOn string) *failingRepo {
	return &failingRepo{
		fakeRepo: fakeRepo{byID: make(map[uuid.UUID]policydomain.Policy)},
		failOn:   failOn,
	}
}

func (r *failingRepo) Create(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	if r.failOn == "create" {
		return policydomain.Policy{}, errors.New("db connection lost")
	}
	return r.fakeRepo.Create(ctx, p)
}

func (r *failingRepo) List(ctx context.Context, f policies.ListFilters) ([]policydomain.Policy, error) {
	if r.failOn == "list" {
		return nil, errors.New("db connection lost")
	}
	return r.fakeRepo.List(ctx, f)
}

func (r *failingRepo) GetByID(ctx context.Context, id uuid.UUID) (policydomain.Policy, error) {
	if r.failOn == "get" {
		return policydomain.Policy{}, errors.New("db connection lost")
	}
	return r.fakeRepo.GetByID(ctx, id)
}

func (r *failingRepo) Update(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	if r.failOn == "update" {
		return policydomain.Policy{}, errors.New("db connection lost")
	}
	return r.fakeRepo.Update(ctx, p)
}

func (r *failingRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	if r.failOn == "delete" {
		return errors.New("db connection lost")
	}
	return r.fakeRepo.DeleteByID(ctx, id)
}

func (r *failingRepo) ArchiveByID(ctx context.Context, id uuid.UUID) error {
	if r.failOn == "archive" {
		return errors.New("db connection lost")
	}
	return r.fakeRepo.ArchiveByID(ctx, id)
}

func (r *failingRepo) RestoreByID(ctx context.Context, id uuid.UUID) error {
	if r.failOn == "restore" {
		return errors.New("db connection lost")
	}
	return r.fakeRepo.RestoreByID(ctx, id)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupPolicyMux() *http.ServeMux {
	uc := policies.NewUsecases(newFakeRepo())
	mux := http.NewServeMux()
	policies.NewHandler(uc).Register(mux)
	return mux
}

func setupPolicyMuxWithRepo(repo policies.Repository) *http.ServeMux {
	uc := policies.NewUsecases(repo)
	mux := http.NewServeMux()
	policies.NewHandler(uc).Register(mux)
	return mux
}

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

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v, body: %s", err, rec.Body.String())
	}
}

// createPolicy crea una policy via HTTP y devuelve el response DTO.
func createPolicy(t *testing.T, mux *http.ServeMux, body string) policydto.PolicyResponse {
	t.Helper()
	rec := doRequest(t, mux, http.MethodPost, "/v1/policies", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create helper: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp policydto.PolicyResponse
	decodeJSON(t, rec, &resp)
	return resp
}

// decodeErrorResponse decodifica un error response JSON.
func decodeErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decodeJSON(t, rec, &errResp)
	return errResp.Code, errResp.Message
}

// decodeListResponse decodifica un list response con data array.
func decodeListResponse(t *testing.T, rec *httptest.ResponseRecorder) []policydto.PolicyResponse {
	t.Helper()
	var resp struct {
		Data []policydto.PolicyResponse `json:"data"`
	}
	decodeJSON(t, rec, &resp)
	return resp.Data
}

// ---------------------------------------------------------------------------
// Tests: CRUD happy paths
// ---------------------------------------------------------------------------

func TestPolicyCRUDLifecycle(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	// Create — 201
	rec := doRequest(t, mux, http.MethodPost, "/v1/policies",
		`{"name":"test-policy","expression":"true","effect":"allow","priority":10,"enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created policydto.PolicyResponse
	decodeJSON(t, rec, &created)
	policyID := created.ID

	// Get — 200
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies/"+policyID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}

	// List — 200
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Update — 200
	rec = doRequest(t, mux, http.MethodPatch, "/v1/policies/"+policyID, `{"name":"updated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Archive — 204
	rec = doRequest(t, mux, http.MethodPost, "/v1/policies/"+policyID+"/archive", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("archive: expected 204, got %d", rec.Code)
	}

	// Archive idempotente
	rec = doRequest(t, mux, http.MethodPost, "/v1/policies/"+policyID+"/archive", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("archive idempotent: expected 204, got %d", rec.Code)
	}

	// List excluye archivados
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data := decodeListResponse(t, rec)
	if len(data) != 0 {
		t.Fatalf("list: expected 0 active, got %d", len(data))
	}

	// List con archived=true
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies?archived=true", "")
	data = decodeListResponse(t, rec)
	if len(data) != 1 {
		t.Fatalf("list archived: expected 1, got %d", len(data))
	}

	// Restore — 204
	rec = doRequest(t, mux, http.MethodPost, "/v1/policies/"+policyID+"/restore", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("restore: expected 204, got %d", rec.Code)
	}

	// Delete (hard) — 204
	rec = doRequest(t, mux, http.MethodDelete, "/v1/policies/"+policyID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}

	// Get despues de delete — 404
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies/"+policyID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", rec.Code)
	}
}

// TestCreatePolicyResponseFields verifica que todos los campos del DTO se mapean correctamente.
func TestCreatePolicyResponseFields(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	body := `{
		"name":"full-policy",
		"description":"politica completa de test",
		"expression":"request.action_type == 'payment'",
		"effect":"deny",
		"risk_override":"high",
		"priority":5,
		"enabled":true,
		"action_type":"payment",
		"target_system":"billing"
	}`
	resp := createPolicy(t, mux, body)

	// Verificar UUID valido
	if _, err := uuid.Parse(resp.ID); err != nil {
		t.Fatalf("id no es UUID valido: %s", resp.ID)
	}
	if resp.Name != "full-policy" {
		t.Errorf("name: expected full-policy, got %s", resp.Name)
	}
	if resp.Description != "politica completa de test" {
		t.Errorf("description: expected 'politica completa de test', got %s", resp.Description)
	}
	if resp.Expression != "request.action_type == 'payment'" {
		t.Errorf("expression: expected valid action_type expression, got %s", resp.Expression)
	}
	if resp.Effect != "deny" {
		t.Errorf("effect: expected deny, got %s", resp.Effect)
	}
	if resp.RiskOverride == nil || *resp.RiskOverride != "high" {
		t.Errorf("risk_override: expected 'high', got %v", resp.RiskOverride)
	}
	if resp.Priority != 5 {
		t.Errorf("priority: expected 5, got %d", resp.Priority)
	}
	if !resp.Enabled {
		t.Error("enabled: expected true")
	}
	if resp.ActionType == nil || *resp.ActionType != "payment" {
		t.Errorf("action_type: expected 'payment', got %v", resp.ActionType)
	}
	if resp.TargetSystem == nil || *resp.TargetSystem != "billing" {
		t.Errorf("target_system: expected 'billing', got %v", resp.TargetSystem)
	}
	if resp.Origin != "manual" {
		t.Errorf("origin: expected manual, got %s", resp.Origin)
	}
	// Mode por defecto es "enforced"
	if resp.Mode != "enforced" {
		t.Errorf("mode: expected enforced, got %s", resp.Mode)
	}
	if resp.ShadowHits != 0 {
		t.Errorf("shadow_hits: expected 0, got %d", resp.ShadowHits)
	}
	if resp.ArchivedAt != nil {
		t.Errorf("archived_at: expected nil, got %v", resp.ArchivedAt)
	}
	// Verificar timestamps validos
	if _, err := time.Parse(time.RFC3339, resp.CreatedAt); err != nil {
		t.Errorf("created_at: formato invalido: %s", resp.CreatedAt)
	}
	if _, err := time.Parse(time.RFC3339, resp.UpdatedAt); err != nil {
		t.Errorf("updated_at: formato invalido: %s", resp.UpdatedAt)
	}
}

// TestCreatePolicyDefaultMode verifica que sin mode se asigna "enforced".
func TestCreatePolicyDefaultMode(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	resp := createPolicy(t, mux,
		`{"name":"no-mode","expression":"true","effect":"allow","priority":1,"enabled":true}`)
	if resp.Mode != "enforced" {
		t.Errorf("mode: expected enforced, got %s", resp.Mode)
	}
}

// TestCreatePolicyDisabled verifica crear una policy deshabilitada.
func TestCreatePolicyDisabled(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	resp := createPolicy(t, mux,
		`{"name":"disabled","expression":"true","effect":"allow","priority":1,"enabled":false}`)
	if resp.Enabled {
		t.Error("enabled: expected false")
	}
}

// ---------------------------------------------------------------------------
// Tests: validacion en create
// ---------------------------------------------------------------------------

func TestPolicyValidation(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{
			name:   "empty body",
			body:   `{}`,
			status: http.StatusBadRequest,
			code:   "VALIDATION",
		},
		{
			name:   "missing expression",
			body:   `{"name":"x","effect":"allow"}`,
			status: http.StatusBadRequest,
			code:   "VALIDATION",
		},
		{
			name:   "missing name",
			body:   `{"expression":"true","effect":"allow"}`,
			status: http.StatusBadRequest,
			code:   "VALIDATION",
		},
		{
			name:   "missing effect",
			body:   `{"name":"x","expression":"true"}`,
			status: http.StatusBadRequest,
			code:   "VALIDATION",
		},
		{
			name:   "invalid json",
			body:   `{bad`,
			status: http.StatusBadRequest,
			code:   "VALIDATION",
		},
		{
			name:   "invalid effect value",
			body:   `{"name":"x","expression":"true","effect":"reject"}`,
			status: http.StatusBadRequest,
			code:   "VALIDATION",
		},
		{
			name:   "expression too long",
			body:   `{"name":"x","expression":"` + strings.Repeat("a", 5001) + `","effect":"allow"}`,
			status: http.StatusBadRequest,
			code:   "VALIDATION",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doRequest(t, mux, http.MethodPost, "/v1/policies", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("expected %d, got %d: %s", tt.status, rec.Code, rec.Body.String())
			}
			errCode, _ := decodeErrorResponse(t, rec)
			if errCode != tt.code {
				t.Errorf("error code: expected %s, got %s", tt.code, errCode)
			}
		})
	}
}

// TestPolicyValidationEffectValues verifica los tres effects validos.
func TestPolicyValidationEffectValues(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()
	validEffects := []string{"allow", "deny", "require_approval"}
	for _, effect := range validEffects {
		t.Run(effect, func(t *testing.T) {
			t.Parallel()
			body := `{"name":"eff-` + effect + `","expression":"true","effect":"` + effect + `","priority":1,"enabled":true}`
			rec := doRequest(t, mux, http.MethodPost, "/v1/policies", body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201 for effect %s, got %d: %s", effect, rec.Code, rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: not found
// ---------------------------------------------------------------------------

func TestPolicyNotFound(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()
	fakeID := "00000000-0000-0000-0000-000000000000"
	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"get", http.MethodGet, "/v1/policies/" + fakeID, http.StatusNotFound},
		{"delete", http.MethodDelete, "/v1/policies/" + fakeID, http.StatusNotFound},
		{"archive", http.MethodPost, "/v1/policies/" + fakeID + "/archive", http.StatusNotFound},
		{"restore", http.MethodPost, "/v1/policies/" + fakeID + "/restore", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doRequest(t, mux, tt.method, tt.path, "")
			if rec.Code != tt.status {
				t.Fatalf("expected %d, got %d: %s", tt.status, rec.Code, rec.Body.String())
			}
			errCode, _ := decodeErrorResponse(t, rec)
			if errCode != "NOT_FOUND" {
				t.Errorf("error code: expected NOT_FOUND, got %s", errCode)
			}
		})
	}
}

// TestPolicyUpdateNotFound verifica 404 al hacer PATCH sobre policy inexistente.
func TestPolicyUpdateNotFound(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()
	fakeID := "00000000-0000-0000-0000-000000000000"

	rec := doRequest(t, mux, http.MethodPatch, "/v1/policies/"+fakeID, `{"name":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Tests: UUID invalido en path
// ---------------------------------------------------------------------------

func TestPolicyInvalidUUID(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"get invalid id", http.MethodGet, "/v1/policies/not-a-uuid"},
		{"update invalid id", http.MethodPatch, "/v1/policies/not-a-uuid"},
		{"delete invalid id", http.MethodDelete, "/v1/policies/not-a-uuid"},
		{"archive invalid id", http.MethodPost, "/v1/policies/not-a-uuid/archive"},
		{"restore invalid id", http.MethodPost, "/v1/policies/not-a-uuid/restore"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := ""
			if tt.method == http.MethodPatch {
				body = `{"name":"x"}`
			}
			rec := doRequest(t, mux, tt.method, tt.path, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			errCode, _ := decodeErrorResponse(t, rec)
			if errCode != "VALIDATION" {
				t.Errorf("error code: expected VALIDATION, got %s", errCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: update (PATCH) parcial
// ---------------------------------------------------------------------------

func TestPolicyUpdatePartialFields(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	// Crear policy base
	created := createPolicy(t, mux,
		`{"name":"base","expression":"true","effect":"allow","priority":10,"enabled":true}`)

	tests := []struct {
		name  string
		body  string
		check func(t *testing.T, resp policydto.PolicyResponse)
	}{
		{
			name: "update name only",
			body: `{"name":"renamed"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.Name != "renamed" {
					t.Errorf("name: expected renamed, got %s", resp.Name)
				}
				// Los demas campos no deben cambiar
				if resp.Expression != "true" {
					t.Errorf("expression should not change: got %s", resp.Expression)
				}
			},
		},
		{
			name: "update description",
			body: `{"description":"nueva desc"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.Description != "nueva desc" {
					t.Errorf("description: expected 'nueva desc', got %s", resp.Description)
				}
			},
		},
		{
			name: "update expression",
			body: `{"expression":"request.action_type == 'transfer'"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.Expression != "request.action_type == 'transfer'" {
					t.Errorf("expression: expected valid transfer expression, got %s", resp.Expression)
				}
			},
		},
		{
			name: "update effect",
			body: `{"effect":"deny"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.Effect != "deny" {
					t.Errorf("effect: expected deny, got %s", resp.Effect)
				}
			},
		},
		{
			name: "update priority",
			body: `{"priority":99}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.Priority != 99 {
					t.Errorf("priority: expected 99, got %d", resp.Priority)
				}
			},
		},
		{
			name: "update enabled to false",
			body: `{"enabled":false}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.Enabled {
					t.Error("enabled: expected false")
				}
			},
		},
		{
			name: "update mode to shadow",
			body: `{"mode":"shadow"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.Mode != "shadow" {
					t.Errorf("mode: expected shadow, got %s", resp.Mode)
				}
			},
		},
		{
			name: "update risk_override",
			body: `{"risk_override":"high"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.RiskOverride == nil || *resp.RiskOverride != "high" {
					t.Errorf("risk_override: expected high, got %v", resp.RiskOverride)
				}
			},
		},
		{
			name: "update action_type",
			body: `{"action_type":"transfer"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.ActionType == nil || *resp.ActionType != "transfer" {
					t.Errorf("action_type: expected transfer, got %v", resp.ActionType)
				}
			},
		},
		{
			name: "update target_system",
			body: `{"target_system":"gateway"}`,
			check: func(t *testing.T, resp policydto.PolicyResponse) {
				if resp.TargetSystem == nil || *resp.TargetSystem != "gateway" {
					t.Errorf("target_system: expected gateway, got %v", resp.TargetSystem)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doRequest(t, mux, http.MethodPatch, "/v1/policies/"+created.ID, tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			var resp policydto.PolicyResponse
			decodeJSON(t, rec, &resp)
			tt.check(t, resp)
		})
	}
}

// TestPolicyUpdateInvalidJSON verifica 400 al enviar JSON invalido en PATCH.
func TestPolicyUpdateInvalidJSON(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	created := createPolicy(t, mux,
		`{"name":"to-update","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	rec := doRequest(t, mux, http.MethodPatch, "/v1/policies/"+created.ID, `{bad json}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Tests: archive y restore
// ---------------------------------------------------------------------------

// TestArchiveRestoreIdempotent verifica idempotencia de archive y restore.
func TestArchiveRestoreIdempotent(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	created := createPolicy(t, mux,
		`{"name":"idem","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	// Archive dos veces, ambas 204
	for i := 0; i < 2; i++ {
		rec := doRequest(t, mux, http.MethodPost, "/v1/policies/"+created.ID+"/archive", "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("archive #%d: expected 204, got %d", i+1, rec.Code)
		}
	}

	// Restore dos veces, ambas 204
	for i := 0; i < 2; i++ {
		rec := doRequest(t, mux, http.MethodPost, "/v1/policies/"+created.ID+"/restore", "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("restore #%d: expected 204, got %d", i+1, rec.Code)
		}
	}
}

// TestArchiveExcludesFromList verifica que las policies archivadas no aparecen en list normal.
func TestArchiveExcludesFromList(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	// Crear dos policies
	p1 := createPolicy(t, mux,
		`{"name":"active","expression":"true","effect":"allow","priority":1,"enabled":true}`)
	createPolicy(t, mux,
		`{"name":"to-archive","expression":"false","effect":"deny","priority":2,"enabled":true}`)

	// Verificar que ambas aparecen
	rec := doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data := decodeListResponse(t, rec)
	if len(data) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(data))
	}

	// Archivar la segunda
	doRequest(t, mux, http.MethodPost, "/v1/policies/"+data[1].ID+"/archive", "")

	// Solo queda una activa
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data = decodeListResponse(t, rec)
	if len(data) != 1 {
		t.Fatalf("expected 1 active, got %d", len(data))
	}
	if data[0].ID != p1.ID {
		t.Errorf("expected active policy to be %s, got %s", p1.ID, data[0].ID)
	}

	// Con archived=true se ven ambas
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies?archived=true", "")
	data = decodeListResponse(t, rec)
	if len(data) != 2 {
		t.Fatalf("expected 2 with archived=true, got %d", len(data))
	}
}

// TestRestoreReappearsInList verifica que una policy restaurada vuelve a aparecer.
func TestRestoreReappearsInList(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	created := createPolicy(t, mux,
		`{"name":"restore-me","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	// Archivar
	doRequest(t, mux, http.MethodPost, "/v1/policies/"+created.ID+"/archive", "")

	// Verificar que no aparece
	rec := doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data := decodeListResponse(t, rec)
	if len(data) != 0 {
		t.Fatalf("expected 0 after archive, got %d", len(data))
	}

	// Restaurar
	doRequest(t, mux, http.MethodPost, "/v1/policies/"+created.ID+"/restore", "")

	// Vuelve a aparecer
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data = decodeListResponse(t, rec)
	if len(data) != 1 {
		t.Fatalf("expected 1 after restore, got %d", len(data))
	}
}

// ---------------------------------------------------------------------------
// Tests: shadow mode
// ---------------------------------------------------------------------------

// TestCreateShadowPolicy verifica crear una policy en modo shadow.
func TestCreateShadowPolicy(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	resp := createPolicy(t, mux,
		`{"name":"shadow-pol","expression":"request.action_type == 'payment'","effect":"deny","priority":1,"enabled":true,"mode":"shadow"}`)

	if resp.Mode != "shadow" {
		t.Errorf("mode: expected shadow, got %s", resp.Mode)
	}
	if resp.ShadowHits != 0 {
		t.Errorf("shadow_hits: expected 0, got %d", resp.ShadowHits)
	}

	// Verificar que al obtenerla mantiene el modo
	rec := doRequest(t, mux, http.MethodGet, "/v1/policies/"+resp.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}
	var fetched policydto.PolicyResponse
	decodeJSON(t, rec, &fetched)
	if fetched.Mode != "shadow" {
		t.Errorf("fetched mode: expected shadow, got %s", fetched.Mode)
	}
}

// TestShadowModeChangeViaUpdate verifica cambiar de enforced a shadow via PATCH.
func TestShadowModeChangeViaUpdate(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	created := createPolicy(t, mux,
		`{"name":"will-shadow","expression":"true","effect":"allow","priority":1,"enabled":true}`)
	if created.Mode != "enforced" {
		t.Fatalf("initial mode: expected enforced, got %s", created.Mode)
	}

	// Cambiar a shadow
	rec := doRequest(t, mux, http.MethodPatch, "/v1/policies/"+created.ID, `{"mode":"shadow"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", rec.Code)
	}
	var updated policydto.PolicyResponse
	decodeJSON(t, rec, &updated)
	if updated.Mode != "shadow" {
		t.Errorf("updated mode: expected shadow, got %s", updated.Mode)
	}

	// Cambiar de vuelta a enforced
	rec = doRequest(t, mux, http.MethodPatch, "/v1/policies/"+created.ID, `{"mode":"enforced"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update back: expected 200, got %d", rec.Code)
	}
	decodeJSON(t, rec, &updated)
	if updated.Mode != "enforced" {
		t.Errorf("reverted mode: expected enforced, got %s", updated.Mode)
	}
}

// TestIncrementShadowHits verifica que el usecase IncrementShadowHits funciona
// a nivel unitario (no HTTP, ya que no hay endpoint directo).
// Se usa el fakeRepo directamente ya que IncrementShadowHits no esta en el port del handler.
// Nota: IncrementShadowHits esta solo en PostgresRepository, no en Repository interface.
// Este test verifica el flujo via usecases que ShadowHits se preserva en el dominio.
func TestShadowHitsPreservedInResponse(t *testing.T) {
	t.Parallel()

	// Crear repo y setear shadow_hits manualmente para verificar mapeo
	repo := newFakeRepo()
	mux := setupPolicyMuxWithRepo(repo)

	created := createPolicy(t, mux,
		`{"name":"shadow-hits-test","expression":"true","effect":"deny","priority":1,"enabled":true,"mode":"shadow"}`)

	// Simular incremento de shadow hits directamente en el repo
	id, err := uuid.Parse(created.ID)
	if err != nil {
		t.Fatalf("parse id: %v", err)
	}
	repo.mu.Lock()
	p := repo.byID[id]
	p.ShadowHits = 42
	repo.byID[id] = p
	repo.mu.Unlock()

	// Verificar que el GET devuelve el shadow_hits actualizado
	rec := doRequest(t, mux, http.MethodGet, "/v1/policies/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}
	var resp policydto.PolicyResponse
	decodeJSON(t, rec, &resp)
	if resp.ShadowHits != 42 {
		t.Errorf("shadow_hits: expected 42, got %d", resp.ShadowHits)
	}
}

// ---------------------------------------------------------------------------
// Tests: list ordering y filtros
// ---------------------------------------------------------------------------

// TestListPoliciesOrderByPriority verifica que list ordena por priority ascendente.
func TestListPoliciesOrderByPriority(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	// Crear en orden inverso de prioridad
	createPolicy(t, mux,
		`{"name":"low-prio","expression":"true","effect":"allow","priority":100,"enabled":true}`)
	createPolicy(t, mux,
		`{"name":"high-prio","expression":"true","effect":"allow","priority":1,"enabled":true}`)
	createPolicy(t, mux,
		`{"name":"mid-prio","expression":"true","effect":"allow","priority":50,"enabled":true}`)

	rec := doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data := decodeListResponse(t, rec)
	if len(data) != 3 {
		t.Fatalf("expected 3 policies, got %d", len(data))
	}

	// Verificar orden: 1, 50, 100
	if data[0].Priority != 1 {
		t.Errorf("first priority: expected 1, got %d", data[0].Priority)
	}
	if data[1].Priority != 50 {
		t.Errorf("second priority: expected 50, got %d", data[1].Priority)
	}
	if data[2].Priority != 100 {
		t.Errorf("third priority: expected 100, got %d", data[2].Priority)
	}
}

// TestListEmptyReturnsEmptyArray verifica que list sin datos retorna array vacio, no null.
func TestListEmptyReturnsEmptyArray(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	rec := doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verificar que data es un array (no null)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dataRaw := string(raw["data"])
	if dataRaw == "null" {
		t.Error("data should be empty array [], not null")
	}
}

// ---------------------------------------------------------------------------
// Tests: delete y luego operaciones
// ---------------------------------------------------------------------------

// TestDeleteThenOperations verifica que despues de delete, todas las operaciones dan 404.
func TestDeleteThenOperations(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	created := createPolicy(t, mux,
		`{"name":"to-delete","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	// Delete
	rec := doRequest(t, mux, http.MethodDelete, "/v1/policies/"+created.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}

	// Todas las operaciones deben dar 404
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"get", http.MethodGet, "/v1/policies/" + created.ID, ""},
		{"update", http.MethodPatch, "/v1/policies/" + created.ID, `{"name":"x"}`},
		{"delete again", http.MethodDelete, "/v1/policies/" + created.ID, ""},
		{"archive", http.MethodPost, "/v1/policies/" + created.ID + "/archive", ""},
		{"restore", http.MethodPost, "/v1/policies/" + created.ID + "/restore", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doRequest(t, mux, tt.method, tt.path, tt.body)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: errores internos del repo (500)
// ---------------------------------------------------------------------------

// TestCreateInternalError verifica que un error interno del repo devuelve 500.
func TestCreateInternalError(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMuxWithRepo(newFailingRepo("create"))

	rec := doRequest(t, mux, http.MethodPost, "/v1/policies",
		`{"name":"fail","expression":"true","effect":"allow","priority":1,"enabled":true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	errCode, _ := decodeErrorResponse(t, rec)
	if errCode != "INTERNAL" {
		t.Errorf("error code: expected INTERNAL, got %s", errCode)
	}
}

// TestListInternalError verifica que un error interno en list devuelve 500.
func TestListInternalError(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMuxWithRepo(newFailingRepo("list"))

	rec := doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteInternalError verifica que un error interno en delete devuelve 500.
func TestDeleteInternalError(t *testing.T) {
	t.Parallel()
	repo := newFailingRepo("delete")
	mux := setupPolicyMuxWithRepo(repo)

	// Crear la policy primero (create no falla)
	created := createPolicy(t, mux,
		`{"name":"del-fail","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	// Activar fallo en delete
	rec := doRequest(t, mux, http.MethodDelete, "/v1/policies/"+created.ID, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestArchiveInternalError verifica que un error interno en archive devuelve 500.
func TestArchiveInternalError(t *testing.T) {
	t.Parallel()
	repo := newFailingRepo("archive")
	mux := setupPolicyMuxWithRepo(repo)

	created := createPolicy(t, mux,
		`{"name":"arch-fail","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	rec := doRequest(t, mux, http.MethodPost, "/v1/policies/"+created.ID+"/archive", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRestoreInternalError verifica que un error interno en restore devuelve 500.
func TestRestoreInternalError(t *testing.T) {
	t.Parallel()
	repo := newFailingRepo("restore")
	mux := setupPolicyMuxWithRepo(repo)

	created := createPolicy(t, mux,
		`{"name":"rest-fail","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	rec := doRequest(t, mux, http.MethodPost, "/v1/policies/"+created.ID+"/restore", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Tests: usecases unitarios
// ---------------------------------------------------------------------------

// TestUsecasesListActive verifica que ListActive solo retorna policies habilitadas.
func TestUsecasesListActive(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	// Crear policy habilitada
	enabled := policydomain.Policy{
		Name:       "enabled",
		Expression: "true",
		Effect:     "allow",
		Priority:   1,
		Enabled:    true,
	}
	_, err := uc.Create(ctx, enabled)
	if err != nil {
		t.Fatalf("create enabled: %v", err)
	}

	// Crear policy deshabilitada
	disabled := policydomain.Policy{
		Name:       "disabled",
		Expression: "true",
		Effect:     "deny",
		Priority:   2,
		Enabled:    false,
	}
	_, err = uc.Create(ctx, disabled)
	if err != nil {
		t.Fatalf("create disabled: %v", err)
	}

	// ListActive solo devuelve la habilitada
	active, err := uc.ListActive(ctx, nil)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active, got %d", len(active))
	}
	if active[0].Name != "enabled" {
		t.Errorf("expected 'enabled', got %s", active[0].Name)
	}
}

// TestUsecasesListActiveExcludesArchived verifica que ListActive excluye archivadas.
func TestUsecasesListActiveExcludesArchived(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	p := policydomain.Policy{
		Name:       "to-archive",
		Expression: "true",
		Effect:     "allow",
		Priority:   1,
		Enabled:    true,
	}
	created, err := uc.Create(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Archivar
	if err := uc.ArchiveByID(ctx, created.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// ListActive no la devuelve
	active, err := uc.ListActive(ctx, nil)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected 0 active, got %d", len(active))
	}
}

// TestUsecasesDeleteErrorWrapping verifica que DeleteByID wrappea el error correctamente.
func TestUsecasesDeleteErrorWrapping(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	err := uc.DeleteByID(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, policies.ErrNotFound) {
		t.Errorf("expected ErrNotFound wrapped, got %v", err)
	}
}

// TestUsecasesArchiveErrorWrapping verifica que ArchiveByID wrappea el error correctamente.
func TestUsecasesArchiveErrorWrapping(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	err := uc.ArchiveByID(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, policies.ErrNotFound) {
		t.Errorf("expected ErrNotFound wrapped, got %v", err)
	}
}

// TestUsecasesRestoreErrorWrapping verifica que RestoreByID wrappea el error correctamente.
func TestUsecasesRestoreErrorWrapping(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	err := uc.RestoreByID(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, policies.ErrNotFound) {
		t.Errorf("expected ErrNotFound wrapped, got %v", err)
	}
}

// TestUsecasesGetByID verifica el happy path de GetByID a nivel usecase.
func TestUsecasesGetByID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	p := policydomain.Policy{
		Name:       "get-test",
		Expression: "true",
		Effect:     "allow",
		Priority:   1,
		Enabled:    true,
	}
	created, err := uc.Create(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	fetched, err := uc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Name != "get-test" {
		t.Errorf("name: expected get-test, got %s", fetched.Name)
	}
	if fetched.ID != created.ID {
		t.Errorf("id: expected %s, got %s", created.ID, fetched.ID)
	}
}

// TestUsecasesUpdate verifica el happy path de Update a nivel usecase.
func TestUsecasesUpdate(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	p := policydomain.Policy{
		Name:       "update-test",
		Expression: "true",
		Effect:     "allow",
		Priority:   1,
		Enabled:    true,
	}
	created, err := uc.Create(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	created.Name = "updated-name"
	updated, err := uc.Update(ctx, created)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "updated-name" {
		t.Errorf("name: expected updated-name, got %s", updated.Name)
	}
}

// TestUsecasesList verifica el happy path de List a nivel usecase.
func TestUsecasesList(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := policies.NewUsecases(repo)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := uc.Create(ctx, policydomain.Policy{
			Name:       "list-test",
			Expression: "true",
			Effect:     "allow",
			Priority:   i,
			Enabled:    true,
		})
		if err != nil {
			t.Fatalf("create #%d: %v", i, err)
		}
	}

	all, err := uc.List(ctx, policies.ListFilters{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
}

// ---------------------------------------------------------------------------
// Tests: archived_at en respuesta
// ---------------------------------------------------------------------------

// TestArchivedAtInResponse verifica que archived_at aparece en el response despues de archivar.
func TestArchivedAtInResponse(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	mux := setupPolicyMuxWithRepo(repo)

	created := createPolicy(t, mux,
		`{"name":"arch-resp","expression":"true","effect":"allow","priority":1,"enabled":true}`)

	// Antes de archivar, archived_at es nil
	rec := doRequest(t, mux, http.MethodGet, "/v1/policies/"+created.ID, "")
	var resp policydto.PolicyResponse
	decodeJSON(t, rec, &resp)
	if resp.ArchivedAt != nil {
		t.Errorf("archived_at: expected nil before archive, got %v", resp.ArchivedAt)
	}

	// Archivar
	doRequest(t, mux, http.MethodPost, "/v1/policies/"+created.ID+"/archive", "")

	// Despues de archivar, archived_at tiene valor (consultar con archived=true via list)
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies?archived=true", "")
	data := decodeListResponse(t, rec)
	found := false
	for _, p := range data {
		if p.ID == created.ID {
			found = true
			if p.ArchivedAt == nil {
				t.Error("archived_at: expected non-nil after archive")
			} else if _, err := time.Parse(time.RFC3339, *p.ArchivedAt); err != nil {
				t.Errorf("archived_at: formato invalido: %s", *p.ArchivedAt)
			}
		}
	}
	if !found {
		t.Error("archived policy not found in list with archived=true")
	}
}

// ---------------------------------------------------------------------------
// Tests: multiples policies y filtros combinados
// ---------------------------------------------------------------------------

// TestListMultiplePoliciesMixed verifica list con mezcla de activas y archivadas.
func TestListMultiplePoliciesMixed(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	// Crear 3 policies
	p1 := createPolicy(t, mux,
		`{"name":"p1","expression":"true","effect":"allow","priority":1,"enabled":true}`)
	p2 := createPolicy(t, mux,
		`{"name":"p2","expression":"true","effect":"deny","priority":2,"enabled":true}`)
	createPolicy(t, mux,
		`{"name":"p3","expression":"true","effect":"allow","priority":3,"enabled":true}`)

	// Archivar p1 y p2
	doRequest(t, mux, http.MethodPost, "/v1/policies/"+p1.ID+"/archive", "")
	doRequest(t, mux, http.MethodPost, "/v1/policies/"+p2.ID+"/archive", "")

	// List normal: solo 1 activa
	rec := doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data := decodeListResponse(t, rec)
	if len(data) != 1 {
		t.Fatalf("expected 1 active, got %d", len(data))
	}
	if data[0].Name != "p3" {
		t.Errorf("expected p3, got %s", data[0].Name)
	}

	// List archived: todas (3)
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies?archived=true", "")
	data = decodeListResponse(t, rec)
	if len(data) != 3 {
		t.Fatalf("expected 3 with archived, got %d", len(data))
	}

	// Restaurar p1
	doRequest(t, mux, http.MethodPost, "/v1/policies/"+p1.ID+"/restore", "")

	// List normal: ahora 2 activas
	rec = doRequest(t, mux, http.MethodGet, "/v1/policies", "")
	data = decodeListResponse(t, rec)
	if len(data) != 2 {
		t.Fatalf("expected 2 active after restore, got %d", len(data))
	}
}

// ---------------------------------------------------------------------------
// Tests: content-type de respuestas
// ---------------------------------------------------------------------------

// TestResponseContentType verifica que las respuestas JSON tienen content-type correcto.
func TestResponseContentType(t *testing.T) {
	t.Parallel()
	mux := setupPolicyMux()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/v1/policies",
			`{"name":"ct","expression":"true","effect":"allow","priority":1,"enabled":true}`},
		{"list", http.MethodGet, "/v1/policies", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doRequest(t, mux, tt.method, tt.path, tt.body)
			ct := rec.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("content-type: expected application/json, got %s", ct)
			}
		})
	}
}
