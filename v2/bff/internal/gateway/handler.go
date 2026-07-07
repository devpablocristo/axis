package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	gatewaydomain "github.com/devpablocristo/bff-v2/internal/gateway/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Resolve(ctx context.Context, input gatewaydomain.ResolveInput) (gatewaydomain.ResolvedContext, error)
	TargetURL(requestPath, rawQuery string) string
	NexusTargetURL(requestPath, rawQuery string) string
}

type SupervisorValidatorPort interface {
	EnsureActive(ctx context.Context, tenantID, userID string) error
}

type Options struct {
	DefaultPrincipalID  string
	Client              *http.Client
	SupervisorValidator SupervisorValidatorPort
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
	router.Any("/capabilities", h.ForwardCompanion)
	router.Any("/capabilities/*path", h.ForwardCompanion)
	router.Any("/job-roles", h.ForwardCompanion)
	router.Any("/job-roles/*path", h.ForwardCompanion)
	router.Any("/profile-templates", h.ForwardCompanion)
	router.Any("/profile-templates/*path", h.ForwardCompanion)
	router.Any("/virployees", h.ForwardCompanion)
	router.Any("/virployees/*path", h.ForwardCompanion)
	router.Any("/action-types", h.ForwardNexus)
	router.Any("/action-types/*path", h.ForwardNexus)
	router.Any("/governance", h.ForwardNexus)
	router.Any("/governance/*path", h.ForwardNexus)
}

func (h *Handler) ForwardVirployees(c *gin.Context) {
	h.ForwardCompanion(c)
}

func (h *Handler) ForwardCompanion(c *gin.Context) {
	h.forward(c, h.ucs.TargetURL, true)
}

func (h *Handler) ForwardNexus(c *gin.Context) {
	h.forward(c, h.ucs.NexusTargetURL, false)
}

func (h *Handler) forward(c *gin.Context, targetURL func(string, string) string, validateSupervisor bool) {
	resolved, err := h.ucs.Resolve(c.Request.Context(), gatewaydomain.ResolveInput{
		TenantID:    c.GetHeader("X-Tenant-ID"),
		PrincipalID: h.principalID(c),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	if validateSupervisor {
		if err := h.validateVirployeeSupervisor(c, resolved); err != nil {
			ginmw.Respond(c, err)
			return
		}
	}

	req, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		targetURL(c.Request.URL.Path, c.Request.URL.RawQuery),
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

func (h *Handler) validateVirployeeSupervisor(c *gin.Context, resolved gatewaydomain.ResolvedContext) error {
	if h.options.SupervisorValidator == nil || !requiresSupervisorValidation(c.Request.Method, c.Request.URL.Path) {
		return nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return domainerr.Validation("invalid request body")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		SupervisorUserID string `json:"supervisor_user_id"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return domainerr.Validation("invalid request body")
		}
	}
	if err := h.options.SupervisorValidator.EnsureActive(c.Request.Context(), resolved.TenantID, payload.SupervisorUserID); err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}

func requiresSupervisorValidation(method, requestPath string) bool {
	if method != http.MethodPost && method != http.MethodPut {
		return false
	}
	path := strings.Trim(strings.TrimPrefix(requestPath, "/api"), "/")
	parts := strings.Split(path, "/")
	if method == http.MethodPost {
		return len(parts) == 1 && parts[0] == "virployees"
	}
	return len(parts) == 2 && parts[0] == "virployees" && parts[1] != ""
}

func (h *Handler) principalID(c *gin.Context) string {
	principalID := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if principalID == "" {
		principalID = strings.TrimSpace(h.options.DefaultPrincipalID)
	}
	return principalID
}
