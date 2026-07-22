package virployees

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	profiletemplatedomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

func TestHandlerCreateValidation(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees", strings.NewReader(`{"name":"","job_role_id":"`+uuid.NewString()+`","supervisor_user_id":"dev-user"}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreateReturnsAutonomy(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	body := `{"name":"Ops","job_role_id":"` + jobRoleID.String() + `","profile_template_id":"` + profileTemplateID.String() + `","supervisor_user_id":"dev-user","autonomy":"A2","employer_subject_id":"` + uuid.NewString() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		JobRoleID         string `json:"job_role_id"`
		ProfileTemplateID string `json:"profile_template_id"`
		Autonomy          string `json:"autonomy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if _, ok := raw["virployee_profile"]; ok {
		t.Fatalf("response must not include virployee_profile: %s", rec.Body.String())
	}
	if payload.JobRoleID != jobRoleID.String() {
		t.Fatalf("expected job_role_id %s, got %q", jobRoleID, payload.JobRoleID)
	}
	if payload.ProfileTemplateID != profileTemplateID.String() {
		t.Fatalf("expected profile_template_id %s, got %q", profileTemplateID, payload.ProfileTemplateID)
	}
	if payload.Autonomy != "A2" {
		t.Fatalf("expected autonomy A2, got %q", payload.Autonomy)
	}
}

func TestHandlerCreateRequiresPrimaryEmployer(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	body := `{"name":"Ops","job_role_id":"` + uuid.NewString() + `","profile_template_id":"` + uuid.NewString() + `","supervisor_user_id":"dev-user"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing employer to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerListsAutonomyLevels(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/autonomy-levels", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			Level                    string   `json:"level"`
			Name                     string   `json:"name"`
			AllowsRequiredAutonomies []string `json:"allows_required_autonomies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data) != 6 {
		t.Fatalf("expected 6 autonomy levels, got %d", len(payload.Data))
	}
	if payload.Data[0].Level != "A0" || payload.Data[0].Name != "Conversation" {
		t.Fatalf("unexpected first autonomy level: %+v", payload.Data[0])
	}
	var a3Autonomies []string
	for _, item := range payload.Data {
		if item.Level == "A3" {
			a3Autonomies = item.AllowsRequiredAutonomies
		}
	}
	if got, want := strings.Join(a3Autonomies, ","), "A0,A1,A2,A3"; got != want {
		t.Fatalf("expected A3 to allow %s, got %s", want, got)
	}
}

func TestHandlerRuntimeContext(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/"+id.String()+"/runtime-context", nil)
	req.Header.Set("X-Tenant-ID", "tenant-1")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastTenant != "tenant-1" {
		t.Fatalf("expected tenant-1, got %q", fake.lastTenant)
	}
	var payload struct {
		Virployee struct {
			ID               string `json:"id"`
			SupervisorUserID string `json:"supervisor_user_id"`
		} `json:"virployee"`
		JobRole struct {
			Name             string `json:"name"`
			Responsibilities []struct {
				Title           string `json:"title"`
				ExpectedOutcome string `json:"expected_outcome"`
			} `json:"responsibilities"`
			SuccessCriteria []struct {
				Title       string `json:"title"`
				TargetValue string `json:"target_value"`
			} `json:"success_criteria"`
		} `json:"job_role"`
		ProfileTemplate struct {
			SystemPrompt string `json:"system_prompt"`
			MaxAutonomy  string `json:"max_autonomy"`
		} `json:"profile_template"`
		Capabilities []struct {
			CapabilityKey    string `json:"capability_key"`
			RequiredAutonomy string `json:"required_autonomy"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Virployee.ID != id.String() || payload.Virployee.SupervisorUserID != "dev-user" {
		t.Fatalf("unexpected virployee payload: %+v", payload.Virployee)
	}
	if payload.JobRole.Name != "Receptionist" || payload.JobRole.Responsibilities == nil || payload.JobRole.SuccessCriteria == nil {
		t.Fatalf("unexpected job role payload: %+v", payload.JobRole)
	}
	if len(payload.JobRole.Responsibilities) != 1 || payload.JobRole.Responsibilities[0].ExpectedOutcome != "Visitors are welcomed" ||
		len(payload.JobRole.SuccessCriteria) != 1 || payload.JobRole.SuccessCriteria[0].TargetValue != "under 2 minutes" {
		t.Fatalf("job role definition missing from runtime context: %+v", payload.JobRole)
	}
	if payload.ProfileTemplate.SystemPrompt != "Be warm." || payload.ProfileTemplate.MaxAutonomy != "A2" {
		t.Fatalf("unexpected profile payload: %+v", payload.ProfileTemplate)
	}
	if len(payload.Capabilities) != 1 || payload.Capabilities[0].CapabilityKey != "calendar.events.create" || payload.Capabilities[0].RequiredAutonomy != "A2" {
		t.Fatalf("unexpected capabilities payload: %+v", payload.Capabilities)
	}
}

func TestHandlerDryRun(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees/"+id.String()+"/dry-run", strings.NewReader(`{"input":"Agendá una reunión para mañana"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastTenant != "tenant-1" {
		t.Fatalf("expected tenant-1, got %q", fake.lastTenant)
	}
	var payload struct {
		Input             string `json:"input"`
		RequiredAutonomy  string `json:"required_autonomy"`
		VirployeeAutonomy string `json:"virployee_autonomy"`
		Decision          string `json:"decision"`
		Intent            struct {
			Matched       bool     `json:"matched"`
			CapabilityKey string   `json:"capability_key"`
			Confidence    float64  `json:"confidence"`
			MatchedBy     []string `json:"matched_by"`
			Rules         []struct {
				Type   string `json:"type"`
				Target string `json:"target"`
				Value  string `json:"value"`
			} `json:"rules"`
		} `json:"intent"`
		RequiredCapability struct {
			CapabilityKey    string `json:"capability_key"`
			RequiredAutonomy string `json:"required_autonomy"`
			Matched          bool   `json:"matched"`
		} `json:"required_capability"`
		RuntimeContext struct {
			Capabilities []struct {
				CapabilityKey string `json:"capability_key"`
			} `json:"capabilities"`
		} `json:"runtime_context"`
		Draft struct {
			Status string `json:"status"`
			Action string `json:"action"`
			Kind   string `json:"kind"`
			Fields []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"fields"`
			MissingFields []struct {
				Key string `json:"key"`
			} `json:"missing_fields"`
		} `json:"draft"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Input != "Agendá una reunión para mañana" || payload.Decision != "allowed" {
		t.Fatalf("unexpected dry run payload: %+v", payload)
	}
	if payload.RequiredCapability.CapabilityKey != "calendar.events.create" || payload.RequiredCapability.RequiredAutonomy != "A2" || !payload.RequiredCapability.Matched {
		t.Fatalf("unexpected required capability: %+v", payload.RequiredCapability)
	}
	if !payload.Intent.Matched || payload.Intent.CapabilityKey != "calendar.events.create" || payload.Intent.Confidence != 0.9 {
		t.Fatalf("unexpected intent: %+v", payload.Intent)
	}
	if len(payload.Intent.MatchedBy) != 2 || len(payload.Intent.Rules) != 2 {
		t.Fatalf("unexpected intent evidence: %+v", payload.Intent)
	}
	if payload.RequiredAutonomy != "A2" || payload.VirployeeAutonomy != "A2" {
		t.Fatalf("unexpected autonomy values: required=%q virployee=%q", payload.RequiredAutonomy, payload.VirployeeAutonomy)
	}
	if len(payload.RuntimeContext.Capabilities) != 1 || payload.RuntimeContext.Capabilities[0].CapabilityKey != "calendar.events.create" {
		t.Fatalf("unexpected runtime context: %+v", payload.RuntimeContext)
	}
	if payload.Draft.Status != "needs_input" || payload.Draft.Action != "calendar.events.create" || payload.Draft.Kind != "calendar_event" {
		t.Fatalf("unexpected draft envelope: %+v", payload.Draft)
	}
	if len(payload.Draft.Fields) != 2 || payload.Draft.Fields[0].Key != "title" || payload.Draft.Fields[1].Key != "date_hint" {
		t.Fatalf("unexpected draft fields: %+v", payload.Draft.Fields)
	}
	if len(payload.Draft.MissingFields) != 2 || payload.Draft.MissingFields[0].Key != "time" || payload.Draft.MissingFields[1].Key != "attendees" {
		t.Fatalf("unexpected missing fields: %+v", payload.Draft.MissingFields)
	}
}

func TestHandlerExecutionGate(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	id := uuid.New()
	assistRunID := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees/"+id.String()+"/execution-gate", strings.NewReader(`{"input":"Agendá una reunión para mañana","assist_run_id":"`+assistRunID.String()+`","principal_type":"person","principal_id":"patient-a","confirmed_draft":{"action":"calendar.events.create","kind":"calendar_event","fields":[{"key":"title","value":"Reunión"},{"key":"date","value":"2026-07-12"},{"key":"time","value":"15:00"},{"key":"timezone","value":"America/Argentina/Buenos_Aires"},{"key":"duration_minutes","value":"60"},{"key":"attendees","value":"ana@example.com"}]}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Input  string `json:"input"`
		DryRun struct {
			Decision string `json:"decision"`
			Draft    struct {
				Status string `json:"status"`
			} `json:"draft"`
		} `json:"dry_run"`
		ExecutionGate struct {
			Decision                  string `json:"decision"`
			Mode                      string `json:"mode"`
			WillExecute               bool   `json:"will_execute"`
			RequiredExecutionAutonomy string `json:"required_execution_autonomy"`
			VirployeeAutonomy         string `json:"virployee_autonomy"`
			Checks                    []struct {
				Key    string `json:"key"`
				Status string `json:"status"`
			} `json:"checks"`
		} `json:"execution_gate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Input == "" || payload.DryRun.Decision != "allowed" || payload.DryRun.Draft.Status != "ready" {
		t.Fatalf("unexpected dry run payload: %+v", payload.DryRun)
	}
	if payload.ExecutionGate.Decision != "blocked" ||
		payload.ExecutionGate.Mode != "simulation" ||
		payload.ExecutionGate.WillExecute ||
		payload.ExecutionGate.RequiredExecutionAutonomy != "A3" ||
		payload.ExecutionGate.VirployeeAutonomy != "A2" {
		t.Fatalf("unexpected execution gate: %+v", payload.ExecutionGate)
	}
	if len(payload.ExecutionGate.Checks) == 0 {
		t.Fatalf("expected checks, got %+v", payload.ExecutionGate)
	}
	if fake.lastPrincipal.Type != "person" || fake.lastPrincipal.ID != "patient-a" {
		t.Fatalf("handler did not forward principal context: %+v", fake.lastPrincipal)
	}
	if fake.lastAssistRun != assistRunID {
		t.Fatalf("handler did not forward assist_run_id: got %s want %s", fake.lastAssistRun, assistRunID)
	}
}

func TestHandlerSimulateApprovedExecution(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	id := uuid.New()
	approvalID := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees/"+id.String()+"/simulated-executions", strings.NewReader(`{"approval_id":"`+approvalID.String()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastTenant != "tenant-1" {
		t.Fatalf("expected tenant-1, got %q", fake.lastTenant)
	}
	var payload struct {
		Operation       string `json:"operation"`
		ExecutionResult struct {
			Status          string `json:"status"`
			ApprovalID      string `json:"approval_id"`
			ExternalEffects bool   `json:"external_effects"`
		} `json:"execution_result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Operation != "simulated_execution" || payload.ExecutionResult.Status != "simulated_executed" || payload.ExecutionResult.ApprovalID != approvalID.String() || payload.ExecutionResult.ExternalEffects {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHandlerListRuns(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/"+id.String()+"/runs?limit=10", nil)
	req.Header.Set("X-Tenant-ID", "tenant-1")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.lastTenant != "tenant-1" {
		t.Fatalf("expected tenant-1, got %q", fake.lastTenant)
	}
	var payload struct {
		Data []struct {
			Operation     string `json:"operation"`
			InputPreview  string `json:"input_preview"`
			CapabilityKey string `json:"capability_key"`
			GateDecision  string `json:"gate_decision"`
			BindingHash   string `json:"binding_hash"`
			NexusResult   struct {
				Decision string `json:"decision"`
			} `json:"nexus_result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Operation != "execution_gate" || payload.Data[0].BindingHash == "" {
		t.Fatalf("unexpected runs payload: %+v", payload.Data)
	}
	if payload.Data[0].NexusResult.Decision != "allow" {
		t.Fatalf("unexpected nexus result: %+v", payload.Data[0].NexusResult)
	}
}

func TestHandlerListRunsRejectsInvalidLimit(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/"+id.String()+"/runs?limit=nope", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRoutesLifecycle(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	id := uuid.New()

	for _, tc := range []struct {
		method string
		path   string
		action string
	}{
		{http.MethodPost, "/v1/virployees/" + id.String() + "/archive", "archive"},
		{http.MethodPost, "/v1/virployees/" + id.String() + "/unarchive", "unarchive"},
		{http.MethodPost, "/v1/virployees/" + id.String() + "/trash", "trash"},
		{http.MethodPost, "/v1/virployees/" + id.String() + "/restore", "restore"},
		{http.MethodDelete, "/v1/virployees/" + id.String() + "/purge", "purge"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-Actor-ID", "tester")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s expected 204, got %d body=%s", tc.action, rec.Code, rec.Body.String())
		}
		if fake.lastAction != tc.action || fake.lastActor != "tester" {
			t.Fatalf("unexpected action call: action=%s actor=%s", fake.lastAction, fake.lastActor)
		}
	}
}

func TestHandlerInvalidUUID(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/nope", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandlerSubmitAssistRunReturns202AndPollingReturnsRun(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	virployeeID := uuid.New()

	post := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees/"+virployeeID.String()+"/assist-runs", strings.NewReader(`{"input":{"documents":[]}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	req.Header.Set("Idempotency-Key", "generation-a")
	router.ServeHTTP(post, req)
	if post.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", post.Code, post.Body.String())
	}
	var submitted struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(post.Body.Bytes(), &submitted); err != nil || submitted.ID == "" || submitted.Status != "received" {
		t.Fatalf("unexpected submit response: %s (%v)", post.Body.String(), err)
	}

	poll := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/virployees/"+virployeeID.String()+"/assist-runs/"+submitted.ID, nil)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	router.ServeHTTP(poll, req)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"status":"done"`) {
		t.Fatalf("expected completed poll response, got %d body=%s", poll.Code, poll.Body.String())
	}
}

func testRouter(ucs UseCasesPort) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ginmw.RegisterHealthEndpoints(router, nil)
	NewHandler(ucs).Routes(router.Group("/v1"))
	return router
}

type handlerFakeUseCases struct {
	lastAction    string
	lastActor     string
	lastTenant    string
	lastPrincipal executiongate.PrincipalContext
	lastAssistRun uuid.UUID
}

func (f *handlerFakeUseCases) Create(_ context.Context, tenantID string, input domain.CreateInput) (domain.Virployee, error) {
	f.lastTenant = tenantID
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return domain.Virployee{ID: uuid.New(), Name: normalized.Name, JobRoleID: normalized.JobRoleID, ProfileTemplateID: normalized.ProfileTemplateID, SupervisorUserID: normalized.SupervisorUserID, Autonomy: normalized.Autonomy}, nil
}

func (f *handlerFakeUseCases) ListActive(context.Context, string) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) ListArchived(context.Context, string) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) ListTrash(context.Context, string) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) Get(_ context.Context, _ string, id uuid.UUID) (domain.Virployee, error) {
	return domain.Virployee{ID: id, Name: "Ops", JobRoleID: uuid.New(), ProfileTemplateID: uuid.New(), SupervisorUserID: "dev-user", Autonomy: domain.AutonomyA1}, nil
}

func (f *handlerFakeUseCases) RuntimeContext(_ context.Context, tenantID string, id uuid.UUID) (runtimecontext.Context, error) {
	f.lastTenant = tenantID
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	capabilityID := uuid.New()
	return runtimecontext.Context{
		Virployee: domain.Virployee{
			ID:                id,
			Name:              "Sofia",
			JobRoleID:         jobRoleID,
			ProfileTemplateID: profileTemplateID,
			Description:       "Reception support",
			SupervisorUserID:  "dev-user",
			Autonomy:          domain.AutonomyA2,
		},
		JobRole: jobroledomain.JobRole{
			ID:               jobRoleID,
			Name:             "Receptionist",
			Mission:          "Welcome visitors",
			Responsibilities: []jobroledomain.Responsibility{{Title: "Welcome", ExpectedOutcome: "Visitors are welcomed", Priority: 1}},
			SuccessCriteria:  []jobroledomain.SuccessCriterion{{Title: "Response time", TargetValue: "under 2 minutes", Priority: 1}},
		},
		ProfileTemplate: profiletemplatedomain.ProfileTemplate{
			ID:           profileTemplateID,
			Name:         "Warm profile",
			SystemPrompt: "Be warm.",
			MaxAutonomy:  domain.AutonomyA2,
		},
		Capabilities: []capabilitydomain.Capability{
			{
				ID:               capabilityID,
				CapabilityKey:    "calendar.events.create",
				Name:             "Create calendar events",
				RequiredAutonomy: domain.AutonomyA2,
			},
		},
	}, nil
}

func (f *handlerFakeUseCases) DryRun(_ context.Context, tenantID string, id uuid.UUID, input string) (dryrun.Result, error) {
	ctx, err := f.RuntimeContext(context.Background(), tenantID, id)
	if err != nil {
		return dryrun.Result{}, err
	}
	result := dryrun.Evaluate(input, ctx)
	return result, nil
}

func (f *handlerFakeUseCases) ExecutionGate(ctx context.Context, tenantID string, id uuid.UUID, input string, confirmedDraft *executiongate.ConfirmedDraft, principalContexts ...executiongate.PrincipalContext) (executiongate.Result, error) {
	principal := executiongate.PrincipalContext{}
	if len(principalContexts) > 0 {
		principal = principalContexts[0]
	}
	return f.ExecutionGateWithAssistRun(ctx, tenantID, id, input, confirmedDraft, principal, uuid.Nil)
}

func (f *handlerFakeUseCases) ExecutionGateWithAssistRun(ctx context.Context, tenantID string, id uuid.UUID, input string, confirmedDraft *executiongate.ConfirmedDraft, principal executiongate.PrincipalContext, assistRunID uuid.UUID) (executiongate.Result, error) {
	f.lastPrincipal = principal
	f.lastAssistRun = assistRunID
	result, err := f.DryRun(ctx, tenantID, id, input)
	if err != nil {
		return executiongate.Result{}, err
	}
	if confirmedDraft != nil {
		result, err = executiongate.ApplyConfirmedDraft(result, *confirmedDraft)
		if err != nil {
			return executiongate.Result{}, err
		}
	}
	return executiongate.Evaluate(result), nil
}

func (f *handlerFakeUseCases) SimulateApprovedExecution(_ context.Context, tenantID string, id uuid.UUID, approvalID uuid.UUID) (runtraces.Trace, error) {
	f.lastTenant = tenantID
	return runtraces.Trace{
		ID:             uuid.New(),
		TenantID:       tenantID,
		VirployeeID:    id,
		Operation:      runtraces.OperationSimulatedExecution,
		InputHash:      runtraces.HashString("Agendá una reunión"),
		InputPreview:   "Agendá una reunión",
		Intent:         map[string]any{"matched": true, "capability_key": "calendar.events.create"},
		CapabilityKey:  "calendar.events.create",
		DryRunDecision: "allowed",
		GateDecision:   "pass",
		NexusResult:    &runtraces.NexusResult{Available: true, Decision: "require_approval", ApprovalID: approvalID.String(), ApprovalStatus: "approved"},
		ExecutionResult: &runtraces.ExecutionResult{
			Status:          "simulated_executed",
			Mode:            "simulation",
			ApprovalID:      approvalID.String(),
			ApprovalStatus:  "approved",
			BindingHash:     "binding-hash",
			Message:         "Simulated execution completed; no external effects were performed.",
			ExternalEffects: false,
		},
		BindingHash: "binding-hash",
	}, nil
}

func (f *handlerFakeUseCases) ExecuteApprovedAction(_ context.Context, tenantID string, id uuid.UUID, approvalID uuid.UUID) (runtraces.Trace, error) {
	f.lastTenant = tenantID
	return runtraces.Trace{
		ID: uuid.New(), TenantID: tenantID, VirployeeID: id, Operation: runtraces.OperationExecution,
		ExecutionResult: &runtraces.ExecutionResult{Status: "succeeded", Mode: "local", ApprovalID: approvalID.String(), ResourceID: uuid.NewString(), NexusReportStatus: "reported"},
	}, nil
}

func (f *handlerFakeUseCases) Assist(_ context.Context, tenantID string, id uuid.UUID, _ json.RawMessage, _ string, _ AssistMetadata) (AssistRun, error) {
	f.lastTenant = tenantID
	return AssistRun{ID: uuid.New(), VirployeeID: id, Status: "done", Answered: true, Output: json.RawMessage(`{"summary":"ok"}`)}, nil
}

func (f *handlerFakeUseCases) SubmitAssistAsync(_ context.Context, tenantID string, id uuid.UUID, _ json.RawMessage, _ string, _ AssistMetadata) (AssistRun, error) {
	f.lastTenant = tenantID
	return AssistRun{ID: uuid.New(), TenantID: tenantID, VirployeeID: id, Status: "received"}, nil
}

func (f *handlerFakeUseCases) GetAssistRun(_ context.Context, tenantID string, virployeeID, runID uuid.UUID) (AssistRun, error) {
	f.lastTenant = tenantID
	return AssistRun{ID: runID, TenantID: tenantID, VirployeeID: virployeeID, Status: "done", Answered: true, Output: json.RawMessage(`{"summary":"ok"}`)}, nil
}

func (f *handlerFakeUseCases) ListRuns(_ context.Context, tenantID string, id uuid.UUID, _ int) ([]runtraces.Trace, error) {
	f.lastTenant = tenantID
	return []runtraces.Trace{
		{
			ID:             uuid.New(),
			TenantID:       tenantID,
			VirployeeID:    id,
			Operation:      runtraces.OperationExecutionGate,
			InputHash:      runtraces.HashString("Agendá una reunión"),
			InputPreview:   "Agendá una reunión",
			Intent:         map[string]any{"matched": true, "capability_key": "calendar.events.create"},
			CapabilityKey:  "calendar.events.create",
			DryRunDecision: "allowed",
			GateDecision:   "pass",
			GateChecks:     []runtraces.GateCheck{{Key: "governance_check", Status: "pass", Reason: "allowed"}},
			NexusResult:    &runtraces.NexusResult{Available: true, Decision: "allow", Status: "allowed"},
			BindingHash:    "binding-hash",
		},
	}, nil
}

func (f *handlerFakeUseCases) Update(_ context.Context, _ string, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return domain.Virployee{ID: id, Name: normalized.Name, JobRoleID: normalized.JobRoleID, ProfileTemplateID: normalized.ProfileTemplateID, SupervisorUserID: normalized.SupervisorUserID, Autonomy: normalized.Autonomy}, nil
}

func (f *handlerFakeUseCases) Archive(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "archive"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Unarchive(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "unarchive"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Trash(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "trash"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Restore(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "restore"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Purge(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "purge"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}
