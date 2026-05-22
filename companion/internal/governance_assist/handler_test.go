package governance_assist

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

type fakeContextualizer struct{}

func (fakeContextualizer) Explain(context.Context, string) (string, bool, error) {
	return "ok", false, nil
}

func TestHandlerProposeRequiresAdminScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(fakeProposer{}, fakeContextualizer{}).Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/governance-assist/propose", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionGovernanceAssistRead)

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

	req := httptest.NewRequest(http.MethodGet, "/v1/governance-assist/explain/request-1", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionGovernanceAssistRead)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
