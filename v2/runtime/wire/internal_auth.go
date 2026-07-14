package wire

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

const internalAuthHeader = "X-Axis-Internal-Token"

func internalAuthMiddleware(secret string) gin.HandlerFunc {
	secret = strings.TrimSpace(secret)
	return func(c *gin.Context) {
		provided := strings.TrimSpace(c.GetHeader(internalAuthHeader))
		if secret == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			ginmw.WriteError(c, http.StatusUnauthorized, "unauthorized", "internal authentication is required")
			c.Abort()
			return
		}
		if strings.TrimSpace(c.GetHeader("X-Tenant-ID")) == "" || strings.TrimSpace(c.GetHeader("X-Actor-ID")) == "" {
			ginmw.WriteError(c, http.StatusUnauthorized, "unauthorized", "trusted tenant and actor are required")
			c.Abort()
			return
		}
		c.Next()
	}
}
