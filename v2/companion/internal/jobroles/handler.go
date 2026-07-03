package jobroles

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion-v2/internal/jobroles/handler/dto"
	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Create(context.Context, string, domain.CreateInput) (domain.JobRole, error)
	ListActive(context.Context, string) ([]domain.JobRole, error)
	ListArchived(context.Context, string) ([]domain.JobRole, error)
	ListTrash(context.Context, string) ([]domain.JobRole, error)
	Get(context.Context, string, uuid.UUID) (domain.JobRole, error)
	Update(context.Context, string, uuid.UUID, domain.UpdateInput) (domain.JobRole, error)
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
	group := router.Group("/job-roles")
	{
		group.POST("", h.Create)
		group.GET("", h.List)
		group.GET("/:job_role_id", h.Get)
		group.PUT("/:job_role_id", h.Update)
		group.POST("/:job_role_id/archive", h.Archive)
		group.POST("/:job_role_id/unarchive", h.Unarchive)
		group.POST("/:job_role_id/trash", h.Trash)
		group.POST("/:job_role_id/restore", h.Restore)
		group.DELETE("/:job_role_id/purge", h.Purge)
	}
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateJobRoleRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Create(c.Request.Context(), tenantID(c), req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.JobRoleFromDomain(out))
}

func (h *Handler) List(c *gin.Context) {
	var out []domain.JobRole
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
	ginmw.WriteJSON(c, http.StatusOK, dto.ListJobRolesFromDomain(out))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "job_role_id")
	if !ok {
		return
	}
	out, err := h.ucs.Get(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.JobRoleFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "job_role_id")
	if !ok {
		return
	}
	var req dto.UpdateJobRoleRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), tenantID(c), id, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.JobRoleFromDomain(out))
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
	id, ok := ginmw.ParseUUIDParam(c, "job_role_id")
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
		ginmw.Respond(c, ginmw.ErrBadInput)
		return dto.LifecycleRequest{}, false
	}
	return req, true
}

func tenantID(c *gin.Context) string {
	tenant := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	if tenant == "" {
		return DefaultTenantID
	}
	return tenant
}

func actorID(c *gin.Context) string {
	actor := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if actor == "" {
		return DefaultActorID
	}
	return actor
}
