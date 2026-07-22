package wire

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

func TestRouteAwareBodySizeLimitOnlyExpandsKnowledgeUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(routeAwareBodySizeLimit(16, 128))
	router.Any("/*path", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if ginmw.IsBodyTooLarge(err) {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	for _, tc := range []struct {
		name, method, path string
		bodyBytes, status  int
	}{
		{name: "upload accepts expanded body", method: http.MethodPost, path: "/api/knowledge-bases/base/ingestions/upload", bodyBytes: 64, status: http.StatusNoContent},
		{name: "connector keeps default", method: http.MethodPost, path: "/api/knowledge-bases/base/ingestions/connector", bodyBytes: 64, status: http.StatusRequestEntityTooLarge},
		{name: "upload remains bounded", method: http.MethodPost, path: "/api/knowledge-bases/base/ingestions/upload", bodyBytes: 129, status: http.StatusRequestEntityTooLarge},
		{name: "non post keeps default", method: http.MethodPut, path: "/api/knowledge-bases/base/ingestions/upload", bodyBytes: 64, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(strings.Repeat("x", tc.bodyBytes)))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d", tc.status, rec.Code)
			}
		})
	}
}
