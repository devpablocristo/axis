package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/identityctx"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

type fakeConnectorHandlerUsecase struct {
	saved domain.Connector
}

func (f *fakeConnectorHandlerUsecase) ListConnectors(context.Context) ([]domain.Connector, error) {
	if f.saved.ID == uuid.Nil {
		return nil, nil
	}
	return []domain.Connector{f.saved}, nil
}

func (f *fakeConnectorHandlerUsecase) GetConnector(context.Context, uuid.UUID) (domain.Connector, error) {
	return f.saved, nil
}

func (f *fakeConnectorHandlerUsecase) SaveConnector(_ context.Context, c domain.Connector) (domain.Connector, error) {
	c.ID = uuid.New()
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	f.saved = c
	return c, nil
}

func (f *fakeConnectorHandlerUsecase) DeleteConnector(context.Context, uuid.UUID) error {
	return nil
}

func (f *fakeConnectorHandlerUsecase) Execute(context.Context, domain.ExecutionSpec) (domain.ExecutionResult, error) {
	return domain.ExecutionResult{}, nil
}

func (f *fakeConnectorHandlerUsecase) BuildActionBinding(context.Context, domain.ExecutionSpec) (map[string]any, string, error) {
	return nil, "", nil
}

func (f *fakeConnectorHandlerUsecase) ListExecutions(context.Context, uuid.UUID, int) ([]domain.ExecutionResult, error) {
	return nil, nil
}

func (f *fakeConnectorHandlerUsecase) Capabilities(domain.CapabilityFilter) []ConnectorCapabilities {
	return nil
}

func (f *fakeConnectorHandlerUsecase) CapabilityManifests(domain.CapabilityFilter) ([]capabilities.Manifest, error) {
	return nil, nil
}

func (f *fakeConnectorHandlerUsecase) RefreshConnectors(context.Context) []registry.RefreshResult {
	return nil
}

func TestHandlerSaveConnectorAllowsCrossOrgSeed(t *testing.T) {
	t.Parallel()

	uc := &fakeConnectorHandlerUsecase{}
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/connectors?org_id=medmory-org", strings.NewReader(`{
		"name":"Medmory",
		"kind":"medmory",
		"enabled":true,
		"config":{"managed_by":"seed"}
	}`))
	req = withConnectorPrincipal(req, []string{scopeCompanionConnectorsAdmin, scopeCompanionCrossOrg})
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if uc.saved.OrgID != "medmory-org" || uc.saved.Kind != "medmory" || !uc.saved.Enabled {
		t.Fatalf("unexpected saved connector: %+v", uc.saved)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["org_id"] != "medmory-org" {
		t.Fatalf("expected response org medmory-org, got %v", body["org_id"])
	}
}

func withConnectorPrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{OrgID: "local-dev-org", Actor: "axis-admin", Scopes: scopes, AuthMethod: "internal_jwt"}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
