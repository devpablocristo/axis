package governancepolicies

import (
	"net/http"
	"strconv"
	"strings"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

type Handler struct{ ucs *UseCases }

func NewHandler(ucs *UseCases) *Handler { return &Handler{ucs: ucs} }

func (h *Handler) Routes(router gin.IRouter) {
	router.GET("/governance-policies", h.list)
	router.POST("/governance-policies", h.create)
	router.GET("/governance-policies/:policy_id", h.get)
	router.POST("/governance-policies/:policy_id/versions", h.createVersion)
	router.POST("/governance-policy-versions/:version_id/simulate", h.simulate)
	router.POST("/governance-policy-versions/:version_id/promotions", h.requestPromotion)
	router.POST("/governance-policy-promotions/:promotion_id/approve", h.approvePromotion)
	router.POST("/governance-policy-promotions/:promotion_id/reject", h.rejectPromotion)
	router.GET("/governance-policy-evaluations", h.evaluations)
	router.GET("/governance-policy-promotions", h.promotions)
	router.GET("/governance-policy-changelog", h.changelog)
}

func (h *Handler) list(c *gin.Context) {
	out, err := h.ucs.ListArtifacts(c, organization(c), actor(c), role(c))
	respond(c, http.StatusOK, out, err)
}

func (h *Handler) create(c *gin.Context) {
	var input CreateArtifactInput
	if err := ginmw.BindJSON(c, &input); err != nil {
		return
	}
	out, err := h.ucs.CreateArtifact(c, organization(c), actor(c), role(c), input)
	respond(c, http.StatusCreated, out, err)
}

func (h *Handler) get(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "policy_id")
	if !ok {
		return
	}
	out, err := h.ucs.GetArtifact(c, organization(c), actor(c), role(c), id)
	respond(c, http.StatusOK, out, err)
}

func (h *Handler) createVersion(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "policy_id")
	if !ok {
		return
	}
	var input CreateVersionInput
	if err := ginmw.BindJSON(c, &input); err != nil {
		return
	}
	out, err := h.ucs.CreateVersion(c, organization(c), actor(c), role(c), id, input)
	respond(c, http.StatusCreated, out, err)
}

func (h *Handler) simulate(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "version_id")
	if !ok {
		return
	}
	out, err := h.ucs.Simulate(c, organization(c), actor(c), role(c), id)
	respond(c, http.StatusOK, out, err)
}

func (h *Handler) requestPromotion(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "version_id")
	if !ok {
		return
	}
	var input PromotionInput
	if err := ginmw.BindJSON(c, &input); err != nil {
		return
	}
	out, err := h.ucs.RequestPromotion(c, organization(c), actor(c), role(c), id, input)
	respond(c, http.StatusAccepted, out, err)
}

func (h *Handler) approvePromotion(c *gin.Context) { h.decidePromotion(c, true) }
func (h *Handler) rejectPromotion(c *gin.Context)  { h.decidePromotion(c, false) }

func (h *Handler) decidePromotion(c *gin.Context, approve bool) {
	id, ok := ginmw.ParseUUIDParam(c, "promotion_id")
	if !ok {
		return
	}
	var input PromotionDecisionInput
	if err := ginmw.BindJSON(c, &input); err != nil {
		return
	}
	out, err := h.ucs.DecidePromotion(c, organization(c), actor(c), role(c), id, approve, input)
	respond(c, http.StatusOK, out, err)
}

func (h *Handler) evaluations(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	out, err := h.ucs.ListEvaluations(c, organization(c), actor(c), role(c), limit)
	respond(c, http.StatusOK, out, err)
}

func (h *Handler) promotions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	out, err := h.ucs.ListPromotions(c, organization(c), actor(c), role(c), limit)
	respond(c, http.StatusOK, out, err)
}

func (h *Handler) changelog(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	out, err := h.ucs.ListChanges(c, organization(c), actor(c), role(c), limit)
	respond(c, http.StatusOK, out, err)
}

func respond(c *gin.Context, status int, value any, err error) {
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, status, value)
}

func organization(c *gin.Context) string { return strings.TrimSpace(c.GetHeader("X-Org-ID")) }
func actor(c *gin.Context) string        { return strings.TrimSpace(c.GetHeader("X-Actor-ID")) }
func role(c *gin.Context) string         { return strings.TrimSpace(c.GetHeader("X-Axis-Org-Role")) }
