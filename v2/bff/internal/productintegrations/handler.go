package productintegrations

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

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
	g := r.Group("/organizations/:org_id/products/:product_id/integration")
	g.GET("", h.get)
	g.GET("/versions", h.get)
	g.POST("/versions", h.createVersion)
	g.POST("/versions/:version_id/validate", h.validate)
	g.POST("/versions/:version_id/activate", h.activate)
	g.POST("/suspend", h.suspend)
	g.POST("/retire", h.retire)
	g.GET("/readiness", h.readiness)
	g.GET("/credentials", h.listCredentials)
	g.POST("/credentials", h.createCredential)
	g.POST("/credentials/:credential_id/rotate", h.rotateCredential)
	g.POST("/credentials/:credential_id/revoke", h.revokeCredential)
}

func parseContext(c *gin.Context) (uuid.UUID, uuid.UUID, string, bool) {
	orgID, orgErr := uuid.Parse(strings.TrimSpace(c.Param("org_id")))
	productID, productErr := uuid.Parse(strings.TrimSpace(c.Param("product_id")))
	actor := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if orgErr != nil || productErr != nil || actor == "" {
		ginmw.Respond(c, domainerr.Validation("organization, product, and authenticated actor are required"))
		return uuid.Nil, uuid.Nil, "", false
	}
	return orgID, productID, actor, true
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

func parseResourceID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param(name)))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation(name+" must be a UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) get(c *gin.Context) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	integration, versions, err := h.service.Get(c, orgID, productID, actor)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"integration": integration, "versions": versions})
}

func (h *Handler) createVersion(c *gin.Context) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	var input CreateVersionInput
	if !strictJSON(c, &input) {
		return
	}
	out, created, err := h.service.CreateVersion(c, orgID, productID, actor, input)
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
	orgID, productID, actor, versionID, ok := versionContext(c)
	if !ok {
		return
	}
	out, err := h.service.Validate(c, orgID, productID, versionID, actor)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) activate(c *gin.Context) {
	orgID, productID, actor, versionID, ok := versionContext(c)
	if !ok {
		return
	}
	out, err := h.service.Activate(c, orgID, productID, versionID, actor)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func versionContext(c *gin.Context) (uuid.UUID, uuid.UUID, string, uuid.UUID, bool) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return uuid.Nil, uuid.Nil, "", uuid.Nil, false
	}
	versionID, ok := parseResourceID(c, "version_id")
	if !ok {
		return uuid.Nil, uuid.Nil, "", uuid.Nil, false
	}
	return orgID, productID, actor, versionID, true
}

func (h *Handler) suspend(c *gin.Context) { h.lifecycle(c, "suspended") }
func (h *Handler) retire(c *gin.Context)  { h.lifecycle(c, "retired") }

func (h *Handler) lifecycle(c *gin.Context, state string) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	out, err := h.service.ChangeLifecycle(c, orgID, productID, actor, state)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) readiness(c *gin.Context) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	out, err := h.service.Readiness(c, orgID, productID, actor)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) listCredentials(c *gin.Context) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	out, err := h.service.ListCredentials(c, orgID, productID, actor)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"items": out})
}

func (h *Handler) createCredential(c *gin.Context) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	var input CreateCredentialInput
	if !strictJSON(c, &input) {
		return
	}
	out, err := h.service.CreateCredential(c, orgID, productID, actor, input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusCreated, out)
}

func (h *Handler) rotateCredential(c *gin.Context) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	credentialID, ok := parseResourceID(c, "credential_id")
	if !ok {
		return
	}
	out, err := h.service.RotateCredential(c, orgID, productID, credentialID, actor)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusCreated, out)
}

func (h *Handler) revokeCredential(c *gin.Context) {
	orgID, productID, actor, ok := parseContext(c)
	if !ok {
		return
	}
	credentialID, ok := parseResourceID(c, "credential_id")
	if !ok {
		return
	}
	if err := h.service.RevokeCredential(c, orgID, productID, credentialID, actor); err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteNoContent(c)
}
