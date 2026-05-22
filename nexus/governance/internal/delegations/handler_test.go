package delegations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	domain "github.com/devpablocristo/nexus/governance/internal/delegations/usecases/domain"
)

// --- fakes ---

type fakeDelegationRepo struct {
	items map[uuid.UUID]domain.Delegation
}

func newFakeRepo() *fakeDelegationRepo {
	return &fakeDelegationRepo{items: make(map[uuid.UUID]domain.Delegation)}
}

func (f *fakeDelegationRepo) Create(_ context.Context, d domain.Delegation) (domain.Delegation, error) {
	d.ID = uuid.New()
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	f.items[d.ID] = d
	return d, nil
}

func (f *fakeDelegationRepo) GetByID(_ context.Context, id uuid.UUID) (domain.Delegation, error) {
	d, ok := f.items[id]
	if !ok {
		return domain.Delegation{}, domainerr.NotFound("not found")
	}
	return d, nil
}

func (f *fakeDelegationRepo) ExistsByAgent(_ context.Context, agentID string) (bool, error) {
	for _, d := range f.items {
		if d.AgentID == agentID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeDelegationRepo) ListByAgentID(_ context.Context, agentID string, orgID *string) ([]domain.Delegation, error) {
	var out []domain.Delegation
	for _, d := range f.items {
		if d.AgentID != agentID {
			continue
		}
		// Espejar el filtro SQL del repo Postgres: incluir global (org_id IS NULL)
		// y match por org. Si orgID es nil, sólo devolver globales.
		if d.OrgID == nil {
			out = append(out, d)
			continue
		}
		if orgID != nil && *d.OrgID == *orgID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeDelegationRepo) List(_ context.Context) ([]domain.Delegation, error) {
	out := make([]domain.Delegation, 0, len(f.items))
	for _, d := range f.items {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeDelegationRepo) Update(_ context.Context, d domain.Delegation) (domain.Delegation, error) {
	if _, ok := f.items[d.ID]; !ok {
		return domain.Delegation{}, domainerr.NotFound("not found")
	}
	d.UpdatedAt = time.Now().UTC()
	f.items[d.ID] = d
	return d, nil
}

func (f *fakeDelegationRepo) DeleteByID(_ context.Context, id uuid.UUID) error {
	if _, ok := f.items[id]; !ok {
		return domainerr.NotFound("not found")
	}
	delete(f.items, id)
	return nil
}

// --- helpers ---

func setupMux() (*http.ServeMux, *fakeDelegationRepo) {
	repo := newFakeRepo()
	uc := NewUsecases(repo)
	h := NewHandler(uc)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, repo
}

func doRequest(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// --- tests ---

func TestDelegations_CreateAndGet(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/delegations", `{
		"owner_id": "team-finops",
		"owner_type": "team",
		"agent_id": "ops-bot",
		"agent_type": "agent",
		"allowed_action_types": ["treasury.transfer"],
		"purpose": "automated transfers"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)
	if created["owner_id"] != "team-finops" {
		t.Fatalf("unexpected owner_id: %v", created["owner_id"])
	}
	if created["enabled"] != true {
		t.Fatal("expected enabled=true by default")
	}
	if created["max_risk_class"] != "high" {
		t.Fatalf("expected default max_risk_class=high, got %v", created["max_risk_class"])
	}

	rec = doRequest(mux, "GET", "/v1/delegations/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDelegations_CreateValidation(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/delegations", `{"owner_id":"team","agent_id":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty agent_id, got %d", rec.Code)
	}

	rec = doRequest(mux, "POST", "/v1/delegations", `{"owner_id":"","agent_id":"bot"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty owner_id, got %d", rec.Code)
	}
}

func TestDelegations_List(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	doRequest(mux, "POST", "/v1/delegations", `{"owner_id":"a","agent_id":"bot-1"}`)
	doRequest(mux, "POST", "/v1/delegations", `{"owner_id":"b","agent_id":"bot-2"}`)

	rec := doRequest(mux, "GET", "/v1/delegations", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2, got %d", len(data))
	}
}

func TestDelegations_Update(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/delegations", `{"owner_id":"team","agent_id":"bot","max_risk_class":"low"}`)
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	rec = doRequest(mux, "PATCH", "/v1/delegations/"+id, `{"max_risk_class":"critical","enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated["max_risk_class"] != "critical" {
		t.Fatalf("expected critical, got %v", updated["max_risk_class"])
	}
	if updated["enabled"] != false {
		t.Fatal("expected enabled=false after update")
	}
}

func TestDelegations_Delete(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/delegations", `{"owner_id":"x","agent_id":"y"}`)
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	rec = doRequest(mux, "DELETE", "/v1/delegations/"+id, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	rec = doRequest(mux, "GET", "/v1/delegations/"+id, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestDelegations_GetNotFound(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "GET", fmt.Sprintf("/v1/delegations/%s", uuid.New()), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDelegations_CheckDelegation(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	// Sin delegaciones = sin restricciones
	ok, _, err := uc.CheckDelegation(context.Background(), "any-agent", "any.action", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected true when no delegations exist")
	}

	// Con delegación con action_types restringidos
	repo.Create(context.Background(), domain.Delegation{
		OwnerID:            "team",
		AgentID:            "bot",
		AllowedActionTypes: []string{"treasury.transfer", "iam.grant"},
		Enabled:            true,
	})

	ok, _, err = uc.CheckDelegation(context.Background(), "bot", "treasury.transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected true for allowed action")
	}

	ok, _, err = uc.CheckDelegation(context.Background(), "bot", "infra.delete", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected false for non-allowed action")
	}
}

func TestDelegations_CheckDelegation_DisabledSkipped(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	repo.Create(context.Background(), domain.Delegation{
		OwnerID:            "team",
		AgentID:            "bot",
		AllowedActionTypes: []string{"treasury.transfer"},
		Enabled:            false,
	})

	ok, _, err := uc.CheckDelegation(context.Background(), "bot", "treasury.transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected false for disabled delegation")
	}
}

func TestDelegations_CheckDelegation_EmptyActionTypes(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	repo.Create(context.Background(), domain.Delegation{
		OwnerID: "team",
		AgentID: "bot",
		Enabled: true,
	})

	ok, _, err := uc.CheckDelegation(context.Background(), "bot", "anything.at.all", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected true when delegation has no action type restrictions")
	}
}

func TestDelegations_CheckDelegation_RespectsOrgID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	orgA := "org-a"
	orgB := "org-b"
	repo.Create(context.Background(), domain.Delegation{
		OrgID:              &orgA,
		OwnerID:            "team",
		AgentID:            "bot",
		AllowedActionTypes: []string{"treasury.transfer"},
		Enabled:            true,
	})

	ok, _, err := uc.CheckDelegation(context.Background(), "bot", "treasury.transfer", &orgA)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected true for matching org delegation")
	}

	ok, _, err = uc.CheckDelegation(context.Background(), "bot", "treasury.transfer", &orgB)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected false when delegation belongs to another org")
	}
}

// Regresión B.1: el filtro de tenancy en SQL no debe convertir un agente
// "restringido en otro org" en "no restringido". El edge case ocurre cuando
// el agente tiene delegaciones sólo en orgs distintos al del caller — la
// lista filtrada queda vacía pero el agente sigue restringido y la respuesta
// debe ser false.
func TestDelegations_CheckDelegation_RestrictedInOtherOrgNotEscalable(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	orgA := "org-a"
	orgB := "org-b"
	// El agente tiene delegación SÓLO en orgA. orgB pregunta.
	repo.Create(context.Background(), domain.Delegation{
		OrgID:              &orgA,
		OwnerID:            "team",
		AgentID:            "bot",
		AllowedActionTypes: []string{"treasury.transfer"},
		Enabled:            true,
	})

	ok, _, err := uc.CheckDelegation(context.Background(), "bot", "treasury.transfer", &orgB)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("agent restricted in another org must NOT become unrestricted in this org")
	}
}

// Regresión B.1: agente que NO tiene ninguna delegación en ningún org
// debe seguir siendo irrestricto (compat PoC).
func TestDelegations_CheckDelegation_NoDelegationAnywhereIsUnrestricted(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	orgA := "org-a"
	ok, _, err := uc.CheckDelegation(context.Background(), "no-delegations-bot", "any.action", &orgA)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("agent with zero delegations must be unrestricted (PoC compat)")
	}
}

func TestDelegations_CheckDelegation_GlobalDelegationAppliesToOrg(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	orgID := "org-a"
	repo.Create(context.Background(), domain.Delegation{
		OwnerID:            "team",
		AgentID:            "bot",
		AllowedActionTypes: []string{"treasury.transfer"},
		Enabled:            true,
	})

	ok, _, err := uc.CheckDelegation(context.Background(), "bot", "treasury.transfer", &orgID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected global delegation to apply to org request")
	}
}
