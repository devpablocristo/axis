package runtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const scopeCompanionRuntimeAdmin = "companion:runtime:admin"

type RuntimeGovernanceHandler struct {
	repo RuntimeGovernance
}

func NewRuntimeGovernanceHandler(repo RuntimeGovernance) *RuntimeGovernanceHandler {
	return &RuntimeGovernanceHandler{repo: repo}
}

func (h *RuntimeGovernanceHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/runtime/policy", h.getPolicy)
	mux.HandleFunc("PUT /v1/runtime/policy", h.putPolicy)
	mux.HandleFunc("GET /v1/runtime/usage", h.getUsage)
}

func (h *RuntimeGovernanceHandler) getPolicy(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.requireRuntimeAdminOrg(w, r)
	if !ok {
		return
	}
	policy, err := h.repo.GetRuntimePolicy(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, ErrRuntimePolicyNotFound) {
			httpjson.WriteJSON(w, http.StatusOK, defaultRuntimePolicy(orgID))
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get runtime policy failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, policy)
}

func (h *RuntimeGovernanceHandler) putPolicy(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.requireRuntimeAdminOrg(w, r)
	if !ok {
		return
	}
	var req runtimePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	policy, err := h.repo.GetRuntimePolicy(r.Context(), orgID)
	if err != nil {
		if !errors.Is(err, ErrRuntimePolicyNotFound) {
			httpjson.WriteFlatInternalError(w, err, "get runtime policy failed")
			return
		}
		policy = defaultRuntimePolicy(orgID)
	}
	applyRuntimePolicyRequest(&policy, req)
	if !validAutonomy(policy.MaxAutonomy) {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "max_autonomy must be A0..A5")
		return
	}
	if policy.MonthlyTokenBudget < 0 || policy.MonthlyToolCallBudget < 0 {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "monthly budgets must be greater than or equal to zero")
		return
	}
	saved, err := h.repo.UpsertRuntimePolicy(r.Context(), policy)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "save runtime policy failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, saved)
}

func (h *RuntimeGovernanceHandler) getUsage(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.requireRuntimeAdminOrg(w, r)
	if !ok {
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = runtimeUsagePeriod(time.Now())
	}
	usage, err := h.repo.GetRuntimeUsage(r.Context(), orgID, period)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "get runtime usage failed")
		return
	}
	policy, err := h.repo.GetRuntimePolicy(r.Context(), orgID)
	if err == nil {
		usage.MonthlyTokenBudget = policy.MonthlyTokenBudget
		usage.MonthlyToolCallBudget = policy.MonthlyToolCallBudget
		if policy.MonthlyTokenBudget > 0 {
			usage.TokenBudgetRemaining = policy.MonthlyTokenBudget - usage.EstimatedTokens
		}
		if policy.MonthlyToolCallBudget > 0 {
			usage.ToolCallBudgetRemaining = policy.MonthlyToolCallBudget - usage.ToolCalls
		}
	}
	httpjson.WriteJSON(w, http.StatusOK, usage)
}

func (h *RuntimeGovernanceHandler) requireRuntimeAdminOrg(w http.ResponseWriter, r *http.Request) (string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "runtime governance endpoints require authenticated admin context")
		return "", false
	}
	if !identityctx.HasAnyScope(r, scopeCompanionRuntimeAdmin, scopeCompanionCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing runtime admin scope")
		return "", false
	}
	orgID := strings.TrimSpace(identityctx.PrincipalOrgID(r))
	if orgID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return "", false
	}
	return orgID, true
}

type runtimePolicyRequest struct {
	Enabled                *bool          `json:"enabled"`
	KillSwitch             *bool          `json:"kill_switch"`
	MaxAutonomy            string         `json:"max_autonomy"`
	AllowedProductSurfaces []string       `json:"allowed_product_surfaces"`
	AllowedModels          []string       `json:"allowed_models"`
	MonthlyTokenBudget     *int64         `json:"monthly_token_budget"`
	MonthlyToolCallBudget  *int64         `json:"monthly_tool_call_budget"`
	Metadata               map[string]any `json:"metadata"`
}

func applyRuntimePolicyRequest(policy *TenantRuntimePolicy, req runtimePolicyRequest) {
	if req.Enabled != nil {
		policy.Enabled = *req.Enabled
	}
	if req.KillSwitch != nil {
		policy.KillSwitch = *req.KillSwitch
	}
	if strings.TrimSpace(req.MaxAutonomy) != "" {
		policy.MaxAutonomy = AutonomyLevel(strings.TrimSpace(req.MaxAutonomy))
	}
	if req.AllowedProductSurfaces != nil {
		policy.AllowedProductSurfaces = normalizeStringList(req.AllowedProductSurfaces)
	}
	if req.AllowedModels != nil {
		policy.AllowedModels = normalizeStringList(req.AllowedModels)
	}
	if req.MonthlyTokenBudget != nil {
		policy.MonthlyTokenBudget = *req.MonthlyTokenBudget
	}
	if req.MonthlyToolCallBudget != nil {
		policy.MonthlyToolCallBudget = *req.MonthlyToolCallBudget
	}
	if req.Metadata != nil {
		policy.Metadata = req.Metadata
	}
}

func validAutonomy(level AutonomyLevel) bool {
	switch level {
	case AutonomyA0, AutonomyA1, AutonomyA2, AutonomyA3, AutonomyA4, AutonomyA5:
		return true
	default:
		return false
	}
}
