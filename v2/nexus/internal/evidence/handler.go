package evidence

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	evidencedomain "github.com/devpablocristo/nexus-v2/internal/evidence/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Generate(ctx context.Context, tenantID, virployeeID, subject string) (evidencedomain.EvidencePack, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/evidence")
	{
		group.GET("/virployees/:virployee_id", h.Generate)
	}
}

func (h *Handler) Generate(c *gin.Context) {
	tenantID := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	virployeeID := strings.TrimSpace(c.Param("virployee_id"))
	subject := strings.TrimSpace(c.Query("subject"))
	pack, err := h.ucs.Generate(c.Request.Context(), tenantID, virployeeID, subject)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, pack)
}
