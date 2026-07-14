package session

import (
	"strings"

	"github.com/gin-gonic/gin"

	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

const authenticatedSessionKey = "axis.authenticated_session"

// NewAuthenticationMiddleware authenticates every BFF API request and replaces
// caller-controlled identity headers with values resolved by the configured
// identity provider. Development mode may intentionally use the dev headers.
func NewAuthenticationMiddleware(ucs UseCasesPort, trustDevHeaders bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if trustDevHeaders && c.Request.URL.Path != "/api/session" {
			c.Request.Header.Del("X-Axis-Tenant-Role")
			c.Request.Header.Del("X-Axis-Internal-Token")
			c.Next()
			return
		}
		input := sessiondomain.ResolveInput{Authorization: c.GetHeader("Authorization")}
		if trustDevHeaders {
			input.PrincipalID = c.GetHeader("X-Actor-ID")
			input.Email = c.GetHeader("X-Actor-Email")
			input.OrgID = c.GetHeader("X-Axis-Org-ID")
		}
		resolved, err := ucs.Resolve(c.Request.Context(), input)
		if err != nil {
			ginmw.Respond(c, err)
			c.Abort()
			return
		}

		c.Request.Header.Del("X-Actor-ID")
		c.Request.Header.Del("X-Actor-Email")
		c.Request.Header.Del("X-Axis-Org-ID")
		c.Request.Header.Del("X-Axis-Tenant-Role")
		c.Request.Header.Del("X-Axis-Internal-Token")
		c.Request.Header.Set("X-Actor-ID", strings.TrimSpace(resolved.PrincipalID))
		c.Request.Header.Set("X-Actor-Email", strings.TrimSpace(resolved.User.Email))
		c.Request.Header.Set("X-Axis-Org-ID", strings.TrimSpace(resolved.OrgID))
		c.Set(authenticatedSessionKey, resolved)
		c.Next()
	}
}

func authenticatedSession(c *gin.Context) (sessiondomain.Session, bool) {
	value, ok := c.Get(authenticatedSessionKey)
	if !ok {
		return sessiondomain.Session{}, false
	}
	resolved, ok := value.(sessiondomain.Session)
	return resolved, ok
}
