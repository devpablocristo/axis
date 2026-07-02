package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	gatewaydomain "github.com/devpablocristo/bff-v2/internal/gateway/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Resolve(ctx context.Context, input gatewaydomain.ResolveInput) (gatewaydomain.ResolvedContext, error)
	TargetURL(requestPath, rawQuery string) string
}

type Options struct {
	DefaultPrincipalID string
	Client             *http.Client
}

type Handler struct {
	ucs     UseCasesPort
	options Options
	client  *http.Client
}

func NewHandler(ucs UseCasesPort, options Options) *Handler {
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &Handler{ucs: ucs, options: options, client: client}
}

func (h *Handler) Routes(router gin.IRouter) {
	router.Any("/virployees", h.ForwardVirployees)
	router.Any("/virployees/*path", h.ForwardVirployees)
}

func (h *Handler) ForwardVirployees(c *gin.Context) {
	resolved, err := h.ucs.Resolve(c.Request.Context(), gatewaydomain.ResolveInput{
		TenantID:    c.GetHeader("X-Tenant-ID"),
		PrincipalID: h.principalID(c),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}

	req, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		h.ucs.TargetURL(c.Request.URL.Path, c.Request.URL.RawQuery),
		c.Request.Body,
	)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "bad_gateway", "downstream request failed")
		return
	}
	req.Header = c.Request.Header.Clone()
	req.Header.Del("Cookie")
	req.Header.Del("Authorization")
	req.Header.Set("X-Actor-ID", resolved.PrincipalID)
	req.Header.Set("X-Tenant-ID", resolved.TenantID)
	req.Header.Set("X-Axis-Org-ID", resolved.OrgID)
	req.Header.Set("X-Product-Surface", resolved.ProductSurface)
	req.Header.Set("X-Axis-Forwarded-By", "bff-v2")

	resp, err := h.client.Do(req)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "downstream request failed")
		return
	}
	defer resp.Body.Close()

	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

func (h *Handler) principalID(c *gin.Context) string {
	principalID := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if principalID == "" {
		principalID = strings.TrimSpace(h.options.DefaultPrincipalID)
	}
	return principalID
}
