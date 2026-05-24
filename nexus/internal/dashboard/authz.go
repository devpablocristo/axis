package dashboard

import (
	"net/http"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusDashboardRead = "nexus:dashboard:read"
	scopeNexusCrossOrg      = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) *string {
	orgID := identityhttp.PrincipalOrgID(r)
	if orgID == "" {
		return nil
	}
	return &orgID
}

