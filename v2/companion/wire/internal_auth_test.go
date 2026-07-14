package wire

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestInternalAuthMiddlewareRejectsUntrustedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(internalAuthMiddleware("trusted-secret"))
	router.GET("/v1/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, test := range []struct {
		name   string
		token  string
		tenant string
		actor  string
	}{
		{name: "missing token", tenant: "tenant-1", actor: "actor-1"},
		{name: "wrong token", token: "wrong", tenant: "tenant-1", actor: "actor-1"},
		{name: "missing actor", token: "trusted-secret", tenant: "tenant-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
			req.Header.Set(internalAuthHeader, test.token)
			req.Header.Set("X-Tenant-ID", test.tenant)
			req.Header.Set("X-Actor-ID", test.actor)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
		})
	}
}

func TestInternalAuthMiddlewareAcceptsTrustedContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(internalAuthMiddleware("trusted-secret"))
	router.GET("/v1/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set(internalAuthHeader, "trusted-secret")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	req.Header.Set("X-Actor-ID", "actor-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}
