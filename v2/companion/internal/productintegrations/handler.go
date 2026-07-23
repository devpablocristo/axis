package productintegrations

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Routes(r gin.IRouter) {
	r.POST("/product-integrations/:product_id/versions", h.create)
	r.POST("/product-integration-versions/:version_id/validate", h.validate)
	r.POST("/product-integration-versions/:version_id/activate", h.activate)
	r.POST("/product-integrations/:product_id/suspend", h.suspend)
	r.GET("/product-integrations/:product_id/readiness", h.readiness)
	r.GET("/operations/served-products", h.served)
}

func contextHeaders(c *gin.Context) (orgID, actor, role, surface string) {
	return strings.TrimSpace(c.GetHeader("X-Org-ID")),
		strings.TrimSpace(c.GetHeader("X-Actor-ID")),
		strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Org-Role"))),
		strings.ToLower(strings.TrimSpace(c.GetHeader("X-Product-Surface")))
}

func parseID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param(name)))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation(name+" must be a UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func strictJSON(c *gin.Context, out any) bool {
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

func (h *Handler) create(c *gin.Context) {
	productID, ok := parseID(c, "product_id")
	if !ok {
		return
	}
	var in CreateVersionInput
	if !strictJSON(c, &in) {
		return
	}
	orgID, actor, role, surface := contextHeaders(c)
	out, created, err := h.service.CreateVersion(c, orgID, actor, role, productID, surface, in)
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

func (h *Handler) validate(c *gin.Context) {
	versionID, ok := parseID(c, "version_id")
	if !ok {
		return
	}
	orgID, actor, role, _ := contextHeaders(c)
	out, err := h.service.Validate(c, orgID, actor, role, versionID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) activate(c *gin.Context) {
	versionID, ok := parseID(c, "version_id")
	if !ok {
		return
	}
	orgID, actor, role, _ := contextHeaders(c)
	out, err := h.service.Activate(c, orgID, actor, role, versionID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) suspend(c *gin.Context) {
	productID, ok := parseID(c, "product_id")
	if !ok {
		return
	}
	orgID, actor, role, _ := contextHeaders(c)
	if err := h.service.Suspend(c, orgID, actor, role, productID); err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteNoContent(c)
}

func (h *Handler) readiness(c *gin.Context) {
	productID, ok := parseID(c, "product_id")
	if !ok {
		return
	}
	orgID, actor, _, _ := contextHeaders(c)
	out, err := h.service.Readiness(c, orgID, actor, productID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) served(c *gin.Context) {
	orgID, actor, _, _ := contextHeaders(c)
	window := 24 * time.Hour
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			ginmw.Respond(c, domainerr.Validation("window must be a duration such as 24h"))
			return
		}
		window = parsed
	}
	out, err := h.service.ListServed(c, orgID, actor, c.Query("product"), window)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"items": out})
}

func (h *Handler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(quotas.WithProductSurface(c.Request.Context(), c.GetHeader("X-Product-Surface")))
		if strings.Contains(c.Request.URL.Path, "/product-integrations") ||
			strings.Contains(c.Request.URL.Path, "/product-integration-versions") ||
			strings.HasSuffix(c.Request.URL.Path, "/operations/served-products") {
			c.Next()
			return
		}
		started := time.Now()
		runtime, hasProduct, hasIntegration := runtimeFromHeaders(c)
		if hasIntegration {
			capabilityID, capabilityKey := readCapabilityIdentity(c)
			if err := h.service.ValidateRuntimeContext(c, runtime, capabilityID, capabilityKey); err != nil {
				ginmw.Respond(c, err)
				_ = h.service.RecordObservation(c, runtime, operationArea(c.Request.URL.Path), c.Writer.Status(), time.Since(started))
				return
			}
		}
		c.Next()
		if hasProduct {
			area := operationArea(c.FullPath())
			if area == "" {
				area = operationArea(c.Request.URL.Path)
			}
			_ = h.service.RecordObservation(c, runtime, area, c.Writer.Status(), time.Since(started))
		}
	}
}

func runtimeFromHeaders(c *gin.Context) (RuntimeContext, bool, bool) {
	productRaw := strings.TrimSpace(c.GetHeader("X-Product-ID"))
	if productRaw == "" {
		productRaw = strings.TrimSpace(c.GetHeader("X-Axis-Product-ID"))
	}
	productID, productErr := uuid.Parse(productRaw)
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
	hasAnyIntegration := c.GetHeader("X-Axis-Integration-ID") != "" ||
		c.GetHeader("X-Axis-Integration-Version") != "" || c.GetHeader("X-Axis-Integration-Hash") != ""
	hasIntegration := hasAnyIntegration && integrationErr == nil && revisionErr == nil
	if hasAnyIntegration && !hasIntegration {
		hasIntegration = true
	}
	return runtime, hasProduct, hasIntegration
}

func readCapabilityIdentity(c *gin.Context) (string, string) {
	if c.Request.Body == nil || c.Request.Method == http.MethodGet {
		return "", ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var value map[string]any
	if json.Unmarshal(body, &value) != nil {
		return "", ""
	}
	capabilityID := ""
	if raw, ok := value["capability_id"].(string); ok {
		capabilityID = strings.TrimSpace(raw)
	}
	for _, key := range []string{"capability_key", "tool_name", "name"} {
		if raw, ok := value[key].(string); ok {
			return capabilityID, strings.ToLower(strings.TrimSpace(raw))
		}
	}
	return capabilityID, ""
}

func operationArea(path string) string {
	switch {
	case strings.Contains(path, "/assist"):
		return "assist"
	case strings.Contains(path, "/mcp"), strings.Contains(path, "/runtime/mcp"):
		return "mcp"
	case strings.Contains(path, "/knowledge"):
		return "knowledge"
	case strings.Contains(path, "/memories"):
		return "memory"
	case strings.Contains(path, "/learning"):
		return "learning"
	case strings.Contains(path, "/watchers"), strings.Contains(path, "/product-events"):
		return "watchers"
	case strings.Contains(path, "/evaluation"):
		return "evaluations"
	case strings.Contains(path, "/finops"):
		return "finops"
	case strings.Contains(path, "/operations"):
		return "jobs"
	case strings.Contains(path, "/capabilities"):
		return "tools"
	default:
		return ""
	}
}
