package bff_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/devpablocristo/bff-v2/wire"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRealClerkUsersAndProductsE2E(t *testing.T) {
	if os.Getenv("BFF_V2_REAL_CLERK_E2E") != "1" {
		t.Skip("set BFF_V2_REAL_CLERK_E2E=1 to run against real Clerk")
	}
	secretKey := firstEnv("BFF_V2_CLERK_SECRET_KEY", "BFF_V2_CLERK_SECRET", "CLERK_SECRET_KEY")
	providerOrgID := strings.TrimSpace(os.Getenv("BFF_V2_REAL_CLERK_PROVIDER_ORG_ID"))
	if secretKey == "" || providerOrgID == "" {
		t.Skip("real Clerk e2e requires a Clerk secret and BFF_V2_REAL_CLERK_PROVIDER_ORG_ID")
	}

	ctx := context.Background()
	databaseURL := createTempPostgresDatabase(t)
	t.Setenv("BFF_V2_DATABASE_URL", databaseURL)
	t.Setenv("BFF_V2_ENV", "test")
	t.Setenv("BFF_V2_INTERNAL_AUTH_SECRET", "test-internal-secret")
	t.Setenv("BFF_V2_COMPANION_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("BFF_V2_IDENTITY_PROVIDER", "clerk")
	t.Setenv("BFF_V2_CLERK_SECRET_KEY", secretKey)
	t.Setenv("BFF_V2_CLERK_API_BASE_URL", firstEnvDefault("BFF_V2_CLERK_API_BASE_URL", "https://api.clerk.com/v1"))
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
	axisProductID := uuid.NewString()
	orgName := firstEnvDefault("BFF_V2_REAL_CLERK_ORG_NAME", "Real Clerk Org")
	seedRealClerkProductFixture(t, deps.DB.Pool(), principalID, orgID, axisProductID, providerOrgID, orgName)

	email := fmt.Sprintf("axis-real-e2e-%d@example.com", time.Now().UnixNano())
	var providerUserID string
	t.Cleanup(func() {
		if providerUserID != "" {
			deleteRealClerkUser(t, secretKey, providerUserID)
		}
	})

	created := doBFFRequest(t, deps.Router, http.MethodPost, "/api/users", orgID, principalID, map[string]any{
		"email": email,
		"role":  "member",
	})
	assertStatus(t, created, http.StatusCreated)
	assertNoNameField(t, created.Payload)
	if got := stringField(created.Payload, "kind"); got != "user" {
		t.Fatalf("expected real Clerk user membership, got kind=%q payload=%v", got, created.Payload)
	}
	if got := stringField(created.Payload, "email"); got != email {
		t.Fatalf("unexpected created email %q payload=%v", got, created.Payload)
	}
	providerUserID = lookupProviderUserID(t, deps.DB.Pool(), email)
	if providerUserID == "" {
		t.Fatalf("created user missing provider_user_id")
	}

	users := doBFFRequest(t, deps.Router, http.MethodGet, "/api/users", orgID, principalID, nil)
	assertStatus(t, users, http.StatusOK)
	if findByEmail(listData(t, users.Payload), email) == nil {
		t.Fatalf("expected real Clerk user in product list, payload=%v", users.Payload)
	}

	updatedEmail := fmt.Sprintf("axis-real-e2e-updated-%d@example.com", time.Now().UnixNano())
	updated := doBFFRequest(t, deps.Router, http.MethodPut, "/api/users/"+stringField(created.Payload, "id"), axisProductID, principalID, map[string]any{
		"email": updatedEmail,
		"role":  "admin",
	})
	assertStatus(t, updated, http.StatusOK)
	if got := stringField(updated.Payload, "email"); got != updatedEmail {
		t.Fatalf("expected updated email %q, got payload=%v", updatedEmail, updated.Payload)
	}
	if got := stringField(updated.Payload, "role"); got != "admin" {
		t.Fatalf("expected updated role admin, got payload=%v", updated.Payload)
	}
	if !realClerkUserHasEmail(t, secretKey, providerUserID, updatedEmail) {
		t.Fatalf("expected Clerk user %s to have updated email %s", providerUserID, updatedEmail)
	}

	trashed := doBFFRequest(t, deps.Router, http.MethodPost, "/api/users/"+stringField(created.Payload, "id")+"/trash", axisProductID, principalID, nil)
	assertStatus(t, trashed, http.StatusNoContent)
	trashUsers := doBFFRequest(t, deps.Router, http.MethodGet, "/api/users?lifecycle=trash", orgID, principalID, nil)
	assertStatus(t, trashUsers, http.StatusOK)
	if findByEmail(listData(t, trashUsers.Payload), updatedEmail) == nil {
		t.Fatalf("expected updated user in trash list, payload=%v", trashUsers.Payload)
	}

	purged := doBFFRequest(t, deps.Router, http.MethodDelete, "/api/users/"+stringField(created.Payload, "id")+"/purge", axisProductID, principalID, nil)
	assertStatus(t, purged, http.StatusNoContent)
	if realClerkUserExists(t, secretKey, providerUserID) {
		t.Fatalf("expected Clerk user %s to be deleted by purge", providerUserID)
	}
	providerUserID = ""

	productb := doBFFRequest(t, deps.Router, http.MethodPost, "/api/organizations/"+orgID+"/products", "", principalID, map[string]any{
		"org_id":          orgID,
		"product_surface": "companion",
	})
	assertStatus(t, productb, http.StatusCreated)
	assertOrgName(t, productb.Payload, orgName, orgID)
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstEnvDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func seedRealClerkProductFixture(t *testing.T, pool *pgxpool.Pool, principalID, orgID, axisProductID, providerOrgID, orgName string) {
	t.Helper()
	now := time.Now().UTC()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin real e2e seed tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_users (id, provider, provider_user_id, email, status, synced_at, created_at, updated_at)
		VALUES ($1::uuid, 'clerk', 'user_real_e2e_owner', 'owner-real-e2e@example.com', 'active', $2, $2, $2)
	`, principalID, now); err != nil {
		t.Fatalf("seed principal user: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_orgs (id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at)
		VALUES ($1::uuid, 'clerk', $2, $3, 'real-clerk-org', 'active', $4, $4, $4)
	`, orgID, providerOrgID, orgName, now); err != nil {
		t.Fatalf("seed clerk org: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_products (id, org_id, product_surface, name, status, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, 'axis', 'Axis', 'active', $3, $3)
	`, axisProductID, orgID, now); err != nil {
		t.Fatalf("seed axis product: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO axis_org_members (org_id, user_id, role, status, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, 'owner', 'active', $3, $3)
	`, orgID, principalID, now); err != nil {
		t.Fatalf("seed principal organization membership: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit real e2e seed tx: %v", err)
	}
}

func lookupProviderUserID(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var providerUserID string
	err := pool.QueryRow(context.Background(), "SELECT provider_user_id FROM axis_users WHERE email = $1", email).Scan(&providerUserID)
	if err != nil {
		t.Fatalf("lookup provider user id: %v", err)
	}
	return providerUserID
}

func deleteRealClerkUser(t *testing.T, secretKey, providerUserID string) {
	t.Helper()
	endpoint := strings.TrimRight(firstEnvDefault("BFF_V2_CLERK_API_BASE_URL", "https://api.clerk.com/v1"), "/") + "/users/" + url.PathEscape(providerUserID)
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		t.Fatalf("build Clerk delete request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secretKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete real Clerk user: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		t.Fatalf("delete real Clerk user status=%d body=%s", resp.StatusCode, string(raw))
	}
}

func realClerkUserExists(t *testing.T, secretKey, providerUserID string) bool {
	t.Helper()
	resp := realClerkUserResponse(t, secretKey, providerUserID)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func realClerkUserHasEmail(t *testing.T, secretKey, providerUserID, email string) bool {
	t.Helper()
	resp := realClerkUserResponse(t, secretKey, providerUserID)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		t.Fatalf("read real Clerk user status=%d body=%s", resp.StatusCode, string(raw))
	}
	var payload struct {
		EmailAddresses []struct {
			EmailAddress string `json:"email_address"`
		} `json:"email_addresses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode real Clerk user: %v", err)
	}
	for _, item := range payload.EmailAddresses {
		if strings.EqualFold(item.EmailAddress, email) {
			return true
		}
	}
	return false
}

func realClerkUserResponse(t *testing.T, secretKey, providerUserID string) *http.Response {
	t.Helper()
	endpoint := strings.TrimRight(firstEnvDefault("BFF_V2_CLERK_API_BASE_URL", "https://api.clerk.com/v1"), "/") + "/users/" + url.PathEscape(providerUserID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("build Clerk get request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secretKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get real Clerk user: %v", err)
	}
	return resp
}
