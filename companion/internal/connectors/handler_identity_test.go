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

func (f *fakeConnectorHandlerUsecase) ListConnectorsByLifecycle(context.Context, string) ([]domain.Connector, error) {
	if f.saved.ID == uuid.Nil {
		return nil, nil
	}
	return []domain.Connector{f.saved}, nil
}

func (f *fakeConnectorHandlerUsecase) ConnectorTypes() []domain.ConnectorType {
	return []domain.ConnectorType{{Kind: "medmory", Name: "Medmory", Status: "active"}}
}

func (f *fakeConnectorHandlerUsecase) GetConnector(context.Context, uuid.UUID) (domain.Connector, error) {
	return f.saved, nil
}

func (f *fakeConnectorHandlerUsecase) SaveConnector(_ context.Context, c domain.Connector) (domain.Connector, error) {
	c.ID = uuid.New()
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	if c.Status == "" {
		c.Status = "active"
	}
	f.saved = c
	return c, nil
}

func (f *fakeConnectorHandlerUsecase) ArchiveConnector(context.Context, uuid.UUID) (domain.Connector, error) {
	f.saved.Status = "archived"
	return f.saved, nil
}

func (f *fakeConnectorHandlerUsecase) TrashConnector(context.Context, uuid.UUID) (domain.Connector, error) {
	f.saved.Status = "trash"
	return f.saved, nil
}

func (f *fakeConnectorHandlerUsecase) RestoreConnector(context.Context, uuid.UUID) (domain.Connector, error) {
	f.saved.Status = "active"
	return f.saved, nil
}

func (f *fakeConnectorHandlerUsecase) TestConnector(context.Context, uuid.UUID) error {
	return nil
}

func (f *fakeConnectorHandlerUsecase) RefreshConnector(context.Context, uuid.UUID) registry.RefreshResult {
	return registry.RefreshResult{ConnectorID: f.saved.Kind, Refreshed: true}
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

func TestHandlerConnectorTypesAndLifecycle(t *testing.T) {
	t.Parallel()

	connectorID := uuid.New()
	uc := &fakeConnectorHandlerUsecase{saved: domain.Connector{
		ID:        connectorID,
		OrgID:     "local-dev-org",
		Name:      "Medmory",
		Kind:      "medmory",
		Enabled:   true,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/connectors/types", nil)
	req = withConnectorPrincipal(req, []string{scopeCompanionConnectorsAdmin})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected types 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind":"medmory"`) {
		t.Fatalf("expected medmory connector type, got %s", rec.Body.String())
	}

	for _, tc := range []struct {
		path   string
		status string
	}{
		{"/v1/connectors/" + connectorID.String() + "/archive", "archived"},
		{"/v1/connectors/" + connectorID.String() + "/trash", "trash"},
		{"/v1/connectors/" + connectorID.String() + "/restore", "active"},
	} {
		req = httptest.NewRequest(http.MethodPost, tc.path, nil)
		req = withConnectorPrincipal(req, []string{scopeCompanionConnectorsAdmin})
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected lifecycle 200 for %s, got %d: %s", tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"status":"`+tc.status+`"`) {
			t.Fatalf("expected status %s, got %s", tc.status, rec.Body.String())
		}
	}
}

func withConnectorPrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{OrgID: "local-dev-org", Actor: "axis-admin", Scopes: scopes, AuthMethod: "internal_jwt"}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
