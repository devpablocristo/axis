package wire

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devpablocristo/nexus/internal/orgctx"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/authn/go/internaljwt"
)

// TestAuthMiddlewarePreservesRequestedOrgBeforeRebind verifica que el
// middleware captura el X-Org-ID inbound en orgctx ANTES de que WithPrincipal
// borre y rebindee el header al org del principal. Sin esa captura, una key
// cross_org sin org bound no puede acotar su vista per-call.
func TestAuthMiddlewarePreservesRequestedOrgBeforeRebind(t *testing.T) {
	t.Parallel()
	mw, err := newAuthMiddleware(
		"pymes=secret-key|scope=nexus:requests:read nexus:cross_org|service=true",
		"", "", internaljwt.Config{}, "",
	)
	if err != nil {
		t.Fatal(err)
	}

	var gotRequested, gotHeaderOrg string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequested = orgctx.RequestedFromRequest(r)
		gotHeaderOrg = r.Header.Get(identityhttp.HeaderOrgID)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/requests", nil)
	req.Header.Set("X-API-Key", "secret-key")
	req.Header.Set("X-Org-ID", "acme")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperaba 204, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	if gotRequested != "acme" {
		t.Fatalf("orgctx requested = %q, esperaba acme", gotRequested)
	}
	// WithPrincipal rebindea X-Org-ID al org del principal (vacío para esta
	// key sin org bound): el header inbound NO debe sobrevivir como header.
	if gotHeaderOrg != "" {
		t.Fatalf("X-Org-ID post-middleware = %q, esperaba vacío", gotHeaderOrg)
	}
}

// TestAuthMiddlewareRebindsHeaderToPrincipalOrg verifica que para una key
// bound a un org, el header X-Org-ID resultante es el del principal aunque el
// caller haya mandado otro (el solicitado solo queda en orgctx).
func TestAuthMiddlewareRebindsHeaderToPrincipalOrg(t *testing.T) {
	t.Parallel()
	mw, err := newAuthMiddleware(
		"acme-svc=acme-key|org=acme|scope=nexus:requests:read|service=true",
		"", "", internaljwt.Config{}, "",
	)
	if err != nil {
		t.Fatal(err)
	}

	var gotRequested, gotHeaderOrg string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequested = orgctx.RequestedFromRequest(r)
		gotHeaderOrg = r.Header.Get(identityhttp.HeaderOrgID)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/requests", nil)
	req.Header.Set("X-API-Key", "acme-key")
	req.Header.Set("X-Org-ID", "globex")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperaba 204, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	if gotRequested != "globex" {
		t.Fatalf("orgctx requested = %q, esperaba globex", gotRequested)
	}
	if gotHeaderOrg != "acme" {
		t.Fatalf("X-Org-ID post-middleware = %q, esperaba acme (org del principal)", gotHeaderOrg)
	}
}

// --- Product JWT (NEXUS_PRODUCT_JWT_KEYS) ---

// mintHS256JWT firma un JWT HS256 de test (mismo formato que valida
// platform/authn/go/internaljwt).
func mintHS256JWT(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsigned := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func pontiProductClaims() map[string]any {
	return map[string]any{
		"iss":               "ponti-core",
		"aud":               "nexus",
		"sub":               "ponti-backend",
		"actor_id":          "ponti-backend",
		"actor_type":        "service",
		"org_id":            "org-1",
		"product_surface":   "ponti",
		"scope":             "nexus:requests:read nexus:approvals:decide",
		"service_principal": true,
		"on_behalf_of":      "user:ponti-operator",
		"exp":               time.Now().Add(time.Minute).Unix(),
		"iat":               time.Now().Unix(),
	}
}

func newProductJWTMiddleware(t *testing.T) func(http.Handler) http.Handler {
	t.Helper()
	mw, err := newAuthMiddleware(
		"admin=nexus-test-key|org=acme|scope=nexus:requests:read|service=true",
		"", "",
		internaljwt.Config{
			Secret:   "axis-internal-secret",
			Issuer:   "axis-bff",
			Audience: "nexus",
		},
		"ponti=local-dev-ponti-jwt-secret|issuer=ponti-core",
	)
	if err != nil {
		t.Fatal(err)
	}
	return mw
}

// TestAuthMiddlewareAcceptsProductJWT verifica que un JWT firmado con la key
// per-producto (NEXUS_PRODUCT_JWT_KEYS) autentica con org/scopes correctos y
// AuthMethod "product_jwt" — explícitamente distinto de "api_key" para que el
// gate de delegación de approvals (decisionActorID) lo excluya.
func TestAuthMiddlewareAcceptsProductJWT(t *testing.T) {
	t.Parallel()
	mw := newProductJWTMiddleware(t)

	var got identityhttp.Context
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = identityhttp.FromRequest(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/requests", nil)
	req.Header.Set("Authorization", "Bearer "+mintHS256JWT(t, "local-dev-ponti-jwt-secret", pontiProductClaims()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperaba 204, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	if got.OrgID != "org-1" || got.Actor != "ponti-backend" {
		t.Fatalf("principal inesperado: %+v", got)
	}
	if !got.ServicePrincipal {
		t.Fatalf("esperaba service principal, obtuvo %+v", got)
	}
	hasScope := false
	for _, scope := range got.Scopes {
		if scope == "nexus:approvals:decide" {
			hasScope = true
		}
	}
	if !hasScope {
		t.Fatalf("esperaba scope nexus:approvals:decide, obtuvo %v", got.Scopes)
	}
	if got.AuthMethod != "product_jwt" {
		t.Fatalf("auth_method = %q, esperaba product_jwt", got.AuthMethod)
	}
	// Guard de seguridad: el gate de delegación exige api_key. Un product JWT
	// jamás debe presentarse con ese AuthMethod.
	if got.AuthMethod == "api_key" {
		t.Fatal("un JWT de producto jamás debe presentarse como api_key")
	}
}

// TestAuthMiddlewareRejectsProductJWTWrongSecret verifica fail-closed con
// firma o issuer inválidos.
func TestAuthMiddlewareRejectsProductJWTWrongSecret(t *testing.T) {
	t.Parallel()
	mw := newProductJWTMiddleware(t)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("el handler no debe ejecutarse con credencial inválida")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/requests", nil)
	req.Header.Set("Authorization", "Bearer "+mintHS256JWT(t, "wrong-secret", pontiProductClaims()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401 con secret inválido, obtuvo %d", rec.Code)
	}

	claims := pontiProductClaims()
	claims["iss"] = "not-ponti"
	req = httptest.NewRequest(http.MethodGet, "/v1/requests", nil)
	req.Header.Set("Authorization", "Bearer "+mintHS256JWT(t, "local-dev-ponti-jwt-secret", claims))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401 con issuer inválido, obtuvo %d", rec.Code)
	}
}

// TestAuthMiddlewareProductJWTKeepsAPIKeyPath verifica que el API key path no
// cambia al configurar product JWT keys.
func TestAuthMiddlewareProductJWTKeepsAPIKeyPath(t *testing.T) {
	t.Parallel()
	mw := newProductJWTMiddleware(t)

	var got identityhttp.Context
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = identityhttp.FromRequest(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/requests", nil)
	req.Header.Set("X-API-Key", "nexus-test-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("api key: esperaba 204, obtuvo %d", rec.Code)
	}
	if got.AuthMethod != "api_key" || got.OrgID != "acme" {
		t.Fatalf("api key path alterado: %+v", got)
	}
}
