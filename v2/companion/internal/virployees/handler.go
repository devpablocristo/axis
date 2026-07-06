package virployees

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/handler/dto"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/devpablocristo/platform/lifecycle/go/paths"
)

type UseCasesPort interface {
	Create(context.Context, string, domain.CreateInput) (domain.Virployee, error)
	ListActive(context.Context, string) ([]domain.Virployee, error)
	ListArchived(context.Context, string) ([]domain.Virployee, error)
	ListTrash(context.Context, string) ([]domain.Virployee, error)
	Get(context.Context, string, uuid.UUID) (domain.Virployee, error)
	RuntimeContext(context.Context, string, uuid.UUID) (runtimecontext.Context, error)
	DryRun(context.Context, string, uuid.UUID, string) (dryrun.Result, error)
	ExecutionGate(context.Context, string, uuid.UUID, string, *executiongate.ConfirmedDraft) (executiongate.Result, error)
	Update(context.Context, string, uuid.UUID, domain.UpdateInput) (domain.Virployee, error)
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
	group := router.Group("/virployees")
	{
		group.POST("", h.Create)
		group.GET("", h.ListActive)
		group.GET("/"+paths.SegmentArchived, h.ListArchived)
		group.GET("/trash", h.ListTrash)
		group.GET("/autonomy-levels", h.ListAutonomyLevels)
		group.GET("/:virployee_id/runtime-context", h.RuntimeContext)
		group.POST("/:virployee_id/dry-run", h.DryRun)
		group.POST("/:virployee_id/execution-gate", h.ExecutionGate)
		group.GET("/:virployee_id", h.Get)
		group.PUT("/:virployee_id", h.Update)
		group.POST("/:virployee_id/"+paths.SegmentArchive, h.Archive)
		group.POST("/:virployee_id/unarchive", h.Unarchive)
		group.POST("/:virployee_id/trash", h.Trash)
		group.POST("/:virployee_id/"+paths.SegmentRestore, h.Restore)
		group.DELETE("/:virployee_id/purge", h.Purge)
	}
}

func (h *Handler) ListAutonomyLevels(c *gin.Context) {
	ginmw.WriteJSON(c, http.StatusOK, dto.ListAutonomyLevelsFromDomain(domain.AutonomyDefinitions()))
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Create(c.Request.Context(), tenantID(c), req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.VirployeeFromDomain(out))
}

func (h *Handler) ListActive(c *gin.Context) {
	out, err := h.ucs.ListActive(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListVirployeesFromDomain(out))
}

func (h *Handler) ListArchived(c *gin.Context) {
	out, err := h.ucs.ListArchived(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListVirployeesFromDomain(out))
}

func (h *Handler) ListTrash(c *gin.Context) {
	out, err := h.ucs.ListTrash(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListVirployeesFromDomain(out))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	out, err := h.ucs.Get(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.VirployeeFromDomain(out))
}

func (h *Handler) RuntimeContext(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	out, err := h.ucs.RuntimeContext(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.RuntimeContextFromDomain(out))
}

func (h *Handler) DryRun(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.DryRunVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.DryRun(c.Request.Context(), tenantID(c), id, req.Input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.DryRunFromDomain(out))
}

func (h *Handler) ExecutionGate(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.ExecutionGateVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.ExecutionGate(c.Request.Context(), tenantID(c), id, req.Input, req.ConfirmedDraftToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ExecutionGateFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.UpdateVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), tenantID(c), id, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.VirployeeFromDomain(out))
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

func (h *Handler) lifecycleAction(c *gin.Context, fn func(context.Context, string, uuid.UUID, string, string) error) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
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
