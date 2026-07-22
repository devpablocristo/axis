package capabilities

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion-v2/internal/capabilities/handler/dto"
	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Create(context.Context, string, domain.CreateInput) (domain.Capability, error)
	ListActive(context.Context, string) ([]domain.Capability, error)
	ListArchived(context.Context, string) ([]domain.Capability, error)
	ListTrash(context.Context, string) ([]domain.Capability, error)
	Get(context.Context, string, uuid.UUID) (domain.Capability, error)
	Update(context.Context, string, uuid.UUID, domain.UpdateInput) (domain.Capability, error)
	UpdateManifest(context.Context, string, uuid.UUID, domain.ManifestInput) (domain.Capability, error)
	Conform(context.Context, string, uuid.UUID) (domain.Capability, domain.ConformanceReport, error)
	Activate(context.Context, string, uuid.UUID) (domain.Capability, domain.ConformanceReport, error)
	Archive(context.Context, string, uuid.UUID, string, string) error
	Unarchive(context.Context, string, uuid.UUID, string, string) error
	Trash(context.Context, string, uuid.UUID, string, string) error
	Restore(context.Context, string, uuid.UUID, string, string) error
	Purge(context.Context, string, uuid.UUID, string, string) error
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/capabilities")
	{
		group.POST("", h.Create)
		group.GET("", h.List)
		group.GET("/:capability_id", h.Get)
		group.PUT("/:capability_id", h.Update)
		group.PUT("/:capability_id/manifest", h.UpdateManifest)
		group.POST("/:capability_id/conform", h.Conform)
		group.POST("/:capability_id/activate", h.Activate)
		group.POST("/:capability_id/archive", h.Archive)
		group.POST("/:capability_id/unarchive", h.Unarchive)
		group.POST("/:capability_id/trash", h.Trash)
		group.POST("/:capability_id/restore", h.Restore)
		group.DELETE("/:capability_id/purge", h.Purge)
	}
}

func (h *Handler) UpdateManifest(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "capability_id")
	if !ok {
		return
	}
	var req dto.CapabilityManifestRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.UpdateManifest(c.Request.Context(), tenantID(c), id, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.CapabilityFromDomain(out))
}

func (h *Handler) Conform(c *gin.Context) {
	h.promotionAction(c, h.ucs.Conform)
}

func (h *Handler) Activate(c *gin.Context) {
	h.promotionAction(c, h.ucs.Activate)
}

func (h *Handler) promotionAction(c *gin.Context, action func(context.Context, string, uuid.UUID) (domain.Capability, domain.ConformanceReport, error)) {
	id, ok := ginmw.ParseUUIDParam(c, "capability_id")
	if !ok {
		return
	}
	out, report, err := action(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	status := http.StatusOK
	if !report.Conformant {
		status = http.StatusUnprocessableEntity
	}
	ginmw.WriteJSON(c, status, dto.CapabilityFromDomain(out))
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateCapabilityRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Create(c.Request.Context(), tenantID(c), req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.CapabilityFromDomain(out))
}

func (h *Handler) List(c *gin.Context) {
	var out []domain.Capability
	var err error
	switch strings.ToLower(strings.TrimSpace(c.Query("lifecycle"))) {
	case "archived":
		out, err = h.ucs.ListArchived(c.Request.Context(), tenantID(c))
	case "trash", "trashed":
		out, err = h.ucs.ListTrash(c.Request.Context(), tenantID(c))
	default:
		out, err = h.ucs.ListActive(c.Request.Context(), tenantID(c))
	}
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListCapabilitiesFromDomain(out))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "capability_id")
	if !ok {
		return
	}
	out, err := h.ucs.Get(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.CapabilityFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "capability_id")
	if !ok {
		return
	}
	var req dto.UpdateCapabilityRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), tenantID(c), id, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.CapabilityFromDomain(out))
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

func (h *Handler) lifecycleAction(
	c *gin.Context,
	fn func(context.Context, string, uuid.UUID, string, string) error,
) {
	id, ok := ginmw.ParseUUIDParam(c, "capability_id")
	if !ok {
		return
	}
	req, ok := bindLifecycleRequest(c)
	if !ok {
		return
	}
	if err := fn(c.Request.Context(), tenantID(c), id, actorID(c), req.Reason); err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteNoContent(c)
}

func bindLifecycleRequest(c *gin.Context) (dto.LifecycleRequest, bool) {
	var req dto.LifecycleRequest
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return req, true
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, true
		}
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "invalid lifecycle body")
		return req, false
	}
	return req, true
}

func tenantID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
}

func actorID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Actor-ID"))
}
