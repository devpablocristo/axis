package tasks

import (
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCompanionTasksRead         = "companion:tasks:read"
	scopeCompanionTasksWrite        = "companion:tasks:write"
	scopeCompanionConnectorsExecute = "companion:connectors:execute"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) string { return identityctx.PrincipalOrgID(r) }

func principalUserID(r *http.Request) string { return identityctx.FromRequest(r).EffectiveActorID() }

func canAccessTaskOrg(r *http.Request, task domain.Task) bool {
	taskOrg := strings.TrimSpace(task.OrgID)
	if identityctx.HasNoAuthContext(r) || identityctx.HasScope(r, "companion:cross_org") {
		return true
	}
	orgID := principalOrgID(r)
	if orgID == "" || taskOrg == "" {
		return false
	}
	return taskOrg == orgID
}

func workIdentity(r *http.Request) (identityctx.IdentityContext, bool) {
	return identityctx.WorkIdentity(r)
}

func resolveCreatedBy(r *http.Request, requested string) (string, bool) {
	requested = strings.TrimSpace(requested)
	id := identityctx.FromRequest(r)
	if requested == "" {
		return id.EffectiveActorID(), true
	}
	if identityctx.HasNoAuthContext(r) || id.CanActAs(requested, scopeCompanionCrossOrg) {
		return requested, true
	}
	return "", false
}
