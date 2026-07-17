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
		// The runtime is a stateless classifier: it receives everything it needs
		// (input + capabilities) in the request body and holds no per-tenant
		// data, so the shared internal token is the only trust boundary.
		provided := strings.TrimSpace(c.GetHeader(internalAuthHeader))
		if secret == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			ginmw.WriteError(c, http.StatusUnauthorized, "unauthorized", "internal authentication is required")
			c.Abort()
			return
		}
		c.Next()
	}
}
