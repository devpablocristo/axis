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
	EnsureActive(ctx context.Context, productID, userID string) error
}

type Options struct {
	DefaultPrincipalID  string
	InternalAuthSecret  string
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
	router.Any("/capability-stats", h.ForwardCompanion)
	router.Any("/quota-policies", h.ForwardCompanion)
	router.Any("/quota-policies/*path", h.ForwardCompanion)
	router.Any("/learning", h.ForwardCompanion)
	router.Any("/learning/*path", h.ForwardCompanion)
	router.Any("/runtime/mcp-policy", h.ForwardCompanion)
	router.Any("/runtime/mcp-policy/*path", h.ForwardCompanion)
	router.Any("/runtime/mcp-invocations", h.ForwardCompanion)
	router.POST("/mcp", h.ForwardCompanion)
	router.Any("/job-roles", h.ForwardCompanion)
	router.Any("/job-roles/*path", h.ForwardCompanion)
	router.Any("/work-subjects", h.ForwardCompanion)
	router.Any("/work-subjects/*path", h.ForwardCompanion)
	router.Any("/routing-pools", h.ForwardCompanion)
	router.Any("/routing-pools/*path", h.ForwardCompanion)
	router.Any("/virployee-routing", h.ForwardCompanion)
	router.Any("/virployee-routing/*path", h.ForwardCompanion)
	router.Any("/virployee-routing:resolve", h.ForwardCompanion)
	router.Any("/knowledge-bases", h.ForwardCompanion)
	router.Any("/knowledge-bases/*path", h.ForwardCompanion)
	router.Any("/professional-policy-packs", h.ForwardCompanion)
	router.Any("/professional-policy-packs/*path", h.ForwardCompanion)
	router.Any("/profile-templates", h.ForwardCompanion)
	router.Any("/profile-templates/*path", h.ForwardCompanion)
	router.Any("/virployees", h.ForwardCompanion)
	router.Any("/virployees/*path", h.ForwardCompanion)
	router.Any("/assist-cases", h.ForwardCompanion)
	router.Any("/assist-cases/*path", h.ForwardCompanion)
	router.Any("/orchestration-policies", h.ForwardCompanion)
	router.Any("/orchestration-policies/*path", h.ForwardCompanion)
	router.Any("/specialist-routes", h.ForwardCompanion)
	router.Any("/specialist-routes/*path", h.ForwardCompanion)
	router.Any("/handoffs", h.ForwardCompanion)
	router.Any("/handoffs/*path", h.ForwardCompanion)
	router.Any("/human-reviews", h.ForwardCompanion)
	router.Any("/human-reviews/*path", h.ForwardCompanion)
	router.Any("/action-types", h.ForwardNexus)
	router.Any("/action-types/*path", h.ForwardNexus)
	router.Any("/governance", h.ForwardNexus)
	router.Any("/governance/*path", h.ForwardNexus)
	router.Any("/approvals", h.ForwardNexus)
	router.Any("/approvals/*path", h.ForwardNexus)
	router.Any("/role-definitions", h.ForwardNexus)
	router.Any("/role-grants", h.ForwardNexus)
	router.Any("/role-grants/*path", h.ForwardNexus)
	router.Any("/governance-policies", h.ForwardNexus)
	router.Any("/governance-policies/*path", h.ForwardNexus)
	router.Any("/governance-policy-versions", h.ForwardNexus)
	router.Any("/governance-policy-versions/*path", h.ForwardNexus)
	router.Any("/governance-policy-promotions", h.ForwardNexus)
	router.Any("/governance-policy-promotions/*path", h.ForwardNexus)
	router.Any("/governance-policy-evaluations", h.ForwardNexus)
	router.Any("/governance-policy-changelog", h.ForwardNexus)
	router.GET("/operations/overview", h.OperationsOverview)
	router.Any("/operations/fleet", h.ForwardCompanion)
	router.Any("/operations/reconciliations", h.ForwardOperationsReconciliations)
	router.Any("/operations/reconciliations/*path", h.ForwardOperationsReconciliations)
	router.GET("/operations/jobs", h.ForwardOperationsJobList)
	router.Any("/operations/jobs/:service/:job_id", h.ForwardOperationsJob)
	router.Any("/operations/jobs/:service/:job_id/*path", h.ForwardOperationsJob)
	router.Any("/operations/worker-controls", h.ForwardOperationsWorkerControls)
	router.Any("/operations/outbox", h.ForwardCompanion)
	router.Any("/operations/outbox/*path", h.ForwardCompanion)
	router.Any("/operations/incidents", h.ForwardNexus)
	router.Any("/operations/incidents/*path", h.ForwardNexus)
	router.Any("/operations/slos", h.ForwardNexus)
	router.Any("/operations/notifications", h.ForwardNexus)
	router.Any("/operations/legal-holds", h.ForwardNexus)
	router.Any("/operations/legal-holds/*path", h.ForwardNexus)
	router.Any("/operations/exports", h.ForwardNexus)
	router.Any("/operations/exports/*path", h.ForwardNexus)
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

func (h *Handler) ForwardOperationsReconciliations(c *gin.Context) {
	if strings.EqualFold(c.Query("service"), "nexus") {
		h.ForwardNexus(c)
		return
	}
	h.ForwardCompanion(c)
}

func (h *Handler) ForwardOperationsJobList(c *gin.Context) {
	if strings.EqualFold(c.Query("service"), "nexus") {
		h.ForwardNexus(c)
		return
	}
	h.ForwardCompanion(c)
}

func (h *Handler) ForwardOperationsWorkerControls(c *gin.Context) {
	if strings.EqualFold(c.Query("service"), "nexus") {
		h.ForwardNexus(c)
		return
	}
	h.ForwardCompanion(c)
}

func (h *Handler) ForwardOperationsJob(c *gin.Context) {
	service := strings.ToLower(strings.TrimSpace(c.Param("service")))
	if service != "companion" && service != "nexus" {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_service", "service must be companion or nexus")
		return
	}
	originalPath := c.Request.URL.Path
	prefix := "/api/operations/jobs/" + service + "/"
	if !strings.HasPrefix(originalPath, prefix) {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_path", "invalid operations job path")
		return
	}
	c.Request.URL.Path = "/api/operations/jobs/" + strings.TrimPrefix(originalPath, prefix)
	defer func() { c.Request.URL.Path = originalPath }()
	if service == "nexus" {
		h.ForwardNexus(c)
		return
	}
	h.ForwardCompanion(c)
}

func (h *Handler) OperationsOverview(c *gin.Context) {
	resolved, err := h.ucs.Resolve(c.Request.Context(), gatewaydomain.ResolveInput{OrgID: c.GetHeader("X-Org-ID"), ProductSurface: c.GetHeader("X-Product-Surface"), PrincipalID: h.principalID(c)})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	type result struct {
		name   string
		value  json.RawMessage
		status int
		err    error
	}
	results := make(chan result, 2)
	for name, target := range map[string]func(string, string) string{"companion": h.ucs.TargetURL, "nexus": h.ucs.NexusTargetURL} {
		go func(name string, target func(string, string) string) {
			req, reqErr := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target(c.Request.URL.Path, c.Request.URL.RawQuery), nil)
			if reqErr != nil {
				results <- result{name: name, err: reqErr}
				return
			}
			h.setTrustedHeaders(req, resolved)
			resp, callErr := h.client.Do(req)
			if callErr != nil {
				results <- result{name: name, err: callErr}
				return
			}
			defer func() { _ = resp.Body.Close() }()
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			results <- result{name: name, value: body, status: resp.StatusCode, err: readErr}
		}(name, target)
	}
	services := map[string]any{}
	available := 0
	for range 2 {
		item := <-results
		if item.err != nil || item.status >= 500 {
			services[item.name] = map[string]any{"status": "unavailable"}
			continue
		}
		var decoded any
		if json.Unmarshal(item.value, &decoded) != nil {
			services[item.name] = map[string]any{"status": "unavailable"}
			continue
		}
		services[item.name] = decoded
		available++
	}
	status := "healthy"
	switch available {
	case 1:
		status = "partial"
	case 0:
		status = "unavailable"
	}
	ginmw.WriteJSON(c, http.StatusOK, map[string]any{"status": status, "services": services})
}

func (h *Handler) setTrustedHeaders(req *http.Request, resolved gatewaydomain.ResolvedContext) {
	req.Header.Set("X-Actor-ID", resolved.PrincipalID)
	req.Header.Set("X-Org-ID", resolved.OrgID)
	req.Header.Set("X-Org-ID", resolved.OrgID)
	req.Header.Set("X-Product-Surface", resolved.ProductSurface)
	req.Header.Set("X-Axis-Forwarded-By", "bff-v2")
	req.Header.Set("X-Axis-Org-Role", resolved.MembershipRole)
	req.Header.Set("X-Axis-Internal-Token", h.options.InternalAuthSecret)
}

func (h *Handler) forward(c *gin.Context, targetURL func(string, string) string, validateSupervisor bool) {
	resolved, err := h.ucs.Resolve(c.Request.Context(), gatewaydomain.ResolveInput{
		OrgID: c.GetHeader("X-Org-ID"), ProductSurface: c.GetHeader("X-Product-Surface"),
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
	if err := h.validateRoleGrantUser(c, resolved); err != nil {
		ginmw.Respond(c, err)
		return
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
	req.Header.Del("X-Axis-Org-Role")
	req.Header.Del("X-Axis-Internal-Token")
	req.Header.Del("X-Axis-Functional-Roles")
	req.Header.Del("X-Axis-Role-Grants")
	req.Header.Del("X-Axis-Permissions")
	req.Header.Del("X-Permissions")
	req.Header.Del("X-Roles")
	req.Header.Set("X-Actor-ID", resolved.PrincipalID)
	req.Header.Set("X-Org-ID", resolved.OrgID)
	req.Header.Set("X-Org-ID", resolved.OrgID)
	req.Header.Set("X-Product-Surface", resolved.ProductSurface)
	req.Header.Set("X-Axis-Forwarded-By", "bff-v2")
	req.Header.Set("X-Axis-Org-Role", resolved.MembershipRole)
	req.Header.Set("X-Axis-Internal-Token", h.options.InternalAuthSecret)

	resp, err := h.client.Do(req)
	if err != nil {
		if ginmw.IsBodyTooLarge(err) {
			ginmw.WriteError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "request body exceeds the allowed size")
			return
		}
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "downstream request failed")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	for _, header := range []string{"Content-Disposition", "X-Manifest-SHA256"} {
		if value := resp.Header.Get(header); value != "" {
			c.Header(header, value)
		}
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
	if err := h.options.SupervisorValidator.EnsureActive(c.Request.Context(), resolved.OrgID, payload.SupervisorUserID); err != nil {
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

func (h *Handler) validateRoleGrantUser(c *gin.Context, resolved gatewaydomain.ResolvedContext) error {
	if h.options.SupervisorValidator == nil || c.Request.Method != http.MethodPost || !isRoleGrantCollection(c.Request.URL.Path) {
		return nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return domainerr.Validation("invalid request body")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		UserID string `json:"user_id"`
	}
	if len(bytes.TrimSpace(body)) == 0 || json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.UserID) == "" {
		return domainerr.Validation("user_id is required")
	}
	if err := h.options.SupervisorValidator.EnsureActive(c.Request.Context(), resolved.OrgID, payload.UserID); err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}

func isRoleGrantCollection(requestPath string) bool {
	path := strings.Trim(strings.TrimPrefix(requestPath, "/api"), "/")
	return path == "role-grants"
}

func (h *Handler) principalID(c *gin.Context) string {
	principalID := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if principalID == "" {
		principalID = strings.TrimSpace(h.options.DefaultPrincipalID)
	}
	return principalID
}
