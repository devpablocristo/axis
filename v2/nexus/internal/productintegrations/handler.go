package productintegrations

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes(r gin.IRouter) {
	r.GET("/product-integrations/:product_id", h.get)
	r.POST("/product-integrations/:product_id/versions", h.createVersion)
	r.POST("/product-integration-versions/:version_id/validate", h.validateVersion)
	r.POST("/product-integration-versions/:version_id/activate", h.activateVersion)
	r.POST("/product-integrations/:product_id/suspend", h.suspend)
	r.GET("/product-integrations/:product_id/readiness", h.readiness)
	r.GET("/operations/served-products", h.servedProducts)
}

func trustedContext(c *gin.Context) (orgID, actor, role, productSurface string) {
	return strings.TrimSpace(c.GetHeader("X-Org-ID")),
		strings.TrimSpace(c.GetHeader("X-Actor-ID")),
		strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Org-Role"))),
		strings.ToLower(strings.TrimSpace(c.GetHeader("X-Product-Surface")))
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	value, err := uuid.Parse(strings.TrimSpace(c.Param(name)))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation(name+" must be a UUID"))
		return uuid.Nil, false
	}
	return value, true
}

func bindStrict(c *gin.Context, out any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		ginmw.Respond(c, domainerr.Validation("product integration payload is invalid"))
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		ginmw.Respond(c, domainerr.Validation("product integration payload must contain one JSON object"))
		return false
	}
	return true
}

func (h *Handler) createVersion(c *gin.Context) {
	productID, ok := parseUUIDParam(c, "product_id")
	if !ok {
		return
	}
	var input CreateVersionInput
	if !bindStrict(c, &input) {
		return
	}
	orgID, actor, role, surface := trustedContext(c)
	out, created, err := h.service.CreateVersion(c, orgID, actor, role, productID, surface, input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ginmw.WriteJSON(c, status, out)
}

func (h *Handler) get(c *gin.Context) {
	productID, ok := parseUUIDParam(c, "product_id")
	if !ok {
		return
	}
	orgID, actor, _, _ := trustedContext(c)
	integration, version, err := h.service.GetIntegration(c, orgID, actor, productID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"integration": integration, "active_version": version})
}

func (h *Handler) validateVersion(c *gin.Context) {
	versionID, ok := parseUUIDParam(c, "version_id")
	if !ok {
		return
	}
	orgID, actor, role, _ := trustedContext(c)
	out, err := h.service.ValidateVersion(c, orgID, actor, role, versionID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) activateVersion(c *gin.Context) {
	versionID, ok := parseUUIDParam(c, "version_id")
	if !ok {
		return
	}
	orgID, actor, role, _ := trustedContext(c)
	out, err := h.service.ActivateVersion(c, orgID, actor, role, versionID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) suspend(c *gin.Context) {
	productID, ok := parseUUIDParam(c, "product_id")
	if !ok {
		return
	}
	orgID, actor, role, _ := trustedContext(c)
	out, err := h.service.Suspend(c, orgID, actor, role, productID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) readiness(c *gin.Context) {
	productID, ok := parseUUIDParam(c, "product_id")
	if !ok {
		return
	}
	orgID, actor, _, _ := trustedContext(c)
	out, err := h.service.Readiness(c, orgID, actor, productID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) servedProducts(c *gin.Context) {
	orgID, actor, _, _ := trustedContext(c)
	window := 24 * time.Hour
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			ginmw.Respond(c, domainerr.Validation("window must be a duration such as 24h"))
			return
		}
		window = parsed
	}
	out, err := h.service.ListServedProducts(c, orgID, actor, c.Query("product"), window)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"items": out})
}

func (h *Handler) RuntimeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/v1/product-integrations") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1/product-integration-versions") ||
			strings.HasSuffix(c.Request.URL.Path, "/operations/served-products") {
			c.Next()
			return
		}
		started := time.Now()
		runtime, hasProduct, hasIntegration := runtimeContextFromHeaders(c)
		actionType := ""
		if hasIntegration {
			actionType = readActionType(c)
			if err := h.service.ValidateRuntimeContext(c, runtime, actionType); err != nil {
				ginmw.Respond(c, err)
				h.record(c, runtime, started, hasProduct)
				return
			}
		}
		c.Next()
		h.record(c, runtime, started, hasProduct)
	}
}

func (h *Handler) record(c *gin.Context, runtime RuntimeContext, started time.Time, hasProduct bool) {
	if !hasProduct {
		return
	}
	area := operationArea(c.FullPath())
	if area == "" {
		area = operationArea(c.Request.URL.Path)
	}
	if area == "" {
		return
	}
	status := c.Writer.Status()
	errorCode := ""
	if status >= 400 {
		errorCode = "http_" + strconv.Itoa(status)
	}
	_ = h.service.RecordObservation(c, Observation{
		RuntimeContext: runtime,
		Area:           area, StatusCode: status, Latency: time.Since(started),
		ErrorCode: errorCode, ObservedAt: time.Now().UTC(),
	})
}

func runtimeContextFromHeaders(c *gin.Context) (RuntimeContext, bool, bool) {
	productIDRaw := strings.TrimSpace(c.GetHeader("X-Product-ID"))
	if productIDRaw == "" {
		productIDRaw = strings.TrimSpace(c.GetHeader("X-Axis-Product-ID"))
	}
	productID, productErr := uuid.Parse(productIDRaw)
	integrationID, integrationErr := uuid.Parse(strings.TrimSpace(c.GetHeader("X-Axis-Integration-ID")))
	revision, revisionErr := strconv.ParseInt(strings.TrimSpace(c.GetHeader("X-Axis-Integration-Version")), 10, 64)
	runtime := RuntimeContext{
		OrgID: strings.TrimSpace(c.GetHeader("X-Org-ID")), ProductID: productID,
		ProductSurface: strings.ToLower(strings.TrimSpace(c.GetHeader("X-Product-Surface"))),
		IntegrationID:  integrationID, IntegrationRevision: revision,
		IntegrationHash: strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Integration-Hash"))),
		AccessMode:      strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Access-Mode"))),
	}
	hasProduct := productErr == nil && productID != uuid.Nil && runtime.OrgID != ""
	hasAnyIntegration := strings.TrimSpace(c.GetHeader("X-Axis-Integration-ID")) != "" ||
		strings.TrimSpace(c.GetHeader("X-Axis-Integration-Version")) != "" ||
		strings.TrimSpace(c.GetHeader("X-Axis-Integration-Hash")) != ""
	hasIntegration := hasAnyIntegration && integrationErr == nil && revisionErr == nil
	if hasAnyIntegration && !hasIntegration {
		// Preserve the partial context so validation fails closed.
		hasIntegration = true
	}
	if runtime.AccessMode == "" {
		runtime.AccessMode = AccessModeDirect
	} else {
		runtime.AccessMode = canonicalAccessMode(runtime.AccessMode)
	}
	return runtime, hasProduct, hasIntegration
}

func readActionType(c *gin.Context) string {
	if c.Request.Body == nil || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 {
		return ""
	}
	var value map[string]any
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	for _, key := range []string{"action_type", "action_type_key"} {
		if raw, ok := value[key].(string); ok {
			return strings.ToLower(strings.TrimSpace(raw))
		}
	}
	if action, ok := value["action"].(map[string]any); ok {
		if raw, ok := action["action_type"].(string); ok {
			return strings.ToLower(strings.TrimSpace(raw))
		}
	}
	return ""
}

func operationArea(path string) string {
	switch {
	case strings.Contains(path, "/authorization"):
		return "authorization"
	case strings.Contains(path, "/governance-policies"), strings.Contains(path, "/governance"):
		return "policy_evaluation"
	case strings.Contains(path, "/approvals"):
		return "approval"
	case strings.Contains(path, "/audit"), strings.Contains(path, "/evidence"):
		return "audit_evidence"
	case strings.Contains(path, "/jobs"):
		return "jobs"
	case strings.Contains(path, "/operations"):
		return "incidents_operations"
	default:
		return ""
	}
}
