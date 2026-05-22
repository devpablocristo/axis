package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/connectors/handler/dto"
	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

const (
	defaultListLimit = 50
)

type connectorUsecase interface {
	ListConnectors(ctx context.Context) ([]domain.Connector, error)
	GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error)
	SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error)
	DeleteConnector(ctx context.Context, id uuid.UUID) error
	Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error)
	ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error)
	Capabilities(filter domain.CapabilityFilter) []ConnectorCapabilities
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
	mux.HandleFunc("GET /v1/connectors", h.list)
	mux.HandleFunc("POST /v1/connectors", h.save)
	mux.HandleFunc("POST /v1/connectors/refresh", h.refresh)
	mux.HandleFunc("GET /v1/connectors/{id}", h.get)
	mux.HandleFunc("DELETE /v1/connectors/{id}", h.delete)
	mux.HandleFunc("POST /v1/connectors/execute", h.execute)
	mux.HandleFunc("GET /v1/connectors/{id}/executions", h.listExecutions)
	mux.HandleFunc("GET /v1/connectors/capabilities", h.capabilities)
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
	conns, err := h.uc.ListConnectors(r.Context())
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
	httpjson.WriteJSON(w, http.StatusOK, dto.ConnectorListResponse{Connectors: out})
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
	if !requestHasNoAuthContext(r) && principalOrgID(r) == "" {
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
	configJSON := body.Config
	if len(configJSON) == 0 {
		configJSON = json.RawMessage(`{}`)
	}
	conn, err := h.uc.SaveConnector(r.Context(), domain.Connector{
		OrgID:      principalOrgID(r),
		Name:       body.Name,
		Kind:       body.Kind,
		Enabled:    body.Enabled,
		ConfigJSON: configJSON,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "save connector failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, dto.ConnectorToResponse(conn))
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

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	if !requestHasNoAuthContext(r) && (principalOrgID(r) == "" || principalActorID(r) == "") {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector execution requires org and actor context")
		return
	}
	var body dto.ExecuteRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.ConnectorID == "" || body.Operation == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "connector_id and operation are required")
		return
	}

	connID, err := uuid.Parse(body.ConnectorID)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid connector_id")
		return
	}

	payload, ok := bindPayloadToPrincipalOrg(r, body.Payload)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "payload org is not allowed for this principal")
		return
	}
	spec := domain.ExecutionSpec{
		ConnectorID:    connID,
		OrgID:          principalOrgID(r),
		ActorID:        principalActorID(r),
		ProductSurface: strings.TrimSpace(r.Header.Get("X-Product-Surface")),
		AuthScopes:     parseHeaderValues(r.Header.Get("X-Auth-Scopes")),
		Operation:      body.Operation,
		Payload:        payload,
		IdempotencyKey: body.IdempotencyKey,
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
		if IsForbidden(err) {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "connector org is not allowed for this principal")
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
	filter := domain.CapabilityFilter{
		TenantID:           principalOrgID(r),
		Roles:              parseHeaderValues(r.Header.Get("X-Auth-Roles")),
		Scopes:             parseHeaderValues(r.Header.Get("X-Auth-Scopes")),
		Modules:            parseHeaderValues(r.Header.Get("X-Enabled-Modules")),
		MaxRiskClass:       strings.TrimSpace(r.URL.Query().Get("max_risk_class")),
		IncludeWrites:      strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_writes")), "true"),
		EnforcePermissions: !requestHasNoAuthContext(r),
	}
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
