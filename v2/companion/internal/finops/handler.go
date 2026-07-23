package finops

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Routes(r gin.IRouter) {
	r.GET("/finops/summary", h.summary)
	r.GET("/finops/events", h.events)
	r.GET("/finops/budgets", h.budgets)
	r.PUT("/finops/budgets", h.putBudget)
}

func contextValues(c *gin.Context) (string, string, string) {
	return strings.TrimSpace(c.GetHeader("X-Org-ID")), strings.TrimSpace(c.GetHeader("X-Actor-ID")), strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Org-Role")))
}

func window(c *gin.Context) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	from := to.Add(-30 * 24 * time.Hour)
	var err error
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, domainerr.Validation("from must be RFC3339")
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, domainerr.Validation("to must be RFC3339")
		}
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, domainerr.Validation("to must be after from")
	}
	return from, to, nil
}

func (h *Handler) summary(c *gin.Context) {
	from, to, err := window(c)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	org, actor, _ := contextValues(c)
	group := c.DefaultQuery("group_by", "product")
	out, err := h.service.Summary(c, org, actor, group, from, to)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"items": out, "from": from, "to": to, "group_by": group})
}

func (h *Handler) events(c *gin.Context) {
	from, to, err := window(c)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	org, actor, _ := contextValues(c)
	out, err := h.service.ListEvents(c, org, actor, from, to)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"items": out})
}

func (h *Handler) budgets(c *gin.Context) {
	org, actor, _ := contextValues(c)
	out, err := h.service.ListBudgets(c, org, actor)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"items": out})
}

func (h *Handler) putBudget(c *gin.Context) {
	var in Budget
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		ginmw.Respond(c, domainerr.Validation("invalid budget"))
		return
	}
	org, actor, role := contextValues(c)
	out, err := h.service.PutBudget(c, org, actor, role, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}
