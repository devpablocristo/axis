package approvals

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/nexus-v2/internal/approvals/handler/dto"
	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/devpablocristo/platform/http/go/pagination"
)

type UseCasesPort interface {
	List(context.Context, string, domain.ListInput) (domain.ListPage, error)
	Get(context.Context, string, uuid.UUID) (domain.Approval, error)
	Approve(context.Context, string, uuid.UUID, domain.DecisionActor, domain.DecisionInput) (domain.Approval, error)
	Reject(context.Context, string, uuid.UUID, domain.DecisionActor, domain.DecisionInput) (domain.Approval, error)
	Review(context.Context, string, uuid.UUID, domain.DecisionActor, domain.DecisionInput) (domain.Approval, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/approvals")
	{
		group.GET("", h.List)
		group.GET("/:approval_id", h.Get)
		group.POST("/:approval_id/approve", h.Approve)
		group.POST("/:approval_id/reject", h.Reject)
		group.POST("/:approval_id/review", h.Review)
	}
}

func (h *Handler) List(c *gin.Context) {
	input, ok := parseListInput(c)
	if !ok {
		return
	}
	out, err := h.ucs.List(c.Request.Context(), orgID(c), input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.ListApprovalsFromDomain(out))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "approval_id")
	if !ok {
		return
	}
	out, err := h.ucs.Get(c.Request.Context(), orgID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.ApprovalFromDomain(out))
}

func (h *Handler) Approve(c *gin.Context) {
	h.decide(c, h.ucs.Approve)
}

func (h *Handler) Reject(c *gin.Context) {
	h.decide(c, h.ucs.Reject)
}

func (h *Handler) Review(c *gin.Context) {
	h.decide(c, h.ucs.Review)
}

func (h *Handler) decide(
	c *gin.Context,
	fn func(context.Context, string, uuid.UUID, domain.DecisionActor, domain.DecisionInput) (domain.Approval, error),
) {
	id, ok := ginmw.ParseUUIDParam(c, "approval_id")
	if !ok {
		return
	}
	var req dto.DecisionRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := fn(c.Request.Context(), orgID(c), id, domain.DecisionActor{ID: actorID(c), Role: actorRole(c)}, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.ApprovalFromDomain(out))
}

func parseListInput(c *gin.Context) (domain.ListInput, bool) {
	params, err := pagination.ParseParams(c.Query("limit"), c.Query("cursor"), pagination.Config{
		DefaultLimit: 20,
		MaxLimit:     100,
	})
	if err != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return domain.ListInput{}, false
	}
	return domain.ListInput{
		StatusRaw: c.Query("status"),
		Limit:     params.Limit,
		Cursor:    params.Cursor,
	}, true
}

func orgID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Org-ID"))
}

func actorID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Actor-ID"))
}

func actorRole(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Axis-Org-Role"))
}
