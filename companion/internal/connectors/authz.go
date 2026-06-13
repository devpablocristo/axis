package connectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCompanionConnectorsExecute = "companion:connectors:execute"
	scopeCompanionConnectorsAdmin   = "companion:connectors:admin"
	scopeCompanionCrossOrg          = "companion:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) string { return identityctx.PrincipalOrgID(r) }

func principalActorID(r *http.Request) string { return identityctx.FromRequest(r).EffectiveActorID() }

func effectiveConnectorOrgID(r *http.Request, requested string) (string, bool) {
	return identityctx.EffectiveOrgID(r, requested, scopeCompanionCrossOrg)
}

func canAccessConnectorOrg(r *http.Request, connector domain.Connector) bool {
	connectorOrg := strings.TrimSpace(connector.OrgID)
	if connectorOrg == "" {
		return true
	}
	return identityctx.CanAccessOrg(r, connectorOrg, scopeCompanionCrossOrg)
}

func canAccessExecutionOrg(r *http.Request, execution domain.ExecutionResult) bool {
	executionOrg := strings.TrimSpace(execution.OrgID)
	if executionOrg == "" {
		return true
	}
	return identityctx.CanAccessOrg(r, executionOrg, scopeCompanionCrossOrg)
}

func bindPayloadToPrincipalOrg(r *http.Request, raw json.RawMessage) (json.RawMessage, bool) {
	requested := strings.TrimSpace(r.URL.Query().Get("org_id"))
	var payload map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			payload = nil
		}
	}
	if payload != nil {
		if value := strings.TrimSpace(rawToString(payload["org_id"])); value != "" {
			requested = value
		}
	}
	orgID, ok := effectiveConnectorOrgID(r, requested)
	if !ok || orgID == "" {
		if len(raw) == 0 {
			return json.RawMessage(`{}`), true
		}
		if !identityctx.HasNoAuthContext(r) {
			return nil, false
		}
		return raw, true
	}
	if payload == nil && len(raw) > 0 {
		return raw, true
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["org_id"] = orgID
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(out), true
}

// parseHeaderValues normaliza listas separadas por coma/espacio/`+`/`;`.
// Usado por handler.go para parsear headers tipo `X-Auth-Scopes` o similares
// donde el formato no es exclusivamente scopes (identityhttp.ParseScopes
// retorna []string también pero se reserva para scopes específicamente).
func parseHeaderValues(raw string) []string {
	raw = strings.NewReplacer(",", " ", ";", " ", "+", " ").Replace(raw)
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := strings.TrimSpace(field); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func rawToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}
