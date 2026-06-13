package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	ai "github.com/devpablocristo/platform/kernels/ai/go"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/secrets"
)

// productMock simula un producto genérico que implementa el contrato
// envelope capability_execution.v1: publica su manifest en discovery_path y
// recibe ejecuciones en execute_path. Valida:
//   - El connector descubre capabilities sin código vertical.
//   - El envelope llega con el shape completo (operation/payload/workspace/
//     idempotency/nexus_request_id/actor/org).
//   - El bearer token proviene de la INSTALACIÓN (secret_ref env:), no de un
//     env global.
//   - El evidence del producto se mergea sin pisar la identidad canónica.
type productMock struct {
	server       *httptest.Server
	calls        []productRecordedCall
	expectToken  string
	executeReply map[string]any
}

type productRecordedCall struct {
	Method       string
	Path         string
	TenantID     string
	AxisScopes   string
	AuthHdr      string
	NexusReqHdr  string
	Body         string
	BodyEnvelope map[string]any
}

func newProductMock(t *testing.T) *productMock {
	t.Helper()
	m := &productMock{
		expectToken: "Bearer agro-installation-secret",
		executeReply: map[string]any{
			"status":       "success",
			"external_ref": "agro:exec:42",
			"result":       map[string]any{"items": []any{map[string]any{"field": "Lote Norte"}}},
			"evidence": map[string]any{
				"source_ref":   "agro.fields.list:42",
				"tenant_scope": "agro-tenant-789",
				"actor_id":     "spoofed-by-product",
			},
		},
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		call := productRecordedCall{
			Method:      r.Method,
			Path:        r.URL.Path,
			TenantID:    r.Header.Get("X-Tenant-Id"),
			AxisScopes:  r.Header.Get("X-Axis-Scopes"),
			AuthHdr:     r.Header.Get("Authorization"),
			NexusReqHdr: r.Header.Get("X-Nexus-Request-ID"),
			Body:        string(body),
		}
		if len(body) > 0 {
			_ = json.Unmarshal(body, &call.BodyEnvelope)
		}
		m.calls = append(m.calls, call)
		if call.AuthHdr != m.expectToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/capabilities":
			writeJSON(w, map[string]any{"items": []ai.CapabilityManifest{stubProductManifest()}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/capability-executions":
			writeJSON(w, m.executeReply)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *productMock) callsExcluding(paths ...string) []productRecordedCall {
	exclude := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		exclude[p] = struct{}{}
	}
	out := make([]productRecordedCall, 0, len(m.calls))
	for _, c := range m.calls {
		if _, skip := exclude[c.Path]; skip {
			continue
		}
		out = append(out, c)
	}
	return out
}

// fakeInstallationSource implementa ProductInstallationSource en memoria.
// Resuelve secretos vía el esquema env: real (EnvResolver con lookup fake)
// para cubrir el mismo path que products.Usecases en producción.
type fakeInstallationSource struct {
	installations map[string]products.Installation // org_id -> installation
	envValues     map[string]string                // env var -> secret value
}

func (f *fakeInstallationSource) ResolveInstallation(_ context.Context, orgID, productSurface string) (products.Installation, error) {
	installation, ok := f.installations[orgID]
	if !ok || installation.ProductSurface != productSurface {
		return products.Installation{}, products.ErrInstallationNotFound
	}
	if !installation.Enabled {
		return products.Installation{}, products.ErrInstallationDisabled
	}
	return installation, nil
}

func (f *fakeInstallationSource) ResolveInstallationSecret(ctx context.Context, orgID, productSurface string) (secrets.Secret, error) {
	installation, err := f.ResolveInstallation(ctx, orgID, productSurface)
	if err != nil {
		return secrets.Secret{}, err
	}
	resolver := secrets.NewEnvResolverWithLookup(func(name string) (string, bool) {
		value, ok := f.envValues[name]
		return value, ok
	})
	return resolver.Resolve(ctx, installation.SecretRef)
}

func (f *fakeInstallationSource) ListInstallationsByProduct(_ context.Context, productSurface string) ([]products.Installation, error) {
	out := make([]products.Installation, 0, len(f.installations))
	for _, installation := range f.installations {
		if installation.ProductSurface == productSurface {
			out = append(out, installation)
		}
	}
	return out, nil
}

func newProductInstallationSource(baseURL string) *fakeInstallationSource {
	return &fakeInstallationSource{
		installations: map[string]products.Installation{
			"tenant-A": {
				OrgID:            "tenant-A",
				ProductSurface:   "agro",
				ExternalTenantID: "agro-tenant-789",
				BaseURL:          baseURL,
				AuthMode:         products.AuthModeAPIKeyRef,
				SecretRef:        "env:AGRO_INSTALLATION_SECRET",
				Enabled:          true,
				Config: map[string]any{
					"connector_mode":  "envelope.v1",
					"required_scopes": []any{"agro:fields:read", "agro:workorders:write", "agro:fields:read"},
				},
			},
		},
		envValues: map[string]string{"AGRO_INSTALLATION_SECRET": "agro-installation-secret"},
	}
}

func newProductConnectorForTest(t *testing.T, m *productMock) (*ProductConnector, uuid.UUID) {
	t.Helper()
	source := newProductInstallationSource(m.server.URL)
	conn := NewProductConnector(NewProductClient("agro", source))
	return conn, uuid.New()
}

func stubProductManifest() ai.CapabilityManifest {
	return ai.CapabilityManifest{
		SchemaVersion: ai.CapabilityManifestSchemaVersion,
		ID:            "agro.core",
		Product:       "agro",
		Version:       "1.0.0",
		TenantScope:   ai.CapabilityTenantScopeOrg,
		Name:          "Agro Core",
		Description:   "Generic envelope-driven product capabilities.",
		Tools: []ai.CapabilityTool{
			{
				Name:        "agro.fields.list",
				Description: "Lists fields for the caller's tenant.",
				Mode:        ai.CapabilityModeRead,
				SideEffect:  false,
				RiskClass:   ai.CapabilityRiskLow,
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"workspace": map[string]any{"type": "object"}},
				},
				OutputSchema:       map[string]any{"type": "object"},
				EvidenceFields:     []string{"source_ref", "captured_at"},
				CapabilityExecutor: ai.CapabilityExecutor{ExecutorRef: "agro-backend.fields.list"},
			},
			{
				Name:        "agro.workorder.draft",
				Description: "Creates a governed work-order draft.",
				Mode:        ai.CapabilityModeWrite,
				SideEffect:  true,
				RiskClass:   ai.CapabilityRiskMedium,
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"workspace": map[string]any{"type": "object"}},
					"required":   []string{"workspace"},
				},
				OutputSchema:       map[string]any{"type": "object"},
				EvidenceFields:     []string{"source_ref", "captured_at", "tenant_scope"},
				CapabilityExecutor: ai.CapabilityExecutor{ExecutorRef: "agro-backend.workorder.draft"},
				Governance: &ai.CapabilityGovernance{
					RequiresApproval: true,
					ActionType:       "agent.capability.invoke",
					TargetSystem:     "agro",
				},
			},
		},
	}
}

// TestProductConnector_Discovery_PopulatesCapabilities valida que el
// connector genérico descubre el manifest del producto vía discovery_path y
// lo normaliza con capabilityFromTool (mismo puente que PontiConnector).
func TestProductConnector_Discovery_PopulatesCapabilities(t *testing.T) {
	t.Parallel()
	mock := newProductMock(t)
	conn, _ := newProductConnectorForTest(t, mock)

	if conn.ID() != "agro" || conn.Kind() != "agro" {
		t.Fatalf("expected connector id/kind agro, got %s/%s", conn.ID(), conn.Kind())
	}
	caps := conn.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities discovered, got %d", len(caps))
	}
	for _, c := range caps {
		switch c.Operation {
		case "agro.fields.list":
			if !c.ReadOnly || c.RequiresNexusApproval {
				t.Errorf("read capability misclassified: %+v", c)
			}
		case "agro.workorder.draft":
			if c.ReadOnly || !c.RequiresNexusApproval || c.NexusActionType != "agent.capability.invoke" {
				t.Errorf("write capability misclassified: %+v", c)
			}
			if !c.Idempotency.Required || c.IdempotencyMode != "required" {
				t.Errorf("write capability must require idempotency: %+v", c)
			}
		default:
			t.Errorf("unexpected capability %q", c.Operation)
		}
	}
}

// TestProductConnector_Execute_PostsEnvelope valida el shape completo del
// envelope capability_execution.v1 y la auth basada en installation.
func TestProductConnector_Execute_PostsEnvelope(t *testing.T) {
	t.Parallel()
	mock := newProductMock(t)
	conn, connID := newProductConnectorForTest(t, mock)
	taskID := uuid.New()
	nexusRequestID := uuid.New()

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:        connID,
		OrgID:              "tenant-A",
		ActorID:            "user-A",
		ActorType:          "human",
		CompanionPrincipal: "companion.employee_ai",
		OnBehalfOf:         "user-A",
		ServicePrincipal:   true,
		ProductSurface:     "agro",
		RunID:              "run-7",
		Operation:          "agro.workorder.draft",
		Payload:            json.RawMessage(`{"work_type":"spray","workspace":{"project_id":10,"campaign_id":3}}`),
		IdempotencyKey:     "idem-1",
		TaskID:             &taskID,
		NexusRequestID:     &nexusRequestID,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	if res.ExternalRef != "agro:exec:42" {
		t.Fatalf("expected product external_ref passthrough, got %q", res.ExternalRef)
	}

	calls := mock.callsExcluding("/api/v1/capabilities")
	if len(calls) != 1 {
		t.Fatalf("expected 1 execute call, got %d", len(calls))
	}
	got := calls[0]
	if got.Method != http.MethodPost || got.Path != "/api/v1/capability-executions" {
		t.Fatalf("unexpected execute call %s %s", got.Method, got.Path)
	}
	// Auth desde la instalación (secret_ref env:), no de un env global.
	if got.AuthHdr != "Bearer agro-installation-secret" {
		t.Errorf("expected installation-based bearer, got %q", got.AuthHdr)
	}
	if got.TenantID != "agro-tenant-789" {
		t.Errorf("expected external tenant header, got %q", got.TenantID)
	}
	if got.AxisScopes != "agro:fields:read agro:workorders:write" {
		t.Errorf("expected installation scopes propagated, got %q", got.AxisScopes)
	}
	if got.NexusReqHdr != nexusRequestID.String() {
		t.Errorf("expected X-Nexus-Request-ID propagated, got %q", got.NexusReqHdr)
	}

	envelope := got.BodyEnvelope
	if envelope["schema_version"] != "capability_execution.v1" {
		t.Errorf("expected schema_version capability_execution.v1, got %v", envelope["schema_version"])
	}
	if envelope["operation"] != "agro.workorder.draft" {
		t.Errorf("unexpected operation %v", envelope["operation"])
	}
	if envelope["executor_ref"] != "agro-backend.workorder.draft" {
		t.Errorf("expected executor_ref from manifest, got %v", envelope["executor_ref"])
	}
	if envelope["idempotency_key"] != "idem-1" || envelope["org_id"] != "tenant-A" {
		t.Errorf("unexpected idempotency/org: %v / %v", envelope["idempotency_key"], envelope["org_id"])
	}
	if envelope["task_id"] != taskID.String() || envelope["run_id"] != "run-7" {
		t.Errorf("unexpected task/run: %v / %v", envelope["task_id"], envelope["run_id"])
	}
	if envelope["nexus_request_id"] != nexusRequestID.String() {
		t.Errorf("expected nexus_request_id in body, got %v", envelope["nexus_request_id"])
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok || payload["work_type"] != "spray" {
		t.Errorf("expected payload passthrough, got %v", envelope["payload"])
	}
	workspace, ok := envelope["workspace"].(map[string]any)
	if !ok || workspace["project_id"] != float64(10) || workspace["campaign_id"] != float64(3) {
		t.Errorf("expected workspace extracted from payload, got %v", envelope["workspace"])
	}
	actor, ok := envelope["actor"].(map[string]any)
	if !ok {
		t.Fatalf("expected actor object, got %v", envelope["actor"])
	}
	if actor["actor_id"] != "user-A" || actor["actor_type"] != "human" ||
		actor["on_behalf_of"] != "user-A" || actor["product_surface"] != "agro" {
		t.Errorf("unexpected actor envelope: %v", actor)
	}
}

// TestProductConnector_Execute_MergesEvidencePassthrough valida que el
// evidence del producto se mergea con el evidence canónico de identidad sin
// poder pisar los campos de atribución de Axis.
func TestProductConnector_Execute_MergesEvidencePassthrough(t *testing.T) {
	t.Parallel()
	mock := newProductMock(t)
	conn, connID := newProductConnectorForTest(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:        connID,
		OrgID:              "tenant-A",
		ActorID:            "user-A",
		ActorType:          "human",
		CompanionPrincipal: "companion.employee_ai",
		OnBehalfOf:         "user-A",
		ProductSurface:     "agro",
		Operation:          "agro.fields.list",
		Payload:            json.RawMessage(`{"workspace":{"project_id":10}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	var evidence map[string]any
	if err := json.Unmarshal(res.EvidenceJSON, &evidence); err != nil {
		t.Fatal(err)
	}
	// Identidad canónica (mismo set que PontiConnector) gana siempre.
	if evidence["actor_id"] != "user-A" {
		t.Fatalf("product must not spoof actor_id, got %v", evidence["actor_id"])
	}
	if evidence["customer_org_id"] != "tenant-A" || evidence["product_surface"] != "agro" ||
		evidence["companion_principal"] != "companion.employee_ai" || evidence["on_behalf_of"] != "user-A" {
		t.Fatalf("expected canonical identity evidence, got %+v", evidence)
	}
	if evidence["capability_operation"] != "agro.fields.list" {
		t.Fatalf("expected capability_operation, got %v", evidence["capability_operation"])
	}
	// Workspace de Ola A presente.
	workspace, ok := evidence["workspace"].(map[string]any)
	if !ok || workspace["project_id"] != float64(10) {
		t.Fatalf("expected workspace evidence, got %v", evidence["workspace"])
	}
	// Passthrough del producto para claves no canónicas.
	if evidence["tenant_scope"] != "agro-tenant-789" {
		t.Fatalf("expected product evidence passthrough, got %+v", evidence)
	}
	var body map[string]any
	if err := json.Unmarshal(res.ResultJSON, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("expected product result passthrough, got %s", string(res.ResultJSON))
	}
}

// TestProductConnector_Execute_MapsProductFailure valida el mapeo de errores
// declarados por el producto (2xx con status=failure) y de errores HTTP.
func TestProductConnector_Execute_MapsProductFailure(t *testing.T) {
	t.Parallel()
	mock := newProductMock(t)
	mock.executeReply = map[string]any{
		"status": "failure",
		"error":  "workspace.project_id is required",
	}
	conn, connID := newProductConnectorForTest(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "agro.fields.list",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected failure, got %s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "workspace.project_id is required") {
		t.Fatalf("expected product error message, got %q", res.ErrorMessage)
	}
	if res.Retryable {
		t.Fatal("product-reported failure must not be marked retryable")
	}
}

func TestProductConnector_Execute_MapsHTTPError(t *testing.T) {
	t.Parallel()
	mock := newProductMock(t)
	conn, connID := newProductConnectorForTest(t, mock)
	// Después del boot discovery, el server empieza a responder 500.
	mock.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	})

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "agro.fields.list",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected failure on http 500, got %s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "500") {
		t.Fatalf("expected http error detail, got %q", res.ErrorMessage)
	}
	if !res.Retryable {
		t.Fatal("transport/http failure should be retryable")
	}
}

// TestProductConnector_Execute_FailsClosedWithoutInstallation valida que un
// org sin instalación activa no puede ejecutar: el producto nunca recibe la
// call y el resultado es failure con error claro.
func TestProductConnector_Execute_FailsClosedWithoutInstallation(t *testing.T) {
	t.Parallel()
	mock := newProductMock(t)
	conn, connID := newProductConnectorForTest(t, mock)
	discoveryCalls := len(mock.calls)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-B", // sin instalación
		ActorID:     "user-B",
		Operation:   "agro.fields.list",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected fail-closed failure, got %s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "installation") {
		t.Fatalf("expected installation error, got %q", res.ErrorMessage)
	}
	if len(mock.calls) != discoveryCalls {
		t.Fatalf("product must not be called without active installation, got %d extra calls", len(mock.calls)-discoveryCalls)
	}
}

// TestProductConnector_Execute_FailsClosedWithDisabledInstallation espeja el
// caso anterior para instalaciones deshabilitadas.
func TestProductConnector_Execute_FailsClosedWithDisabledInstallation(t *testing.T) {
	t.Parallel()
	mock := newProductMock(t)
	source := newProductInstallationSource(mock.server.URL)
	conn := NewProductConnector(NewProductClient("agro", source))
	if got := len(conn.Capabilities()); got != 2 {
		t.Fatalf("expected discovery before disable, got %d capabilities", got)
	}
	installation := source.installations["tenant-A"]
	installation.Enabled = false
	source.installations["tenant-A"] = installation
	discoveryCalls := len(mock.calls)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: uuid.New(),
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "agro.fields.list",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected fail-closed failure, got %s", res.Status)
	}
	if len(mock.calls) != discoveryCalls {
		t.Fatal("product must not be called with disabled installation")
	}
}

// TestProductConnector_ConfigurablePaths valida discovery_path y execute_path
// parametrizados por config de la instalación.
func TestProductConnector_ConfigurablePaths(t *testing.T) {
	t.Parallel()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/custom/manifest":
			writeJSON(w, map[string]any{"items": []ai.CapabilityManifest{stubProductManifest()}})
		case r.Method == http.MethodPost && r.URL.Path == "/custom/execute":
			writeJSON(w, map[string]any{"status": "success", "result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	source := newProductInstallationSource(srv.URL)
	installation := source.installations["tenant-A"]
	installation.AuthMode = products.AuthModeNone
	installation.SecretRef = ""
	installation.Config = map[string]any{
		"connector_mode": "envelope.v1",
		"discovery_path": "/custom/manifest",
		"execute_path":   "/custom/execute",
	}
	source.installations["tenant-A"] = installation
	conn := NewProductConnector(NewProductClient("agro", source))

	if got := len(conn.Capabilities()); got != 2 {
		t.Fatalf("expected discovery via custom path, got %d capabilities (calls: %v)", got, calls)
	}
	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: uuid.New(),
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "agro.fields.list",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	found := false
	for _, call := range calls {
		if call == "POST /custom/execute" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected execute via custom path, calls: %v", calls)
	}
}

// TestProductConnector_DiscoveryDownAtBoot espeja el comportamiento de
// PontiConnector: producto caído al boot deja el connector unavailable;
// Refresh posterior lo recupera.
func TestProductConnector_DiscoveryDownAtBoot(t *testing.T) {
	t.Parallel()
	var alive bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !alive {
			http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"items": []ai.CapabilityManifest{stubProductManifest()}})
	}))
	defer srv.Close()

	source := newProductInstallationSource(srv.URL)
	conn := NewProductConnector(NewProductClient("agro", source))
	if caps := conn.Capabilities(); len(caps) != 0 {
		t.Fatalf("expected 0 capabilities while product down, got %d", len(caps))
	}
	if err := conn.Validate(domain.ExecutionSpec{Operation: "agro.fields.list"}); err == nil ||
		!strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected unavailable validate error, got %v", err)
	}

	alive = true
	if err := conn.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh after recovery: %v", err)
	}
	if caps := conn.Capabilities(); len(caps) != 2 {
		t.Fatalf("expected 2 capabilities after refresh, got %d", len(caps))
	}
}
