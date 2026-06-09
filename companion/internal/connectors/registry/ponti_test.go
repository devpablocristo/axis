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
)

// pontiMock simula el backend de Ponti para validar:
//   - El adapter llama el path correcto.
//   - El header X-Tenant-Id se propaga.
//   - El bearer token se envía.
//   - La respuesta cruda llega al ResultJSON sin alteración.
//   - Si la URL de un tenant intentara consultar datos de otro, el mock
//     devuelve 403 — espejando lo que haría el Ponti real.
type pontiMock struct {
	server      *httptest.Server
	calls       []recordedCall
	tenantData  map[string]map[string]any // org_id -> insight_id -> insight
	expectToken string
}

type recordedCall struct {
	Method   string
	Path     string
	OrgID    string
	AuthHdr  string
	RawQuery string
	Body     string
}

func newPontiMock(t *testing.T) *pontiMock {
	t.Helper()
	m := &pontiMock{
		expectToken: "Bearer ponti-test-key",
		tenantData: map[string]map[string]any{
			"tenant-A": {
				"insight-A1": map[string]any{
					"id":         "insight-A1",
					"title":      "Stock negativo en producto X (tenant A)",
					"event_type": "ponti.stock.negative",
				},
			},
			"tenant-B": {
				"insight-B1": map[string]any{
					"id":         "insight-B1",
					"title":      "Confidential B-only insight",
					"event_type": "ponti.stock.negative",
				},
			},
		},
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		orgID := r.Header.Get("X-Tenant-Id")
		auth := r.Header.Get("Authorization")
		m.calls = append(m.calls, recordedCall{
			Method:   r.Method,
			Path:     r.URL.Path,
			OrgID:    orgID,
			AuthHdr:  auth,
			RawQuery: r.URL.RawQuery,
			Body:     string(body),
		})
		if auth != m.expectToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		// /api/v1/capabilities es metadata del producto: no requiere tenant
		// real (Ponti acepta cualquier X-Tenant-Id no vacío para auth). El
		// caller de Companion manda "companion-discovery" como sentinel.
		if r.URL.Path == "/api/v1/capabilities" {
			if orgID == "" {
				http.Error(w, `{"error":"missing tenant"}`, http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{"items": []ai.CapabilityManifest{stubPontiManifest()}})
			return
		}

		if orgID == "" {
			http.Error(w, `{"error":"missing tenant"}`, http.StatusBadRequest)
			return
		}
		tenantInsights, ok := m.tenantData[orgID]
		if !ok {
			http.Error(w, `{"error":"tenant not found"}`, http.StatusForbidden)
			return
		}

		switch {
		case r.URL.Path == "/api/v1/insights":
			items := make([]any, 0, len(tenantInsights))
			for _, v := range tenantInsights {
				items = append(items, v)
			}
			writeJSON(w, map[string]any{"items": items})
		case r.URL.Path == "/api/v1/insights/summary":
			writeJSON(w, map[string]any{
				"summary": map[string]any{
					"total":       len(tenantInsights),
					"by_status":   map[string]int{"notified": len(tenantInsights)},
					"by_severity": map[string]int{"high": len(tenantInsights)},
					"by_kind":     map[string]int{"stock_negative": len(tenantInsights)},
				},
				"evidence": map[string]any{
					"source_ref":   "ponti.businessinsights.summary",
					"captured_at":  "2026-05-06T12:00:00Z",
					"tenant_scope": orgID,
				},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/insights/") && strings.HasSuffix(r.URL.Path, "/explain"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/insights/"), "/explain")
			insight, ok := tenantInsights[id]
			if !ok {
				// Defense-in-depth: si el caller pidió un ID que no le
				// pertenece (cross-tenant probe), devolvemos 404 sin filtrar
				// info del otro tenant.
				http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]any{
				"insight": insight,
				"evidence": map[string]any{
					"source_ref":   "ponti.businessinsights.candidate:" + id,
					"tenant_scope": orgID,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/ai/actions/insight-resolve/prepare":
			writeDraftPreview(w, orgID, "ponti.insight.resolve.prepare")
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/ai/actions/workorder-draft/prepare":
			writeDraftPreview(w, orgID, "ponti.workorder.draft.prepare")
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/ai/actions/stock-adjustment/prepare":
			writeDraftPreview(w, orgID, "ponti.stock_adjustment.prepare")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

// callsExcluding devuelve las calls registradas que no coinciden con
// ninguno de los paths dados — útil para descontar la call de
// /api/v1/capabilities que hace discovery al boot.
func (m *pontiMock) callsExcluding(paths ...string) []recordedCall {
	exclude := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		exclude[p] = struct{}{}
	}
	out := make([]recordedCall, 0, len(m.calls))
	for _, c := range m.calls {
		if _, skip := exclude[c.Path]; skip {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (m *pontiMock) callPaths() []string {
	out := make([]string, 0, len(m.calls))
	for _, c := range m.calls {
		out = append(out, c.Path)
	}
	return out
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDraftPreview(w http.ResponseWriter, orgID, operation string) {
	writeJSON(w, map[string]any{
		"status":    "preview",
		"operation": operation,
		"proposal": map[string]any{
			"approval_required": true,
			"preview_only":      true,
			"write_performed":   false,
			"execution_allowed": false,
		},
		"evidence": map[string]any{
			"source_ref":        operation,
			"tenant_scope":      orgID,
			"approval_required": true,
		},
	})
}

func newPontiConnector(t *testing.T, m *pontiMock) (*PontiConnector, uuid.UUID) {
	t.Helper()
	client := NewPontiClient(m.server.URL, "ponti-test-key")
	conn := NewPontiConnector(client)
	return conn, uuid.New()
}

func TestPontiConnector_ListInsights_PropagatesTenant(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:        connID,
		OrgID:              "tenant-A",
		ActorID:            "user-A",
		ActorType:          "human",
		CompanionPrincipal: "companion.employee_ai",
		OnBehalfOf:         "user-A",
		ServicePrincipal:   true,
		ProductSurface:     "ponti",
		Operation:          "ponti.insights.list",
		Payload:            json.RawMessage(`{"limit":10}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}

	// Filtramos la call de discovery (/api/v1/capabilities) que el connector
	// hace al boot. Para este test importa solo la call de la operación.
	insightCalls := mock.callsExcluding("/api/v1/capabilities")
	if len(insightCalls) != 1 {
		t.Fatalf("expected 1 insight call, got %d (paths: %v)", len(insightCalls), mock.callPaths())
	}
	got := insightCalls[0]
	if got.OrgID != "tenant-A" {
		t.Errorf("expected X-Tenant-Id=tenant-A, got %q", got.OrgID)
	}
	if got.AuthHdr != "Bearer ponti-test-key" {
		t.Errorf("expected bearer token, got %q", got.AuthHdr)
	}
	if !strings.Contains(got.RawQuery, "limit=10") {
		t.Errorf("expected limit=10 in query, got %q", got.RawQuery)
	}
	// La respuesta del mock debe llegar verbatim.
	var body map[string]any
	if err := json.Unmarshal(res.ResultJSON, &body); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("expected items key, got %s", string(res.ResultJSON))
	}
	// Evidence pack obligatorio.
	if len(res.EvidenceJSON) == 0 {
		t.Fatal("expected evidence_json populated")
	}
	var evidence map[string]any
	if err := json.Unmarshal(res.EvidenceJSON, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["actor_id"] != "user-A" || evidence["companion_principal"] != "companion.employee_ai" || evidence["on_behalf_of"] != "user-A" {
		t.Fatalf("expected canonical identity evidence, got %+v", evidence)
	}
	if evidence["customer_org_id"] != "tenant-A" || evidence["product_surface"] != "ponti" {
		t.Fatalf("expected customer org/product evidence, got %+v", evidence)
	}
}

func TestPontiConnector_Summary_RespondsWithCounts(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.summary",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	var body map[string]any
	json.Unmarshal(res.ResultJSON, &body)
	if _, ok := body["summary"]; !ok {
		t.Fatalf("expected summary key, got %s", string(res.ResultJSON))
	}
}

func TestPontiConnector_Explain_RejectsCrossTenantProbe(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	// Tenant A intenta pedir explain de un insight que pertenece al tenant B.
	// El mock (espejando Ponti real) responde 404 en lugar de filtrar info.
	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "attacker",
		Operation:   "ponti.insights.explain",
		Payload:     json.RawMessage(`{"insight_id":"insight-B1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected failure (cross-tenant probe), got %s", res.Status)
	}
	if !strings.Contains(strings.ToLower(res.ErrorMessage), "not found") &&
		!strings.Contains(res.ErrorMessage, "404") {
		t.Errorf("expected 404 not_found error, got %q", res.ErrorMessage)
	}
	// Crítico: el ResultJSON del fallo no debe contener el title del tenant B.
	if strings.Contains(string(res.ResultJSON), "Confidential B-only insight") {
		t.Fatal("cross-tenant leakage: tenant A response contains tenant B data")
	}
	if strings.Contains(res.ErrorMessage, "Confidential B-only") {
		t.Fatal("cross-tenant leakage: error message contains tenant B data")
	}
}

func TestPontiConnector_Explain_HappyPath(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.explain",
		Payload:     json.RawMessage(`{"insight_id":"insight-A1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	var body map[string]any
	json.Unmarshal(res.ResultJSON, &body)
	insight, ok := body["insight"].(map[string]any)
	if !ok {
		t.Fatalf("expected insight key, got %s", string(res.ResultJSON))
	}
	if insight["id"] != "insight-A1" {
		t.Fatalf("expected insight-A1, got %v", insight["id"])
	}
}

func TestPontiConnector_Explain_RequiresInsightID(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connID,
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.explain",
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecFailure {
		t.Fatalf("expected failure when insight_id missing, got %s", res.Status)
	}
	if !strings.Contains(res.ErrorMessage, "insight_id is required") {
		t.Errorf("expected validation error, got %q", res.ErrorMessage)
	}
}

func TestPontiConnector_RejectsInvalidToken(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	// Token mal configurado: discovery al boot falla con 401, el connector
	// queda unavailable y rechaza Execute con error claro (no expone el
	// detalle del 401 al runtime — eso queda en el log warn del boot).
	client := NewPontiClient(mock.server.URL, "wrong-key")
	conn := NewPontiConnector(client)

	if got := conn.Capabilities(); len(got) != 0 {
		t.Fatalf("expected 0 capabilities when discovery fails, got %d", len(got))
	}

	_, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: uuid.New(),
		OrgID:       "tenant-A",
		ActorID:     "user-A",
		Operation:   "ponti.insights.list",
		Payload:     json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected error when connector is unavailable")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("expected unavailable error, got %q", err.Error())
	}
}

func TestPontiConnector_DraftAction_ForwardsRawPayload(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, connID := newPontiConnector(t, mock)

	payload := json.RawMessage(`{"project_id":10,"work_type":"spray","workspace":{"project_id":10}}`)
	res, err := conn.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connID,
		OrgID:          "tenant-A",
		ActorID:        "user-A",
		ProductSurface: "ponti",
		Operation:      "ponti.workorder.draft.prepare",
		Payload:        payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got %s err=%s", res.Status, res.ErrorMessage)
	}
	calls := mock.callsExcluding("/api/v1/capabilities")
	if len(calls) != 1 {
		t.Fatalf("expected 1 draft call, got %d (paths: %v)", len(calls), mock.callPaths())
	}
	got := calls[0]
	if got.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", got.Method)
	}
	if got.Path != "/api/v1/ai/actions/workorder-draft/prepare" {
		t.Fatalf("unexpected path %q", got.Path)
	}
	if got.OrgID != "tenant-A" {
		t.Fatalf("expected tenant-A, got %q", got.OrgID)
	}
	if got.Body != string(payload) {
		t.Fatalf("payload should be forwarded verbatim, got %s", got.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(res.ResultJSON, &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "preview" {
		t.Fatalf("expected preview response, got %s", string(res.ResultJSON))
	}
}

// stubPontiManifest replica el shape canónico que Ponti publica en
// /api/v1/capabilities. Sirve a los tests para asegurar que el discovery
// dinámico es lo que Companion consume; pasar ValidateCapabilityManifest
// es responsabilidad de Ponti (publicador).
func stubPontiManifest() ai.CapabilityManifest {
	roles := []string{"ponti.insights.viewer"}
	modules := []string{"ponti", "insights"}
	return ai.CapabilityManifest{
		SchemaVersion: ai.CapabilityManifestSchemaVersion,
		ID:            "ponti.insights",
		Product:       "ponti",
		Version:       "1.0.0",
		TenantScope:   ai.CapabilityTenantScopeOrg,
		Name:          "Ponti Insights",
		Description:   "Read-only access to agricultural insights computed for the caller's tenant.",
		Agents: []ai.CapabilityAgentDescriptor{
			{Name: "ponti_insights", Description: "Answers questions about active insights for the caller's tenant."},
		},
		Tools: []ai.CapabilityTool{
			{
				Name:        "ponti.insights.list",
				Description: "Lists insights for the caller's tenant with optional filters.",
				Mode:        ai.CapabilityModeRead,
				SideEffect:  false,
				RiskClass:   ai.CapabilityRiskLow,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit":            map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
						"include_resolved": map[string]any{"type": "boolean"},
					},
				},
				OutputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"items": map[string]any{"type": "array"}},
					"required":   []string{"items"},
				},
				EvidenceFields:     []string{"source_ref", "captured_at"},
				CapabilityAuthz:    ai.CapabilityAuthz{RequiredRoles: roles, RequiredModules: modules},
				CapabilityExecutor: ai.CapabilityExecutor{ExecutorRef: "ponti-backend.insights.list"},
			},
			{
				Name:        "ponti.insights.summary",
				Description: "Returns aggregate counts of insights by status and category for the tenant.",
				Mode:        ai.CapabilityModeRead,
				SideEffect:  false,
				RiskClass:   ai.CapabilityRiskLow,
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
				OutputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"summary": map[string]any{"type": "object"}, "evidence": map[string]any{"type": "object"}},
					"required":   []string{"summary", "evidence"},
				},
				EvidenceFields:     []string{"source_ref", "captured_at", "tenant_scope"},
				CapabilityAuthz:    ai.CapabilityAuthz{RequiredRoles: roles, RequiredModules: modules},
				CapabilityExecutor: ai.CapabilityExecutor{ExecutorRef: "ponti-backend.insights.summary"},
			},
			{
				Name:        "ponti.insights.explain",
				Description: "Returns an insight together with its provenance and evidence.",
				Mode:        ai.CapabilityModeRead,
				SideEffect:  false,
				RiskClass:   ai.CapabilityRiskLow,
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"insight_id": map[string]any{"type": "string", "format": "uuid"}},
					"required":   []string{"insight_id"},
				},
				OutputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"insight": map[string]any{"type": "object"}, "evidence": map[string]any{"type": "object"}},
					"required":   []string{"insight", "evidence"},
				},
				EvidenceFields:     []string{"source_ref", "captured_at", "first_seen", "event_type", "entity"},
				CapabilityAuthz:    ai.CapabilityAuthz{RequiredRoles: roles, RequiredModules: modules},
				CapabilityExecutor: ai.CapabilityExecutor{ExecutorRef: "ponti-backend.insights.explain"},
			},
			stubPontiDraftTool(
				"ponti.insight.resolve.prepare",
				"Prepares a proposed insight resolution for Nexus approval without resolving it directly.",
				"ponti-backend.actions.insight.resolve.prepare",
				map[string]any{
					"type":       "object",
					"properties": map[string]any{"insight_id": map[string]any{"type": "string", "format": "uuid"}},
					"required":   []string{"insight_id"},
				},
			),
			stubPontiDraftTool(
				"ponti.workorder.draft.prepare",
				"Prepares a work-order draft proposal for Nexus approval without publishing an order.",
				"ponti-backend.actions.workorder.draft.prepare",
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"project_id": map[string]any{"type": "integer", "minimum": 1},
						"work_type":  map[string]any{"type": "string", "minLength": 1},
					},
					"required": []string{"project_id", "work_type"},
				},
			),
			stubPontiDraftTool(
				"ponti.stock_adjustment.prepare",
				"Prepares a stock adjustment proposal for Nexus approval without applying inventory changes.",
				"ponti-backend.actions.stock_adjustment.prepare",
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"project_id":     map[string]any{"type": "integer", "minimum": 1},
						"supply_id":      map[string]any{"type": "integer", "minimum": 1},
						"quantity_delta": map[string]any{"type": "number"},
						"reason":         map[string]any{"type": "string", "minLength": 1},
					},
					"required": []string{"project_id", "supply_id", "quantity_delta", "reason"},
				},
			),
		},
	}
}

func stubPontiDraftTool(name, description, executorRef string, inputSchema map[string]any) ai.CapabilityTool {
	return ai.CapabilityTool{
		Name:        name,
		Description: description,
		Mode:        ai.CapabilityModeWrite,
		SideEffect:  true,
		RiskClass:   ai.CapabilityRiskMedium,
		InputSchema: inputSchema,
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"status": map[string]any{"type": "string"}, "proposal": map[string]any{"type": "object"}, "evidence": map[string]any{"type": "object"}},
			"required":   []string{"status", "proposal", "evidence"},
		},
		EvidenceFields:     []string{"source_ref", "captured_at", "tenant_scope", "workspace", "approval_required"},
		CapabilityAuthz:    ai.CapabilityAuthz{RequiredRoles: []string{"ponti.actions.preparer"}, RequiredModules: []string{"ponti", "actions"}},
		CapabilityExecutor: ai.CapabilityExecutor{ExecutorRef: executorRef},
		Governance: &ai.CapabilityGovernance{
			RequiresApproval: true,
			ActionType:       "agent.capability.invoke",
			TargetSystem:     "ponti",
		},
	}
}

// TestPontiConnector_Discovery_PopulatesCapabilities valida que el
// connector descubre el manifest desde /api/v1/capabilities al boot y lo
// expone como capabilities normalizadas (sin copia hardcoded).
func TestPontiConnector_Discovery_PopulatesCapabilities(t *testing.T) {
	t.Parallel()
	mock := newPontiMock(t)
	conn, _ := newPontiConnector(t, mock)

	caps := conn.Capabilities()
	if len(caps) != 6 {
		t.Fatalf("expected 6 capabilities discovered from Ponti, got %d", len(caps))
	}
	for _, c := range caps {
		if strings.HasSuffix(c.ID, ".prepare") {
			if c.ReadOnly || !c.SideEffect {
				t.Errorf("draft capability %q must be write side-effect", c.ID)
			}
			if !c.RequiresNexusApproval || c.NexusActionType != "agent.capability.invoke" {
				t.Errorf("draft capability %q must require Nexus action, got requires=%v type=%q", c.ID, c.RequiresNexusApproval, c.NexusActionType)
			}
			if !c.Idempotency.Required || c.IdempotencyMode != "required" {
				t.Errorf("draft capability %q must require idempotency, got %+v mode=%q", c.ID, c.Idempotency, c.IdempotencyMode)
			}
			if c.RiskClass != domain.RiskClassMedium {
				t.Errorf("draft capability %q must risk_class=medium, got %s", c.ID, c.RiskClass)
			}
			continue
		}
		if !c.ReadOnly {
			t.Errorf("read capability %q must be read_only=true", c.ID)
		}
		if c.RequiresNexusApproval {
			t.Errorf("read capability %q must NOT require nexus", c.ID)
		}
		if c.RiskClass != domain.RiskClassLow {
			t.Errorf("read capability %q must risk_class=low, got %s", c.ID, c.RiskClass)
		}
	}

	// El discovery debe haber pegado /api/v1/capabilities al menos una vez.
	hits := 0
	for _, call := range mock.calls {
		if call.Path == "/api/v1/capabilities" {
			hits++
		}
	}
	if hits == 0 {
		t.Fatal("expected connector to call /api/v1/capabilities at boot")
	}
}

// TestPontiConnector_Discovery_DownAtBoot valida que si Ponti no responde
// al boot, el connector queda unavailable pero no rompe Companion. Refresh
// posterior debe recuperarlo.
func TestPontiConnector_Discovery_DownAtBoot(t *testing.T) {
	t.Parallel()
	// Servidor que devuelve 503 hasta que cambiemos la flag.
	var alive bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !alive {
			http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/api/v1/capabilities" {
			writeJSON(w, map[string]any{"items": []ai.CapabilityManifest{stubPontiManifest()}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewPontiClient(srv.URL, "ponti-test-key")
	conn := NewPontiConnector(client)

	if caps := conn.Capabilities(); len(caps) != 0 {
		t.Fatalf("expected 0 capabilities while Ponti down, got %d", len(caps))
	}

	// Levantamos Ponti y forzamos refresh.
	alive = true
	if err := conn.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh after Ponti recovery: %v", err)
	}
	if caps := conn.Capabilities(); len(caps) != 6 {
		t.Fatalf("expected 6 capabilities after refresh, got %d", len(caps))
	}
}
