package executionstats

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	List(ctx context.Context, tenantID string) ([]CapabilityStats, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	router.GET("/capability-stats", h.List)
}

func (h *Handler) List(c *gin.Context) {
	out, err := h.ucs.List(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func tenantID(c *gin.Context) string {
	// The /v1 internal-auth middleware already rejects requests without a
	// trusted X-Tenant-ID, so no fallback here.
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
}
