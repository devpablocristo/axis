package nexus_assist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeProposer struct{}

func (fakeProposer) AnalyzeAndPropose(context.Context, string) (int, int, []string, error) {
	return 1, 1, nil, nil
}

type fakeContextualizer struct {
	err error
}

func (f fakeContextualizer) Explain(context.Context, string, string, bool) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	return "ok", false, nil
}

func TestHandlerProposeRequiresAdminScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(fakeProposer{}, fakeContextualizer{}).Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/nexus-assist/propose", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionNexusAssistRead)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerExplainAllowsReadScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(fakeProposer{}, fakeContextualizer{}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/nexus-assist/explain/request-1", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionNexusAssistRead)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlerExplainCrossOrgMapsTo404 verifica que un request de otro org
// (el Contextualizer devuelve ErrRequestForbidden) se traduce a 404 — sin
// revelar la existencia del request ajeno.
func TestHandlerExplainCrossOrgMapsTo404(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(fakeProposer{}, fakeContextualizer{err: ErrRequestForbidden}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/nexus-assist/explain/request-2", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionNexusAssistRead)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
