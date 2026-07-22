package inbound

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseBindings(t *testing.T) {
	raw := "medkey=8c3a623a|3e5a24e1|service:medmory|medmory\n other=t2|v2|a2|ponti"
	b := ParseBindings(raw)
	if len(b) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(b))
	}
	m := b["medkey"]
	if m.TenantID != "8c3a623a" || m.VirployeeID != "3e5a24e1" || m.ActorID != "service:medmory" || m.ProductSurface != "medmory" {
		t.Fatalf("unexpected binding: %+v", m)
	}
	if _, ok := b["nope"]; ok {
		t.Fatal("unknown key must not resolve")
	}
}

func newTestEngine(companionURL string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	bindings := map[string]Binding{
		"secret-key": {TenantID: "tenant-1", VirployeeID: "vp-1", ActorID: "service:medmory", ProductSurface: "medmory"},
	}
	h := NewHandler(bindings, companionURL, "internal-token", nil)
	r := gin.New()
	h.Routes(r)
	return r
}

func TestAssistRunProxiesAndMapsResponse(t *testing.T) {
	var gotPath, gotToken, gotTenant, gotActor string
	var gotBody map[string]json.RawMessage
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Axis-Internal-Token")
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotActor = r.Header.Get("X-Actor-ID")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"id":"run-1","status":"done","output":{"summary":"paciente estable"}}`))
	}))
	defer companion.Close()

	body := `{"owner_system":"medmory","product_surface":"medmory","assist_type":"clinical_diagnosis","subject_type":"repository","subject_id":"patient-a","repository_generation":"generation-a","input":{"schema_version":"medmory.diagnosis_input.v1","documents":[{"key":"labs.txt","read_url":"https://x/labs","content_type":"text/plain"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(body))
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Idempotency-Key", "doc-123")
	rec := httptest.NewRecorder()
	newTestEngine(companion.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Forwarded to the bound virployee's assist endpoint with internal auth + context.
	if gotPath != "/v1/virployees/vp-1/assist-runs" || gotToken != "internal-token" || gotTenant != "tenant-1" || gotActor != "service:medmory" {
		t.Fatalf("bad forward: path=%s token=%s tenant=%s actor=%s", gotPath, gotToken, gotTenant, gotActor)
	}
	// Only the product's `input` object is forwarded as input_json.
	if in := string(gotBody["input_json"]); !strings.Contains(in, "documents") || !strings.Contains(in, "labs.txt") {
		t.Fatalf("input_json must be the product input object, got %s", in)
	}
	if idem := string(gotBody["idempotency_key"]); !strings.Contains(idem, "doc-123") {
		t.Fatalf("idempotency key must be forwarded, got %s", idem)
	}
	if string(gotBody["subject_id"]) != `"patient-a"` || string(gotBody["repository_generation"]) != `"generation-a"` || string(gotBody["assist_type"]) != `"clinical_diagnosis"` {
		t.Fatalf("stable artifact scope must be forwarded, got %+v", gotBody)
	}
	// Response mapped to the product contract (done -> completed).
	var out assistRunResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Status != "completed" || out.ID != "run-1" || !strings.Contains(string(out.Output), "paciente estable") {
		t.Fatalf("unexpected mapped response: %+v", out)
	}
}

func TestAssistRunReturns202AndStatusURLWhileDurableWorkContinues(t *testing.T) {
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"run-2","status":"received"}`))
	}))
	defer companion.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"medmory","input":{"documents":[]}}`))
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Idempotency-Key", "manifest-generation-2")
	rec := httptest.NewRecorder()
	newTestEngine(companion.URL).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var out assistRunResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ID != "run-2" || out.Status != "received" || out.StatusURL != "/v1/assist-runs/run-2" {
		t.Fatalf("unexpected async result: %+v", out)
	}
}

func TestAssistRunPreservesQuotaResponseAndRetryAfter(t *testing.T) {
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"quota_exceeded"}}`))
	}))
	defer companion.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"medmory","input":{"documents":[]}}`))
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Idempotency-Key", "quota-test")
	rec := httptest.NewRecorder()
	newTestEngine(companion.URL).ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "17" || !strings.Contains(rec.Body.String(), "quota_exceeded") {
		t.Fatalf("quota response was not preserved: code=%d retry=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestAssistRunPreferWaitObservesCompletion(t *testing.T) {
	gets := 0
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"run-3","status":"received"}`))
			return
		}
		gets++
		_, _ = w.Write([]byte(`{"id":"run-3","status":"done","output":{"summary":"ready"}}`))
	}))
	defer companion.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"medmory","input":{"documents":[]}}`))
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Idempotency-Key", "manifest-generation-3")
	req.Header.Set("Prefer", "wait=1")
	rec := httptest.NewRecorder()
	newTestEngine(companion.URL).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gets == 0 || !strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("wait did not observe completion: code=%d gets=%d body=%s", rec.Code, gets, rec.Body.String())
	}
}

func TestAssistRunRequiresStableIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"medmory","input":{}}`))
	req.Header.Set("X-API-Key", "secret-key")
	rec := httptest.NewRecorder()
	newTestEngine("http://unused").ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAssistCapabilitiesAreMachineAuthenticatedAndConservative(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/assist-capabilities", nil)
	req.Header.Set("X-API-Key", "secret-key")
	rec := httptest.NewRecorder()
	newTestEngine("http://unused").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "application/pdf") || !strings.Contains(rec.Body.String(), "application/dicom") || strings.Contains(rec.Body.String(), "pending") {
		t.Fatalf("unexpected capabilities: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssistRunRejectsUnknownKey(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{}`))
	req.Header.Set("X-API-Key", "wrong-key")
	newTestEngine("http://unused").ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unknown key, got %d", rec.Code)
	}
}

func TestAssistRunRejectsProductMismatch(t *testing.T) {
	rec := httptest.NewRecorder()
	body := `{"product_surface":"ponti","input":{}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(body))
	req.Header.Set("X-API-Key", "secret-key")
	newTestEngine("http://unused").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when product_surface mismatches the key, got %d", rec.Code)
	}
}
