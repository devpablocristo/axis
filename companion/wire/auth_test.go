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

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/authn/go/internaljwt"
)

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
		"aud":               "companion",
		"sub":               "ponti-backend",
		"actor_id":          "ponti-backend",
		"actor_type":        "service",
		"org_id":            "org-1",
		"product_surface":   "ponti",
		"scope":             "companion:tasks:read companion:tasks:write",
		"service_principal": true,
		"on_behalf_of":      "user-9",
		"exp":               time.Now().Add(time.Minute).Unix(),
		"iat":               time.Now().Unix(),
	}
}

func newProductJWTMiddleware(t *testing.T) func(http.Handler) http.Handler {
	t.Helper()
	mw, err := newAuthMiddleware(
		"admin=companion-test-key|org=acme|scope=companion:tasks:read|service=true",
		"", "",
		internaljwt.Config{
			Secret:   "axis-internal-secret",
			Issuer:   "axis-bff",
			Audience: "companion",
		},
		"ponti=local-dev-ponti-jwt-secret|issuer=ponti-core",
	)
	if err != nil {
		t.Fatal(err)
	}
	return mw
}

// TestAuthMiddlewareAcceptsProductJWT verifica que un JWT firmado con la key
// per-producto (COMPANION_PRODUCT_JWT_KEYS) autentica y que org_id,
// product_surface, scopes y on_behalf_of fluyen al IdentityContext con
// AuthMethod "product_jwt".
func TestAuthMiddlewareAcceptsProductJWT(t *testing.T) {
	t.Parallel()
	mw := newProductJWTMiddleware(t)

	var got identityctx.IdentityContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = identityctx.FromRequest(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+mintHS256JWT(t, "local-dev-ponti-jwt-secret", pontiProductClaims()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperaba 204, obtuvo %d: %s", rec.Code, rec.Body.String())
	}
	if got.CustomerOrgID != "org-1" {
		t.Fatalf("customer_org_id = %q, esperaba org-1", got.CustomerOrgID)
	}
	if got.ProductSurface != "ponti" {
		t.Fatalf("product_surface = %q, esperaba ponti", got.ProductSurface)
	}
	if !got.HasScope("companion:tasks:write") {
		t.Fatalf("esperaba scope companion:tasks:write, obtuvo %v", got.Scopes)
	}
	if got.AuthMethod != "product_jwt" {
		t.Fatalf("auth_method = %q, esperaba product_jwt", got.AuthMethod)
	}
	if got.AuthMethod == "api_key" {
		t.Fatal("un JWT de producto jamás debe presentarse como api_key")
	}
	if !got.ServicePrincipal || got.OnBehalfOf != "user-9" {
		t.Fatalf("esperaba service principal delegado, obtuvo %+v", got)
	}
}

// TestAuthMiddlewareRejectsProductJWTWrongSecret verifica que un token firmado
// con otra key (o un issuer ajeno) no autentica.
func TestAuthMiddlewareRejectsProductJWTWrongSecret(t *testing.T) {
	t.Parallel()
	mw := newProductJWTMiddleware(t)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("el handler no debe ejecutarse con credencial inválida")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+mintHS256JWT(t, "wrong-secret", pontiProductClaims()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401 con secret inválido, obtuvo %d", rec.Code)
	}

	claims := pontiProductClaims()
	claims["iss"] = "not-ponti"
	req = httptest.NewRequest(http.MethodGet, "/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+mintHS256JWT(t, "local-dev-ponti-jwt-secret", claims))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401 con issuer inválido, obtuvo %d", rec.Code)
	}
}

// TestAuthMiddlewareProductJWTKeepsExistingPaths verifica que agregar product
// JWT keys no rompe los paths existentes: API key y JWT interno de plataforma
// siguen autenticando con sus AuthMethod originales.
func TestAuthMiddlewareProductJWTKeepsExistingPaths(t *testing.T) {
	t.Parallel()
	mw := newProductJWTMiddleware(t)

	var got identityctx.IdentityContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = identityctx.FromRequest(r)
		w.WriteHeader(http.StatusNoContent)
	}))

	// API key path.
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks", nil)
	req.Header.Set("X-API-Key", "companion-test-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("api key: esperaba 204, obtuvo %d", rec.Code)
	}
	if got.AuthMethod != "api_key" || got.CustomerOrgID != "acme" {
		t.Fatalf("api key path alterado: %+v", got)
	}

	// JWT interno de plataforma.
	req = httptest.NewRequest(http.MethodGet, "/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+mintHS256JWT(t, "axis-internal-secret", map[string]any{
		"iss":    "axis-bff",
		"aud":    "companion",
		"sub":    "local-dev-admin",
		"org_id": "org-1",
		"scope":  "companion:tasks:read",
		"exp":    time.Now().Add(time.Minute).Unix(),
	}))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("internal jwt: esperaba 204, obtuvo %d", rec.Code)
	}
	if got.AuthMethod != internaljwt.AuthMethod {
		t.Fatalf("internal jwt auth_method = %q, esperaba %q", got.AuthMethod, internaljwt.AuthMethod)
	}
}
