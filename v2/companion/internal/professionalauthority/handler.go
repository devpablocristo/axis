package professionalauthority

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UseCasesPort interface {
	GetScopePolicy(context.Context, string, uuid.UUID) (ScopePolicy, error)
	PutScopePolicy(context.Context, string, uuid.UUID, PutScopePolicyInput, Actor) (ScopePolicy, error)
	CreatePolicyPack(context.Context, string, CreatePolicyPackInput, Actor) (PolicyPack, error)
	ListPolicyPacks(context.Context, string) ([]PolicyPack, error)
	GetPolicyPack(context.Context, string, uuid.UUID) (PolicyPack, error)
	GetPolicyBinding(context.Context, string, uuid.UUID) (PolicyBinding, error)
	PutPolicyBinding(context.Context, string, uuid.UUID, PutPolicyBindingInput, Actor) (PolicyBinding, error)
	CreateDelegation(context.Context, string, uuid.UUID, CreateDelegationInput, Actor) (Delegation, error)
	ListDelegations(context.Context, string, uuid.UUID, ...Actor) ([]Delegation, error)
	RevokeDelegation(context.Context, string, uuid.UUID, uuid.UUID, RevokeDelegationInput, Actor) (Delegation, error)
	ReviewDelegation(context.Context, string, uuid.UUID, uuid.UUID, ReviewDelegationInput, Actor) (Delegation, error)
}

type Handler struct{ ucs UseCasesPort }

func NewHandler(ucs UseCasesPort) *Handler { return &Handler{ucs: ucs} }

func (h *Handler) Routes(router gin.IRouter) {
	packs := router.Group("/professional-policy-packs")
	packs.POST("", h.CreatePolicyPack)
	packs.GET("", h.ListPolicyPacks)
	packs.GET("/:policy_pack_id", h.GetPolicyPack)

	virployees := router.Group("/virployees/:virployee_id")
	virployees.GET("/scope-policy", h.GetScopePolicy)
	virployees.PUT("/scope-policy", h.PutScopePolicy)
	virployees.GET("/professional-policy-packs", h.GetPolicyBinding)
	virployees.PUT("/professional-policy-packs", h.PutPolicyBinding)
	virployees.GET("/delegations", h.ListDelegations)
	virployees.POST("/delegations", h.CreateDelegation)
	virployees.POST("/delegations/:delegation_id/revoke", h.RevokeDelegation)
	virployees.POST("/delegations/:delegation_id/review", h.ReviewDelegation)
}

type scopePolicyRequest struct {
	AllowedTopics    []string   `json:"allowed_topics"`
	ProhibitedTopics []string   `json:"prohibited_topics"`
	OutOfScope       OutOfScope `json:"out_of_scope"`
	ExpectedRevision int64      `json:"expected_revision"`
}

type policyPackRequest struct {
	PolicyKey string      `json:"policy_key" binding:"required"`
	Name      string      `json:"name" binding:"required"`
	Version   int         `json:"version" binding:"required"`
	JobRoleID string      `json:"job_role_id"`
	Rules     PolicyRules `json:"rules"`
}

type policyBindingRequest struct {
	PolicyPackIDs    []string `json:"policy_pack_ids"`
	ExpectedRevision int64    `json:"expected_revision"`
}

type delegationRequest struct {
	PrincipalType    string          `json:"principal_type" binding:"required"`
	PrincipalID      string          `json:"principal_id" binding:"required"`
	CapabilityScopes []string        `json:"capability_scopes" binding:"required"`
	ProductScopes    []string        `json:"product_scopes" binding:"required"`
	ResourceScopes   []ResourceScope `json:"resource_scopes" binding:"required"`
	MaxRiskClass     string          `json:"max_risk_class" binding:"required"`
	Purpose          string          `json:"purpose" binding:"required"`
	ValidFrom        *time.Time      `json:"valid_from"`
	ValidUntil       time.Time       `json:"valid_until" binding:"required"`
}

type reviewDelegationRequest struct {
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
	Note             string `json:"note" binding:"required"`
}

type revokeDelegationRequest struct {
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
	Reason           string `json:"reason" binding:"required"`
}

func (h *Handler) GetScopePolicy(c *gin.Context) {
	id, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	out, err := h.ucs.GetScopePolicy(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, scopePolicyResponse(out))
}

func (h *Handler) PutScopePolicy(c *gin.Context) {
	id, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	var req scopePolicyRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.PutScopePolicy(c.Request.Context(), tenantID(c), id, PutScopePolicyInput(req), actor(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, scopePolicyResponse(out))
}

func (h *Handler) CreatePolicyPack(c *gin.Context) {
	var req policyPackRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.CreatePolicyPack(c.Request.Context(), tenantID(c), CreatePolicyPackInput(req), actor(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, policyPackResponse(out))
}

func (h *Handler) ListPolicyPacks(c *gin.Context) {
	out, err := h.ucs.ListPolicyPacks(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	items := make([]map[string]any, 0, len(out))
	for _, item := range out {
		items = append(items, policyPackResponse(item))
	}
	ginmw.WriteJSON(c, http.StatusOK, items)
}

func (h *Handler) GetPolicyPack(c *gin.Context) {
	id, ok := parseUUIDParam(c, "policy_pack_id")
	if !ok {
		return
	}
	out, err := h.ucs.GetPolicyPack(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, policyPackResponse(out))
}

func (h *Handler) GetPolicyBinding(c *gin.Context) {
	id, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	out, err := h.ucs.GetPolicyBinding(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, policyBindingResponse(out))
}

func (h *Handler) PutPolicyBinding(c *gin.Context) {
	id, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	var req policyBindingRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.PutPolicyBinding(c.Request.Context(), tenantID(c), id, PutPolicyBindingInput(req), actor(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, policyBindingResponse(out))
}

func (h *Handler) ListDelegations(c *gin.Context) {
	id, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	out, err := h.ucs.ListDelegations(c.Request.Context(), tenantID(c), id, actor(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	items := make([]map[string]any, 0, len(out))
	principalIDs := map[string]struct{}{}
	for _, principalID := range c.QueryArray("principal_id") {
		if principalID = strings.TrimSpace(principalID); principalID != "" {
			principalIDs[principalID] = struct{}{}
		}
	}
	for _, item := range out {
		if len(principalIDs) > 0 {
			if _, ok := principalIDs[item.PrincipalID]; !ok {
				continue
			}
		}
		items = append(items, delegationResponse(item))
	}
	ginmw.WriteJSON(c, http.StatusOK, items)
}

func (h *Handler) ReviewDelegation(c *gin.Context) {
	virployeeID, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	delegationID, ok := parseUUIDParam(c, "delegation_id")
	if !ok {
		return
	}
	var req reviewDelegationRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.ReviewDelegation(c.Request.Context(), tenantID(c), virployeeID, delegationID,
		ReviewDelegationInput(req), actor(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, delegationResponse(out))
}

func (h *Handler) CreateDelegation(c *gin.Context) {
	id, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	var req delegationRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.CreateDelegation(c.Request.Context(), tenantID(c), id, CreateDelegationInput(req), actor(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, delegationResponse(out))
}

func (h *Handler) RevokeDelegation(c *gin.Context) {
	virployeeID, ok := parseVirployeeID(c)
	if !ok {
		return
	}
	delegationID, ok := parseUUIDParam(c, "delegation_id")
	if !ok {
		return
	}
	var req revokeDelegationRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.RevokeDelegation(c.Request.Context(), tenantID(c), virployeeID, delegationID,
		RevokeDelegationInput(req), actor(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, delegationResponse(out))
}

func scopePolicyResponse(in ScopePolicy) map[string]any {
	return map[string]any{
		"virployee_id": in.VirployeeID.String(), "allowed_topics": nonNilStrings(in.AllowedTopics),
		"prohibited_topics": nonNilStrings(in.ProhibitedTopics), "out_of_scope": in.OutOfScope,
		"revision": in.Revision, "created_at": in.CreatedAt, "updated_at": in.UpdatedAt,
	}
}

func policyPackResponse(in PolicyPack) map[string]any {
	jobRoleID := ""
	if in.JobRoleID != nil {
		jobRoleID = in.JobRoleID.String()
	}
	return map[string]any{
		"id": in.ID.String(), "policy_key": in.PolicyKey, "name": in.Name, "version": in.Version,
		"job_role_id": jobRoleID, "rules": in.Rules, "revision": in.Revision, "active": in.Active,
		"created_at": in.CreatedAt, "updated_at": in.UpdatedAt,
	}
}

func policyBindingResponse(in PolicyBinding) map[string]any {
	ids := make([]string, 0, len(in.PolicyPackIDs))
	for _, id := range in.PolicyPackIDs {
		ids = append(ids, id.String())
	}
	return map[string]any{
		"virployee_id": in.VirployeeID.String(), "policy_pack_ids": ids,
		"revision": in.Revision, "created_at": in.CreatedAt, "updated_at": in.UpdatedAt,
	}
}

func delegationResponse(in Delegation) map[string]any {
	status := "active"
	if in.RevokedAt != nil {
		status = "revoked"
	} else if !time.Now().UTC().Before(in.ValidUntil) {
		status = "expired"
	} else if time.Now().UTC().Before(in.ValidFrom) {
		status = "scheduled"
	}
	return map[string]any{
		"id": in.ID.String(), "virployee_id": in.VirployeeID.String(),
		"principal_type": in.PrincipalType, "principal_id": in.PrincipalID,
		"capability_scopes": nonNilStrings(in.CapabilityScopes), "product_scopes": nonNilStrings(in.ProductScopes),
		"resource_scopes": in.ResourceScopes, "max_risk_class": in.MaxRiskClass, "purpose": in.Purpose,
		"granted_by": in.GrantedBy, "valid_from": in.ValidFrom,
		"valid_until": in.ValidUntil, "status": status, "revision": in.Revision,
		"revoked_at": in.RevokedAt, "revoked_by": in.RevokedBy,
		"revocation_reason": in.RevocationReason, "reviewed_at": in.ReviewedAt, "reviewed_by": in.ReviewedBy,
		"review_note": in.ReviewNote, "created_at": in.CreatedAt, "updated_at": in.UpdatedAt,
	}
}

func parseVirployeeID(c *gin.Context) (uuid.UUID, bool) { return parseUUIDParam(c, "virployee_id") }

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param(name)))
	if err != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return uuid.Nil, false
	}
	return id, true
}

func tenantID(c *gin.Context) string { return strings.TrimSpace(c.GetHeader("X-Tenant-ID")) }

func actor(c *gin.Context) Actor {
	return Actor{ID: strings.TrimSpace(c.GetHeader("X-Actor-ID")), Role: strings.TrimSpace(c.GetHeader("X-Axis-Tenant-Role"))}
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
