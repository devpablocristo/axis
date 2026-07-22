package audit

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/devpablocristo/nexus-v2/internal/audit/handler/dto"
	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Append(context.Context, string, auditdomain.AppendInput) (auditdomain.AuditEvent, error)
	Replay(context.Context, string, string) (ReplayOutput, error)
	Verify(context.Context, string, string) (IntegrityOutput, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/audit")
	{
		group.POST("/events", h.Append)
		group.GET("/virployees/:virployee_id/replay", h.Replay)
		group.GET("/virployees/:virployee_id/verify", h.Verify)
	}
}

func (h *Handler) Append(c *gin.Context) {
	var req dto.AppendRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	in := req.ToDomain()
	in.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if strings.TrimSpace(in.ActorID) == "" {
		in.ActorID = actorID(c)
	}
	out, err := h.ucs.Append(c.Request.Context(), tenantID(c), in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.AppendResponseFromDomain(out))
}

func (h *Handler) Replay(c *gin.Context) {
	out, err := h.ucs.Replay(c.Request.Context(), tenantID(c), virployeeID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, out)
}

func (h *Handler) Verify(c *gin.Context) {
	out, err := h.ucs.Verify(c.Request.Context(), tenantID(c), virployeeID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, out)
}

func tenantID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
}

func actorID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Actor-ID"))
}

func virployeeID(c *gin.Context) string {
	return strings.TrimSpace(c.Param("virployee_id"))
}
