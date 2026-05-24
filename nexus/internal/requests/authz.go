package requests

import (
	"net/http"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusRequestsRead   = "nexus:requests:read"
	scopeNexusRequestsWrite  = "nexus:requests:write"
	scopeNexusRequestsResult = "nexus:requests:result"
	scopeNexusEvidenceWrite  = "nexus:evidence:write"
	scopeNexusCrossOrg       = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

