package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	configdomain "github.com/devpablocristo/nexus/governance/internal/config/usecases/domain"
)

// --- Fakes ---

type fakeConfigRepo struct {
	store map[string][]byte
}

func newFakeRepo() *fakeConfigRepo {
	return &fakeConfigRepo{store: make(map[string][]byte)}
}

func (r *fakeConfigRepo) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := r.store[key]
	if !ok {
		return nil, context.DeadlineExceeded // simula "no encontrado"
	}
	return v, nil
}

func (r *fakeConfigRepo) Set(_ context.Context, key string, value []byte) error {
	r.store[key] = value
	return nil
}

// --- Helpers ---

func setupMux() *http.ServeMux {
	repo := newFakeRepo()
	uc := NewUsecases(repo)
	h := NewHandler(uc)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func doReq(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// --- Tests ---

func TestGetConfigReturnsDefaults(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodGet, "/v1/config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	// Verificar que tiene las 4 secciones (Nexus es AI-independent: sin sección "ai")
	for _, section := range []string{"risk", "approvals", "learning", "general"} {
		if _, ok := resp[section]; !ok {
			t.Errorf("falta sección %s en respuesta", section)
		}
	}
	if _, ok := resp["ai"]; ok {
		t.Errorf("Nexus es AI-independent: la sección 'ai' no debe estar en la respuesta")
	}
}

func TestGetConfigDefaultValues(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodGet, "/v1/config", "")

	var resp map[string]json.RawMessage
	json.NewDecoder(rec.Body).Decode(&resp)

	// Verificar un valor default conocido
	var risk map[string]json.RawMessage
	json.Unmarshal(resp["risk"], &risk)

	var thresholds map[string]float64
	json.Unmarshal(risk["thresholds"], &thresholds)

	if thresholds["deny"] != 2.0 {
		t.Errorf("esperaba deny=2.0, obtuvo %v", thresholds["deny"])
	}
	if thresholds["allow"] != 0.5 {
		t.Errorf("esperaba allow=0.5, obtuvo %v", thresholds["allow"])
	}
}

func TestUpdateConfigFullBody(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	// Obtener default, modificar, enviar
	rec := doReq(t, mux, http.MethodGet, "/v1/config", "")
	var cfg map[string]any
	json.NewDecoder(rec.Body).Decode(&cfg)

	// Modificar un valor
	risk := cfg["risk"].(map[string]any)
	thresholds := risk["thresholds"].(map[string]any)
	thresholds["deny"] = 3.0
	risk["thresholds"] = thresholds
	cfg["risk"] = risk

	body, _ := json.Marshal(cfg)
	rec2 := doReq(t, mux, http.MethodPatch, "/v1/config", string(body))
	if rec2.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec2.Code, rec2.Body.String())
	}

	// Verificar que persistió
	rec3 := doReq(t, mux, http.MethodGet, "/v1/config", "")
	var updated map[string]json.RawMessage
	json.NewDecoder(rec3.Body).Decode(&updated)

	var riskUpdated map[string]json.RawMessage
	json.Unmarshal(updated["risk"], &riskUpdated)
	var th map[string]float64
	json.Unmarshal(riskUpdated["thresholds"], &th)

	if th["deny"] != 3.0 {
		t.Errorf("esperaba deny=3.0 después de update, obtuvo %v", th["deny"])
	}
}

func TestUpdateConfigInvalidJSON(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodPatch, "/v1/config", "not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
	}
}

func TestUpdateConfigValidationError(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	// deny < require_approval debería fallar validación
	rec := doReq(t, mux, http.MethodGet, "/v1/config", "")
	var cfg map[string]any
	json.NewDecoder(rec.Body).Decode(&cfg)

	risk := cfg["risk"].(map[string]any)
	thresholds := risk["thresholds"].(map[string]any)
	thresholds["deny"] = 0.5              // menor que require_approval
	thresholds["require_approval"] = 1.5
	risk["thresholds"] = thresholds
	cfg["risk"] = risk

	body, _ := json.Marshal(cfg)
	rec2 := doReq(t, mux, http.MethodPatch, "/v1/config", string(body))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400 por validación, obtuvo %d: %s", rec2.Code, rec2.Body.String())
	}

	// Verificar que NO expone err.Error()
	var errResp map[string]string
	json.NewDecoder(rec2.Body).Decode(&errResp)
	if strings.Contains(errResp["message"], "threshold") {
		t.Error("la respuesta expone detalles internos del error")
	}
}

func TestUpdateSectionRisk(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	body := `{"thresholds":{"allow":0.3,"enhanced_log":0.8,"require_approval":1.2,"deny":1.8,"max_amplification":2.5},"action_types":{"high":["delete"],"medium":["deploy"]},"business_hours":{"start":8,"end":20},"frequency_thresholds":{"warning":5,"critical":15},"actor_thresholds":{"unknown":0,"new":5},"success_rate_thresholds":{"low":0.4,"moderate":0.7,"excellent":0.9},"amplifications":[],"sensitive_systems":["prod"]}`

	rec := doReq(t, mux, http.MethodPatch, "/v1/config/risk", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}

	// Verificar que solo risk cambió
	rec2 := doReq(t, mux, http.MethodGet, "/v1/config", "")
	var cfg map[string]json.RawMessage
	json.NewDecoder(rec2.Body).Decode(&cfg)

	var risk map[string]json.RawMessage
	json.Unmarshal(cfg["risk"], &risk)
	var th map[string]float64
	json.Unmarshal(risk["thresholds"], &th)

	if th["allow"] != 0.3 {
		t.Errorf("esperaba allow=0.3, obtuvo %v", th["allow"])
	}
}

func TestUpdateSectionInvalidSection(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodPatch, "/v1/config/nonexistent", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400 para sección inválida, obtuvo %d", rec.Code)
	}
}

func TestUpdateSectionInvalidJSON(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodPatch, "/v1/config/risk", "not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuvo %d", rec.Code)
	}
}

func TestResetConfig(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	// Modificar primero
	rec := doReq(t, mux, http.MethodGet, "/v1/config", "")
	var cfg map[string]any
	json.NewDecoder(rec.Body).Decode(&cfg)
	risk := cfg["risk"].(map[string]any)
	thresholds := risk["thresholds"].(map[string]any)
	thresholds["deny"] = 5.0
	risk["thresholds"] = thresholds
	cfg["risk"] = risk
	body, _ := json.Marshal(cfg)
	doReq(t, mux, http.MethodPatch, "/v1/config", string(body))

	// Resetear
	rec2 := doReq(t, mux, http.MethodPost, "/v1/config/reset", "")
	if rec2.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d", rec2.Code)
	}

	// Verificar que volvió al default
	rec3 := doReq(t, mux, http.MethodGet, "/v1/config", "")
	var resetCfg map[string]json.RawMessage
	json.NewDecoder(rec3.Body).Decode(&resetCfg)

	var riskReset map[string]json.RawMessage
	json.Unmarshal(resetCfg["risk"], &riskReset)
	var th map[string]float64
	json.Unmarshal(riskReset["thresholds"], &th)

	defaults := configdomain.DefaultSystemConfig()
	if th["deny"] != defaults.Risk.Thresholds.Deny {
		t.Errorf("esperaba deny=%v después de reset, obtuvo %v", defaults.Risk.Thresholds.Deny, th["deny"])
	}
}

func TestUpdateSectionApprovals(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodPatch, "/v1/config/approvals", `{"default_ttl_seconds":7200}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSectionLearning(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodPatch, "/v1/config/learning", `{"min_samples":25,"min_approval_rate":0.85,"max_requests":5000}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSectionGeneral(t *testing.T) {
	t.Parallel()
	mux := setupMux()

	rec := doReq(t, mux, http.MethodPatch, "/v1/config/general", `{"default_list_limit":100,"max_list_limit":500,"max_expression_length":3000,"max_idempotency_key_length":128,"idempotency_cache_ttl_seconds":43200,"max_body_size_bytes":524288}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
}
