package learning

import (
	"context"
	"errors"
	"io"
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
	Scan(ctx context.Context, tenantID string, minExecutions int) (ScanResult, error)
	Accept(ctx context.Context, tenantID string, id uuid.UUID, actor, role string) (AcceptResult, error)
	Dismiss(ctx context.Context, tenantID string, id uuid.UUID, actor, role string) (Proposal, error)
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
		group.POST("/proposals/:proposal_id/accept", h.Accept)
		group.POST("/proposals/:proposal_id/dismiss", h.Dismiss)
		group.POST("/scan", h.Scan)
	}
}

// Accept installs the proposal as a procedure memory after the mandatory eval.
func (h *Handler) Accept(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("proposal_id")))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation("proposal_id must be a valid uuid"))
		return
	}
	out, err := h.ucs.Accept(c.Request.Context(), tenantID(c), id, actorID(c), role(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// Dismiss discards a pending proposal.
func (h *Handler) Dismiss(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("proposal_id")))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation("proposal_id must be a valid uuid"))
		return
	}
	out, err := h.ucs.Dismiss(c.Request.Context(), tenantID(c), id, actorID(c), role(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// Scan triggers one analyzer pass for the tenant. min_executions is optional
// and can only RAISE the configured threshold (the usecase clamps to the
// configured floor); 0 or absent uses the default. The body guard mirrors the
// sibling handlers: BFF-proxied requests arrive chunked (ContentLength -1)
// and must still be bound.
func (h *Handler) Scan(c *gin.Context) {
	var req struct {
		MinExecutions int `json:"min_executions"`
	}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			ginmw.Respond(c, domainerr.Validation("invalid scan request"))
			return
		}
	}
	out, err := h.ucs.Scan(c.Request.Context(), tenantID(c), req.MinExecutions)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
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

func actorID(c *gin.Context) string {
	if actor := strings.TrimSpace(c.GetHeader("X-Actor-ID")); actor != "" {
		return actor
	}
	return DefaultActorID
}

// role is the caller's tenant membership role, set by the BFF from the resolved
// membership (X-Axis-Tenant-Role). The accept/dismiss authorization uses it.
func role(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Axis-Tenant-Role"))
}
