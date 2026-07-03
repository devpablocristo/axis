package session

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/devpablocristo/bff-v2/internal/session/handler/dto"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type UseCasesPort interface {
	Resolve(ctx context.Context, input sessiondomain.ResolveInput) (sessiondomain.Session, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	router.GET("/session", h.Get)
}

func (h *Handler) Get(c *gin.Context) {
	out, err := h.ucs.Resolve(c.Request.Context(), sessiondomain.ResolveInput{
		PrincipalID:   c.GetHeader("X-Actor-ID"),
		Email:         c.GetHeader("X-Actor-Email"),
		OrgID:         c.GetHeader("X-Axis-Org-ID"),
		Authorization: c.GetHeader("Authorization"),
	})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.SessionFromDomain(out))
}
