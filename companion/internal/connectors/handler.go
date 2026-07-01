package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/connectors/handler/dto"
	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

const (
	defaultListLimit = 50
)

type connectorUsecase interface {
	ListConnectorsByLifecycle(ctx context.Context, lifecycle string) ([]domain.Connector, error)
	ConnectorTypes() []domain.ConnectorType
	GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error)
	SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error)
	ArchiveConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error)
	TrashConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error)
	RestoreConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error)
	TestConnector(ctx context.Context, id uuid.UUID) error
	RefreshConnector(ctx context.Context, id uuid.UUID) registry.RefreshResult
	DeleteConnector(ctx context.Context, id uuid.UUID) error
	Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error)
	BuildActionBinding(ctx context.Context, spec domain.ExecutionSpec) (map[string]any, string, error)
	ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error)
	Capabilities(filter domain.CapabilityFilter) []ConnectorCapabilities
	CapabilityManifests(filter domain.CapabilityFilter) ([]capabilities.Manifest, error)
	RefreshConnectors(ctx context.Context) []registry.RefreshResult
}

// Handler HTTP adapter para conectores.
type Handler struct {
	uc connectorUsecase
}

// NewHandler crea un nuevo handler de conectores.
func NewHandler(uc connectorUsecase) *Handler {
	return &Handler{uc: uc}
}

// Register registra las rutas de conectores en el mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/connectors/types", h.types)
	mux.HandleFunc("GET /v1/connectors", h.list)
	mux.HandleFunc("POST /v1/connectors", h.save)
	mux.HandleFunc("POST /v1/connectors/refresh", h.refresh)
	mux.HandleFunc("GET /v1/connectors/{id}", h.get)
	mux.HandleFunc("PATCH /v1/connectors/{id}", h.patch)
	mux.HandleFunc("DELETE /v1/connectors/{id}", h.delete)
	mux.HandleFunc("POST /v1/connectors/{id}/archive", h.archive)
	mux.HandleFunc("POST /v1/connectors/{id}/trash", h.trash)
	mux.HandleFunc("POST /v1/connectors/{id}/restore", h.restore)
	mux.HandleFunc("POST /v1/connectors/{id}/test", h.test)
	mux.HandleFunc("POST /v1/connectors/{id}/refresh", h.refreshOne)
	mux.HandleFunc("POST /v1/connectors/execute", h.execute)
	mux.HandleFunc("POST /v1/connectors/action-binding", h.actionBinding)
	mux.HandleFunc("GET /v1/connectors/{id}/executions", h.listExecutions)
	mux.HandleFunc("GET /v1/connectors/capabilities", h.capabilities)
	mux.HandleFunc("GET /v1/connectors/capability-manifests", h.capabilityManifests)
}

func (h *Handler) types(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsAdmin, scopeCompanionConnectorsExecute) {
		return
	}
	types := h.uc.ConnectorTypes()
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorTypesResponse{Types: types, Data: types})
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	results := h.uc.RefreshConnectors(r.Context())
	out := make([]dto.ConnectorRefreshResult, 0, len(results))
	for _, res := range results {
		out = append(out, dto.ConnectorRefreshResult{
			ConnectorID: res.ConnectorID,
			Refreshed:   res.Refreshed,
			Error:       res.Error,
		})
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorRefreshResponse{Results: out})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	conns, err := h.uc.ListConnectorsByLifecycle(r.Context(), r.URL.Query().Get("lifecycle"))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list connectors failed")
		return
	}
	out := make([]dto.ConnectorResponse, 0, len(conns))
	for _, c := range conns {
		if !canAccessConnectorOrg(r, c) {
			continue
		}
		out = append(out, dto.ConnectorToResponse(c))
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorListResponse{Connectors: out, Data: out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	conn, err := h.uc.GetConnector(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "connector not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get connector failed")
		return
	}
	if !canAccessConnectorOrg(r, conn) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorToResponse(conn))
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsAdmin) {
		return
	}
	orgID, ok := effectiveConnectorOrgID(r, r.URL.Query().Get("org_id"))
	if !ok || (!identityctx.HasNoAuthContext(r) && orgID == "") {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector save requires org context")
		return
	}
	var body dto.SaveConnectorRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Name == "" || body.Kind == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "name and kind are required")
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	configJSON := body.Config
	if len(configJSON) == 0 {
		configJSON = json.RawMessage(`{}`)
	}
	conn, err := h.uc.SaveConnector(r.Context(), domain.Connector{
		OrgID:      orgID,
		Name:       body.Name,
		Kind:       body.Kind,
		Enabled:    enabled,
		Status:     body.Status,
		ConfigJSON: configJSON,
	})
	if err != nil {
		writeConnectorError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, dto.ConnectorToResponse(conn))
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	current, err := h.uc.GetConnector(r.Context(), id)
	if err != nil {
		writeConnectorError(w, err)
		return
	}
	if !canAccessConnectorOrg(r, current) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector org is not allowed for this principal")
		return
	}
	var body dto.SaveConnectorRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if strings.TrimSpace(body.Name) != "" {
		current.Name = body.Name
	}
	if body.Enabled != nil {
		current.Enabled = *body.Enabled
		if *body.Enabled {
			current.Status = "active"
		} else {
			current.Status = "disabled"
		}
	}
	if strings.TrimSpace(body.Status) != "" {
		switch strings.TrimSpace(strings.ToLower(body.Status)) {
		case "active":
			current.Enabled = true
			current.Status = "active"
		case "disabled":
			current.Enabled = false
			current.Status = "disabled"
		default:
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "lifecycle status changes must use lifecycle endpoints")
			return
		}
	}
	if len(body.Config) > 0 {
		current.ConfigJSON = body.Config
	}
	conn, err := h.uc.SaveConnector(r.Context(), current)
	if err != nil {
		writeConnectorError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorToResponse(conn))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	conn, err := h.uc.GetConnector(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "connector not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get connector failed")
		return
	}
	if !canAccessConnectorOrg(r, conn) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector org is not allowed for this principal")
		return
	}
	if err := h.uc.DeleteConnector(r.Context(), id); err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "connector not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "delete connector failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.uc.ArchiveConnector)
}

func (h *Handler) trash(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.uc.TrashConnector)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.uc.RestoreConnector)
}

func (h *Handler) lifecycle(w http.ResponseWriter, r *http.Request, action func(context.Context, uuid.UUID) (domain.Connector, error)) {
	if !requireScope(w, r, scopeCompanionConnectorsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	current, err := h.uc.GetConnector(r.Context(), id)
	if err != nil {
		writeConnectorError(w, err)
		return
	}
	if !canAccessConnectorOrg(r, current) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector org is not allowed for this principal")
		return
	}
	conn, err := action(r.Context(), id)
	if err != nil {
		writeConnectorError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorToResponse(conn))
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	current, err := h.uc.GetConnector(r.Context(), id)
	if err != nil {
		writeConnectorError(w, err)
		return
	}
	if !canAccessConnectorOrg(r, current) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector org is not allowed for this principal")
		return
	}
	if err := h.uc.TestConnector(r.Context(), id); err != nil {
		writeConnectorError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "connector_id": id.String()})
}

func (h *Handler) refreshOne(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	current, err := h.uc.GetConnector(r.Context(), id)
	if err != nil {
		writeConnectorError(w, err)
		return
	}
	if !canAccessConnectorOrg(r, current) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector org is not allowed for this principal")
		return
	}
	result := h.uc.RefreshConnector(r.Context(), id)
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorRefreshResponse{Results: []dto.ConnectorRefreshResult{{
		ConnectorID: result.ConnectorID,
		Refreshed:   result.Refreshed,
		Error:       result.Error,
	}}})
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	spec, ok := h.executionSpecFromRequest(w, r)
	if !ok {
		return
	}

	result, err := h.uc.Execute(r.Context(), spec)
	if err != nil {
		if IsUngated(err) {
			httpjson.WriteFlatError(w, http.StatusForbidden, "UNGATED", "execution requires nexus approval")
			return
		}
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "connector not found")
			return
		}
		if err == ErrDisabled {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "connector is disabled")
			return
		}
		if err == ErrOperationUnknown {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "unknown operation for connector")
			return
		}
		if IsInvalidPayload(err) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		if IsValidation(err) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		if IsRateLimited(err) {
			httpjson.WriteFlatError(w, http.StatusTooManyRequests, "RATE_LIMITED", err.Error())
			return
		}
		if IsForbidden(err) {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		if IsConflict(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "connector execution already in progress")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "execute connector failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ExecutionToResponse(result))
}

func (h *Handler) actionBinding(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	spec, ok := h.executionSpecFromRequest(w, r)
	if !ok {
		return
	}
	binding, hash, err := h.uc.BuildActionBinding(r.Context(), spec)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "connector not found")
			return
		}
		if err == ErrOperationUnknown {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "unknown operation for connector")
			return
		}
		if IsInvalidPayload(err) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		if IsValidation(err) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		if IsRateLimited(err) {
			httpjson.WriteFlatError(w, http.StatusTooManyRequests, "RATE_LIMITED", err.Error())
			return
		}
		if IsForbidden(err) {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		httpjson.WriteFlatInternalError(w, err, "build connector action binding failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ActionBindingResponse{ActionBinding: binding, BindingHash: hash})
}

func (h *Handler) executionSpecFromRequest(w http.ResponseWriter, r *http.Request) (domain.ExecutionSpec, bool) {
	id := identityctx.FromRequest(r)
	if !identityctx.HasNoAuthContext(r) && (id.CustomerOrgID == "" || id.EffectiveActorID() == "") {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector execution requires org and actor context")
		return domain.ExecutionSpec{}, false
	}
	var body dto.ExecuteRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return domain.ExecutionSpec{}, false
	}
	if body.ConnectorID == "" || body.Operation == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "connector_id and operation are required")
		return domain.ExecutionSpec{}, false
	}

	connID, err := uuid.Parse(body.ConnectorID)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid connector_id")
		return domain.ExecutionSpec{}, false
	}

	payload, ok := bindPayloadToPrincipalOrg(r, body.Payload)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "payload org is not allowed for this principal")
		return domain.ExecutionSpec{}, false
	}
	orgID, ok := effectiveConnectorOrgID(r, connectorOrgIDFromPayload(payload))
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector execution org is not allowed for this principal")
		return domain.ExecutionSpec{}, false
	}
	spec := domain.ExecutionSpec{
		ConnectorID:        connID,
		OrgID:              orgID,
		ActorID:            id.EffectiveActorID(),
		ActorType:          id.ActorType,
		CompanionPrincipal: id.CompanionPrincipal,
		OnBehalfOf:         id.OnBehalfOf,
		ServicePrincipal:   id.ServicePrincipal,
		ProductSurface:     id.ProductSurface,
		AuthScopes:         append([]string(nil), id.Scopes...),
		Operation:          body.Operation,
		Payload:            payload,
		IdempotencyKey:     body.IdempotencyKey,
	}
	if body.TaskID != "" {
		tid, err := uuid.Parse(body.TaskID)
		if err == nil {
			spec.TaskID = &tid
		}
	}
	if body.NexusRequestID != "" {
		rid, err := uuid.Parse(body.NexusRequestID)
		if err == nil {
			spec.NexusRequestID = &rid
		}
	}
	return spec, true
}

func connectorOrgIDFromPayload(raw json.RawMessage) string {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(rawToString(payload["org_id"]))
}

func (h *Handler) listExecutions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	execs, err := h.uc.ListExecutions(r.Context(), id, defaultListLimit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list executions failed")
		return
	}
	out := make([]dto.ExecutionResponse, 0, len(execs))
	for _, e := range execs {
		if !canAccessExecutionOrg(r, e) {
			continue
		}
		out = append(out, dto.ExecutionToResponse(e))
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ExecutionListResponse{Executions: out})
}

func (h *Handler) capabilities(w http.ResponseWriter, r *http.Request) {
	filter := capabilityFilterFromRequest(r)
	caps := h.uc.Capabilities(filter)
	out := make([]dto.CapabilityResponse, 0, len(caps))
	for _, c := range caps {
		decisions := make([]domain.CapabilityDecision, 0, len(c.Capabilities))
		for _, capability := range c.Capabilities {
			decisions = append(decisions, capability.RuntimeDecision())
		}
		out = append(out, dto.CapabilityResponse{
			ConnectorID:      c.ID,
			Kind:             c.Kind,
			Capabilities:     c.Capabilities,
			RuntimeDecisions: decisions,
		})
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.CapabilitiesListResponse{Connectors: out})
}

func (h *Handler) capabilityManifests(w http.ResponseWriter, r *http.Request) {
	manifests, err := h.uc.CapabilityManifests(capabilityFilterFromRequest(r))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list capability manifests failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.CapabilityManifestListResponse{Capabilities: manifests})
}

func capabilityFilterFromRequest(r *http.Request) domain.CapabilityFilter {
	id := identityctx.FromRequest(r)
	return domain.CapabilityFilter{
		TenantID:           id.CustomerOrgID,
		Roles:              parseHeaderValues(r.Header.Get("X-Auth-Roles")),
		Scopes:             append([]string(nil), id.Scopes...),
		Modules:            parseHeaderValues(r.Header.Get("X-Enabled-Modules")),
		MaxRiskClass:       strings.TrimSpace(r.URL.Query().Get("max_risk_class")),
		IncludeWrites:      strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_writes")), "true"),
		EnforcePermissions: !identityctx.HasNoAuthContext(r),
	}
}

func writeConnectorError(w http.ResponseWriter, err error) {
	switch {
	case IsNotFound(err):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "connector not found")
	case IsValidation(err):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case err == ErrDisabled:
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "connector is disabled")
	case IsConflict(err):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
	case IsForbidden(err):
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "connector operation failed")
	}
}
