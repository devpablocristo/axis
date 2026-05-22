package connectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCompanionConnectorsExecute = "companion:connectors:execute"
	scopeCompanionConnectorsAdmin   = "companion:connectors:admin"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Org-ID"))
}

func principalActorID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-User-ID"))
}

func canAccessConnectorOrg(r *http.Request, connector domain.Connector) bool {
	orgID := principalOrgID(r)
	if requestHasNoAuthContext(r) || strings.TrimSpace(connector.OrgID) == "" {
		return true
	}
	if orgID == "" {
		return false
	}
	return strings.TrimSpace(connector.OrgID) == orgID
}

func canAccessExecutionOrg(r *http.Request, execution domain.ExecutionResult) bool {
	orgID := principalOrgID(r)
	if requestHasNoAuthContext(r) {
		return true
	}
	if orgID == "" || strings.TrimSpace(execution.OrgID) == "" {
		return false
	}
	return strings.TrimSpace(execution.OrgID) == orgID
}

func bindPayloadToPrincipalOrg(r *http.Request, raw json.RawMessage) (json.RawMessage, bool) {
	orgID := principalOrgID(r)
	if orgID == "" {
		if len(raw) == 0 {
			return json.RawMessage(`{}`), true
		}
		if !requestHasNoAuthContext(r) {
			return nil, false
		}
		return raw, true
	}
	var payload map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return raw, true
		}
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	if requested, ok := payload["org_id"]; ok {
		if requestedOrg := strings.TrimSpace(rawToString(requested)); requestedOrg != "" && requestedOrg != orgID {
			return nil, false
		}
	}
	payload["org_id"] = orgID
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(out), true
}

func requestHasNoAuthContext(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-Auth-Method")) == "" &&
		strings.TrimSpace(r.Header.Get("X-Auth-Scopes")) == ""
}

func requestHasScope(r *http.Request, scopes ...string) bool {
	have := parseHeaderScopes(r.Header.Get("X-Auth-Scopes"))
	for _, scope := range scopes {
		if _, ok := have[scope]; ok {
			return true
		}
	}
	return false
}

func parseHeaderScopes(raw string) map[string]struct{} {
	values := parseHeaderValues(raw)
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

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
