package products

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/devpablocristo/bff-v2/internal/products/handler/dto"
	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Create(context.Context, domain.CreateProductInput) (domain.Product, error)
	Update(context.Context, domain.UpdateProductInput) (domain.Product, error)
	AddMember(context.Context, domain.AddMemberInput) (domain.OrgMember, error)
	List(context.Context, domain.ListInput) ([]domain.Product, error)
	ListForPrincipal(context.Context, string) ([]domain.Product, error)
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
	group := router.Group("/products")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.PUT("/:product_id", h.Update)
		group.POST("/:product_id/archive", h.Archive)
		group.POST("/:product_id/unarchive", h.Unarchive)
		group.POST("/:product_id/trash", h.Trash)
		group.POST("/:product_id/restore", h.Restore)
		group.DELETE("/:product_id/purge", h.Purge)
		group.POST("/:product_id/members", h.AddMember)
	}
}

// OrganizationProductRoutes exposes the organization -> products model. The
// legacy product handlers remain unregistered and are kept only while internal
// service scopes migrate away from their historical naming.
func (h *Handler) OrganizationProductRoutes(router gin.IRouter) {
	group := router.Group("/organizations/:org_id/products")
	{
		group.GET("", h.ListOrganizationProducts)
		group.POST("", h.CreateOrganizationProduct)
		group.PUT("/:product_id", h.UpdateOrganizationProduct)
		group.POST("/:product_id/archive", h.ArchiveOrganizationProduct)
		group.POST("/:product_id/unarchive", h.UnarchiveOrganizationProduct)
		group.POST("/:product_id/trash", h.TrashOrganizationProduct)
		group.POST("/:product_id/restore", h.RestoreOrganizationProduct)
		group.DELETE("/:product_id/purge", h.PurgeOrganizationProduct)
	}
}

func (h *Handler) ListOrganizationProducts(c *gin.Context) {
	out, err := h.ucs.List(c.Request.Context(), domain.ListInput{
		PrincipalID: h.principalID(c), Lifecycle: c.Query("lifecycle"),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	filtered := make([]domain.Product, 0, len(out))
	for _, product := range out {
		if product.OrgID == c.Param("org_id") {
			filtered = append(filtered, product)
		}
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.OrganizationProductsFromDomain(filtered))
}

func (h *Handler) CreateOrganizationProduct(c *gin.Context) {
	var req dto.CreateProductRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	req.OrgID = c.Param("org_id")
	input := req.ToDomain(h.principalID(c))
	out, err := h.ucs.Create(c.Request.Context(), input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.OrganizationProductFromDomain(out))
}

func (h *Handler) UpdateOrganizationProduct(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "product_id", Value: c.Param("product_id")})
	h.Update(c)
}

func (h *Handler) ArchiveOrganizationProduct(c *gin.Context) {
	h.organizationProductLifecycle(c, h.ucs.Archive)
}
func (h *Handler) UnarchiveOrganizationProduct(c *gin.Context) {
	h.organizationProductLifecycle(c, h.ucs.Unarchive)
}
func (h *Handler) TrashOrganizationProduct(c *gin.Context) {
	h.organizationProductLifecycle(c, h.ucs.Trash)
}
func (h *Handler) RestoreOrganizationProduct(c *gin.Context) {
	h.organizationProductLifecycle(c, h.ucs.Restore)
}
func (h *Handler) PurgeOrganizationProduct(c *gin.Context) {
	h.organizationProductLifecycle(c, h.ucs.Purge)
}

func (h *Handler) organizationProductLifecycle(c *gin.Context, fn func(context.Context, domain.LifecycleInput) error) {
	err := fn(c.Request.Context(), domain.LifecycleInput{
		ProductID: c.Param("product_id"), PrincipalID: h.principalID(c),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteNoContent(c)
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
	ginmw.WriteJSON(c, http.StatusOK, dto.ProductsFromDomain(out))
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateProductRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	input := req.ToDomain(h.principalID(c))
	out, err := h.ucs.Create(c.Request.Context(), input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.ProductFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	var req dto.UpdateProductRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), req.ToDomain(c.Param("product_id"), h.principalID(c)))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ProductFromDomain(out))
}

func (h *Handler) AddMember(c *gin.Context) {
	var req dto.AddOrgMemberRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.AddMember(c.Request.Context(), req.ToDomain(c.Param("product_id")))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.OrgMemberFromDomain(out))
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
		ProductID:   c.Param("product_id"),
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
