package users

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/devpablocristo/bff-v2/internal/users/handler/dto"
	"github.com/devpablocristo/bff-v2/internal/users/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	List(context.Context, domain.ListInput) ([]domain.User, error)
	Create(context.Context, domain.CreateInput) (domain.User, error)
	Update(context.Context, domain.UpdateInput) (domain.User, error)
	Archive(context.Context, domain.LifecycleInput) error
	Unarchive(context.Context, domain.LifecycleInput) error
	Trash(context.Context, domain.LifecycleInput) error
	Restore(context.Context, domain.LifecycleInput) error
	Purge(context.Context, domain.LifecycleInput) error
}

type HandlerOptions struct {
	DefaultPrincipalID string
}

type Handler struct {
	ucs     UseCasesPort
	options HandlerOptions
}

func NewHandler(ucs UseCasesPort, options HandlerOptions) *Handler {
	return &Handler{ucs: ucs, options: options}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/users")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.PUT("/:user_id", h.Update)
		group.POST("/:user_id/archive", h.Archive)
		group.POST("/:user_id/unarchive", h.Unarchive)
		group.POST("/:user_id/trash", h.Trash)
		group.POST("/:user_id/restore", h.Restore)
		group.DELETE("/:user_id/purge", h.Purge)
	}
}

func (h *Handler) List(c *gin.Context) {
	out, err := h.ucs.List(c.Request.Context(), domain.ListInput{
		OrgID:       c.GetHeader("X-Org-ID"),
		PrincipalID: h.principalID(c),
		State:       c.Query("lifecycle"),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.UsersFromDomain(out))
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Create(c.Request.Context(), req.ToDomain(c.GetHeader("X-Org-ID"), h.principalID(c)))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.UserFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	var req dto.UpdateUserRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), req.ToDomain(c.GetHeader("X-Org-ID"), h.principalID(c), c.Param("user_id")))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.UserFromDomain(out))
}

func (h *Handler) Archive(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Archive)
}

func (h *Handler) Unarchive(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Unarchive)
}

func (h *Handler) Trash(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Trash)
}

func (h *Handler) Restore(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Restore)
}

func (h *Handler) Purge(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Purge)
}

func (h *Handler) lifecycleAction(c *gin.Context, fn func(context.Context, domain.LifecycleInput) error) {
	err := fn(c.Request.Context(), domain.LifecycleInput{
		OrgID:       c.GetHeader("X-Org-ID"),
		PrincipalID: h.principalID(c),
		UserID:      c.Param("user_id"),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteNoContent(c)
}

func (h *Handler) principalID(c *gin.Context) string {
	principalID := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if principalID == "" {
		principalID = strings.TrimSpace(h.options.DefaultPrincipalID)
	}
	return principalID
}
