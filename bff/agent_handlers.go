package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

const unprofiledAgentProfile = "legacy.unprofiled"

type companionAgent struct {
	OrgID               string         `json:"org_id"`
	ProductSurface      string         `json:"product_surface"`
	AgentID             string         `json:"agent_id"`
	DisplayName         string         `json:"display_name,omitempty"`
	Role                string         `json:"role,omitempty"`
	ProfileID           string         `json:"profile_id,omitempty"`
	Status              string         `json:"status"`
	LifecycleStatus     string         `json:"lifecycle_status,omitempty"`
	OriginKind          string         `json:"origin_kind,omitempty"`
	ReviewStatus        string         `json:"review_status,omitempty"`
	MaxAutonomy         string         `json:"max_autonomy"`
	AllowedTools        []string       `json:"allowed_tools,omitempty"`
	AllowedCapabilities []string       `json:"allowed_capabilities,omitempty"`
	AllowedConnectors   []string       `json:"allowed_connectors,omitempty"`
	MemoryScopeID       string         `json:"memory_scope_id,omitempty"`
	SharedMemoryPolicy  map[string]any `json:"shared_memory_policy,omitempty"`
	Limits              map[string]any `json:"limits,omitempty"`
	SLA                 map[string]any `json:"sla,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	Version             int64          `json:"version,omitempty"`
	CreatedBy           string         `json:"created_by,omitempty"`
	CreatedAt           time.Time      `json:"created_at,omitempty"`
	UpdatedAt           time.Time      `json:"updated_at,omitempty"`
}

type agentRuntimeKey struct {
	OrgID          string
	ProductSurface string
	AgentID        string
}

type agentRoutingContext struct {
	AxisOrgID      string
	RuntimeOrgID   string
	ProductSurface string
}

func (s *server) agentsAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts := agentRouteParts(r.URL.Path)
	if r.Method == http.MethodGet && isListRequest(parts) {
		if !requireScope(w, p, "axis:agents:read", "axis:agents:admin") {
			return
		}
		axisOrgID, ok := s.agentAxisOrgFromRequest(w, r, p)
		if !ok {
			return
		}
		routing, ok := s.agentRoutingForAxisOrg(w, r, p, axisOrgID, true)
		if !ok {
			return
		}
		agents, err := s.listCompanionAgents(r, p, routing)
		if err != nil {
			writeLoggedError(w, http.StatusBadGateway, "COMPANION_AGENTS_FAILED", "companion agents request failed", err)
			return
		}
		items := make([]IAMAgent, 0, len(agents))
		for _, agent := range agents {
			items = append(items, companionAgentToView(agent, routing.AxisOrgID))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if len(parts) == 0 && r.Method == http.MethodPost {
		if !requireScope(w, p, "axis:agents:write", "axis:agents:admin") {
			return
		}
		input, ok := decodeJSONBody[IAMAgent](w, r)
		if !ok {
			return
		}
		axisOrgID := strings.TrimSpace(input.OrgID)
		if axisOrgID == "" {
			axisOrgID, ok = s.agentAxisOrgFromRequest(w, r, p)
			if !ok {
				return
			}
		}
		if !agentHasRealProfile(input) {
			writeError(w, http.StatusBadRequest, "VALIDATION", "agent profile is required")
			return
		}
		routing, ok := s.agentRoutingForAxisOrg(w, r, p, axisOrgID, false)
		if !ok {
			return
		}
		agentID := slugify(input.Name)
		if agentID == "" {
			agentID = "agent"
		}
		agentID = agentID + "_" + randomHex(4)
		input.Status = "active"
		input.OriginKind = "manual"
		input.ReviewStatus = "approved"
		input.ValidationStatus = "approved"
		payload := viewToCompanionAgent(input, routing.RuntimeOrgID, routing.ProductSurface, agentID, p.Actor, nil)
		agent, err := s.putCompanionAgent(r, p, routing, agentID, payload)
		if err != nil {
			writeLoggedError(w, http.StatusBadGateway, "COMPANION_AGENTS_FAILED", "companion agents request failed", err)
			return
		}
		s.auditIAM(r, p, routing.AxisOrgID, "agent.created", "agent", agentID, map[string]any{"name": input.Name, "profile": input.Profile, "source": "companion"})
		writeJSON(w, http.StatusCreated, map[string]any{"item": companionAgentToView(agent, routing.AxisOrgID)})
		return
	}
	if len(parts) >= 1 {
		key, err := decodeAgentRuntimeKey(parts[0])
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", "invalid agent id")
			return
		}
		if len(parts) == 1 && (r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if !requireScope(w, p, "axis:agents:write", "axis:agents:admin") {
				return
			}
			if !s.canAccessCompanionAgent(r, p, key) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "selected agent is not allowed for this principal")
				return
			}
			input, ok := decodeJSONBody[IAMAgent](w, r)
			if !ok {
				return
			}
			if !agentHasRealProfile(input) {
				writeError(w, http.StatusBadRequest, "VALIDATION", "agent profile is required")
				return
			}
			routing := agentRoutingContext{AxisOrgID: strings.TrimSpace(input.OrgID), RuntimeOrgID: key.OrgID, ProductSurface: key.ProductSurface}
			if routing.AxisOrgID == "" {
				routing.AxisOrgID = key.OrgID
			}
			existing, err := s.getCompanionAgent(r, p, key)
			if err != nil {
				writeLoggedError(w, http.StatusBadGateway, "COMPANION_AGENTS_FAILED", "companion agents request failed", err)
				return
			}
			payload := viewToCompanionAgent(input, key.OrgID, key.ProductSurface, key.AgentID, p.Actor, &existing)
			agent, err := s.putCompanionAgent(r, p, routing, key.AgentID, payload)
			if err != nil {
				writeLoggedError(w, http.StatusBadGateway, "COMPANION_AGENTS_FAILED", "companion agents request failed", err)
				return
			}
			s.auditIAM(r, p, routing.AxisOrgID, "agent.updated", "agent", key.AgentID, map[string]any{"name": input.Name, "profile": input.Profile, "source": "companion"})
			writeJSON(w, http.StatusOK, map[string]any{"item": companionAgentToView(agent, routing.AxisOrgID)})
			return
		}
		if len(parts) == 2 {
			s.agentLifecycle(w, r, p, key, parts[1])
			return
		}
	}
	http.NotFound(w, r)
}

func (s *server) agentLifecycle(w http.ResponseWriter, r *http.Request, p authn.Principal, key agentRuntimeKey, action string) {
	if action == "purge" {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireScope(w, p, "axis:iam:purge") {
			return
		}
		if !s.canAccessCompanionAgent(r, p, key) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected agent is not allowed for this principal")
			return
		}
		if err := s.deleteCompanionAgent(r, p, key); err != nil {
			writeLoggedError(w, http.StatusBadGateway, "COMPANION_AGENTS_FAILED", "companion agents request failed", err)
			return
		}
		s.auditIAM(r, p, key.OrgID, "agent.purged", "agent", key.AgentID, map[string]any{"source": "companion", "product_surface": key.ProductSurface})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireScope(w, p, "axis:agents:write", "axis:agents:admin") {
		return
	}
	if !s.canAccessCompanionAgent(r, p, key) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "selected agent is not allowed for this principal")
		return
	}
	if !validCompanionAgentAction(action) {
		http.NotFound(w, r)
		return
	}
	agent, err := s.postCompanionAgentAction(r, p, key, action)
	if err != nil {
		writeLoggedError(w, http.StatusBadGateway, "COMPANION_AGENTS_FAILED", "companion agents request failed", err)
		return
	}
	s.auditIAM(r, p, key.OrgID, "agent."+action, "agent", key.AgentID, map[string]any{"source": "companion", "product_surface": key.ProductSurface})
	writeJSON(w, http.StatusOK, map[string]any{"item": companionAgentToView(agent, key.OrgID)})
}

func (s *server) agentAxisOrgFromRequest(w http.ResponseWriter, r *http.Request, p authn.Principal) (string, bool) {
	orgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if orgID == "" {
		var err error
		orgID, err = s.selectedOrg(r, p)
		if err != nil {
			writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return "", false
		}
	}
	if !s.canAccessOrg(r, p, orgID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
		return "", false
	}
	return orgID, true
}

func (s *server) agentRoutingForAxisOrg(w http.ResponseWriter, r *http.Request, p authn.Principal, axisOrgID string, list bool) (agentRoutingContext, bool) {
	if !s.canAccessOrg(r, p, axisOrgID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
		return agentRoutingContext{}, false
	}
	routing := agentRoutingContext{
		AxisOrgID:      axisOrgID,
		RuntimeOrgID:   axisOrgID,
		ProductSurface: "companion",
	}
	products, err := s.iam.ListProducts(r.Context(), axisOrgID, "active")
	if err == nil && len(products) > 0 {
		routing.ProductSurface = strings.TrimSpace(products[0].ProductSurface)
		if runtimeOrg, ok := products[0].Config["runtime_org_id"].(string); ok && strings.TrimSpace(runtimeOrg) != "" {
			routing.RuntimeOrgID = strings.TrimSpace(runtimeOrg)
		}
	}
	if requestedSurface := strings.TrimSpace(r.URL.Query().Get("product_surface")); requestedSurface != "" {
		routing.ProductSurface = requestedSurface
	}
	_ = list
	return routing, true
}

func (s *server) canAccessCompanionAgent(r *http.Request, p authn.Principal, key agentRuntimeKey) bool {
	if hasScope(p.Scopes, "axis:cross_org", "companion:cross_org") {
		return true
	}
	return s.canAccessOrg(r, p, key.OrgID)
}

func (s *server) listCompanionAgents(r *http.Request, p authn.Principal, routing agentRoutingContext) ([]companionAgent, error) {
	target, err := s.companionAgentURL("/v1/agents")
	if err != nil {
		return nil, err
	}
	q := target.Query()
	q.Set("org_id", routing.RuntimeOrgID)
	q.Set("product_surface", routing.ProductSurface)
	target.RawQuery = q.Encode()
	req, err := s.companionAgentRequest(r, p, http.MethodGet, target, routing.AxisOrgID, routing.ProductSurface, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, companionResponseError(resp)
	}
	var payload struct {
		Data []companionAgent `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func (s *server) putCompanionAgent(r *http.Request, p authn.Principal, routing agentRoutingContext, agentID string, payload companionAgent) (companionAgent, error) {
	target, err := s.companionAgentURL("/v1/agents/" + url.PathEscape(agentID))
	if err != nil {
		return companionAgent{}, err
	}
	q := target.Query()
	q.Set("org_id", routing.RuntimeOrgID)
	q.Set("product_surface", routing.ProductSurface)
	target.RawQuery = q.Encode()
	body, err := json.Marshal(payload)
	if err != nil {
		return companionAgent{}, err
	}
	req, err := s.companionAgentRequest(r, p, http.MethodPut, target, routing.AxisOrgID, routing.ProductSurface, bytes.NewReader(body))
	if err != nil {
		return companionAgent{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return companionAgent{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return companionAgent{}, companionResponseError(resp)
	}
	var agent companionAgent
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		return companionAgent{}, err
	}
	return agent, nil
}

func (s *server) getCompanionAgent(r *http.Request, p authn.Principal, key agentRuntimeKey) (companionAgent, error) {
	target, err := s.companionAgentURL("/v1/agents/" + url.PathEscape(key.AgentID))
	if err != nil {
		return companionAgent{}, err
	}
	q := target.Query()
	q.Set("org_id", key.OrgID)
	q.Set("product_surface", key.ProductSurface)
	target.RawQuery = q.Encode()
	req, err := s.companionAgentRequest(r, p, http.MethodGet, target, key.OrgID, key.ProductSurface, nil)
	if err != nil {
		return companionAgent{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return companionAgent{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return companionAgent{}, companionResponseError(resp)
	}
	var agent companionAgent
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		return companionAgent{}, err
	}
	return agent, nil
}

func (s *server) postCompanionAgentAction(r *http.Request, p authn.Principal, key agentRuntimeKey, action string) (companionAgent, error) {
	target, err := s.companionAgentURL("/v1/agents/" + url.PathEscape(key.AgentID) + "/" + action)
	if err != nil {
		return companionAgent{}, err
	}
	q := target.Query()
	q.Set("org_id", key.OrgID)
	q.Set("product_surface", key.ProductSurface)
	target.RawQuery = q.Encode()
	req, err := s.companionAgentRequest(r, p, http.MethodPost, target, key.OrgID, key.ProductSurface, nil)
	if err != nil {
		return companionAgent{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return companionAgent{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return companionAgent{}, companionResponseError(resp)
	}
	var agent companionAgent
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		return companionAgent{}, err
	}
	return agent, nil
}

func (s *server) deleteCompanionAgent(r *http.Request, p authn.Principal, key agentRuntimeKey) error {
	target, err := s.companionAgentURL("/v1/agents/" + url.PathEscape(key.AgentID))
	if err != nil {
		return err
	}
	q := target.Query()
	q.Set("org_id", key.OrgID)
	q.Set("product_surface", key.ProductSurface)
	target.RawQuery = q.Encode()
	req, err := s.companionAgentRequest(r, p, http.MethodDelete, target, key.OrgID, key.ProductSurface, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return companionResponseError(resp)
	}
	return nil
}

func (s *server) companionAgentURL(path string) (*url.URL, error) {
	target, err := url.Parse(s.cfg.CompanionBaseURL)
	if err != nil {
		return nil, err
	}
	target.Path = path
	return target, nil
}

func (s *server) companionAgentRequest(r *http.Request, p authn.Principal, method string, target *url.URL, tokenOrgID string, productSurface string, body io.Reader) (*http.Request, error) {
	token, err := s.signDownstreamToken(p, tokenOrgID, s.cfg.CompanionAudience)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), method, target.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Request-ID", requestID(r))
	req.Header.Set("X-Axis-Forwarded-By", "axis-bff")
	req.Header.Set("X-Product-Surface", productSurface)
	return req, nil
}

func companionResponseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("companion status %d: %s", resp.StatusCode, message)
}

func viewToCompanionAgent(input IAMAgent, orgID string, productSurface string, agentID string, actor string, existing *companionAgent) companionAgent {
	memoryScopeID := ""
	if input.MemoryEnabled {
		memoryScopeID = "shared"
	}
	profile := strings.TrimSpace(input.Profile)
	if profile == unprofiledAgentProfile {
		profile = ""
	}
	status := "active"
	lifecycleStatus := "active"
	originKind := "manual"
	reviewStatus := "approved"
	if existing != nil {
		status = firstNonEmpty(existing.Status, status)
		lifecycleStatus = firstNonEmpty(existing.LifecycleStatus, lifecycleStatus)
		originKind = firstNonEmpty(existing.OriginKind, originKind)
		reviewStatus = firstNonEmpty(existing.ReviewStatus, reviewStatus)
	}
	if trimmed := strings.TrimSpace(input.Status); trimmed != "" {
		lifecycleStatus = trimmed
		if trimmed == "active" {
			status = "active"
		} else {
			status = "disabled"
		}
	}
	if trimmed := strings.TrimSpace(input.OriginKind); trimmed != "" {
		originKind = trimmed
	}
	if trimmed := firstNonEmpty(input.ValidationStatus, input.ReviewStatus); trimmed != "" {
		reviewStatus = trimmed
	}
	allowedConnectors := []string(nil)
	sharedMemoryPolicy := map[string]any(nil)
	limits := map[string]any(nil)
	sla := map[string]any(nil)
	metadata := input.Metadata
	if existing != nil {
		allowedConnectors = cleanStringList(existing.AllowedConnectors)
		sharedMemoryPolicy = existing.SharedMemoryPolicy
		limits = existing.Limits
		sla = existing.SLA
		if metadata == nil {
			metadata = existing.Metadata
		}
	}
	return companionAgent{
		OrgID:               orgID,
		ProductSurface:      productSurface,
		AgentID:             agentID,
		DisplayName:         firstNonEmpty(input.Name, agentID),
		Role:                strings.TrimSpace(input.Description),
		ProfileID:           profile,
		Status:              status,
		LifecycleStatus:     lifecycleStatus,
		OriginKind:          originKind,
		ReviewStatus:        reviewStatus,
		MaxAutonomy:         normalizeAutonomy(input.Autonomy),
		AllowedTools:        cleanStringList(input.Tools),
		AllowedCapabilities: cleanStringList(input.Capabilities),
		AllowedConnectors:   allowedConnectors,
		MemoryScopeID:       memoryScopeID,
		SharedMemoryPolicy:  sharedMemoryPolicy,
		Limits:              limits,
		SLA:                 sla,
		Metadata:            metadata,
		CreatedBy:           actor,
	}
}

func companionAgentToView(agent companionAgent, axisOrgID string) IAMAgent {
	status := firstNonEmpty(agent.LifecycleStatus, "active")
	if agent.Status == "disabled" && status == "active" {
		status = "archived"
	}
	profile := strings.TrimSpace(agent.ProfileID)
	if profile == "" {
		profile = unprofiledAgentProfile
	}
	return IAMAgent{
		ID:                   encodeAgentRuntimeKey(agentRuntimeKey{OrgID: agent.OrgID, ProductSurface: agent.ProductSurface, AgentID: agent.AgentID}),
		OrgID:                firstNonEmpty(axisOrgID, agent.OrgID),
		Name:                 firstNonEmpty(agent.DisplayName, agent.AgentID),
		Profile:              profile,
		Autonomy:             normalizeAutonomy(agent.MaxAutonomy),
		MemoryEnabled:        strings.TrimSpace(agent.MemoryScopeID) != "" || len(agent.SharedMemoryPolicy) > 0,
		Description:          strings.TrimSpace(agent.Role),
		Capabilities:         cleanStringList(agent.AllowedCapabilities),
		Tools:                cleanStringList(agent.AllowedTools),
		Status:               status,
		SourceSystem:         "companion",
		SourceOrgID:          agent.OrgID,
		SourceProductSurface: agent.ProductSurface,
		SourceAgentID:        agent.AgentID,
		SourceStatus:         agent.Status,
		OriginKind:           firstNonEmpty(agent.OriginKind, "companion_fleet"),
		ReviewStatus:         firstNonEmpty(agent.ReviewStatus, "approved"),
		ValidationStatus:     firstNonEmpty(agent.ReviewStatus, "approved"),
		Metadata:             agent.Metadata,
		CreatedAt:            agent.CreatedAt,
		UpdatedAt:            agent.UpdatedAt,
	}
}

func validCompanionAgentAction(action string) bool {
	switch action {
	case "archive", "trash", "restore", "approve", "ignore":
		return true
	default:
		return false
	}
}

func agentHasRealProfile(agent IAMAgent) bool {
	profile := strings.TrimSpace(agent.Profile)
	return profile != "" && profile != unprofiledAgentProfile
}

func encodeAgentRuntimeKey(key agentRuntimeKey) string {
	raw := key.OrgID + "\x00" + key.ProductSurface + "\x00" + key.AgentID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeAgentRuntimeKey(value string) (agentRuntimeKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return agentRuntimeKey{}, err
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return agentRuntimeKey{}, fmt.Errorf("invalid agent id")
	}
	return agentRuntimeKey{OrgID: parts[0], ProductSurface: parts[1], AgentID: parts[2]}, nil
}

func agentRouteParts(path string) []string {
	path = strings.TrimPrefix(path, "/api/agents")
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
