package audit

import (
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusRequestsRead = "nexus:requests:read"
	scopeNexusCrossOrg     = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func canAccessReplayOrg(r *http.Request, out ReplayOutput) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	if orgID == "" {
		return false
	}
	return strings.TrimSpace(out.OrgID) == orgID
}
