package rbac

import (
	"net/http"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusRBACAdmin = "nexus:rbac:admin"
	scopeNexusCrossOrg  = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) string { return identityhttp.PrincipalOrgID(r) }

func canAccessOrg(r *http.Request, orgID string) bool {
	return identityhttp.CanAccessOrg(r, orgID, scopeNexusCrossOrg)
}

func requestHasNoAuthContext(r *http.Request) bool { return identityhttp.HasNoAuthContext(r) }

func requestHasScope(r *http.Request, scopes ...string) bool {
	return identityhttp.HasAnyScope(r, scopes...)
}
