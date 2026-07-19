package learning

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	List(ctx context.Context, tenantID, statusFilter string, virployeeID *uuid.UUID) ([]Proposal, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (Proposal, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/learning")
	{
		group.GET("/proposals", h.List)
		group.GET("/proposals/:proposal_id", h.Get)
	}
}

func (h *Handler) List(c *gin.Context) {
	var virployeeID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("virployee_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			ginmw.Respond(c, domainerr.Validation("virployee_id must be a valid uuid"))
			return
		}
		virployeeID = &parsed
	}
	out, err := h.ucs.List(c.Request.Context(), tenantID(c), c.Query("status"), virployeeID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *Handler) Get(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("proposal_id")))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation("proposal_id must be a valid uuid"))
		return
	}
	out, err := h.ucs.Get(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func tenantID(c *gin.Context) string {
	// The /v1 internal-auth middleware already rejects requests without a
	// trusted X-Tenant-ID, so no fallback here.
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
}
