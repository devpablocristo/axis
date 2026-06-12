package runtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const scopeCompanionRuntimeAdmin = "companion:runtime:admin"

type RuntimeControlsHandler struct {
	repo RuntimeControls
}

func NewRuntimeControlsHandler(repo RuntimeControls) *RuntimeControlsHandler {
	return &RuntimeControlsHandler{repo: repo}
}

func (h *RuntimeControlsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/runtime/policy", h.getPolicy)
	mux.HandleFunc("PUT /v1/runtime/policy", h.putPolicy)
	mux.HandleFunc("GET /v1/runtime/policy/audit", h.getPolicyAudit)
	mux.HandleFunc("GET /v1/runtime/mcp-policy", h.getMCPPolicy)
	mux.HandleFunc("PUT /v1/runtime/mcp-policy", h.putMCPPolicy)
	mux.HandleFunc("GET /v1/runtime/usage", h.getUsage)
}

func (h *RuntimeControlsHandler) getPolicy(w http.ResponseWriter, r *http.Request) {
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

func (h *RuntimeControlsHandler) putPolicy(w http.ResponseWriter, r *http.Request) {
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
	if err := validateOrgControlPlaneSettings(policy.ControlPlane); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	saved, err := h.repo.UpsertRuntimePolicy(r.Context(), policy)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "save runtime policy failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, saved)
}

func (h *RuntimeControlsHandler) getPolicyAudit(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.requireRuntimeAdminOrg(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	entries, err := h.repo.ListRuntimePolicyAudit(r.Context(), orgID, limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list runtime policy audit failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *RuntimeControlsHandler) getMCPPolicy(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.requireRuntimeAdminOrg(w, r)
	if !ok {
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
	httpjson.WriteJSON(w, http.StatusOK, mcpRuntimePolicyViewFrom(policy))
}

func (h *RuntimeControlsHandler) putMCPPolicy(w http.ResponseWriter, r *http.Request) {
	orgID, ok := h.requireRuntimeAdminOrg(w, r)
	if !ok {
		return
	}
	var req mcpRuntimePolicyRequest
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
	if err := applyMCPRuntimePolicyRequest(&policy, req); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if err := validateOrgControlPlaneSettings(policy.ControlPlane); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	saved, err := h.repo.UpsertRuntimePolicy(r.Context(), policy)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "save runtime policy failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, mcpRuntimePolicyViewFrom(saved))
}

func (h *RuntimeControlsHandler) getUsage(w http.ResponseWriter, r *http.Request) {
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

func (h *RuntimeControlsHandler) requireRuntimeAdminOrg(w http.ResponseWriter, r *http.Request) (string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "runtime controls endpoints require authenticated admin context")
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
	Enabled                *bool                    `json:"enabled"`
	KillSwitch             *bool                    `json:"kill_switch"`
	MaxAutonomy            string                   `json:"max_autonomy"`
	AllowedProductSurfaces []string                 `json:"allowed_product_surfaces"`
	AllowedModels          []string                 `json:"allowed_models"`
	MonthlyTokenBudget     *int64                   `json:"monthly_token_budget"`
	MonthlyToolCallBudget  *int64                   `json:"monthly_tool_call_budget"`
	ControlPlane           *OrgControlPlaneSettings `json:"control_plane"`
	Metadata               map[string]any           `json:"metadata"`
}

type mcpRuntimePolicyView struct {
	OrgID                  string                             `json:"org_id"`
	Enabled                bool                               `json:"enabled"`
	KillSwitch             bool                               `json:"kill_switch"`
	AllowedProductSurfaces []string                           `json:"allowed_product_surfaces,omitempty"`
	AllowedTools           []string                           `json:"allowed_tools,omitempty"`
	DeniedTools            []string                           `json:"denied_tools,omitempty"`
	ToolKillSwitches       map[string]bool                    `json:"tool_kill_switches,omitempty"`
	ProductPolicies        map[string]mcpProductRuntimePolicy `json:"product_policies,omitempty"`
	Metadata               mcpRuntimePolicyMetadata           `json:"metadata,omitempty"`
	SettingsVersion        int64                              `json:"settings_version"`
	UpdatedAt              time.Time                          `json:"updated_at,omitempty"`
}

type mcpRuntimePolicyRequest struct {
	Enabled                *bool                                     `json:"enabled"`
	KillSwitch             *bool                                     `json:"kill_switch"`
	AllowedProductSurfaces []string                                  `json:"allowed_product_surfaces"`
	AllowedTools           []string                                  `json:"allowed_tools"`
	DeniedTools            []string                                  `json:"denied_tools"`
	ToolKillSwitches       map[string]bool                           `json:"tool_kill_switches"`
	ProductPolicies        map[string]mcpProductRuntimePolicyRequest `json:"product_policies"`
	Metadata               *mcpRuntimePolicyMetadataRequest          `json:"metadata"`
}

type mcpProductRuntimePolicy struct {
	Denied bool `json:"denied"`
}

type mcpProductRuntimePolicyRequest struct {
	Denied *bool `json:"denied"`
}

type mcpRuntimePolicyMetadata struct {
	ChangedBy    string `json:"changed_by,omitempty"`
	ChangeReason string `json:"change_reason,omitempty"`
}

type mcpRuntimePolicyMetadataRequest struct {
	ChangedBy    *string `json:"changed_by"`
	ChangeReason *string `json:"change_reason"`
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
	if req.ControlPlane != nil {
		policy.ControlPlane = normalizeOrgControlPlaneSettings(*req.ControlPlane)
	}
	if req.Metadata != nil {
		policy.Metadata = req.Metadata
	}
}

func applyMCPRuntimePolicyRequest(policy *TenantRuntimePolicy, req mcpRuntimePolicyRequest) error {
	if req.Enabled != nil {
		policy.Enabled = *req.Enabled
	}
	if req.KillSwitch != nil {
		policy.KillSwitch = *req.KillSwitch
	}
	if req.AllowedProductSurfaces != nil {
		if err := validateProductSurfaceList(req.AllowedProductSurfaces); err != nil {
			return err
		}
		policy.AllowedProductSurfaces = normalizeStringList(req.AllowedProductSurfaces)
	}
	settings := policy.ControlPlane
	if req.AllowedTools != nil {
		if err := validateMCPToolPatterns(req.AllowedTools); err != nil {
			return err
		}
		settings.AllowedTools = normalizeStringList(req.AllowedTools)
	}
	if req.DeniedTools != nil {
		if err := validateMCPToolPatterns(req.DeniedTools); err != nil {
			return err
		}
		settings.DeniedTools = normalizeStringList(req.DeniedTools)
	}
	if req.ToolKillSwitches != nil {
		if err := validateMCPToolPatternMap(req.ToolKillSwitches); err != nil {
			return err
		}
		settings.ToolKillSwitches = normalizeBoolMap(req.ToolKillSwitches)
	}
	if req.ProductPolicies != nil {
		if settings.ProductPolicies == nil {
			settings.ProductPolicies = map[string]ProductRuntimePolicy{}
		}
		for productSurface, patch := range req.ProductPolicies {
			productSurface = strings.TrimSpace(strings.ToLower(productSurface))
			if productSurface == "" {
				return errors.New("product_policies product_surface is required")
			}
			current := settings.ProductPolicies[productSurface]
			if patch.Denied != nil {
				current.Denied = *patch.Denied
			}
			settings.ProductPolicies[productSurface] = current
		}
	}
	policy.ControlPlane = normalizeOrgControlPlaneSettings(settings)
	if req.Metadata != nil {
		if policy.Metadata == nil {
			policy.Metadata = map[string]any{}
		}
		applyOptionalMetadataString(policy.Metadata, "changed_by", req.Metadata.ChangedBy)
		applyOptionalMetadataString(policy.Metadata, "change_reason", req.Metadata.ChangeReason)
	}
	return nil
}

func mcpRuntimePolicyViewFrom(policy TenantRuntimePolicy) mcpRuntimePolicyView {
	policy = normalizeRuntimePolicy(policy)
	productPolicies := make(map[string]mcpProductRuntimePolicy, len(policy.ControlPlane.ProductPolicies))
	for productSurface, productPolicy := range policy.ControlPlane.ProductPolicies {
		productSurface = strings.TrimSpace(productSurface)
		if productSurface == "" {
			continue
		}
		productPolicies[productSurface] = mcpProductRuntimePolicy{Denied: productPolicy.Denied}
	}
	if len(productPolicies) == 0 {
		productPolicies = nil
	}
	return mcpRuntimePolicyView{
		OrgID:                  policy.OrgID,
		Enabled:                policy.Enabled,
		KillSwitch:             policy.KillSwitch,
		AllowedProductSurfaces: append([]string(nil), policy.AllowedProductSurfaces...),
		AllowedTools:           append([]string(nil), policy.ControlPlane.AllowedTools...),
		DeniedTools:            append([]string(nil), policy.ControlPlane.DeniedTools...),
		ToolKillSwitches:       cloneBoolMap(policy.ControlPlane.ToolKillSwitches),
		ProductPolicies:        productPolicies,
		Metadata: mcpRuntimePolicyMetadata{
			ChangedBy:    stringMetadata(policy.Metadata, "changed_by"),
			ChangeReason: stringMetadata(policy.Metadata, "change_reason"),
		},
		SettingsVersion: policy.SettingsVersion,
		UpdatedAt:       policy.UpdatedAt,
	}
}

func validateProductSurfaceList(values []string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return errors.New("allowed_product_surfaces cannot contain empty product_surface")
		}
	}
	return nil
}

func validateMCPToolPatterns(patterns []string) error {
	for _, pattern := range patterns {
		if !validMCPToolPattern(pattern) {
			return errors.New("mcp tool patterns must be exact names, *, or prefix patterns ending in *")
		}
	}
	return nil
}

func validateMCPToolPatternMap(values map[string]bool) error {
	for pattern := range values {
		if !validMCPToolPattern(pattern) {
			return errors.New("mcp tool kill switch keys must be exact names, *, or prefix patterns ending in *")
		}
	}
	return nil
}

func validMCPToolPattern(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || strings.ContainsAny(pattern, " \t\r\n") {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.Contains(pattern, "*") {
		return strings.Count(pattern, "*") == 1 && strings.HasSuffix(pattern, "*") && strings.TrimSuffix(pattern, "*") != ""
	}
	return true
}

func applyOptionalMetadataString(metadata map[string]any, key string, value *string) {
	if value == nil {
		return
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		delete(metadata, key)
		return
	}
	metadata[key] = trimmed
}

func stringMetadata(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	if value, ok := metadata[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]bool, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func validAutonomy(level AutonomyLevel) bool {
	switch level {
	case AutonomyA0, AutonomyA1, AutonomyA2, AutonomyA3, AutonomyA4, AutonomyA5:
		return true
	default:
		return false
	}
}

func validateOrgControlPlaneSettings(settings OrgControlPlaneSettings) error {
	settings = normalizeOrgControlPlaneSettings(settings)
	if settings.MonthlyCostBudgetCents < 0 {
		return errors.New("monthly_cost_budget_cents must be greater than or equal to zero")
	}
	if settings.Retention.RunTraceDays < 0 || settings.Retention.ToolEvidenceDays < 0 || settings.Retention.MemoryDays < 0 {
		return errors.New("retention days must be greater than or equal to zero")
	}
	if settings.Memory.RetentionDays < 0 || settings.Memory.CompactionAfterDays < 0 {
		return errors.New("memory policy days must be greater than or equal to zero")
	}
	if settings.Memory.MinConfidence < 0 || settings.Memory.MinConfidence > 1 {
		return errors.New("memory min_confidence must be between 0 and 1")
	}
	if !validRiskClass(settings.MaxRiskClass) {
		return errors.New("max_risk_class must be none, low, medium, high, or critical")
	}
	if !validDataIsolationMode(settings.DataIsolation.Mode) {
		return errors.New("data_isolation.mode must be strict_org, dedicated_store, or inherited")
	}
	if !validTraceLevel(settings.Observability.TraceLevel) {
		return errors.New("observability.trace_level must be minimal, standard, or debug")
	}
	if !validRedactionMode(settings.Observability.RedactionMode) {
		return errors.New("observability.redaction_mode must be strict, standard, or disabled")
	}
	if settings.Embedding.Dimensions <= 0 {
		return errors.New("embedding.dimensions must be greater than zero")
	}
	if settings.Embedding.BatchSize <= 0 {
		return errors.New("embedding.batch_size must be greater than zero")
	}
	if !validEmbeddingVectorStore(settings.Embedding.VectorStore) {
		return errors.New("embedding.vector_store must be postgres, external, or disabled")
	}
	for _, threshold := range settings.EvalThresholds {
		if threshold < 0 || threshold > 1 {
			return errors.New("eval thresholds must be between 0 and 1")
		}
	}
	return nil
}

func validRiskClass(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none", "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func validDataIsolationMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "strict_org", "dedicated_store", "inherited":
		return true
	default:
		return false
	}
}

func validTraceLevel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "minimal", "standard", "debug":
		return true
	default:
		return false
	}
}

func validRedactionMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "strict", "standard", "disabled":
		return true
	default:
		return false
	}
}

func validEmbeddingVectorStore(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "postgres", "external", "disabled":
		return true
	default:
		return false
	}
}
