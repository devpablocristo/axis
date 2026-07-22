package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
)

func TestAuthenticationMiddlewareReplacesSpoofedIdentityHeaders(t *testing.T) {
	resolver := &middlewareSessionResolver{session: sessiondomain.Session{
		PrincipalID: "trusted-user",
		OrgID:       "trusted-org",
		User:        userdomain.User{Email: "trusted@example.com"},
	}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(NewAuthenticationMiddleware(resolver, false))
	router.GET("/api/test", func(c *gin.Context) {
		if c.GetHeader("X-Actor-ID") != "trusted-user" || c.GetHeader("X-Actor-Email") != "trusted@example.com" || c.GetHeader("X-Axis-Org-ID") != "trusted-org" {
			t.Fatalf("identity headers were not rebound: %+v", c.Request.Header)
		}
		if c.GetHeader("X-Axis-Org-Role") != "" || c.GetHeader("X-Axis-Internal-Token") != "" {
			t.Fatalf("untrusted internal headers survived: %+v", c.Request.Header)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("X-Actor-ID", "spoofed-user")
	req.Header.Set("X-Actor-Email", "spoofed@example.com")
	req.Header.Set("X-Axis-Org-ID", "spoofed-org")
	req.Header.Set("X-Axis-Org-Role", "owner")
	req.Header.Set("X-Axis-Internal-Token", "spoofed-secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	if resolver.input.PrincipalID != "" || resolver.input.Email != "" || resolver.input.OrgID != "" || resolver.input.Authorization != "Bearer valid" {
		t.Fatalf("Clerk authentication trusted caller identity: %+v", resolver.input)
	}
}

type middlewareSessionResolver struct {
	input   sessiondomain.ResolveInput
	session sessiondomain.Session
	err     error
}

func (f *middlewareSessionResolver) Resolve(_ context.Context, input sessiondomain.ResolveInput) (sessiondomain.Session, error) {
	f.input = input
	return f.session, f.err
}
