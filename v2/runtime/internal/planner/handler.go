package planner

import (
	"net/http"

	"github.com/gin-gonic/gin"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type Handler struct {
	planner *Planner
}

func NewHandler(planner *Planner) *Handler {
	return &Handler{planner: planner}
}

func (h *Handler) Routes(router gin.IRouter) {
	router.POST("/propose", h.Propose)
}

func (h *Handler) Propose(c *gin.Context) {
	var req ProposeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "invalid propose request")
		return
	}
	resp, err := h.planner.Propose(c.Request.Context(), req)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "runtime_error", "runtime proposal failed")
		return
	}
	c.JSON(http.StatusOK, resp)
}
