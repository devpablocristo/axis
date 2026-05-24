package delegations

import (
	"net/http"
	"strings"

	domain "github.com/devpablocristo/nexus/internal/delegations/usecases/domain"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusDelegationsAdmin = "nexus:policies:admin"
	scopeNexusCrossOrg         = "nexus:cross_org"
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

func canAccessDelegationOrg(r *http.Request, d domain.Delegation) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	if d.OrgID == nil {
		return true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	return orgID != "" && strings.TrimSpace(*d.OrgID) == orgID
}

func canWriteDelegationOrg(r *http.Request, d domain.Delegation) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	return orgID != "" && d.OrgID != nil && strings.TrimSpace(*d.OrgID) == orgID
}

