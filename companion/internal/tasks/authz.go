package tasks

import (
	"net/http"
	"strings"

	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCompanionTasksRead         = "companion:tasks:read"
	scopeCompanionTasksWrite        = "companion:tasks:write"
	scopeCompanionConnectorsExecute = "companion:connectors:execute"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) string { return identityhttp.PrincipalOrgID(r) }

func canAccessTaskOrg(r *http.Request, task domain.Task) bool {
	taskOrg := strings.TrimSpace(task.OrgID)
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, "companion:cross_org") {
		return true
	}
	orgID := principalOrgID(r)
	if orgID == "" || taskOrg == "" {
		return false
	}
	return taskOrg == orgID
}

func principalScopes(r *http.Request) []string { return identityhttp.FromRequest(r).Scopes }

func requestHasNoAuthContext(r *http.Request) bool { return identityhttp.HasNoAuthContext(r) }

func requestHasScope(r *http.Request, scopes ...string) bool {
	return identityhttp.HasAnyScope(r, scopes...)
}
