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
	router.POST("/enrich", h.Enrich)
	router.POST("/answer", h.Answer)
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

func (h *Handler) Enrich(c *gin.Context) {
	var req EnrichRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "invalid enrich request")
		return
	}
	resp, err := h.planner.Enrich(c.Request.Context(), req)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "runtime_error", "runtime enrichment failed")
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) Answer(c *gin.Context) {
	var req AnswerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "invalid answer request")
		return
	}
	resp, err := h.planner.Answer(c.Request.Context(), req)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "runtime_error", "runtime answer failed")
		return
	}
	c.JSON(http.StatusOK, resp)
}
