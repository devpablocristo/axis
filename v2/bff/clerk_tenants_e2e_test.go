package bff_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devpablocristo/bff-v2/wire"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultE2EDatabaseURL = "postgres://postgres:postgres@127.0.0.1:19438/postgres?sslmode=disable"

func TestClerkUsersAndTenantsE2E(t *testing.T) {
	ctx := context.Background()
	databaseURL := createTempPostgresDatabase(t)
	clerk := newFakeClerkE2E(t)

	t.Setenv("BFF_V2_DATABASE_URL", databaseURL)
	t.Setenv("BFF_V2_COMPANION_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("BFF_V2_IDENTITY_PROVIDER", "clerk")
	t.Setenv("BFF_V2_CLERK_SECRET_KEY", "sk_test_fake")
	t.Setenv("BFF_V2_CLERK_API_BASE_URL", clerk.server.URL)
	t.Setenv("BFF_V2_RUN_MIGRATIONS", "true")
	t.Setenv("BFF_V2_DEV_ACTOR_ID", "dev-user")
	t.Setenv("BFF_V2_DEV_ACTOR_EMAIL", "dev@example.local")
	t.Setenv("BFF_V2_DEV_ORG_ID", "dev-org")

	deps, err := wire.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize bff: %v", err)
	}
	t.Cleanup(deps.Close)

	principalID := uuid.NewString()
	orgID := uuid.NewString()
	axisTenantID := uuid.NewString()
	seedClerkTenantFixture(t, deps.DB.Pool(), principalID, orgID, axisTenantID)

	existing := doBFFRequest(t, deps.Router, http.MethodPost, "/api/users", axisTenantID, principalID, map[string]any{
		"email": "existing@cristo.tech",
		"role":  "member",
	})
	assertStatus(t, existing, http.StatusCreated)
	assertNoNameField(t, existing.Payload)
	if got := stringField(existing.Payload, "kind"); got != "user" {
		t.Fatalf("expected existing Clerk user to become tenant user, got kind=%q payload=%v", got, existing.Payload)
	}
	if got := stringField(existing.Payload, "email"); got != "existing@cristo.tech" {
		t.Fatalf("unexpected existing user email %q payload=%v", got, existing.Payload)
	}
	if !clerk.hit("POST /organizations/org_FAKE/memberships") {
		t.Fatalf("expected Clerk org membership call with provider org id, hits=%v", clerk.hits())
	}
	if clerk.hit(orgID) {
		t.Fatalf("Clerk call leaked internal axis org id %q, hits=%v", orgID, clerk.hits())
	}

	axisUsers := doBFFRequest(t, deps.Router, http.MethodGet, "/api/users", axisTenantID, principalID, nil)
	assertStatus(t, axisUsers, http.StatusOK)
	assertListItemsHaveNoName(t, axisUsers.Payload)
	if findByEmail(listData(t, axisUsers.Payload), "existing@cristo.tech") == nil {
		t.Fatalf("expected existing user in axis tenant list, payload=%v", axisUsers.Payload)
	}

	created := doBFFRequest(t, deps.Router, http.MethodPost, "/api/users", axisTenantID, principalID, map[string]any{
		"email": "created@cristo.tech",
		"role":  "member",
	})
	assertStatus(t, created, http.StatusCreated)
	assertNoNameField(t, created.Payload)
	if got := stringField(created.Payload, "kind"); got != "user" {
		t.Fatalf("expected missing Clerk user to be created as tenant user, got kind=%q payload=%v", got, created.Payload)
	}
	if got := stringField(created.Payload, "state"); got != "active" {
		t.Fatalf("expected active tenant user, got state=%q payload=%v", got, created.Payload)
	}
	if !clerk.hit(`POST /users body={"email_address":["created@cristo.tech"]`) {
		t.Fatalf("expected Clerk user creation call, hits=%v", clerk.hits())
	}

	rejected := doBFFRequest(t, deps.Router, http.MethodPost, "/api/users", axisTenantID, principalID, map[string]any{
		"email": "reject@cristo.tech",
		"role":  "member",
	})
	assertStatus(t, rejected, http.StatusBadRequest)
	if strings.Contains(rejected.Body, "unexpected error") {
		t.Fatalf("Clerk rejection must not be masked as unexpected error: %s", rejected.Body)
	}

	ponti := doBFFRequest(t, deps.Router, http.MethodPost, "/api/tenants", "", principalID, map[string]any{
		"org_id":          orgID,
		"product_surface": "ponti",
	})
	assertStatus(t, ponti, http.StatusCreated)
	assertOrgName(t, ponti.Payload, "cristo.tech", orgID)
	pontiID := stringField(ponti.Payload, "id")
	if pontiID == "" {
		t.Fatalf("created tenant missing id: %v", ponti.Payload)
	}

	pontiAgain := doBFFRequest(t, deps.Router, http.MethodPost, "/api/tenants", "", principalID, map[string]any{
		"org_id":          orgID,
		"product_surface": "ponti",
	})
	assertStatus(t, pontiAgain, http.StatusCreated)
	assertOrgName(t, pontiAgain.Payload, "cristo.tech", orgID)
	if got := stringField(pontiAgain.Payload, "id"); got != pontiID {
		t.Fatalf("tenant create must be idempotent by org+product, got %q want %q", got, pontiID)
	}

	tenants := doBFFRequest(t, deps.Router, http.MethodGet, "/api/tenants", "", principalID, nil)
	assertStatus(t, tenants, http.StatusOK)
	tenantItems := listData(t, tenants.Payload)
	if findTenantByProduct(tenantItems, "axis") == nil || findTenantByProduct(tenantItems, "ponti") == nil {
		t.Fatalf("expected axis and ponti tenants for principal, payload=%v", tenants.Payload)
	}
	for _, item := range tenantItems {
		assertOrgName(t, item, "cristo.tech", orgID)
	}

	pontiUsers := doBFFRequest(t, deps.Router, http.MethodGet, "/api/users", pontiID, principalID, nil)
	assertStatus(t, pontiUsers, http.StatusOK)
	if findByEmail(listData(t, pontiUsers.Payload), "existing@cristo.tech") != nil {
		t.Fatalf("axis tenant user leaked into ponti tenant, payload=%v", pontiUsers.Payload)
	}

	addExistingToPonti := doBFFRequest(t, deps.Router, http.MethodPost, "/api/users", pontiID, principalID, map[string]any{
		"email": "existing@cristo.tech",
		"role":  "member",
	})
	assertStatus(t, addExistingToPonti, http.StatusCreated)
	pontiUsers = doBFFRequest(t, deps.Router, http.MethodGet, "/api/users", pontiID, principalID, nil)
	assertStatus(t, pontiUsers, http.StatusOK)
	if findByEmail(listData(t, pontiUsers.Payload), "existing@cristo.tech") == nil {
		t.Fatalf("expected user after explicit ponti membership, payload=%v", pontiUsers.Payload)
	}

	archived := doBFFRequest(t, deps.Router, http.MethodPost, "/api/tenants/"+pontiID+"/archive", "", principalID, nil)
	assertStatus(t, archived, http.StatusNoContent)
	archivedTenants := doBFFRequest(t, deps.Router, http.MethodGet, "/api/tenants?lifecycle=archived", "", principalID, nil)
	assertStatus(t, archivedTenants, http.StatusOK)
	if findTenantByProduct(listData(t, archivedTenants.Payload), "ponti") == nil {
		t.Fatalf("expected ponti in archived tenant list, payload=%v", archivedTenants.Payload)
	}

	unarchived := doBFFRequest(t, deps.Router, http.MethodPost, "/api/tenants/"+pontiID+"/unarchive", "", principalID, nil)
	assertStatus(t, unarchived, http.StatusNoContent)
	archived = doBFFRequest(t, deps.Router, http.MethodPost, "/api/tenants/"+pontiID+"/archive", "", principalID, nil)
	assertStatus(t, archived, http.StatusNoContent)
	lastActive := doBFFRequest(t, deps.Router, http.MethodPost, "/api/tenants/"+axisTenantID+"/archive", "", principalID, nil)
	assertStatus(t, lastActive, http.StatusConflict)
}

type bffResponse struct {
	Status  int
	Body    string
	Payload map[string]any
}

func doBFFRequest(t *testing.T, router http.Handler, method, path, tenantID, principalID string, body any) bffResponse {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if principalID != "" {
		req.Header.Set("X-Actor-ID", principalID)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	out := bffResponse{Status: rec.Code, Body: rec.Body.String()}
	if strings.TrimSpace(out.Body) != "" {
		if err := json.Unmarshal(rec.Body.Bytes(), &out.Payload); err != nil {
			t.Fatalf("decode response %s %s status=%d body=%s: %v", method, path, rec.Code, out.Body, err)
		}
	}
	return out
}

func createTempPostgresDatabase(t *testing.T) string {
	t.Helper()
	adminURL := strings.TrimSpace(os.Getenv("BFF_V2_E2E_DATABASE_URL"))
	if adminURL == "" {
		adminURL = defaultE2EDatabaseURL
	}
	parsed, err := url.Parse(adminURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Skipf("BFF_V2_E2E_DATABASE_URL must be a postgres URL: %v", err)
	}
	dbName := "bff_v2_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	tempURL := databaseURLWithName(t, adminURL, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("postgres e2e database is not available: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("postgres e2e database is not reachable: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(dbName)); err != nil {
		adminPool.Close()
		t.Fatalf("create temp database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = adminPool.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoteIdentifier(dbName)+" WITH (FORCE)")
		adminPool.Close()
	})
	return tempURL
}

func databaseURLWithName(t *testing.T, rawURL, dbName string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	parsed.Path = "/" + dbName
	return parsed.String()
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func seedClerkTenantFixture(t *testing.T, pool *pgxpool.Pool, principalID, orgID, axisTenantID string) {
	t.Helper()
	now := time.Now().UTC()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin e2e seed tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_users (id, provider, provider_user_id, email, status, synced_at, created_at, updated_at)
		VALUES ($1::uuid, 'clerk', 'user_owner', 'owner@cristo.tech', 'active', $2, $2, $2)
	`, principalID, now); err != nil {
		t.Fatalf("seed principal user: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_orgs (id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at)
		VALUES ($1::uuid, 'clerk', 'org_FAKE', 'cristo.tech', 'cristo-tech', 'active', $2, $2, $2)
	`, orgID, now); err != nil {
		t.Fatalf("seed clerk org: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_tenants (id, org_id, product_surface, status, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, 'axis', 'active', $3, $3)
	`, axisTenantID, orgID, now); err != nil {
		t.Fatalf("seed axis tenant: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_tenant_members (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, 'owner', 'active', $3, $3)
	`, axisTenantID, principalID, now); err != nil {
		t.Fatalf("seed principal tenant membership: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit e2e seed tx: %v", err)
	}
}

type fakeClerkE2E struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []string
}

func newFakeClerkE2E(t *testing.T) *fakeClerkE2E {
	t.Helper()
	f := &fakeClerkE2E{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.record(r.Method, r.URL.RequestURI(), string(raw))
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users":
			f.handleUsers(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/users":
			f.handleCreateUser(w, raw)
		case r.Method == http.MethodPost && r.URL.Path == "/organizations/org_FAKE/memberships":
			_, _ = w.Write([]byte(`{"id":"membership_fake"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/organizations/org_FAKE/invitations":
			f.handleInvitation(w, raw)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"code":"not_found"}]}`))
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeClerkE2E) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("email_address[]") {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":"invalid_query","message":"use email_address, not email_address[]"}]}`))
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("email_address")))
	switch email {
	case "existing@cristo.tech":
		_, _ = w.Write([]byte(`[{"id":"user_existing","email_addresses":[{"id":"email_existing","email_address":"existing@cristo.tech"}],"primary_email_address_id":"email_existing"}]`))
	default:
		_, _ = w.Write([]byte(`[]`))
	}
}

func (f *fakeClerkE2E) handleCreateUser(w http.ResponseWriter, raw []byte) {
	var body struct {
		Email []string `json:"email_address"`
	}
	_ = json.Unmarshal(raw, &body)
	email := ""
	if len(body.Email) > 0 {
		email = strings.TrimSpace(strings.ToLower(body.Email[0]))
	}
	if email == "reject@cristo.tech" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"code":"form_identifier_not_allowed","message":"email rejected"}]}`))
		return
	}
	_, _ = w.Write([]byte(`{"id":"user_created","email_address":"` + email + `"}`))
}

func (f *fakeClerkE2E) handleInvitation(w http.ResponseWriter, raw []byte) {
	var body struct {
		Email string `json:"email_address"`
	}
	_ = json.Unmarshal(raw, &body)
	if strings.EqualFold(strings.TrimSpace(body.Email), "reject@cristo.tech") {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"code":"form_identifier_not_allowed","message":"email rejected"}]}`))
		return
	}
	_, _ = w.Write([]byte(`{"id":"invite_fake","status":"pending"}`))
}

func (f *fakeClerkE2E) record(method, requestURI, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := method + " " + requestURI
	if body != "" {
		entry += " body=" + body
	}
	f.requests = append(f.requests, entry)
}

func (f *fakeClerkE2E) hit(substr string) bool {
	for _, request := range f.hits() {
		if strings.Contains(request, substr) {
			return true
		}
	}
	return false
}

func (f *fakeClerkE2E) hits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func assertStatus(t *testing.T, response bffResponse, want int) {
	t.Helper()
	if response.Status != want {
		t.Fatalf("expected status %d, got %d body=%s", want, response.Status, response.Body)
	}
}

func assertNoNameField(t *testing.T, payload map[string]any) {
	t.Helper()
	if _, ok := payload["name"]; ok {
		t.Fatalf("human user payload must not expose name: %v", payload)
	}
}

func assertListItemsHaveNoName(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, item := range listData(t, payload) {
		assertNoNameField(t, item)
	}
}

func assertOrgName(t *testing.T, payload map[string]any, wantName, forbiddenID string) {
	t.Helper()
	if got := stringField(payload, "org_name"); got != wantName {
		t.Fatalf("expected org_name %q, got %q payload=%v", wantName, got, payload)
	}
	if got := stringField(payload, "org_name"); got == forbiddenID {
		t.Fatalf("org_name must not be internal org id %q payload=%v", forbiddenID, payload)
	}
}

func listData(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, ok := payload["data"].([]any)
	if !ok {
		t.Fatalf("expected data array, payload=%v", payload)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		mapped, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected object item, got %T in %v", item, payload)
		}
		out = append(out, mapped)
	}
	return out
}

func findByEmail(items []map[string]any, email string) map[string]any {
	for _, item := range items {
		if strings.EqualFold(stringField(item, "email"), email) {
			return item
		}
	}
	return nil
}

func findTenantByProduct(items []map[string]any, productSurface string) map[string]any {
	for _, item := range items {
		if stringField(item, "product_surface") == productSurface {
			return item
		}
	}
	return nil
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func TestClerkE2EDatabaseURLWithName(t *testing.T) {
	got := databaseURLWithName(t, "postgres://user:pass@127.0.0.1:5432/postgres?sslmode=disable", "next_db")
	if got != "postgres://user:pass@127.0.0.1:5432/next_db?sslmode=disable" {
		t.Fatalf("unexpected database URL: %s", got)
	}
}

func Example_quoteIdentifier() {
	fmt.Println(quoteIdentifier(`axis"test`))
	// Output: "axis""test"
}
