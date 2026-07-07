package actiontypes

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/handler/dto"
	"github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Create(context.Context, string, domain.CreateInput) (domain.ActionType, error)
	List(context.Context, string) ([]domain.ActionType, error)
	Get(context.Context, string, uuid.UUID) (domain.ActionType, error)
	GetByKey(context.Context, string, string) (domain.ActionType, error)
	Update(context.Context, string, uuid.UUID, domain.UpdateInput) (domain.ActionType, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/action-types")
	{
		group.POST("", h.Create)
		group.GET("", h.List)
		group.GET("/:action_type_id", h.Get)
		group.PUT("/:action_type_id", h.Update)
	}
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateActionTypeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Create(c.Request.Context(), tenantID(c), req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.ActionTypeFromDomain(out))
}

func (h *Handler) List(c *gin.Context) {
	out, err := h.ucs.List(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.ListActionTypesFromDomain(out))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "action_type_id")
	if !ok {
		return
	}
	out, err := h.ucs.Get(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.ActionTypeFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "action_type_id")
	if !ok {
		return
	}
	var req dto.UpdateActionTypeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), tenantID(c), id, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.ActionTypeFromDomain(out))
}

func tenantID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
}
