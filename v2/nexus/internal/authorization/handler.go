package authorization

import (
	"net/http"
	"strings"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

type Handler struct{ ucs *UseCases }

func NewHandler(ucs *UseCases) *Handler { return &Handler{ucs: ucs} }
func (h *Handler) Routes(r gin.IRouter) {
	r.GET("/role-definitions", h.definitions)
	r.GET("/role-grants", h.list)
	r.POST("/role-grants", h.create)
	r.POST("/role-grants/:grant_id/revoke", h.revoke)
	r.POST("/internal/authorization:check", h.check)
}
func (h *Handler) definitions(c *gin.Context) { ginmw.WriteJSON(c, http.StatusOK, h.ucs.Definitions()) }
func (h *Handler) list(c *gin.Context) {
	out, err := h.ucs.List(c, c.GetHeader("X-Tenant-ID"), c.GetHeader("X-Actor-ID"), c.GetHeader("X-Axis-Tenant-Role"), c.Query("user_id"))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}
func (h *Handler) create(c *gin.Context) {
	var in CreateGrantInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	out, err := h.ucs.Create(c, c.GetHeader("X-Tenant-ID"), c.GetHeader("X-Actor-ID"), c.GetHeader("X-Axis-Tenant-Role"), in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusCreated, out)
}
func (h *Handler) revoke(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "grant_id")
	if !ok {
		return
	}
	var in RevokeInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	out, err := h.ucs.Revoke(c, c.GetHeader("X-Tenant-ID"), c.GetHeader("X-Actor-ID"), c.GetHeader("X-Axis-Tenant-Role"), id, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}
func (h *Handler) check(c *gin.Context) {
	var in CheckInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	in.TenantID = strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	if in.ActorID == "" {
		in.ActorID = strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	}
	if in.ActorRole == "" {
		in.ActorRole = strings.TrimSpace(c.GetHeader("X-Axis-Tenant-Role"))
	}
	out, err := h.ucs.Check(c, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}
