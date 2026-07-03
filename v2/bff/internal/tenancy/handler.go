package tenancy

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/devpablocristo/bff-v2/internal/tenancy/handler/dto"
	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Create(context.Context, domain.CreateTenantInput) (domain.Tenant, error)
	Update(context.Context, domain.UpdateTenantInput) (domain.Tenant, error)
	AddMember(context.Context, domain.AddMemberInput) (domain.TenantMember, error)
	List(context.Context, domain.ListInput) ([]domain.Tenant, error)
	ListForPrincipal(context.Context, string) ([]domain.Tenant, error)
	Archive(context.Context, domain.LifecycleInput) error
	Unarchive(context.Context, domain.LifecycleInput) error
	Trash(context.Context, domain.LifecycleInput) error
	Restore(context.Context, domain.LifecycleInput) error
	Purge(context.Context, domain.LifecycleInput) error
	Products(context.Context) ([]domain.Product, error)
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
	group := router.Group("/tenants")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.PUT("/:tenant_id", h.Update)
		group.POST("/:tenant_id/archive", h.Archive)
		group.POST("/:tenant_id/unarchive", h.Unarchive)
		group.POST("/:tenant_id/trash", h.Trash)
		group.POST("/:tenant_id/restore", h.Restore)
		group.DELETE("/:tenant_id/purge", h.Purge)
		group.POST("/:tenant_id/members", h.AddMember)
	}
	router.GET("/products", h.Products)
}

func (h *Handler) List(c *gin.Context) {
	out, err := h.ucs.List(c.Request.Context(), domain.ListInput{
		PrincipalID: h.principalID(c),
		Lifecycle:   c.Query("lifecycle"),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.TenantsFromDomain(out))
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateTenantRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	input := req.ToDomain(h.principalID(c))
	out, err := h.ucs.Create(c.Request.Context(), input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.TenantFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	var req dto.UpdateTenantRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), req.ToDomain(c.Param("tenant_id"), h.principalID(c)))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.TenantFromDomain(out))
}

func (h *Handler) AddMember(c *gin.Context) {
	var req dto.AddTenantMemberRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.AddMember(c.Request.Context(), req.ToDomain(c.Param("tenant_id")))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.TenantMemberFromDomain(out))
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

func (h *Handler) Products(c *gin.Context) {
	out, err := h.ucs.Products(c.Request.Context())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ProductsFromDomain(out))
}

func (h *Handler) lifecycleAction(c *gin.Context, fn func(context.Context, domain.LifecycleInput) error) {
	err := fn(c.Request.Context(), domain.LifecycleInput{
		TenantID:    c.Param("tenant_id"),
		PrincipalID: h.principalID(c),
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
