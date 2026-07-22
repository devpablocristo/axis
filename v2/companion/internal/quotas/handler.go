package quotas

import (
	"context"
	"errors"
	"net/http"
	"strings"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

type PolicyRepository interface {
	UpsertPolicy(context.Context, Policy) (Policy, error)
	ListPolicies(context.Context, string, string) ([]Policy, error)
}

type Handler struct{ repository PolicyRepository }

func NewHandler(repository PolicyRepository) *Handler { return &Handler{repository: repository} }

func (h *Handler) Routes(router gin.IRouter) {
	router.PUT("/quota-policies/:product_surface/:area", h.Upsert)
	router.GET("/quota-policies/:product_surface", h.List)
}

func (h *Handler) Upsert(c *gin.Context) {
	var request struct {
		WindowSeconds int   `json:"window_seconds" binding:"required"`
		RequestLimit  int64 `json:"request_limit" binding:"required"`
		UnitLimit     int64 `json:"unit_limit" binding:"required"`
		Active        *bool `json:"active"`
	}
	if err := ginmw.BindJSON(c, &request); err != nil {
		return
	}
	active := true
	if request.Active != nil {
		active = *request.Active
	}
	policy, err := h.repository.UpsertPolicy(c.Request.Context(), Policy{
		Key:           Key{OrgID: orgID(c), ProductSurface: c.Param("product_surface"), Area: c.Param("area")},
		WindowSeconds: request.WindowSeconds, RequestLimit: request.RequestLimit, UnitLimit: request.UnitLimit, Active: active,
	})
	if err != nil {
		if errors.Is(err, ErrPolicyInUse) {
			ginmw.WriteError(c, http.StatusConflict, "quota_policy_in_use", "quota policy is required by an active capability")
			return
		}
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_quota_policy", "quota policy is invalid")
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, policy)
}

func (h *Handler) List(c *gin.Context) {
	policies, err := h.repository.ListPolicies(c.Request.Context(), orgID(c), c.Param("product_surface"))
	if err != nil {
		ginmw.WriteError(c, http.StatusInternalServerError, "quota_unavailable", "quota policies unavailable")
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": policies})
}

func orgID(c *gin.Context) string { return strings.TrimSpace(c.GetHeader("X-Org-ID")) }
