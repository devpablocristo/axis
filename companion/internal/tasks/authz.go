package tasks

import (
	"net/http"
	"strings"

	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCompanionTasksRead         = "companion:tasks:read"
	scopeCompanionTasksWrite        = "companion:tasks:write"
	scopeCompanionConnectorsExecute = "companion:connectors:execute"
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

func canAccessTaskOrg(r *http.Request, task domain.Task) bool {
	orgID := principalOrgID(r)
	if requestHasNoAuthContext(r) {
		return true
	}
	if orgID == "" || strings.TrimSpace(task.OrgID) == "" {
		return false
	}
	return strings.TrimSpace(task.OrgID) == orgID
}

func principalScopes(r *http.Request) []string {
	raw := strings.NewReplacer(",", " ", ";", " ", "+", " ").Replace(r.Header.Get("X-Auth-Scopes"))
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if scope := strings.TrimSpace(field); scope != "" {
			out = append(out, scope)
		}
	}
	return out
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
	raw = strings.NewReplacer(",", " ", ";", " ", "+", " ").Replace(raw)
	fields := strings.Fields(raw)
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if scope := strings.TrimSpace(field); scope != "" {
			out[scope] = struct{}{}
		}
	}
	return out
}
