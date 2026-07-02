package contracts

import (
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusContractsAdmin = "nexus:contracts:admin"
	scopeNexusPoliciesAdmin  = "nexus:policies:admin"
	scopeNexusCrossOrg       = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func contractOrgScope(r *http.Request) (*string, bool) {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		raw := strings.TrimSpace(r.URL.Query().Get("org_id"))
		if raw == "" {
			return nil, true
		}
		return &raw, true
	}
	orgID := strings.TrimSpace(identityhttp.PrincipalOrgID(r))
	if orgID == "" {
		return nil, false
	}
	return &orgID, true
}
