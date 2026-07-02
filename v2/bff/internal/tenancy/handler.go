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
	AddMember(context.Context, domain.AddMemberInput) (domain.TenantMember, error)
	ListForPrincipal(context.Context, string) ([]domain.Tenant, error)
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
		group.POST("/:tenant_id/members", h.AddMember)
	}
}

func (h *Handler) List(c *gin.Context) {
	out, err := h.ucs.ListForPrincipal(c.Request.Context(), h.principalID(c))
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
	input := req.ToDomain()
	if strings.TrimSpace(input.OwnerUserID) == "" {
		input.OwnerUserID = h.principalID(c)
	}
	out, err := h.ucs.Create(c.Request.Context(), input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.TenantFromDomain(out))
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

func (h *Handler) principalID(c *gin.Context) string {
	principalID := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if principalID == "" {
		principalID = strings.TrimSpace(h.options.DefaultPrincipalID)
	}
	return principalID
}
