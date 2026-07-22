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
	Revalidate(context.Context, string, string, domain.RevalidationInput) (domain.RevalidationResult, error)
	ReportExecutionResult(context.Context, string, string, domain.ExecutionResultInput) (domain.ExecutionResult, error)
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
		group.POST("/checks/:check_id/result", h.ReportExecutionResult)
		group.POST("/checks/:check_id/revalidate", h.Revalidate)
	}
}

func (h *Handler) Revalidate(c *gin.Context) {
	checkID := strings.TrimSpace(c.Param("check_id"))
	if checkID == "" {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	var req dto.RevalidationRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Revalidate(c.Request.Context(), orgID(c), checkID, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.RevalidationFromDomain(out))
}

func (h *Handler) ReportExecutionResult(c *gin.Context) {
	checkID := strings.TrimSpace(c.Param("check_id"))
	if checkID == "" {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	var req dto.ExecutionResultRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.ReportExecutionResult(c.Request.Context(), orgID(c), checkID, req.ToDomain(c.GetHeader("Idempotency-Key")))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.ExecutionResultFromDomain(out))
}

func (h *Handler) Check(c *gin.Context) {
	var req dto.CheckRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	input := req.ToDomain(c.GetHeader("X-Axis-Org-Role"))
	if strings.EqualFold(strings.TrimSpace(input.RequesterType), "human") {
		input.RequesterID = strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	}
	out, err := h.ucs.Check(c.Request.Context(), orgID(c), input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, 200, dto.CheckFromDomain(out))
}

func orgID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("X-Org-ID"))
}
