package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devpablocristo/bff-v2/internal/adapters/companionhttp"
	"github.com/devpablocristo/bff-v2/internal/productedge"
	"github.com/gin-gonic/gin"
)

type authenticatorFunc func(context.Context, string) (productedge.MachineBinding, error)

func (f authenticatorFunc) AuthenticateAPIKey(ctx context.Context, key string) (productedge.MachineBinding, error) {
	return f(ctx, key)
}

func newLegacyTestHandler(bindings map[string]Binding, companionURL, secret string) *Handler {
	adapter := companionhttp.New(companionURL, secret, nil)
	return NewHandlerWithPorts(nil, bindings, productedge.Ports{
		StartAssist: adapter, GetAssistRun: adapter,
		PublishProductEvent: adapter, ResolveRouting: adapter,
	}, HandlerOptions{AllowLegacyBindings: true, RouteSigningSecret: secret})
}

func newGovernedTestHandler(
	authenticator APIKeyAuthenticator,
	companionURL, secret string,
) *Handler {
	adapter := companionhttp.New(companionURL, secret, nil)
	return NewHandlerWithPorts(authenticator, nil, productedge.Ports{
		StartAssist: adapter, GetAssistRun: adapter,
		PublishProductEvent: adapter, ResolveRouting: adapter,
	}, HandlerOptions{RouteSigningSecret: secret})
}

func TestParseBindings(t *testing.T) {
	raw := "medkey=8c3a623a|3e5a24e1|service:producta|producta|clinical-pool\n other=t2|v2|a2|productb"
	b := ParseBindings(raw)
	if len(b) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(b))
	}
	m := b["medkey"]
	if m.ProductID != "8c3a623a" || m.VirployeeID != "3e5a24e1" || m.ActorID != "service:producta" || m.ProductSurface != "producta" {
		t.Fatalf("unexpected binding: %+v", m)
	}
	if m.RoutingPoolID != "clinical-pool" {
		t.Fatalf("routing pool was not parsed: %+v", m)
	}
	if _, ok := b["nope"]; ok {
		t.Fatal("unknown key must not resolve")
	}
}

func TestAssistRunResolvesStableAssignmentAndPreservesRouteForPolling(t *testing.T) {
	var assistBody map[string]json.RawMessage
	var getPath string
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/virployee-routing:resolve":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"pool_id":"pool-clinical"`) || !strings.Contains(string(raw), `"subject_id":"patient-a"`) {
				t.Errorf("unexpected routing request: %s", raw)
			}
			_, _ = w.Write([]byte(`{"status":"assigned","created":false,"assignment":{"id":"assignment-1","virployee_id":"vp-routed"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/virployees/vp-routed/assist-runs":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &assistBody)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"run-routed","responsible_virployee_id":"vp-routed","status":"received"}`))
		case r.Method == http.MethodGet:
			getPath = r.URL.Path
			_, _ = w.Write([]byte(`{"id":"run-routed","responsible_virployee_id":"vp-routed","status":"done","output":{"summary":"ready"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer companion.Close()

	bindings := map[string]Binding{"routed-key": {
		ProductID: "product-1", VirployeeID: "vp-legacy", ActorID: "service:producta",
		ProductSurface: "producta", RoutingPoolID: "pool-clinical",
	}}
	handler := newLegacyTestHandler(bindings, companion.URL, "internal-token")
	router := gin.New()
	handler.Routes(router)
	body := `{"product_surface":"producta","assist_type":"clinical","subject_id":"patient-a","case_id":"case-1","input":{"question":"status"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(body))
	req.Header.Set("X-API-Key", "routed-key")
	req.Header.Set("Idempotency-Key", "patient-a-status")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if string(assistBody["assignment_id"]) != `"assignment-1"` || string(assistBody["case_id"]) != `"case-1"` {
		t.Fatalf("continuity scope was not forwarded: %+v", assistBody)
	}
	var pending assistRunResult
	if err := json.Unmarshal(rec.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pending.StatusURL, "route=") {
		t.Fatalf("routed polling URL must carry a signed route: %q", pending.StatusURL)
	}
	poll := httptest.NewRequest(http.MethodGet, pending.StatusURL, nil)
	poll.Header.Set("X-API-Key", "routed-key")
	pollRec := httptest.NewRecorder()
	router.ServeHTTP(pollRec, poll)
	if pollRec.Code != http.StatusOK || getPath != "/v1/virployees/vp-routed/assist-runs/run-routed" {
		t.Fatalf("poll did not preserve routed Virployee: code=%d path=%q body=%s", pollRec.Code, getPath, pollRec.Body.String())
	}
	tampered := httptest.NewRequest(http.MethodGet, "/v1/assist-runs/run-routed?route=tampered", nil)
	tampered.Header.Set("X-API-Key", "routed-key")
	tamperedRec := httptest.NewRecorder()
	router.ServeHTTP(tamperedRec, tampered)
	if tamperedRec.Code != http.StatusForbidden {
		t.Fatalf("tampered routed polling token must be rejected, got %d", tamperedRec.Code)
	}
}

func newTestEngine(companionURL string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	bindings := map[string]Binding{
		"secret-key": {ProductID: "product-1", VirployeeID: "vp-1", ActorID: "service:producta", ProductSurface: "producta"},
	}
	h := newLegacyTestHandler(bindings, companionURL, "internal-token")
	r := gin.New()
	h.Routes(r)
	return r
}

func TestAssistRunProxiesAndMapsResponse(t *testing.T) {
	var gotPath, gotToken, gotProduct, gotActor string
	var gotBody map[string]json.RawMessage
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Axis-Internal-Token")
		gotProduct = r.Header.Get("X-Product-ID")
		gotActor = r.Header.Get("X-Actor-ID")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"id":"run-1","status":"done","output":{"summary":"paciente estable"}}`))
	}))
	defer companion.Close()

	body := `{"owner_system":"producta","product_surface":"producta","assist_type":"clinical_diagnosis","subject_type":"repository","subject_id":"patient-a","repository_generation":"generation-a","input":{"schema_version":"producta.diagnosis_input.v1","documents":[{"key":"labs.txt","read_url":"https://x/labs","content_type":"text/plain"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(body))
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Idempotency-Key", "doc-123")
	rec := httptest.NewRecorder()
	newTestEngine(companion.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Forwarded to the bound virployee's assist endpoint with internal auth + context.
	if gotPath != "/v1/virployees/vp-1/assist-runs" || gotToken != "internal-token" || gotProduct != "product-1" || gotActor != "service:producta" {
		t.Fatalf("bad forward: path=%s token=%s product=%s actor=%s", gotPath, gotToken, gotProduct, gotActor)
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

func TestAssistRunNormalizesGenericCapabilityKeyWithoutProductAliases(t *testing.T) {
	var gotBody map[string]json.RawMessage
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"id":"run-capability","status":"done","capability_key":"clinical.timeline.build","capability_manifest_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","answer_status":"completed","citations":[],"output":{"status":"completed"}}`))
	}))
	defer companion.Close()

	body := `{"product_surface":"producta","capability_key":"Clinical.Timeline.Build","subject_id":"11111111-1111-4111-8111-111111111111","repository_generation":"g1","input":{"order":"desc"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(body))
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Idempotency-Key", "timeline-g1")
	rec := httptest.NewRecorder()
	newTestEngine(companion.URL).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Deprecation") != "" || string(gotBody["capability_key"]) != `"clinical.timeline.build"` {
		t.Fatalf("capability key was not generically normalized: header=%q body=%+v", rec.Header().Get("Deprecation"), gotBody)
	}
	if !strings.Contains(rec.Body.String(), `"capability_manifest_hash"`) || !strings.Contains(rec.Body.String(), `"answer_status":"completed"`) {
		t.Fatalf("clinical response fields were not propagated: %s", rec.Body.String())
	}
}

func TestAssistRunMapsNeedsHumanAsATraceableTerminalResult(t *testing.T) {
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"run-human","case_id":"case-human","responsible_virployee_id":"vp-owner","status":"needs_human","output":{"needs_human":true},"orchestration":{"state":"needs_human","pending_human_review":true}}`))
	}))
	defer companion.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"producta","input":{"documents":[]}}`))
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("Idempotency-Key", "manifest-needs-human")
	rec := httptest.NewRecorder()
	newTestEngine(companion.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected a terminal 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out assistRunResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "needs_human" || out.CaseID != "case-human" || out.ResponsibleVirployeeID != "vp-owner" {
		t.Fatalf("coordination trace was not preserved: %+v", out)
	}
	if !strings.Contains(string(out.Orchestration), "pending_human_review") {
		t.Fatalf("orchestration summary was not preserved: %s", out.Orchestration)
	}
}

func TestAssistRunReturns202AndStatusURLWhileDurableWorkContinues(t *testing.T) {
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"run-2","status":"received"}`))
	}))
	defer companion.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"producta","input":{"documents":[]}}`))
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
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"producta","input":{"documents":[]}}`))
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

	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"producta","input":{"documents":[]}}`))
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
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"product_surface":"producta","input":{}}`))
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
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "application/pdf") || !strings.Contains(rec.Body.String(), "application/dicom") || !strings.Contains(rec.Body.String(), "needs_human") || !strings.Contains(rec.Body.String(), "orchestration") || !strings.Contains(rec.Body.String(), `"catalog":"tenant"`) || strings.Contains(rec.Body.String(), "clinical.records.search") || strings.Contains(rec.Body.String(), "medmory.timeline") || strings.Contains(rec.Body.String(), `"status":"pending"`) {
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
	body := `{"product_surface":"productb","input":{}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(body))
	req.Header.Set("X-API-Key", "secret-key")
	newTestEngine("http://unused").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when product_surface mismatches the key, got %d", rec.Code)
	}
}

func TestAssistRunRequiresAssistWriteForPersistedCredential(t *testing.T) {
	authenticator := authenticatorFunc(func(context.Context, string) (productedge.MachineBinding, error) {
		return productedge.MachineBinding{
			Context: productedge.InvocationContext{
				ProductID: "product-1", ProductSurface: "producta", PrincipalID: "service:reader",
				PrincipalType: "service", Scopes: []string{"assist.read"},
			},
			VirployeeID: "vp-1",
		}, nil
	})
	handler := NewHandlerWithPorts(authenticator, nil, productedge.Ports{}, HandlerOptions{})
	router := gin.New()
	handler.Routes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/assist-runs", strings.NewReader(`{"input":{}}`))
	req.Header.Set("X-API-Key", "axis_pk_read_only_credential_value")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "assist.write") {
		t.Fatalf("expected assist.write rejection, got code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPersistedCredentialFailureNeverFallsBackToLegacyBinding(t *testing.T) {
	const key = "axis_pk_revoked_but_still_configured_legacy_key"
	authenticator := authenticatorFunc(func(context.Context, string) (productedge.MachineBinding, error) {
		return productedge.MachineBinding{}, errors.New("repository unavailable")
	})
	handler := NewHandlerWithPorts(
		authenticator,
		map[string]Binding{key: {
			ProductID: "product-legacy", VirployeeID: "vp-legacy", ActorID: "legacy",
			ProductSurface: "legacy",
		}},
		productedge.Ports{},
		HandlerOptions{AllowLegacyBindings: true},
	)
	router := gin.New()
	handler.Routes(router)
	req := httptest.NewRequest(http.MethodGet, "/v1/assist-capabilities", nil)
	req.Header.Set("X-API-Key", key)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("persisted auth failure must be authoritative, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProductEventTranslatesVersionAndPropagatesInvocationContext(t *testing.T) {
	var got map[string]json.RawMessage
	var headers http.Header
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("decode forwarded event: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"run_ids":[]}`))
	}))
	defer companion.Close()

	authenticator := authenticatorFunc(func(context.Context, string) (productedge.MachineBinding, error) {
		return productedge.MachineBinding{
			Context: productedge.InvocationContext{
				OrgID: "org-1", ProductID: "product-1", ProductSurface: "surface-a",
				IntegrationID: "integration-1", IntegrationRevision: 7,
				IntegrationHash: strings.Repeat("a", 64), PrincipalID: "service:event-source",
				PrincipalType: "service", Scopes: []string{"events.write"},
				AccessMode: productedge.AccessModeDirect,
			},
			AllowedEvents: []productedge.EventContract{{
				Type: "artifact.updated", Version: "v1", SchemaHash: strings.Repeat("b", 64),
			}},
		}, nil
	})
	handler := newGovernedTestHandler(authenticator, companion.URL, "internal-token")
	router := gin.New()
	handler.Routes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/product-events", strings.NewReader(
		`{"event_id":"11111111-1111-4111-8111-111111111111","event_type":"artifact.updated","version":"v1","payload":{"state":"ready"}}`,
	))
	req.Header.Set("X-API-Key", "axis_pk_event_source_credential")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected event acceptance, got %d: %s", rec.Code, rec.Body.String())
	}
	if string(got["event_version"]) != `"v1"` || got["version"] != nil {
		t.Fatalf("public version was not translated to event_version: %+v", got)
	}
	if headers.Get("X-Product-ID") != "product-1" || headers.Get("X-Axis-Product-ID") != "product-1" ||
		headers.Get("X-Org-ID") != "org-1" || headers.Get("X-Axis-Principal-Type") != "service" ||
		headers.Get("X-Axis-Integration-ID") != "integration-1" ||
		headers.Get("X-Axis-Integration-Version") != "7" ||
		headers.Get("X-Axis-Access-Mode") != "direct" {
		t.Fatalf("invocation context was not propagated: %+v", headers)
	}
}
