package professionalauthority

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestHandlerScopePolicyUsesTrustedTenantRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &fakeRepository{}
	handler := NewHandler(NewUseCases(repo))
	router := gin.New()
	handler.Routes(router)
	id := uuid.New()

	do := func(role string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/virployees/"+id.String()+"/scope-policy",
			strings.NewReader(`{"allowed_topics":["appointments"],"prohibited_topics":["diagnosis"],"out_of_scope":"escalate","expected_revision":0}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "tenant-a")
		req.Header.Set("X-Actor-ID", "user-a")
		req.Header.Set("X-Axis-Tenant-Role", role)
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("member"); rec.Code != http.StatusForbidden {
		t.Fatalf("member status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := do("admin"); rec.Code != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.putScopeCalls != 1 {
		t.Fatalf("only authorized request may mutate, calls=%d", repo.putScopeCalls)
	}
}
