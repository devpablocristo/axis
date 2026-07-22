package virployees

import (
	"net/http"
	"strconv"
	"strings"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CoordinationHandler struct{ ucs *UseCases }

func NewCoordinationHandler(ucs *UseCases) *CoordinationHandler {
	return &CoordinationHandler{ucs: ucs}
}

func (h *CoordinationHandler) Routes(router gin.IRouter) {
	router.GET("/assist-cases", h.listCases)
	router.GET("/assist-cases/:case_id", h.getCase)
	router.GET("/orchestration-policies", h.listPolicies)
	router.PUT("/orchestration-policies", h.upsertPolicy)
	router.GET("/specialist-routes", h.listRoutes)
	router.PUT("/specialist-routes", h.upsertRoute)
	router.GET("/handoffs", h.listHandoffs)
	router.POST("/handoffs", h.createHandoff)
	router.GET("/handoffs/:handoff_id", h.getHandoff)
	router.POST("/handoffs/:handoff_id/accept", h.acceptHandoff)
	router.POST("/handoffs/:handoff_id/reject", h.rejectHandoff)
	router.POST("/handoffs/:handoff_id/cancel", h.cancelHandoff)
	router.GET("/human-reviews", h.listReviews)
	router.POST("/human-reviews/:review_id/claim", h.claimReview)
	router.POST("/human-reviews/:review_id/resolve", h.resolveReview)
}

func (h *CoordinationHandler) listCases(c *gin.Context) {
	limit := parseCoordinationLimit(c)
	items, err := h.ucs.ListAssistCases(c.Request.Context(), orgID(c), c.Query("status"), limit)
	if err == nil {
		subjectID := strings.TrimSpace(c.Query("subject_id"))
		ownerID := strings.TrimSpace(c.Query("owner_virployee_id"))
		caseID := strings.TrimSpace(c.Query("case_id"))
		if ownerID != "" {
			if parsed, parseErr := uuid.Parse(ownerID); parseErr != nil || parsed == uuid.Nil {
				ginmw.Respond(c, ginmw.ErrBadInput)
				return
			}
		}
		filtered := make([]AssistCase, 0, len(items))
		for _, item := range items {
			if subjectID != "" && item.SubjectID != subjectID {
				continue
			}
			if ownerID != "" && item.OwnerVirployeeID.String() != ownerID {
				continue
			}
			if caseID != "" && item.ID.String() != caseID {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}
	respondCoordination(c, items, err)
}
func (h *CoordinationHandler) getCase(c *gin.Context) {
	id, ok := coordinationUUID(c, "case_id")
	if !ok {
		return
	}
	item, err := h.ucs.GetAssistCase(c.Request.Context(), orgID(c), id)
	respondCoordination(c, item, err)
}
func (h *CoordinationHandler) listPolicies(c *gin.Context) {
	items, err := h.ucs.ListOrchestrationPolicies(c.Request.Context(), orgID(c))
	respondCoordination(c, items, err)
}
func (h *CoordinationHandler) upsertPolicy(c *gin.Context) {
	var req orchestrationPolicyRequest
	if c.ShouldBindJSON(&req) != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	item, err := h.ucs.UpsertOrchestrationPolicy(c.Request.Context(), orgID(c), coordinationActor(c), req.domain())
	respondCoordination(c, item, err)
}
func (h *CoordinationHandler) listRoutes(c *gin.Context) {
	entrypoint := uuid.Nil
	if raw := strings.TrimSpace(c.Query("entrypoint_virployee_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			ginmw.Respond(c, ginmw.ErrBadInput)
			return
		}
		entrypoint = parsed
	}
	items, err := h.ucs.ListSpecialistRoutes(c.Request.Context(), orgID(c), c.Query("product_surface"), c.Query("assist_type"), entrypoint)
	respondCoordination(c, items, err)
}
func (h *CoordinationHandler) upsertRoute(c *gin.Context) {
	var req specialistRouteRequest
	if c.ShouldBindJSON(&req) != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	item, err := h.ucs.UpsertSpecialistRoute(c.Request.Context(), orgID(c), coordinationActor(c), req.domain())
	respondCoordination(c, item, err)
}
func (h *CoordinationHandler) listHandoffs(c *gin.Context) {
	items, err := h.ucs.ListHandoffs(c.Request.Context(), orgID(c), c.Query("status"), parseCoordinationLimit(c))
	respondCoordination(c, items, err)
}
func (h *CoordinationHandler) getHandoff(c *gin.Context) {
	id, ok := coordinationUUID(c, "handoff_id")
	if !ok {
		return
	}
	item, err := h.ucs.GetHandoff(c.Request.Context(), orgID(c), id)
	respondCoordination(c, item, err)
}
func (h *CoordinationHandler) createHandoff(c *gin.Context) {
	var req createHandoffRequest
	if c.ShouldBindJSON(&req) != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	in, ok := req.domain()
	if !ok {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	item, err := h.ucs.CreateHandoff(c.Request.Context(), orgID(c), coordinationActor(c), in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusCreated, item)
}
func (h *CoordinationHandler) acceptHandoff(c *gin.Context) { h.decideHandoff(c, "accept") }
func (h *CoordinationHandler) rejectHandoff(c *gin.Context) { h.decideHandoff(c, "reject") }
func (h *CoordinationHandler) decideHandoff(c *gin.Context, decision string) {
	id, ok := coordinationUUID(c, "handoff_id")
	if !ok {
		return
	}
	var req versionNoteRequest
	if c.ShouldBindJSON(&req) != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	item, err := h.ucs.DecideHandoff(c.Request.Context(), orgID(c), id, coordinationActor(c), decision, DecideHandoffInput(req))
	respondCoordination(c, item, err)
}
func (h *CoordinationHandler) cancelHandoff(c *gin.Context) {
	id, ok := coordinationUUID(c, "handoff_id")
	if !ok {
		return
	}
	var req versionNoteRequest
	if c.ShouldBindJSON(&req) != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	item, err := h.ucs.CancelHandoff(c.Request.Context(), orgID(c), id, coordinationActor(c), req.Version)
	respondCoordination(c, item, err)
}
func (h *CoordinationHandler) listReviews(c *gin.Context) {
	items, err := h.ucs.ListHumanReviews(c.Request.Context(), orgID(c), c.Query("status"))
	respondCoordination(c, items, err)
}
func (h *CoordinationHandler) claimReview(c *gin.Context) {
	id, ok := coordinationUUID(c, "review_id")
	if !ok {
		return
	}
	item, err := h.ucs.ClaimHumanReview(c.Request.Context(), orgID(c), id, coordinationActor(c))
	respondCoordination(c, item, err)
}
func (h *CoordinationHandler) resolveReview(c *gin.Context) {
	id, ok := coordinationUUID(c, "review_id")
	if !ok {
		return
	}
	var req resolveReviewRequest
	if c.ShouldBindJSON(&req) != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	in, ok := req.domain()
	if !ok {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	item, err := h.ucs.ResolveHumanReview(c.Request.Context(), orgID(c), id, coordinationActor(c), in)
	respondCoordination(c, item, err)
}

type orchestrationPolicyRequest struct {
	ProductSurface              string         `json:"product_surface"`
	AssistType                  string         `json:"assist_type"`
	EntrypointVirployeeID       string         `json:"entrypoint_virployee_id"`
	Mode                        string         `json:"mode"`
	SelectorCapabilityID        string         `json:"selector_capability_id"`
	SynthesisCapabilityID       string         `json:"synthesis_capability_id"`
	OutputSchema                map[string]any `json:"output_schema"`
	MaxSpecialists              int            `json:"max_specialists"`
	ConsultationTimeoutSeconds  int            `json:"consultation_timeout_seconds"`
	OrchestrationTimeoutSeconds int            `json:"orchestration_timeout_seconds"`
}

func (r orchestrationPolicyRequest) domain() OrchestrationPolicy {
	entrypoint, _ := uuid.Parse(r.EntrypointVirployeeID)
	selector, _ := uuid.Parse(r.SelectorCapabilityID)
	synthesis, _ := uuid.Parse(r.SynthesisCapabilityID)
	return OrchestrationPolicy{ProductSurface: r.ProductSurface, AssistType: r.AssistType, EntrypointVirployeeID: entrypoint, Mode: r.Mode, SelectorCapabilityID: selector, SynthesisCapabilityID: synthesis, OutputSchema: r.OutputSchema, MaxSpecialists: r.MaxSpecialists, MaxDepth: 1, ConsultationTimeoutSeconds: r.ConsultationTimeoutSeconds, OrchestrationTimeoutSeconds: r.OrchestrationTimeoutSeconds}
}

type specialistRouteRequest struct {
	ProductSurface        string `json:"product_surface"`
	AssistType            string `json:"assist_type"`
	EntrypointVirployeeID string `json:"entrypoint_virployee_id"`
	SpecialtyCode         string `json:"specialty_code"`
	TargetVirployeeID     string `json:"target_virployee_id"`
	CapabilityID          string `json:"capability_id"`
	RequirementMode       string `json:"requirement_mode"`
	Enabled               *bool  `json:"enabled"`
}

func (r specialistRouteRequest) domain() SpecialistRoute {
	entrypoint, _ := uuid.Parse(r.EntrypointVirployeeID)
	target, _ := uuid.Parse(r.TargetVirployeeID)
	capability, _ := uuid.Parse(r.CapabilityID)
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return SpecialistRoute{ProductSurface: r.ProductSurface, AssistType: r.AssistType, EntrypointVirployeeID: entrypoint, SpecialtyCode: r.SpecialtyCode, TargetVirployeeID: target, CapabilityID: capability, RequirementMode: r.RequirementMode, Enabled: enabled}
}

type createHandoffRequest struct {
	CaseID        string `json:"case_id"`
	SourceRunID   string `json:"source_run_id"`
	ToVirployeeID string `json:"to_virployee_id"`
	ReasonCode    string `json:"reason_code"`
	Note          string `json:"note"`
}

func (r createHandoffRequest) domain() (CreateHandoffInput, bool) {
	caseID, err := uuid.Parse(r.CaseID)
	if err != nil {
		return CreateHandoffInput{}, false
	}
	toID, err := uuid.Parse(r.ToVirployeeID)
	if err != nil {
		return CreateHandoffInput{}, false
	}
	var source *uuid.UUID
	if strings.TrimSpace(r.SourceRunID) != "" {
		parsed, err := uuid.Parse(r.SourceRunID)
		if err != nil {
			return CreateHandoffInput{}, false
		}
		source = &parsed
	}
	return CreateHandoffInput{CaseID: caseID, SourceRunID: source, ToID: toID, ReasonCode: r.ReasonCode, Note: r.Note}, true
}

type versionNoteRequest struct {
	Version int64  `json:"version"`
	Note    string `json:"note"`
}
type resolveReviewRequest struct {
	Outcome   string `json:"outcome"`
	Note      string `json:"note"`
	HandoffID string `json:"handoff_id"`
}

func (r resolveReviewRequest) domain() (ResolveReviewInput, bool) {
	var handoff *uuid.UUID
	if strings.TrimSpace(r.HandoffID) != "" {
		parsed, err := uuid.Parse(r.HandoffID)
		if err != nil {
			return ResolveReviewInput{}, false
		}
		handoff = &parsed
	}
	return ResolveReviewInput{Outcome: r.Outcome, Note: r.Note, HandoffID: handoff}, true
}

func coordinationActor(c *gin.Context) CoordinationActor {
	return CoordinationActor{ID: actorID(c), Role: strings.TrimSpace(c.GetHeader("X-Axis-Org-Role"))}
}
func coordinationUUID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param(param)))
	if err != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return uuid.Nil, false
	}
	return id, true
}
func parseCoordinationLimit(c *gin.Context) int {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 200 {
		return 100
	}
	return limit
}
func respondCoordination(c *gin.Context, value any, err error) {
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, value)
}
