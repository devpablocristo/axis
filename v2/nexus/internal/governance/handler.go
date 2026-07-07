package governance

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/devpablocristo/nexus-v2/internal/governance/handler/dto"
	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Check(context.Context, string, domain.CheckInput) (domain.CheckResult, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/governance")
	{
		group.POST("/check", h.Check)
	}
}

func (h *Handler) Check(c *gin.Context) {
	var req dto.CheckRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Check(c.Request.Context(), tenantID(c), req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.CheckFromDomain(out))
}

func tenantID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
}
