package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/watchers/pymesclient"
)

// pymesMock simula la API de Pymes-core para validar el patrón de cada
// capability nueva agregada en Sprint 1 de la migración pymes/ai → Companion:
//   - El path correcto se construye en el cliente.
//   - El header X-API-Key se propaga.
//   - El query org_id se inyecta vía withOrgQuery.
//   - Para writes, el body llega como JSON pass-through.
//   - La respuesta cruda llega al ResultJSON sin alteración.
type pymesMock struct {
	server      *httptest.Server
	calls       []pymesCall
	expectKey   string
	respondJSON map[string]string // path → JSON body a devolver
}

type pymesCall struct {
	Method   string
	Path     string
	RawQuery string
	APIKey   string
	Body     string
}

func newPymesMock(t *testing.T) *pymesMock {
	t.Helper()
	m := &pymesMock{
		expectKey:   "pymes-test-key",
		respondJSON: map[string]string{},
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := ""
		if r.Body != nil {
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			body = string(buf[:n])
		}
		m.calls = append(m.calls, pymesCall{
			Method:   r.Method,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
			APIKey:   r.Header.Get("X-API-Key"),
			Body:     body,
		})
		if r.Header.Get("X-API-Key") != m.expectKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if resp, ok := m.respondJSON[r.URL.Path]; ok {
			_, _ = w.Write([]byte(resp))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(m.server.Close)
	return m
}

func newPymesConnectorTest(m *pymesMock) *PymesConnector {
	return NewPymesConnector(pymesclient.NewClient(m.server.URL, m.expectKey))
}

func execSpec(op string, payload map[string]any) domain.ExecutionSpec {
	raw, _ := json.Marshal(payload)
	return domain.ExecutionSpec{
		ConnectorID: uuid.New(),
		OrgID:       "tenant-A",
		ActorID:     "user-1",
		Operation:   op,
		Payload:     raw,
	}
}

func TestPymesCustomersSearch(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/customers"] = `{"items":[{"id":"c1","name":"Pérez"}],"total":1,"has_more":false}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.customers.search", map[string]any{
		"org_id": "tenant-A",
		"query":  "Pérez",
		"limit":  10,
	}))
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if res.Status != domain.ExecSuccess {
		t.Fatalf("expected success, got status=%s err=%s", res.Status, res.ErrorMessage)
	}
	if len(m.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.calls))
	}
	c := m.calls[0]
	if c.Path != "/v1/customers" {
		t.Errorf("path mismatch: %s", c.Path)
	}
	if !strings.Contains(c.RawQuery, "search=P") || !strings.Contains(c.RawQuery, "limit=10") || !strings.Contains(c.RawQuery, "org_id=tenant-A") {
		t.Errorf("query missing expected params: %s", c.RawQuery)
	}
	if c.APIKey != m.expectKey {
		t.Errorf("api key not propagated: %q", c.APIKey)
	}
	var parsed pymesclient.PagedItems
	if err := json.Unmarshal(res.ResultJSON, &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Total != 1 || len(parsed.Items) != 1 {
		t.Errorf("expected 1 item, got %+v", parsed)
	}
}

func TestPymesServicesSearch(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/services"] = `{"items":[{"id":"s1"}],"total":1,"has_more":false}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.services.search", map[string]any{
		"org_id": "tenant-A", "query": "corte",
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s msg=%s", err, res.Status, res.ErrorMessage)
	}
	if m.calls[0].Path != "/v1/services" {
		t.Errorf("path: %s", m.calls[0].Path)
	}
}

func TestPymesInventorySearch(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/inventory"] = `{"items":[],"total":0,"has_more":false}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.inventory.search", map[string]any{
		"org_id": "tenant-A", "query": "tornillo",
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s", err, res.Status)
	}
	if m.calls[0].Path != "/v1/inventory" {
		t.Errorf("path: %s", m.calls[0].Path)
	}
}

func TestPymesCashflowSummary(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/cashflow/summary"] = `{"income":1000,"expense":300,"net":700}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.cashflow.summary", map[string]any{
		"org_id": "tenant-A", "period": "month",
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s", err, res.Status)
	}
	if !strings.Contains(m.calls[0].RawQuery, "period=month") {
		t.Errorf("period not propagated: %s", m.calls[0].RawQuery)
	}
}

func TestPymesAccountsSummary(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/accounts/summary"] = `{"balance":5000}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.accounts.summary", map[string]any{
		"org_id": "tenant-A",
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s", err, res.Status)
	}
	if m.calls[0].Path != "/v1/accounts/summary" {
		t.Errorf("path: %s", m.calls[0].Path)
	}
}

func TestPymesSchedulingBook(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/scheduling/bookings"] = `{"id":"b1","start_at":"2026-06-01T10:00:00Z","end_at":"2026-06-01T10:30:00Z","service_id":"s1","branch_id":"br1"}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.scheduling.book", map[string]any{
		"org_id":         "tenant-A",
		"branch_id":      "br1",
		"service_id":     "s1",
		"start_at":       "2026-06-01T10:00:00Z",
		"customer_name":  "Pérez",
		"customer_phone": "+54911",
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s msg=%s", err, res.Status, res.ErrorMessage)
	}
	c := m.calls[0]
	if c.Method != "POST" || c.Path != "/v1/scheduling/bookings" {
		t.Errorf("expected POST /v1/scheduling/bookings, got %s %s", c.Method, c.Path)
	}
	if !strings.Contains(c.Body, `"service_id":"s1"`) {
		t.Errorf("body not passed through: %s", c.Body)
	}
}

func TestPymesQuotesCreate(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/quotes"] = `{"quote_id":"q1","total":1500}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.quotes.create", map[string]any{
		"org_id":   "tenant-A",
		"party_id": "p1",
		"items":    []map[string]any{{"product_id": "pr1", "qty": 2, "price": 750}},
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s msg=%s", err, res.Status, res.ErrorMessage)
	}
	if m.calls[0].Method != "POST" || m.calls[0].Path != "/v1/quotes" {
		t.Errorf("expected POST /v1/quotes, got %s %s", m.calls[0].Method, m.calls[0].Path)
	}
}

func TestPymesSalesCreate(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/sales"] = `{"sale_id":"sl1","total":500}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.sales.create", map[string]any{
		"org_id":   "tenant-A",
		"party_id": "p1",
		"items":    []map[string]any{{"product_id": "pr1", "qty": 1}},
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s msg=%s", err, res.Status, res.ErrorMessage)
	}
	if m.calls[0].Path != "/v1/sales" {
		t.Errorf("path: %s", m.calls[0].Path)
	}
}

func TestPymesPaymentsLink(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/sales/sl1/payments"] = `{"payment_id":"pay1"}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.payments.link", map[string]any{
		"org_id":  "tenant-A",
		"sale_id": "sl1",
		"amount":  500,
		"method":  "cash",
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s msg=%s", err, res.Status, res.ErrorMessage)
	}
	if m.calls[0].Path != "/v1/sales/sl1/payments" {
		t.Errorf("path: %s", m.calls[0].Path)
	}
}

func TestPymesProcurementRequestsCreate(t *testing.T) {
	m := newPymesMock(t)
	m.respondJSON["/v1/procurement-requests"] = `{"request_id":"pr1"}`
	conn := newPymesConnectorTest(m)

	res, err := conn.Execute(context.Background(), execSpec("pymes.procurement_requests.create", map[string]any{
		"org_id": "tenant-A",
		"items":  []map[string]any{{"sku": "X", "qty": 5}},
	}))
	if err != nil || res.Status != domain.ExecSuccess {
		t.Fatalf("unexpected: err=%v status=%s msg=%s", err, res.Status, res.ErrorMessage)
	}
	if m.calls[0].Path != "/v1/procurement-requests" {
		t.Errorf("path: %s", m.calls[0].Path)
	}
}

func TestPymesCapabilitiesIncludesAllSprint1(t *testing.T) {
	conn := NewPymesConnector(nil)
	got := map[string]bool{}
	for _, c := range conn.Capabilities() {
		got[c.Operation] = true
	}
	want := []string{
		"pymes.customers.search",
		"pymes.services.search",
		"pymes.inventory.search",
		"pymes.cashflow.summary",
		"pymes.accounts.summary",
		"pymes.scheduling.book",
		"pymes.quotes.create",
		"pymes.sales.create",
		"pymes.payments.link",
		"pymes.procurement_requests.create",
	}
	for _, op := range want {
		if !got[op] {
			t.Errorf("missing capability: %s", op)
		}
	}
}

func TestPymesWriteCapabilitiesRequireGovernance(t *testing.T) {
	conn := NewPymesConnector(nil)
	writes := map[string]bool{
		"pymes.scheduling.book":              true,
		"pymes.quotes.create":                true,
		"pymes.sales.create":                 true,
		"pymes.payments.link":                true,
		"pymes.procurement_requests.create":  true,
	}
	for _, c := range conn.Capabilities() {
		if !writes[c.Operation] {
			continue
		}
		if !c.RequiresGovernance {
			t.Errorf("%s should require governance", c.Operation)
		}
		if c.Mode != domain.CapabilityModeWrite {
			t.Errorf("%s should be Write mode", c.Operation)
		}
		if !c.SideEffect {
			t.Errorf("%s should have SideEffect=true", c.Operation)
		}
	}
}
